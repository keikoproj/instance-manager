/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package eks

import (
	"encoding/base64"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"sort"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	"github.com/onsi/gomega"
	"github.com/pkg/errors"
)

func TestAutoscalerTags(t *testing.T) {
	var (
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
		eksMock = NewEksMocker()
		ec2Mock = NewEc2Mocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock)

	ig.Annotations = map[string]string{
		ClusterAutoscalerEnabledAnnotation: "true",
	}

	ig.Spec.EKSSpec.EKSConfiguration.Labels = make(map[string]string)
	ig.Spec.EKSSpec.EKSConfiguration.Labels["foo"] = "bar"

	ig.Spec.EKSSpec.EKSConfiguration.Taints = []corev1.Taint{}
	ig.Spec.EKSSpec.EKSConfiguration.Taints = append(ig.Spec.EKSSpec.EKSConfiguration.Taints, corev1.Taint{
		Key:    "red",
		Value:  "green",
		Effect: "NoSchedule",
	})
	ctx := MockContext(ig, k, w)

	tags := make(map[string]string)
	expectedTags := make(map[string]string)

	expectedTags["k8s.io/cluster-autoscaler/enabled"] = "true"
	expectedTags["k8s.io/cluster-autoscaler/"+ig.Spec.EKSSpec.EKSConfiguration.EksClusterName] = "owned"
	expectedTags["k8s.io/cluster-autoscaler/node-template/label/foo"] = "bar"
	expectedTags["k8s.io/cluster-autoscaler/node-template/taint/red"] = "green:NoSchedule"

	tagSlice := ctx.GetAddedTags("foo")
	for _, tag := range tagSlice {
		tags[*tag.Key] = *tag.Value
	}
	for expectedKey, expectedValue := range expectedTags {
		if tags[expectedKey] != expectedValue {
			t.Fatalf("Expected %v=%v, Got %v", expectedKey, expectedValue, tags[expectedKey])
		}
	}

}

func TestResolveSecurityGroups(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		config  = ig.GetEKSConfiguration()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
		eksMock = NewEksMocker()
		ec2Mock = NewEc2Mocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock)
	ctx := MockContext(ig, k, w)

	tests := []struct {
		requested []string
		groups    []*ec2.SecurityGroup
		result    []string
		withErr   bool
	}{
		{requested: []string{"sg-111", "sg-222"}, groups: []*ec2.SecurityGroup{MockSecurityGroup("sg-111", false, ""), MockSecurityGroup("sg-222", false, "")}, result: []string{"sg-111", "sg-222"}, withErr: false},
		{requested: []string{"my-sg-1", "sg-222"}, groups: []*ec2.SecurityGroup{MockSecurityGroup("sg-111", true, "my-sg-1"), MockSecurityGroup("sg-222", false, "")}, result: []string{"sg-111", "sg-222"}, withErr: false},
		{requested: []string{"my-sg-1", "my-sg-2"}, groups: []*ec2.SecurityGroup{MockSecurityGroup("sg-111", true, "my-sg-1"), MockSecurityGroup("sg-222", true, "my-sg-2")}, result: []string{"sg-111", "sg-222"}, withErr: false},
		{requested: []string{"my-sg-1"}, groups: []*ec2.SecurityGroup{MockSecurityGroup("sg-111", true, "my-sg-1")}, result: []string{}, withErr: true},
		{requested: []string{"my-sg-1", "my-sg-2"}, groups: []*ec2.SecurityGroup{MockSecurityGroup("sg-111", true, "my-sg-2")}, result: []string{"sg-111"}, withErr: false},
	}

	for i, tc := range tests {
		t.Logf("Test #%v - %+v", i, tc)
		config.NodeSecurityGroups = tc.requested
		ec2Mock.DescribeSecurityGroupsErr = nil
		ec2Mock.SecurityGroups = tc.groups
		if tc.withErr {
			ec2Mock.DescribeSecurityGroupsErr = errors.New("an error occured")
		}
		groups := ctx.ResolveSecurityGroups()
		g.Expect(groups).To(gomega.Equal(tc.result))
	}
}

