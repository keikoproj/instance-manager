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
	"sort"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/keikoproj/instance-manager/api/instancemgr/v1alpha1"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	"github.com/onsi/gomega"
	"github.com/pkg/errors"
)

func mockUserDataStages() []v1alpha1.UserDataStage {
	preBootstrapData := base64.StdEncoding.EncodeToString([]byte("echo Pre-bootstrap actions"))
	postBootstrapData := base64.StdEncoding.EncodeToString([]byte("echo Post-bootstrap actions"))
	nodeConfigYamlData := base64.StdEncoding.EncodeToString([]byte("image: my-custom-image"))

	return []v1alpha1.UserDataStage{
		{Stage: v1alpha1.PreBootstrapStage, Data: preBootstrapData},
		{Stage: v1alpha1.PostBootstrapStage, Data: postBootstrapData},
		{Stage: v1alpha1.NodeConfigYamlStage, Data: nodeConfigYamlData},
	}
}

func TestAutoscalerTags(t *testing.T) {
	var (
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
		eksMock = NewEksMocker()
		ec2Mock = NewEc2Mocker()
		ssmMock = NewSsmMocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)

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

func TestGetBasicUserDataAmazonLinux2(t *testing.T) {
	var (
		k             = MockKubernetesClientSet()
		ig            = MockInstanceGroup()
		asgMock       = NewAutoScalingMocker()
		iamMock       = NewIamMocker()
		eksMock       = NewEksMocker()
		ec2Mock       = NewEc2Mocker()
		ssmMock       = NewSsmMocker()
		configuration = ig.GetEKSConfiguration()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)
	ctx := MockContext(ig, k, w)

	configuration.BootstrapOptions = &v1alpha1.BootstrapOptions{
		MaxPods:          4,
		ContainerRuntime: "containerd",
	}
	configuration.Labels = map[string]string{
		"foo": "bar",
	}
	configuration.Taints = []corev1.Taint{
		{
			Key:    "foo",
			Value:  "bar",
			Effect: "NoSchedule",
		},
	}
	persistance := true
	configuration.Volumes = []v1alpha1.NodeVolume{
		{
			Name: "/dev/xvda",
			Type: "gp2",
			MountOptions: &v1alpha1.NodeVolumeMountOptions{
				FileSystem:  "xfs",
				Mount:       "/mnt/foo",
				Persistance: &persistance,
			},
		},
	}
	configuration.BootstrapArguments = "--eviction-hard=memory.available<300Mi,nodefs.available<5% --system-reserved=memory=2.5Gi --v=2"
	configuration.UserData = []v1alpha1.UserDataStage{
		{
			Stage: "PreBootstrap",
			Data:  "foo",
		},
		{
			Stage: "PostBootstrap",
			Data:  "bar",
		},
	}

	// validate that wrong value still defaults to amazonlinux2
	ig.Annotations[OsFamilyAnnotation] = "wrong"

	var (
		args            = ctx.GetBootstrapArgs()
		kubeletArgs     = ctx.GetKubeletExtraArgs()
		userDataPayload = ctx.GetUserDataStages()
		mounts          = ctx.GetMountOpts()
	)

	expectedDataLinux := `#!/bin/bash
foo
mkfs.xfs /dev/xvda
mkdir /mnt/foo
mount /dev/xvda /mnt/foo
mount
echo "/dev/xvda    /mnt/foo    xfs    defaults    0    2" >> /etc/fstab
if [[ $(type -P $(which aws)) ]] && [[ $(type -P $(which jq)) ]] ; then
	TOKEN=$(curl -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600")
	INSTANCE_ID=$(curl url -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/instance-id)
	REGION=$(curl url -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/placement/region)
	LIFECYCLE=$(curl url -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/autoscaling/target-lifecycle-state)
	if [[ $LIFECYCLE == *"Warmed"* ]]; then
		rm /var/lib/cloud/instances/$INSTANCE_ID/sem/config_scripts_user
		exit 0
	fi
fi
set -o xtrace
/etc/eks/bootstrap.sh foo --use-max-pods false --container-runtime containerd --b64-cluster-ca dGVzdA== --apiserver-endpoint foo.amazonaws.com --dns-cluster-ip 172.20.0.10 --kubelet-extra-args '--node-labels=foo=bar,instancemgr.keikoproj.io/image=ami-123456789012,node.kubernetes.io/role=instance-group-1 --register-with-taints=foo=bar:NoSchedule --eviction-hard=memory.available<300Mi,nodefs.available<5% --system-reserved=memory=2.5Gi --v=2 --max-pods=4'
set +o xtrace
bar`
	userData := ctx.GetBasicUserData("foo", args, kubeletArgs, userDataPayload, mounts)
	basicUserDataDecoded, _ := base64.StdEncoding.DecodeString(userData)
	basicUserDataString := string(basicUserDataDecoded)
	if basicUserDataString != expectedDataLinux {
		t.Fatalf("\nExpected: START>%v<END\n Got: START>%v<END", expectedDataLinux, basicUserDataString)
	}
}

func TestGetBasicUserDataAmazonLinux2023(t *testing.T) {
	var (
		k             = MockKubernetesClientSet()
		ig            = MockInstanceGroup()
		asgMock       = NewAutoScalingMocker()
		iamMock       = NewIamMocker()
		eksMock       = NewEksMocker()
		ec2Mock       = NewEc2Mocker()
		ssmMock       = NewSsmMocker()
		configuration = ig.GetEKSConfiguration()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)
	ctx := MockContext(ig, k, w)

	configuration.BootstrapOptions = &v1alpha1.BootstrapOptions{
		MaxPods:          4,
		ContainerRuntime: "containerd",
	}
	configuration.Labels = map[string]string{
		"foo": "bar",
	}
	configuration.Taints = []corev1.Taint{
		{
			Key:    "foo",
			Value:  "bar",
			Effect: "NoSchedule",
		},
	}
	persistance := true
	configuration.Volumes = []v1alpha1.NodeVolume{
		{
			Name: "/dev/xvda",
			Type: "gp2",
			MountOptions: &v1alpha1.NodeVolumeMountOptions{
				FileSystem:  "xfs",
				Mount:       "/mnt/foo",
				Persistance: &persistance,
			},
		},
	}
	configuration.BootstrapArguments = "--eviction-hard=memory.available<300Mi,nodefs.available<5% --system-reserved=memory=2.5Gi --v=2"
	configuration.UserData = []v1alpha1.UserDataStage{
		{
			Stage: "PreBootstrap",
			Data:  "foo",
		},
		{
			Stage: "PostBootstrap",
			Data:  "bar",
		},
	}

	ig.Annotations[OsFamilyAnnotation] = OsFamilyAmazonLinux2023

	var (
		args            = ctx.GetBootstrapArgs()
		kubeletArgs     = ctx.GetKubeletExtraArgs()
		userDataPayload = ctx.GetUserDataStages()
		mounts          = ctx.GetMountOpts()
	)

	expectedDataLinux := `MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="BOUNDARY"

--BOUNDARY
Content-Type: text/x-shellscript; charset="us-ascii"

#!/bin/bash
echo "IG manager using AL2023 amis"
foo
mkfs.xfs /dev/xvda
mkdir /mnt/foo
mount /dev/xvda /mnt/foo
mount
echo "/dev/xvda    /mnt/foo    xfs    defaults    0    2" >> /etc/fstab
if [[ $(type -P $(which aws)) ]] && [[ $(type -P $(which jq)) ]] ; then
	TOKEN=$(curl -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600")
	INSTANCE_ID=$(curl url -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/instance-id)
	REGION=$(curl url -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/placement/region)
	LIFECYCLE=$(curl url -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/autoscaling/target-lifecycle-state)
	if [[ $LIFECYCLE == *"Warmed"* ]]; then
		rm /var/lib/cloud/instances/$INSTANCE_ID/sem/config_scripts_user
		exit 0
	fi
fi
--BOUNDARY
Content-Type: application/node.eks.aws



--BOUNDARY
Content-Type: application/node.eks.aws

---
apiVersion: node.eks.aws/v1alpha1
kind: NodeConfig
spec:
  kubelet:
    flags:
      - --node-labels=foo=bar,instancemgr.keikoproj.io/image=ami-123456789012,node.kubernetes.io/role=instance-group-1
      - --register-with-taints=foo=bar:NoSchedule

--BOUNDARY
Content-Type: text/x-shellscript; charset="us-ascii"

#!/bin/bash
set +o xtrace
bar
--BOUNDARY--`
	userData := ctx.GetBasicUserData("foo", args, kubeletArgs, userDataPayload, mounts)
	basicUserDataDecoded, _ := base64.StdEncoding.DecodeString(userData)
	basicUserDataString := string(basicUserDataDecoded)
	if basicUserDataString != expectedDataLinux {
		t.Fatalf("\nExpected: START>%v<END\n Got: START>%v<END", expectedDataLinux, basicUserDataString)
	}
}

func TestGetBasicUserDataWindows(t *testing.T) {
	var (
		k             = MockKubernetesClientSet()
		ig            = MockInstanceGroup()
		asgMock       = NewAutoScalingMocker()
		iamMock       = NewIamMocker()
		eksMock       = NewEksMocker()
		ec2Mock       = NewEc2Mocker()
		ssmMock       = NewSsmMocker()
		configuration = ig.GetEKSConfiguration()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)
	ctx := MockContext(ig, k, w)

	configuration.BootstrapOptions = &v1alpha1.BootstrapOptions{
		MaxPods:          4,
		ContainerRuntime: "containerd",
	}
	configuration.Labels = map[string]string{
		"foo": "bar",
	}
	configuration.Taints = []corev1.Taint{
		{
			Key:    "foo",
			Value:  "bar",
			Effect: "NoSchedule",
		},
	}

	configuration.BootstrapArguments = "--eviction-hard=memory.available<300Mi,nodefs.available<5% --system-reserved=memory=2.5Gi --v=2"
	configuration.UserData = []v1alpha1.UserDataStage{
		{
			Stage: "PreBootstrap",
			Data:  "foo",
		},
		{
			Stage: "PostBootstrap",
			Data:  "bar",
		},
	}

	ig.Annotations[OsFamilyAnnotation] = OsFamilyWindows

	expectedDataWindows := `
<powershell>
  foo
  [string]$EKSBinDir = "$env:ProgramFiles\Amazon\EKS"
  [string]$EKSBootstrapScriptName = 'Start-EKSBootstrap.ps1'
  [string]$EKSBootstrapScriptFile = "$EKSBinDir\$EKSBootstrapScriptName"
  [string]$IMDSToken=(curl -UseBasicParsing -Method PUT "http://169.254.169.254/latest/api/token" -H @{ "X-aws-ec2-metadata-token-ttl-seconds" = "21600"} | % { Echo $_.Content})
  [string]$InstanceID=(curl -UseBasicParsing -Method GET "http://169.254.169.254/latest/meta-data/instance-id" -H @{ "X-aws-ec2-metadata-token" = "$IMDSToken"} | % { Echo $_.Content})
  [string]$Lifecycle=(curl -UseBasicParsing -Method GET "http://169.254.169.254/latest/meta-data/autoscaling/target-lifecycle-state" -H @{ "X-aws-ec2-metadata-token" = "$IMDSToken"} | % { Echo $_.Content})
  if ($Lifecycle -like "*Warmed*") {
    Echo "Not starting Kubelet due to warmed state."
    & C:\ProgramData\Amazon\EC2-Windows\Launch\Scripts\InitializeInstance.ps1 -Schedule
  } else {
    & $EKSBootstrapScriptFile -EKSClusterName foo -Base64ClusterCA dGVzdA== -APIServerEndpoint foo.amazonaws.com -ContainerRuntime containerd -KubeletExtraArgs '--node-labels=foo=bar,instancemgr.keikoproj.io/image=ami-123456789012,node.kubernetes.io/role=instance-group-1 --register-with-taints=foo=bar:NoSchedule --eviction-hard=memory.available<300Mi,nodefs.available<5% --system-reserved=memory=2.5Gi --v=2 --max-pods=4' 3>&1 4>&1 5>&1 6>&1
    bar
  }
</powershell>`

	var (
		args            = ctx.GetBootstrapArgs()
		kubeletArgs     = ctx.GetKubeletExtraArgs()
		userDataPayload = ctx.GetUserDataStages()
		mounts          = ctx.GetMountOpts()
	)

	userData := ctx.GetBasicUserData("foo", args, kubeletArgs, userDataPayload, mounts)
	basicUserDataDecoded, _ := base64.StdEncoding.DecodeString(userData)
	basicUserDataString := string(basicUserDataDecoded)
	if basicUserDataString != expectedDataWindows {
		t.Fatalf("\nExpected: START>%v<END\n Got: START>%v<END", expectedDataWindows, basicUserDataString)
	}
}

func TestGetBasicUserDataWindowsWithInjectionDisabled(t *testing.T) {
	var (
		k             = MockKubernetesClientSet()
		ig            = MockInstanceGroup()
		asgMock       = NewAutoScalingMocker()
		iamMock       = NewIamMocker()
		eksMock       = NewEksMocker()
		ec2Mock       = NewEc2Mocker()
		ssmMock       = NewSsmMocker()
		configuration = ig.GetEKSConfiguration()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)
	ctx := MockContext(ig, k, w)

	configuration.BootstrapOptions = &v1alpha1.BootstrapOptions{
		MaxPods: 4,
	}
	configuration.Labels = map[string]string{
		"foo": "bar",
	}
	configuration.Taints = []corev1.Taint{
		{
			Key:    "foo",
			Value:  "bar",
			Effect: "NoSchedule",
		},
	}

	configuration.BootstrapArguments = "--eviction-hard=memory.available<300Mi,nodefs.available<5% --system-reserved=memory=2.5Gi --v=2"
	configuration.UserData = []v1alpha1.UserDataStage{
		{
			Stage: "PreBootstrap",
			Data:  "foo",
		},
		{
			Stage: "PostBootstrap",
			Data:  "bar",
		},
	}

	ig.Annotations[OsFamilyAnnotation] = OsFamilyWindows

	expectedDataWindows := `
<powershell>
  foo
  [string]$EKSBinDir = "$env:ProgramFiles\Amazon\EKS"
  [string]$EKSBootstrapScriptName = 'Start-EKSBootstrap.ps1'
  [string]$EKSBootstrapScriptFile = "$EKSBinDir\$EKSBootstrapScriptName"
  [string]$IMDSToken=(curl -UseBasicParsing -Method PUT "http://169.254.169.254/latest/api/token" -H @{ "X-aws-ec2-metadata-token-ttl-seconds" = "21600"} | % { Echo $_.Content})
  [string]$InstanceID=(curl -UseBasicParsing -Method GET "http://169.254.169.254/latest/meta-data/instance-id" -H @{ "X-aws-ec2-metadata-token" = "$IMDSToken"} | % { Echo $_.Content})
  [string]$Lifecycle=(curl -UseBasicParsing -Method GET "http://169.254.169.254/latest/meta-data/autoscaling/target-lifecycle-state" -H @{ "X-aws-ec2-metadata-token" = "$IMDSToken"} | % { Echo $_.Content})
  if ($Lifecycle -like "*Warmed*") {
    Echo "Not starting Kubelet due to warmed state."
    & C:\ProgramData\Amazon\EC2-Windows\Launch\Scripts\InitializeInstance.ps1 -Schedule
  } else {
    & $EKSBootstrapScriptFile -EKSClusterName foo -KubeletExtraArgs '--node-labels=foo=bar,instancemgr.keikoproj.io/image=ami-123456789012,node.kubernetes.io/role=instance-group-1 --register-with-taints=foo=bar:NoSchedule --eviction-hard=memory.available<300Mi,nodefs.available<5% --system-reserved=memory=2.5Gi --v=2 --max-pods=4' 3>&1 4>&1 5>&1 6>&1
    bar
  }
</powershell>`

	ctx.DisableWinClusterInjection = true
	var (
		args            = ctx.GetBootstrapArgs()
		kubeletArgs     = ctx.GetKubeletExtraArgs()
		userDataPayload = ctx.GetUserDataStages()
		mounts          = ctx.GetMountOpts()
	)

	userData := ctx.GetBasicUserData("foo", args, kubeletArgs, userDataPayload, mounts)
	basicUserDataDecoded, _ := base64.StdEncoding.DecodeString(userData)
	basicUserDataString := string(basicUserDataDecoded)
	if basicUserDataString != expectedDataWindows {
		t.Fatalf("\nExpected: START>%v<END\n Got: START>%v<END", expectedDataWindows, basicUserDataString)
	}
}

func TestCustomNetworkingMaxPods(t *testing.T) {
	var (
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
		eksMock = NewEksMocker()
		ec2Mock = NewEc2Mocker()
		ssmMock = NewSsmMocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)

	ig.Annotations = map[string]string{
		ClusterAutoscalerEnabledAnnotation: "true",
		CustomNetworkingHostPodsAnnotation: "2",
		CustomNetworkingEnabledAnnotation:  "true",
	}

	ctx := MockContext(ig, k, w)
	ctx.GetDiscoveredState().SetInstanceTypeInfo([]*ec2.InstanceTypeInfo{
		&ec2.InstanceTypeInfo{
			InstanceType: aws.String("m5.large"),
			NetworkInfo: &ec2.NetworkInfo{
				MaximumNetworkInterfaces:  aws.Int64(3),
				Ipv4AddressesPerInterface: aws.Int64(10),
			},
		},
	})

	userData := ctx.GetBasicUserData("foo", ctx.GetBootstrapArgs(), "", UserDataPayload{}, []MountOpts{})
	basicUserDataDecoded, _ := base64.StdEncoding.DecodeString(userData)
	basicUserDataString := string(basicUserDataDecoded)
	if !strings.Contains(basicUserDataString, "--max-pods=20") {
		t.Fail()
	}

	tests := []struct {
		bootstrapOptions *v1alpha1.BootstrapOptions
		annotations      map[string]string
		expectedMaxPods  string
	}{
		{
			annotations: map[string]string{
				ClusterAutoscalerEnabledAnnotation: "true",
				CustomNetworkingHostPodsAnnotation: "2",
				CustomNetworkingEnabledAnnotation:  "true",
			},
			bootstrapOptions: &v1alpha1.BootstrapOptions{MaxPods: 22},
			expectedMaxPods:  "--max-pods=22",
		},
		{
			annotations: map[string]string{
				ClusterAutoscalerEnabledAnnotation: "true",
				CustomNetworkingHostPodsAnnotation: "2",
				CustomNetworkingEnabledAnnotation:  "true",
			},
			bootstrapOptions: &v1alpha1.BootstrapOptions{MaxPods: 0},
			expectedMaxPods:  "--max-pods=20",
		},
		{
			annotations: map[string]string{
				ClusterAutoscalerEnabledAnnotation: "true",
				CustomNetworkingHostPodsAnnotation: "2",
				CustomNetworkingEnabledAnnotation:  "true",
			},
			bootstrapOptions: nil,
			expectedMaxPods:  "--max-pods=20",
		},
		{
			annotations: map[string]string{
				ClusterAutoscalerEnabledAnnotation:                "true",
				CustomNetworkingPrefixAssignmentEnabledAnnotation: "true",
				CustomNetworkingHostPodsAnnotation:                "2",
				CustomNetworkingEnabledAnnotation:                 "true",
			},
			bootstrapOptions: nil,
			expectedMaxPods:  "--max-pods=110",
		},
		{
			annotations: map[string]string{
				ClusterAutoscalerEnabledAnnotation: "true",
				CustomNetworkingHostPodsAnnotation: "",
				CustomNetworkingEnabledAnnotation:  "true",
			},
			bootstrapOptions: nil,
			expectedMaxPods:  "--max-pods=20",
		},
	}

	for _, tc := range tests {
		ctx.InstanceGroup.GetEKSConfiguration().BootstrapOptions = tc.bootstrapOptions
		ctx.InstanceGroup.Annotations = tc.annotations
		userData = ctx.GetBasicUserData("foo", ctx.GetBootstrapArgs(), "", UserDataPayload{}, []MountOpts{})
		basicUserDataDecoded, _ = base64.StdEncoding.DecodeString(userData)
		basicUserDataString = string(basicUserDataDecoded)
		if !strings.Contains(basicUserDataString, tc.expectedMaxPods) {
			t.Fail()
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
		ssmMock = NewSsmMocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)
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
		{requested: []string{"my-sg-2", "my-sg-1"}, groups: []*ec2.SecurityGroup{MockSecurityGroup("sg-111", true, "my-sg-1"), MockSecurityGroup("sg-222", true, "my-sg-2")}, result: []string{"sg-111", "sg-222"}, withErr: false},
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
		ssmMock = NewSsmMocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)
	ctx := MockContext(ig, k, w)

	tests := []struct {
		requested []string
		subnets   []*ec2.Subnet
		result    []string
		withErr   bool
	}{
		{requested: []string{"subnet-111", "subnet-222"}, subnets: []*ec2.Subnet{MockSubnet("subnet-111", false, ""), MockSubnet("subnet-222", false, "")}, result: []string{"subnet-111", "subnet-222"}, withErr: false},
		{requested: []string{"subnet-111", "subnet-111", "subnet-222"}, subnets: []*ec2.Subnet{MockSubnet("subnet-111", false, ""), MockSubnet("subnet-222", false, "")}, result: []string{"subnet-111", "subnet-222"}, withErr: false},
		{requested: []string{"my-subnet-1", "subnet-222"}, subnets: []*ec2.Subnet{MockSubnet("subnet-111", true, "my-subnet-1"), MockSubnet("subnet-222", false, "")}, result: []string{"subnet-111", "subnet-222"}, withErr: false},
		{requested: []string{"my-subnet-1", "my-subnet-2"}, subnets: []*ec2.Subnet{MockSubnet("subnet-111", true, "my-subnet-1"), MockSubnet("subnet-222", true, "my-subnet-2")}, result: []string{"subnet-111", "subnet-222"}, withErr: false},
		{requested: []string{"my-subnet-2", "my-subnet-1"}, subnets: []*ec2.Subnet{MockSubnet("subnet-111", true, "my-subnet-1"), MockSubnet("subnet-222", true, "my-subnet-2")}, result: []string{"subnet-111", "subnet-222"}, withErr: false},
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
		ssmMock = NewSsmMocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)
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
		ssmMock = NewSsmMocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)
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
		status                     = ig.GetStatus()
		asgMock                    = NewAutoScalingMocker()
		iamMock                    = NewIamMocker()
		eksMock                    = NewEksMocker()
		ec2Mock                    = NewEc2Mocker()
		ssmMock                    = NewSsmMocker()
		defaultLifecycleLabel      = "instancemgr.keikoproj.io/lifecycle=normal"
		defaultImageLabel          = fmt.Sprintf("instancemgr.keikoproj.io/image=%v", configuration.GetImage())
		expectedLabels115          = []string{defaultImageLabel, defaultLifecycleLabel, "node-role.kubernetes.io/instance-group-1=\"\"", "node.kubernetes.io/role=instance-group-1"}
		expectedLabels116          = []string{defaultImageLabel, defaultLifecycleLabel, "node.kubernetes.io/role=instance-group-1"}
		expectedLabelsWithCustom   = []string{defaultImageLabel, defaultLifecycleLabel, "custom.kubernetes.io=customlabel", "node.kubernetes.io/role=instance-group-1"}
		expectedLabelsWithOverride = []string{defaultImageLabel, defaultLifecycleLabel, "custom.kubernetes.io=customlabel", "override.kubernetes.io=instance-group-1", "override2.kubernetes.io=instance-group-1"}
		overrideAnnotation         = map[string]string{OverrideDefaultLabelsAnnotation: "override.kubernetes.io=instance-group-1,override2.kubernetes.io=instance-group-1"}
		expectedSpotLabel          = []string{defaultImageLabel, "instancemgr.keikoproj.io/lifecycle=spot", "node-role.kubernetes.io/instance-group-1=\"\"", "node.kubernetes.io/role=instance-group-1"}
		expectedMixedLabel         = []string{defaultImageLabel, "instancemgr.keikoproj.io/lifecycle=mixed", "node-role.kubernetes.io/instance-group-1=\"\"", "node.kubernetes.io/role=instance-group-1"}
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)
	ctx := MockContext(ig, k, w)

	tests := []struct {
		clusterVersion           string
		instanceGroupLabels      map[string]string
		instanceGroupAnnotations map[string]string
		expectedLabels           []string
		withSpot                 bool
		withMixedInstances       bool
	}{
		{clusterVersion: "", withSpot: true, expectedLabels: expectedSpotLabel},
		{clusterVersion: "", withMixedInstances: true, expectedLabels: expectedMixedLabel},
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
		status.SetLifecycle(v1alpha1.LifecycleStateNormal)
		if tc.withSpot {
			status.SetLifecycle(v1alpha1.LifecycleStateSpot)
		} else if tc.withMixedInstances {
			status.SetLifecycle(v1alpha1.LifecycleStateMixed)
		}
		ctx.SetDiscoveredState(&DiscoveredState{
			Publisher: kubeprovider.EventPublisher{
				Client: k.Kubernetes,
			},
			Cluster: MockEksCluster(tc.clusterVersion),
		})
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
		ssmMock       = NewSsmMocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)
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