func TestResolveSubnets(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		config  = ig.GetEKSConfiguration()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
		eksMock = NewEksMocker()
		ec2Mock = NewEc2Mocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock)
	ctx := MockContext(ig, k, w)

	tests := []struct {
		requested []string
		subnets   []*ec2.Subnet
		result    []string
		withErr   bool
	}{
		{requested: []string{"subnet-111", "subnet-222"}, subnets: []*ec2.Subnet{MockSubnet("subnet-111", false, ""), MockSubnet("subnet-222", false, "")}, result: []string{"subnet-111", "subnet-222"}, withErr: false},
		{requested: []string{"my-subnet-1", "subnet-222"}, subnets: []*ec2.Subnet{MockSubnet("subnet-111", true, "my-subnet-1"), MockSubnet("subnet-222", false, "")}, result: []string{"subnet-111", "subnet-222"}, withErr: false},
		{requested: []string{"my-subnet-1", "my-subnet-2"}, subnets: []*ec2.Subnet{MockSubnet("subnet-111", true, "my-subnet-1"), MockSubnet("subnet-222", true, "my-subnet-2")}, result: []string{"subnet-111", "subnet-222"}, withErr: false},
		{requested: []string{"my-subnet-1"}, subnets: []*ec2.Subnet{MockSubnet("subnet-111", true, "my-subnet-1")}, result: []string{}, withErr: true},
		{requested: []string{"my-subnet-1", "my-subnet-2"}, subnets: []*ec2.Subnet{MockSubnet("subnet-111", true, "my-subnet-2")}, result: []string{"subnet-111"}, withErr: false},
	}

	for i, tc := range tests {
		t.Logf("Test #%v - %+v", i, tc)
		config.Subnets = tc.requested
		ec2Mock.Subnets = tc.subnets
		ec2Mock.DescribeSubnetsErr = nil
		if tc.withErr {
			ec2Mock.DescribeSubnetsErr = errors.New("an error occured")
		}
		groups := ctx.ResolveSubnets()
		g.Expect(groups).To(gomega.Equal(tc.result))
	}
}

func TestGetDisabledMetrics(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
		eksMock = NewEksMocker()
		ec2Mock = NewEc2Mocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock)
	ctx := MockContext(ig, k, w)

	// No disable required
	ig.GetEKSConfiguration().SetMetricsCollection([]string{"all"})
	ctx.SetDiscoveredState(&DiscoveredState{
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		ScalingGroup: &autoscaling.Group{
			EnabledMetrics: MockEnabledMetrics("GroupMinSize"),
		},
	})

	metrics, ok := ctx.GetDisabledMetrics()
	g.Expect(ok).To(gomega.BeFalse())
	g.Expect(metrics).To(gomega.BeEmpty())

	// Disable all metrics
	ig.GetEKSConfiguration().SetMetricsCollection([]string{})
	ctx.SetDiscoveredState(&DiscoveredState{
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		ScalingGroup: &autoscaling.Group{
			EnabledMetrics: MockEnabledMetrics(awsprovider.DefaultAutoscalingMetrics...),
		},
	})

	metrics, ok = ctx.GetDisabledMetrics()
	g.Expect(ok).To(gomega.BeTrue())
	g.Expect(metrics).To(gomega.ConsistOf(awsprovider.DefaultAutoscalingMetrics))

	// Disable specific metric
	ig.GetEKSConfiguration().SetMetricsCollection([]string{"GroupMinSize"})
	ctx.SetDiscoveredState(&DiscoveredState{
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		ScalingGroup: &autoscaling.Group{
			EnabledMetrics: MockEnabledMetrics("GroupMinSize", "GroupMaxSize"),
		},
	})

	metrics, ok = ctx.GetDisabledMetrics()
	g.Expect(ok).To(gomega.BeTrue())
	g.Expect(metrics).To(gomega.ContainElement("GroupMaxSize"))
}

func TestGetEnabledMetrics(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
		eksMock = NewEksMocker()
		ec2Mock = NewEc2Mocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock)
	ctx := MockContext(ig, k, w)

	// Enable all metrics
	ig.GetEKSConfiguration().SetMetricsCollection([]string{"all"})
	ctx.SetDiscoveredState(&DiscoveredState{
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		ScalingGroup: &autoscaling.Group{
			EnabledMetrics: MockEnabledMetrics(),
		},
	})

	metrics, ok := ctx.GetEnabledMetrics()
	g.Expect(ok).To(gomega.BeTrue())
	g.Expect(metrics).To(gomega.ConsistOf(awsprovider.DefaultAutoscalingMetrics))

	// Enable specific metric
	ig.GetEKSConfiguration().SetMetricsCollection([]string{"GroupMinSize", "GroupMaxSize"})
	ctx.SetDiscoveredState(&DiscoveredState{
		Publisher: kubeprovider.EventPublisher{
			Client: k.Kubernetes,
		},
		ScalingGroup: &autoscaling.Group{
			EnabledMetrics: MockEnabledMetrics("GroupMinSize"),
		},
	})

	metrics, ok = ctx.GetEnabledMetrics()
	g.Expect(ok).To(gomega.BeTrue())
	g.Expect(metrics).To(gomega.ContainElement("GroupMaxSize"))
}

func TestGetLabelList(t *testing.T) {
	var (
		g                          = gomega.NewGomegaWithT(t)
		k                          = MockKubernetesClientSet()
		ig                         = MockInstanceGroup()
		configuration              = ig.GetEKSConfiguration()
		asgMock                    = NewAutoScalingMocker()
		iamMock                    = NewIamMocker()
		eksMock                    = NewEksMocker()
		ec2Mock                    = NewEc2Mocker()
		expectedLabels115          = []string{"node-role.kubernetes.io/instance-group-1=\"\"", "node.kubernetes.io/role=instance-group-1"}
		expectedLabels116          = []string{"node.kubernetes.io/role=instance-group-1"}
		expectedLabelsWithCustom   = []string{"custom.kubernetes.io=customlabel", "node.kubernetes.io/role=instance-group-1"}
		expectedLabelsWithOverride = []string{"custom.kubernetes.io=customlabel", "override.kubernetes.io=instance-group-1", "override2.kubernetes.io=instance-group-1"}
		overrideAnnotation         = map[string]string{OverrideDefaultLabelsAnnotationKey: "override.kubernetes.io=instance-group-1,override2.kubernetes.io=instance-group-1"}
		expectedSpotLable          = []string{"instancemgr.keikoproj.io/lifecycle=spot", "node-role.kubernetes.io/instance-group-1=\"\"", "node.kubernetes.io/role=instance-group-1"}
		defaultLifecycleLable      = "instancemgr.keikoproj.io/lifecycle=normal"
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock)
	ctx := MockContext(ig, k, w)

	tests := []struct {
		clusterVersion           string
		instanceGroupLabels      map[string]string
		instanceGroupAnnotations map[string]string
		expectedLabels           []string
		spotPrice                string
	}{
		{clusterVersion: "", spotPrice: "0.7773", expectedLabels: expectedSpotLable},
		// Default labels with missing cluster version
		{clusterVersion: "", expectedLabels: expectedLabels115},
		// Kubernetes 1.15 default labels
		{clusterVersion: "1.15", expectedLabels: expectedLabels115},
		// Kubernetes 1.16 default labels
		{clusterVersion: "1.16", expectedLabels: expectedLabels116},
		// Kubernetes 1.16 default labels with custom labels
		{clusterVersion: "1.16", instanceGroupLabels: map[string]string{"custom.kubernetes.io": "customlabel"}, expectedLabels: expectedLabelsWithCustom},
		// custom labels with override labels
		{clusterVersion: "1.16", instanceGroupAnnotations: overrideAnnotation, instanceGroupLabels: map[string]string{"custom.kubernetes.io": "customlabel"}, expectedLabels: expectedLabelsWithOverride},
	}

	for i, tc := range tests {
		t.Logf("Test #%v - %+v", i, tc)
		configuration.SetLabels(tc.instanceGroupLabels)
		ig.SetAnnotations(tc.instanceGroupAnnotations)
		configuration.SetSpotPrice(tc.spotPrice)
		ctx.SetDiscoveredState(&DiscoveredState{
			Publisher: kubeprovider.EventPublisher{
				Client: k.Kubernetes,
			},
			Cluster: &eks.Cluster{
				Version: aws.String(tc.clusterVersion),
			},
		})
		if tc.spotPrice == "" {
			tc.expectedLabels = append(tc.expectedLabels, defaultLifecycleLable)
		}
		sort.Strings(tc.expectedLabels)
		labels := ctx.GetLabelList()
		g.Expect(labels).To(gomega.Equal(tc.expectedLabels))
	}
}