func TestGetOverrides(t *testing.T) {
	var (
		g             = gomega.NewGomegaWithT(t)
		k             = MockKubernetesClientSet()
		ig            = MockInstanceGroup()
		configuration = ig.GetEKSConfiguration()
		asgMock       = NewAutoScalingMocker()
		iamMock       = NewIamMocker()
		eksMock       = NewEksMocker()
		ec2Mock       = NewEc2Mocker()
		ssmMock       = NewSsmMocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)
	ctx := MockContext(ig, k, w)
	state := ctx.GetDiscoveredState()

	instancePool := "SubFamilyFlexible"

	tests := []struct {
		primaryType        string
		scalingGroup       *autoscaling.Group
		mixedInstancesSpec *v1alpha1.MixedInstancesPolicySpec
		expectedOverrides  []*autoscaling.LaunchTemplateOverrides
	}{
		{
			primaryType:  "m5.xlarge",
			scalingGroup: MockScalingGroup("asg-1", true),
			mixedInstancesSpec: &v1alpha1.MixedInstancesPolicySpec{
				InstanceTypes: []*v1alpha1.InstanceTypeSpec{
					{
						Type:   "m5a.xlarge",
						Weight: 1,
					},
					{
						Type:   "m5g.xlarge",
						Weight: 1,
					},
				},
			},
			expectedOverrides: MockTemplateOverrides("1", "m5a.xlarge", "m5g.xlarge", "m5.xlarge"),
		},
		{
			primaryType:  "m5.xlarge",
			scalingGroup: MockScalingGroup("asg-1", true),
			mixedInstancesSpec: &v1alpha1.MixedInstancesPolicySpec{
				InstancePool: &instancePool,
			},
			expectedOverrides: MockTemplateOverrides("1", "m5.xlarge"),
		},
		{
			primaryType:  "t2.xlarge",
			scalingGroup: MockScalingGroup("asg-1", true),
			mixedInstancesSpec: &v1alpha1.MixedInstancesPolicySpec{
				InstanceTypes: []*v1alpha1.InstanceTypeSpec{
					{
						Type:   "t2a.xlarge",
						Weight: 1,
					},
					{
						Type:   "t2g.xlarge",
						Weight: 1,
					},
				},
			},
			expectedOverrides: MockTemplateOverrides("1", "t2a.xlarge", "t2g.xlarge", "t2.xlarge", "m5.xlarge"),
		},
	}

	for i, tc := range tests {
		t.Logf("Test #%v - %+v", i, tc.expectedOverrides)
		configuration.MixedInstancesPolicy = tc.mixedInstancesSpec
		state.ScalingGroup = tc.scalingGroup
		ig.Spec.EKSSpec.EKSConfiguration.InstanceType = tc.primaryType
		overrides := ctx.GetOverrides()
		g.Expect(overrides).To(gomega.ConsistOf(tc.expectedOverrides))
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
		ssmMock       = NewSsmMocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)
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

func TestMaxPodsSetCorrectly(t *testing.T) {
	var (
		k                         = MockKubernetesClientSet()
		bottleRocketIgWithMaxPods = MockBottleRocketInstanceGroup()
		bottleRocketIg            = MockBottleRocketInstanceGroup()
		amazonLinuxIgWithMaxPods  = MockInstanceGroup()
		asgMock                   = NewAutoScalingMocker()
		iamMock                   = NewIamMocker()
		eksMock                   = NewEksMocker()
		ec2Mock                   = NewEc2Mocker()
		ssmMock                   = NewSsmMocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)

	bottleRocketIgWithMaxPods.Spec.EKSSpec.EKSConfiguration.BootstrapOptions = &v1alpha1.BootstrapOptions{
		MaxPods: 15,
	}

	amazonLinuxIgWithMaxPods.Spec.EKSSpec.EKSConfiguration.BootstrapOptions = &v1alpha1.BootstrapOptions{
		MaxPods: 15,
	}

	tests := []struct {
		ig                         *v1alpha1.InstanceGroup
		expectedScriptSubstrings   string
		unexpectedScriptSubstrings string
	}{
		{
			ig:                       bottleRocketIgWithMaxPods,
			expectedScriptSubstrings: "max-pods = 15",
		},
		{
			ig:                       amazonLinuxIgWithMaxPods,
			expectedScriptSubstrings: "--max-pods=15",
		},
		{
			ig:                         bottleRocketIg,
			expectedScriptSubstrings:   "",
			unexpectedScriptSubstrings: "max-pods",
		},
	}

	for i, tc := range tests {
		t.Logf("Test #%v - %+v", i, tc)
		ctx := MockContext(tc.ig, k, w)
		args := ctx.GetBootstrapArgs()
		basicUserData := ctx.GetBasicUserData("", args, "", UserDataPayload{}, []MountOpts{})
		basicUserDataDecoded, _ := base64.StdEncoding.DecodeString(basicUserData)
		basicUserDataString := string(basicUserDataDecoded)
		if !strings.Contains(basicUserDataString, tc.expectedScriptSubstrings) {
			t.Fatalf("Cound not find expected string %v script in %v", tc.expectedScriptSubstrings, basicUserDataString)
		}
		if tc.unexpectedScriptSubstrings != "" && strings.Contains(basicUserDataString, tc.unexpectedScriptSubstrings) {
			t.Fatalf("Found unexpected string %v script in %v", tc.unexpectedScriptSubstrings, basicUserDataString)
		}
	}
}

func TestBootstrapDataForOSFamily(t *testing.T) {
	var (
		k              = MockKubernetesClientSet()
		bottleRocketIg = MockBottleRocketInstanceGroup()
		linuxIg        = MockInstanceGroup()
		windowsIg      = MockWindowsInstanceGroup()
		asgMock        = NewAutoScalingMocker()
		iamMock        = NewIamMocker()
		eksMock        = NewEksMocker()
		ec2Mock        = NewEc2Mocker()
		ssmMock        = NewSsmMocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)

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
		{
			ig:                       bottleRocketIg,
			expectedScriptSubstrings: "[settings.kubernetes]",
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
		ssmMock       = NewSsmMocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)
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

		err := ctx.UpdateLifecycleHooks("my-asg")
		g.Expect(err).NotTo(gomega.HaveOccurred())
		if tc.shouldRemove {
			g.Expect(asgMock.DeleteLifecycleHookCallCount).To(gomega.Equal(uint(len(tc.expectedRemoved))))
		}
		if tc.shouldAdd {
			g.Expect(asgMock.PutLifecycleHookCallCount).To(gomega.Equal(uint(len(tc.expectedAdded))))
		}
	}
}