func TestGetMountOpts(t *testing.T) {
	var (
		g             = gomega.NewGomegaWithT(t)
		k             = MockKubernetesClientSet()
		ig            = MockInstanceGroup()
		configuration = ig.GetEKSConfiguration()
		asgMock       = NewAutoScalingMocker()
		iamMock       = NewIamMocker()
		eksMock       = NewEksMocker()
		ec2Mock       = NewEc2Mocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock)
	ctx := MockContext(ig, k, w)

	volumeNoOpts := v1alpha1.NodeVolume{
		Name:                "/dev/xvda1",
		Type:                "gp2",
		Size:                100,
		DeleteOnTermination: aws.Bool(true),
		Encrypted:           aws.Bool(true),
	}

	volumeWithOpts := v1alpha1.NodeVolume{
		Name:                "/dev/xvda2",
		Type:                "gp2",
		Size:                100,
		DeleteOnTermination: aws.Bool(true),
		Encrypted:           aws.Bool(true),
		MountOptions: &v1alpha1.NodeVolumeMountOptions{
			FileSystem:  "xfs",
			Mount:       "/data",
			Persistance: aws.Bool(false),
		},
	}

	volumeWithOpts2 := v1alpha1.NodeVolume{
		Name:                "/dev/xvda3",
		Type:                "gp2",
		Size:                200,
		DeleteOnTermination: aws.Bool(true),
		Encrypted:           aws.Bool(true),
		MountOptions: &v1alpha1.NodeVolumeMountOptions{
			FileSystem: "xfs",
			Mount:      "/data2",
		},
	}

	volumeInvalidOpts := v1alpha1.NodeVolume{
		Name:                "/dev/xvda2",
		Type:                "gp2",
		Size:                100,
		DeleteOnTermination: aws.Bool(true),
		Encrypted:           aws.Bool(true),
		MountOptions: &v1alpha1.NodeVolumeMountOptions{
			FileSystem:  "ext3",
			Mount:       "/data",
			Persistance: aws.Bool(true),
		},
	}

	volumeInvalidOpts2 := v1alpha1.NodeVolume{
		Name:                "/dev/xvda2",
		Type:                "gp2",
		Size:                100,
		DeleteOnTermination: aws.Bool(true),
		Encrypted:           aws.Bool(true),
		MountOptions: &v1alpha1.NodeVolumeMountOptions{
			FileSystem:  "ext4",
			Mount:       "data",
			Persistance: aws.Bool(false),
		},
	}

	tests := []struct {
		volumes        []v1alpha1.NodeVolume
		expectedMounts []MountOpts
	}{
		{volumes: []v1alpha1.NodeVolume{volumeNoOpts}, expectedMounts: []MountOpts{}},
		{volumes: []v1alpha1.NodeVolume{volumeNoOpts, volumeWithOpts}, expectedMounts: []MountOpts{
			{
				FileSystem:  "xfs",
				Device:      "/dev/xvda2",
				Mount:       "/data",
				Persistance: false,
			},
		}},
		{volumes: []v1alpha1.NodeVolume{volumeWithOpts2, volumeWithOpts}, expectedMounts: []MountOpts{
			{
				FileSystem:  "xfs",
				Device:      "/dev/xvda2",
				Mount:       "/data",
				Persistance: false,
			},
			{
				FileSystem:  "xfs",
				Device:      "/dev/xvda3",
				Mount:       "/data2",
				Persistance: true,
			},
		}},
		{volumes: []v1alpha1.NodeVolume{volumeNoOpts}, expectedMounts: []MountOpts{}},
		{volumes: []v1alpha1.NodeVolume{volumeInvalidOpts, volumeInvalidOpts2}, expectedMounts: []MountOpts{}},
	}

	for i, tc := range tests {
		t.Logf("Test #%v - %+v", i, tc)
		configuration.Volumes = tc.volumes
		mounts := ctx.GetMountOpts()
		g.Expect(mounts).To(gomega.ConsistOf(tc.expectedMounts))
	}
}

func TestGetUserDataStages(t *testing.T) {
	var (
		g             = gomega.NewGomegaWithT(t)
		k             = MockKubernetesClientSet()
		ig            = MockInstanceGroup()
		configuration = ig.GetEKSConfiguration()
		asgMock       = NewAutoScalingMocker()
		iamMock       = NewIamMocker()
		eksMock       = NewEksMocker()
		ec2Mock       = NewEc2Mocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock)
	ctx := MockContext(ig, k, w)

	tests := []struct {
		preBootstrapScript  []string
		postBootstrapScript []string
		bootstrapScript     string
		expectedPayload     UserDataPayload
	}{
		{},
		{preBootstrapScript: []string{""}, postBootstrapScript: []string{""}, expectedPayload: UserDataPayload{PreBootstrap: []string{""}, PostBootstrap: []string{""}}},
		{preBootstrapScript: []string{"dGVzdA=="}, postBootstrapScript: []string{"dGVzdDE="}, expectedPayload: UserDataPayload{PreBootstrap: []string{"test"}, PostBootstrap: []string{"test1"}}},
		{preBootstrapScript: []string{"prebootstrap1"}, postBootstrapScript: []string{"postbootstrap"}, expectedPayload: UserDataPayload{PreBootstrap: []string{"prebootstrap1"}, PostBootstrap: []string{"postbootstrap"}}},
		{preBootstrapScript: []string{"prebootstrap1", "prebootstrap2"}, postBootstrapScript: []string{"postbootstrap1", "postbootstrap2"}, expectedPayload: UserDataPayload{PreBootstrap: []string{"prebootstrap1", "prebootstrap2"}, PostBootstrap: []string{"postbootstrap1", "postbootstrap2"}}},
	}

	for i, tc := range tests {
		t.Logf("Test #%v - %+v", i, tc)
		configuration.UserData = []v1alpha1.UserDataStage{}
		for _, data := range tc.preBootstrapScript {
			configuration.UserData = append(configuration.UserData, v1alpha1.UserDataStage{
				Name:  fmt.Sprintf("stage-%v", i),
				Stage: v1alpha1.PreBootstrapStage,
				Data:  data,
			})
		}

		for _, data := range tc.postBootstrapScript {
			configuration.UserData = append(configuration.UserData, v1alpha1.UserDataStage{
				Name:  fmt.Sprintf("stage-%v", i),
				Stage: v1alpha1.PostBootstrapStage,
				Data:  data,
			})
		}

		configuration.UserData = append(configuration.UserData, v1alpha1.UserDataStage{
			Name:  fmt.Sprintf("stage-%v", i),
			Stage: "invalid-stage",
			Data:  "test",
		})
		payload := ctx.GetUserDataStages()
		g.Expect(payload).To(gomega.Equal(tc.expectedPayload))
	}
}

func TestBootstrapDataForOSFamily(t *testing.T) {
	var (
		k         = MockKubernetesClientSet()
		linuxIg   = MockInstanceGroup()
		windowsIg = MockWindowsInstanceGroup()
		asgMock   = NewAutoScalingMocker()
		iamMock   = NewIamMocker()
		eksMock   = NewEksMocker()
		ec2Mock   = NewEc2Mocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock)

	tests := []struct {
		ig                       *v1alpha1.InstanceGroup
		expectedScriptSubstrings string
	}{
		{
			ig:                       linuxIg,
			expectedScriptSubstrings: "/etc/eks/bootstrap.sh",
		},
		{
			ig:                       windowsIg,
			expectedScriptSubstrings: "<powershell>",
		},
	}

	for i, tc := range tests {
		t.Logf("Test #%v - %+v", i, tc)
		ctx := MockContext(tc.ig, k, w)
		basicUserData := ctx.GetBasicUserData("", "", "", UserDataPayload{}, []MountOpts{})
		basicUserDataDecoded, _ := base64.StdEncoding.DecodeString(basicUserData)
		basicUserDataString := string(basicUserDataDecoded)
		if !strings.Contains(basicUserDataString, tc.expectedScriptSubstrings) {
			t.Fatalf("Cound not find expected string %v script in %v", tc.expectedScriptSubstrings, basicUserDataString)
		}
	}

}