func TestUpdateWarmPool(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		k       = MockKubernetesClientSet()
		ig      = MockInstanceGroup()
		asgMock = NewAutoScalingMocker()
		iamMock = NewIamMocker()
		eksMock = NewEksMocker()
		ec2Mock = NewEc2Mocker()
		ssmMock = NewSsmMocker()
	)

	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)
	ctx := MockContext(ig, k, w)

	tests := []struct {
		warmPoolSpec          *v1alpha1.WarmPoolSpec
		warmPoolConfiguration *autoscaling.WarmPoolConfiguration
		shouldDelete          bool
		shouldUpdate          bool
		shouldRequeue         bool
	}{
		// no update/delete needed
		{warmPoolConfiguration: nil, warmPoolSpec: nil},
		// enable warm pool - update
		{warmPoolConfiguration: nil, warmPoolSpec: MockWarmPoolSpec(-1, 0), shouldUpdate: true},
		// disable warm pool - delete
		{warmPoolConfiguration: MockWarmPool(-1, 0, ""), warmPoolSpec: nil, shouldDelete: true},
		// scale change (min)
		{warmPoolConfiguration: MockWarmPool(-1, 1, ""), warmPoolSpec: MockWarmPoolSpec(-1, 0), shouldUpdate: true},
		{warmPoolConfiguration: MockWarmPool(-1, 0, ""), warmPoolSpec: MockWarmPoolSpec(-1, 1), shouldUpdate: true},
		// scale change (max)
		{warmPoolConfiguration: MockWarmPool(3, 0, ""), warmPoolSpec: MockWarmPoolSpec(-1, 0), shouldUpdate: true},
		{warmPoolConfiguration: MockWarmPool(-1, 0, ""), warmPoolSpec: MockWarmPoolSpec(3, 0), shouldUpdate: true},
		// deleting - should requeue
		{warmPoolConfiguration: MockWarmPool(-1, 0, autoscaling.WarmPoolStatusPendingDelete), warmPoolSpec: nil, shouldRequeue: true},
	}

	for i, tc := range tests {
		t.Logf("test #%v", i)
		asgMock.DeleteWarmPoolCallCount = 0
		asgMock.PutWarmPoolCallCount = 0
		ig.Spec.EKSSpec.WarmPool = tc.warmPoolSpec
		scalingGroup := MockScalingGroup("my-asg", false)
		scalingGroup.WarmPoolConfiguration = tc.warmPoolConfiguration

		ctx.SetDiscoveredState(&DiscoveredState{
			Publisher: kubeprovider.EventPublisher{
				Client: k.Kubernetes,
			},
			ScalingGroup: scalingGroup,
		})

		err := ctx.UpdateWarmPool("my-asg")
		g.Expect(err).NotTo(gomega.HaveOccurred())
		if tc.shouldDelete {
			g.Expect(asgMock.DeleteWarmPoolCallCount).To(gomega.Equal(uint(1)))
		}
		if tc.shouldUpdate {
			g.Expect(asgMock.PutWarmPoolCallCount).To(gomega.Equal(uint(1)))
		}
		if tc.shouldRequeue {
			g.Expect(asgMock.DeleteWarmPoolCallCount).To(gomega.Equal(uint(0)))
			g.Expect(asgMock.PutWarmPoolCallCount).To(gomega.Equal(uint(0)))
		}
	}
}