func TestUpdateLifecycleHooks(t *testing.T) {
	var (
		g             = gomega.NewGomegaWithT(t)
		k             = MockKubernetesClientSet()
		ig            = MockInstanceGroup()
		configuration = ig.GetEKSConfiguration()
		asgMock       = NewAutoScalingMocker()
		iamMock       = NewIamMocker()
		eksMock       = NewEksMocker()
		ec2Mock       = NewEc2Mocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock)
	ctx := MockContext(ig, k, w)

	testScalingHooks := []*autoscaling.LifecycleHook{
		{
			LifecycleHookName: aws.String("my-hook-1"),
		},
		{
			LifecycleHookName: aws.String("my-hook-2"),
		},
	}

	hook1 := v1alpha1.LifecycleHookSpec{
		Name: "my-hook-1",
	}

	hook2 := v1alpha1.LifecycleHookSpec{
		Name: "my-hook-2",
	}

	hook3 := v1alpha1.LifecycleHookSpec{
		Name: "my-hook-3",
	}

	tests := []struct {
		asgHooks        []*autoscaling.LifecycleHook
		desiredHooks    []v1alpha1.LifecycleHookSpec
		expectedRemoved []string
		expectedAdded   []v1alpha1.LifecycleHookSpec
		shouldRemove    bool
		shouldAdd       bool
	}{
		{expectedRemoved: []string{}, expectedAdded: []v1alpha1.LifecycleHookSpec{}},
		{asgHooks: testScalingHooks, desiredHooks: []v1alpha1.LifecycleHookSpec{hook1, hook2, hook3}, expectedRemoved: []string{}, expectedAdded: []v1alpha1.LifecycleHookSpec{hook3}, shouldAdd: true},
		{asgHooks: testScalingHooks, desiredHooks: []v1alpha1.LifecycleHookSpec{hook1, hook3}, expectedRemoved: []string{"my-hook-2"}, expectedAdded: []v1alpha1.LifecycleHookSpec{hook3}, shouldAdd: true, shouldRemove: true},
		{asgHooks: testScalingHooks, desiredHooks: []v1alpha1.LifecycleHookSpec{hook1, hook2}, expectedRemoved: []string{}, expectedAdded: []v1alpha1.LifecycleHookSpec{}},
		{asgHooks: testScalingHooks, desiredHooks: []v1alpha1.LifecycleHookSpec{hook1}, expectedRemoved: []string{"my-hook-2"}, expectedAdded: []v1alpha1.LifecycleHookSpec{}, shouldRemove: true},
		{asgHooks: testScalingHooks, desiredHooks: []v1alpha1.LifecycleHookSpec{}, expectedRemoved: []string{"my-hook-1", "my-hook-2"}, expectedAdded: []v1alpha1.LifecycleHookSpec{}, shouldRemove: true},
	}

	for i, tc := range tests {
		t.Logf("Test #%v - %+v", i, tc)
		asgMock.DeleteLifecycleHookCallCount = 0
		asgMock.PutLifecycleHookCallCount = 0

		ctx.SetDiscoveredState(&DiscoveredState{
			Publisher: kubeprovider.EventPublisher{
				Client: k.Kubernetes,
			},
			LifecycleHooks: tc.asgHooks,
		})
		configuration.SetLifecycleHooks(tc.desiredHooks)
		removed, ok := ctx.GetRemovedHooks()
		g.Expect(ok).To(gomega.Equal(tc.shouldRemove))
		g.Expect(removed).To(gomega.Equal(tc.expectedRemoved))

		added, ok := ctx.GetAddedHooks()
		g.Expect(ok).To(gomega.Equal(tc.shouldAdd))
		g.Expect(added).To(gomega.Equal(tc.expectedAdded))

		ctx.UpdateLifecycleHooks("my-asg")
		g.Expect(len(tc.expectedRemoved)).To(gomega.Equal(asgMock.DeleteLifecycleHookCallCount))
		g.Expect(len(tc.expectedAdded)).To(gomega.Equal(asgMock.PutLifecycleHookCallCount))
	}
}