func TestFilterSupportedArch(t *testing.T) {
	var (
		g = gomega.NewGomegaWithT(t)
	)

	tests := []struct {
		name          string
		architectures []string
		expected      string
	}{
		{
			name:          "supported architecture x86",
			architectures: []string{"x86_64"},
			expected:      "x86_64",
		},
		{
			name:          "supported architecture arm64",
			architectures: []string{"arm64"},
			expected:      "arm64",
		},
		{
			name:          "no supported architecture",
			architectures: []string{},
			expected:      "",
		},
	}

	for _, tc := range tests {
		result := FilterSupportedArch(tc.architectures)
		g.Expect(result).To(gomega.Equal(tc.expected))
	}

}

func TestGetEksLatestAmi(t *testing.T) {
	var (
		k            = MockKubernetesClientSet()
		ig           = MockInstanceGroup()
		config       = ig.GetEKSConfiguration()
		asgMock      = NewAutoScalingMocker()
		iamMock      = NewIamMocker()
		eksMock      = NewEksMocker()
		ec2Mock      = NewEc2Mocker()
		ssmMock      = NewSsmMocker()
		instanceType = "m5.large"
	)
	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)

	tests := []struct {
		name          string
		OSFamily      string
		arch          string
		expectedError error
	}{
		{
			name:          "AmazonLinux2-x86_64",
			OSFamily:      "amazonlinux2",
			arch:          "x86_64",
			expectedError: nil,
		},
		{
			name:          "bottlerocket-x86_64",
			OSFamily:      "bottlerocket",
			arch:          "x86_64",
			expectedError: nil,
		},
		{
			name:          "AmazonLinux2-noarch",
			OSFamily:      "amazonlinux2",
			arch:          "noarch",
			expectedError: fmt.Errorf("no supported CPU architecture found for instance type %s", instanceType),
		},
	}

	for _, tc := range tests {
		ig.SetAnnotations(map[string]string{
			OsFamilyAnnotation: tc.OSFamily,
		})
		config.InstanceType = instanceType
		ctx := MockContext(ig, k, w)
		ctx.GetDiscoveredState().SetInstanceTypeInfo([]*ec2.InstanceTypeInfo{
			{
				InstanceType: aws.String(instanceType),
				ProcessorInfo: &ec2.ProcessorInfo{
					SupportedArchitectures: []*string{aws.String(tc.arch)},
				},
			},
		})
		_, err := ctx.GetEksLatestAmi()
		if err == nil && tc.expectedError == nil {
			continue
		}
		if err != nil && tc.expectedError != nil && err.Error() != tc.expectedError.Error() {
			t.Fatalf("expected %v got %v, test %s", tc.expectedError, err, tc.name)
		}

	}
}

func TestGetEksLatestAmiForAL2023(t *testing.T) {
	var (
		k            = MockKubernetesClientSet()
		ig           = MockInstanceGroup()
		config       = ig.GetEKSConfiguration()
		asgMock      = NewAutoScalingMocker()
		iamMock      = NewIamMocker()
		eksMock      = NewEksMocker()
		ec2Mock      = NewEc2Mocker()
		ssmMock      = NewSsmMocker()
		instanceType = "m5.large"
	)
	w := MockAwsWorker(asgMock, iamMock, eksMock, ec2Mock, ssmMock)
	ig.GetEKSConfiguration().UserData = mockUserDataStages()

	tests := []struct {
		name          string
		OSFamily      string
		arch          string
		expectedError error
	}{
		{
			name:          "amazonlinux2023",
			OSFamily:      "",
			arch:          "x86_64",
			expectedError: nil,
		},
	}

	for _, tc := range tests {
		config.InstanceType = instanceType
		ctx := MockContext(ig, k, w)
		ctx.GetDiscoveredState().SetInstanceTypeInfo([]*ec2.InstanceTypeInfo{
			{
				InstanceType: aws.String(instanceType),
				ProcessorInfo: &ec2.ProcessorInfo{
					SupportedArchitectures: []*string{aws.String(tc.arch)},
				},
			},
		})
		_, err := ctx.GetEksLatestAmi()
		if err == nil && tc.expectedError == nil {
			continue
		}
		if err != nil && tc.expectedError != nil && err.Error() != tc.expectedError.Error() {
			t.Fatalf("expected %v got %v, test %s", tc.expectedError, err, tc.name)
		}

	}
}
