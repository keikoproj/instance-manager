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

package scaling

import (
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"

	"github.com/onsi/gomega"
)

type MockAutoScalingClient struct {
	autoscalingiface.AutoScalingAPI
	DescribeLaunchConfigurationsErr    error
	CreateLaunchConfigurationErr       error
	DeleteLaunchConfigurationErr       error
	DeleteLaunchConfigurationCallCount int
	LaunchConfigurations               []*autoscaling.LaunchConfiguration
}

func (a *MockAutoScalingClient) CreateLaunchConfiguration(input *autoscaling.CreateLaunchConfigurationInput) (*autoscaling.CreateLaunchConfigurationOutput, error) {
	return &autoscaling.CreateLaunchConfigurationOutput{}, a.CreateLaunchConfigurationErr
}

func (a *MockAutoScalingClient) DescribeLaunchConfigurationsPages(input *autoscaling.DescribeLaunchConfigurationsInput, callback func(*autoscaling.DescribeLaunchConfigurationsOutput, bool) bool) error {
	page, err := a.DescribeLaunchConfigurations(input)
	if err != nil {
		return err
	}
	callback(page, false)
	return nil
}

func (a *MockAutoScalingClient) DescribeLaunchConfigurations(input *autoscaling.DescribeLaunchConfigurationsInput) (*autoscaling.DescribeLaunchConfigurationsOutput, error) {
	return &autoscaling.DescribeLaunchConfigurationsOutput{LaunchConfigurations: a.LaunchConfigurations}, a.DescribeLaunchConfigurationsErr
}

func (a *MockAutoScalingClient) DeleteLaunchConfiguration(input *autoscaling.DeleteLaunchConfigurationInput) (*autoscaling.DeleteLaunchConfigurationOutput, error) {
	a.DeleteLaunchConfigurationCallCount++
	return &autoscaling.DeleteLaunchConfigurationOutput{}, a.DeleteLaunchConfigurationErr
}

func TestDiscover(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		asgMock = &MockAutoScalingClient{}
	)

	w := awsprovider.AwsWorker{
		AsgClient: asgMock,
	}

	discoveryInput := &DiscoverConfigurationInput{
		ScalingGroup: &autoscaling.Group{
			AutoScalingGroupName:    aws.String("my-asg"),
			LaunchConfigurationName: aws.String("my-launch-config"),
		},
	}

	targetResource := &autoscaling.LaunchConfiguration{
		LaunchConfigurationName: aws.String("my-launch-config"),
	}

	resourceList := []*autoscaling.LaunchConfiguration{
		targetResource,
		{
			LaunchConfigurationName: aws.String("other-launch-config"),
		},
	}

	asgMock.LaunchConfigurations = resourceList
	lc, err := NewLaunchConfiguration("", w, discoveryInput)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(lc.TargetResource).To(gomega.Equal(targetResource))
	g.Expect(lc.ResourceList).To(gomega.Equal(resourceList))
	g.Expect(lc.Provisioned()).To(gomega.BeTrue())
	g.Expect(lc.Resource().(*autoscaling.LaunchConfiguration)).To(gomega.Equal(targetResource))
	g.Expect(lc.Name()).To(gomega.Equal("my-launch-config"))

	asgMock.LaunchConfigurations = []*autoscaling.LaunchConfiguration{}
	lc, err = NewLaunchConfiguration("", w, discoveryInput)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(lc.TargetResource).To(gomega.BeNil())
	g.Expect(lc.ResourceList).To(gomega.Equal([]*autoscaling.LaunchConfiguration{}))
	g.Expect(lc.Provisioned()).To(gomega.BeFalse())
	g.Expect(lc.Resource().(*autoscaling.LaunchConfiguration)).To(gomega.BeNil())
	g.Expect(lc.Name()).To(gomega.BeEmpty())

	asgMock.LaunchConfigurations = resourceList
	asgMock.DescribeLaunchConfigurationsErr = errors.New("some-error")
	lc, err = NewLaunchConfiguration("", w, discoveryInput)
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(lc.TargetResource).To(gomega.BeNil())
	g.Expect(lc.ResourceList).To(gomega.BeNil())
	g.Expect(lc.Provisioned()).To(gomega.BeFalse())
	g.Expect(lc.Resource().(*autoscaling.LaunchConfiguration)).To(gomega.BeNil())
	g.Expect(lc.Name()).To(gomega.BeEmpty())
	asgMock.DescribeLaunchConfigurationsErr = nil

	discoveryInput.ScalingGroup.LaunchConfigurationName = aws.String("different-launch-configuration")
	lc, err = NewLaunchConfiguration("", w, discoveryInput)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(lc.TargetResource).To(gomega.BeNil())
	g.Expect(lc.ResourceList).To(gomega.Equal(resourceList))
	g.Expect(lc.Provisioned()).To(gomega.BeFalse())
	g.Expect(lc.Resource().(*autoscaling.LaunchConfiguration)).To(gomega.BeNil())
	g.Expect(lc.Name()).To(gomega.BeEmpty())

	discoveryInput.ScalingGroup = nil
	lc, err = NewLaunchConfiguration("", w, discoveryInput)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(lc.TargetResource).To(gomega.BeNil())
	g.Expect(lc.ResourceList).To(gomega.Equal(resourceList))
	g.Expect(lc.Provisioned()).To(gomega.BeFalse())
	g.Expect(lc.Resource().(*autoscaling.LaunchConfiguration)).To(gomega.BeNil())
	g.Expect(lc.Name()).To(gomega.BeEmpty())
}

func TestCreate(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		asgMock = &MockAutoScalingClient{}
	)

	w := awsprovider.AwsWorker{
		AsgClient: asgMock,
	}

	discoveryInput := &DiscoverConfigurationInput{
		ScalingGroup: &autoscaling.Group{
			AutoScalingGroupName:    aws.String("my-asg"),
			LaunchConfigurationName: aws.String("my-launch-config"),
		},
	}

	resourceList := []*autoscaling.LaunchConfiguration{
		{
			LaunchConfigurationName: aws.String("other-launch-config"),
		},
	}

	asgMock.LaunchConfigurations = resourceList

	lc, err := NewLaunchConfiguration("", w, discoveryInput)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(lc.TargetResource).To(gomega.BeNil())
	g.Expect(lc.ResourceList).To(gomega.Equal(resourceList))

	err = lc.Create(&CreateConfigurationInput{
		Name:      "some-config",
		SpotPrice: "1.0",
		Volumes: []v1alpha1.NodeVolume{
			{
				Name: "/dev/xvda1",
				Type: "gp2",
				Size: 30,
			},
		},
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	resourceList = append(resourceList, &autoscaling.LaunchConfiguration{
		LaunchConfigurationName: aws.String("my-launch-config"),
	})
	asgMock.LaunchConfigurations = resourceList

	lc, err = NewLaunchConfiguration("", w, discoveryInput)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(lc.TargetResource).To(gomega.Equal(&autoscaling.LaunchConfiguration{
		LaunchConfigurationName: aws.String("my-launch-config"),
	}))
	g.Expect(lc.ResourceList).To(gomega.Equal(resourceList))

	err = lc.Create(&CreateConfigurationInput{
		Name:      "some-config",
		SpotPrice: "1.0",
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())

	asgMock.CreateLaunchConfigurationErr = errors.New("some-error")
	err = lc.Create(&CreateConfigurationInput{
		Name:      "some-config",
		SpotPrice: "1.0",
	})
	g.Expect(err).To(gomega.HaveOccurred())
	asgMock.CreateLaunchConfigurationErr = nil

	discoveryInput.ScalingGroup = nil
	lc, err = NewLaunchConfiguration("", w, discoveryInput)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(lc.TargetResource).To(gomega.BeNil())
	g.Expect(lc.ResourceList).To(gomega.Equal(resourceList))
	err = lc.Create(&CreateConfigurationInput{
		Name:      "some-config",
		SpotPrice: "1.0",
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())
}

func TestDelete(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		asgMock = &MockAutoScalingClient{}
	)

	w := awsprovider.AwsWorker{
		AsgClient: asgMock,
	}

	discoveryInput := &DiscoverConfigurationInput{
		ScalingGroup: &autoscaling.Group{
			AutoScalingGroupName:    aws.String("my-asg"),
			LaunchConfigurationName: aws.String("prefix-my-launch-config"),
		},
	}

	now := time.Now()

	resourceList := []*autoscaling.LaunchConfiguration{
		{
			LaunchConfigurationName: aws.String("prefix-my-launch-config"),
			CreatedTime:             aws.Time(now.Add(time.Duration(-1) * time.Minute)),
		},
		{
			LaunchConfigurationName: aws.String("diff-prefix-my-launch-config"),
			CreatedTime:             aws.Time(now),
		},
		{
			LaunchConfigurationName: aws.String("prefix-old-launch-config"),
			CreatedTime:             aws.Time(now.Add(time.Duration(-2) * time.Minute)),
		},
		{
			LaunchConfigurationName: aws.String("prefix-older-launch-config"),
			CreatedTime:             aws.Time(now.Add(time.Duration(-3) * time.Minute)),
		},
		{
			LaunchConfigurationName: aws.String("prefix-veryold-launch-config"),
			CreatedTime:             aws.Time(now.Add(time.Duration(-4) * time.Minute)),
		},
	}

	asgMock.LaunchConfigurations = resourceList

	lc, err := NewLaunchConfiguration("", w, discoveryInput)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(lc.TargetResource).To(gomega.Equal(&autoscaling.LaunchConfiguration{
		LaunchConfigurationName: aws.String("prefix-my-launch-config"),
		CreatedTime:             aws.Time(now.Add(time.Duration(-1) * time.Minute)),
	}))
	g.Expect(lc.ResourceList).To(gomega.Equal(resourceList))

	lc.Delete(&DeleteConfigurationInput{
		Name:           "prefix-my-launch-config",
		Prefix:         "prefix-",
		RetainVersions: 2,
		DeleteAll:      false,
	})

	g.Expect(asgMock.DeleteLaunchConfigurationCallCount).To(gomega.Equal(2))
	asgMock.DeleteLaunchConfigurationCallCount = 0

	lc.Delete(&DeleteConfigurationInput{
		Name:           "prefix-my-launch-config",
		Prefix:         "prefix-",
		RetainVersions: 1,
		DeleteAll:      false,
	})

	g.Expect(asgMock.DeleteLaunchConfigurationCallCount).To(gomega.Equal(3))
	asgMock.DeleteLaunchConfigurationCallCount = 0

	lc.Delete(&DeleteConfigurationInput{
		Name:      "prefix-my-launch-config",
		Prefix:    "prefix-",
		DeleteAll: true,
	})
	g.Expect(asgMock.DeleteLaunchConfigurationCallCount).To(gomega.Equal(4))
}

func TestDrifted(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		asgMock = &MockAutoScalingClient{}
	)

	w := awsprovider.AwsWorker{
		AsgClient: asgMock,
	}

	discoveryInput := &DiscoverConfigurationInput{
		ScalingGroup: &autoscaling.Group{
			AutoScalingGroupName:    aws.String("my-asg"),
			LaunchConfigurationName: aws.String("my-launch-config"),
		},
	}

	tests := []struct {
		launchConfig *autoscaling.LaunchConfiguration
		input        *CreateConfigurationInput
		shouldDrift  bool
	}{
		{
			launchConfig: &autoscaling.LaunchConfiguration{
				LaunchConfigurationName: aws.String("my-launch-config"),
			},
			input: &CreateConfigurationInput{
				SecurityGroups: []string{},
			},
			shouldDrift: false,
		},
		{
			launchConfig: &autoscaling.LaunchConfiguration{
				LaunchConfigurationName: aws.String("other-launch-config"),
			},
			input: &CreateConfigurationInput{
				SecurityGroups: []string{},
			},
			shouldDrift: true,
		},
		{
			launchConfig: &autoscaling.LaunchConfiguration{
				LaunchConfigurationName: aws.String("my-launch-config"),
				ImageId:                 aws.String("ami-12345678"),
			},
			input: &CreateConfigurationInput{
				SecurityGroups: []string{},
				ImageId:        "ami-22222222",
			},
			shouldDrift: true,
		},
		{
			launchConfig: &autoscaling.LaunchConfiguration{
				LaunchConfigurationName: aws.String("my-launch-config"),
				InstanceType:            aws.String("m5.xlarge"),
			},
			input: &CreateConfigurationInput{
				SecurityGroups: []string{},
				InstanceType:   "m5.2xlarge",
			},
			shouldDrift: true,
		},
		{
			launchConfig: &autoscaling.LaunchConfiguration{
				LaunchConfigurationName: aws.String("my-launch-config"),
				IamInstanceProfile:      aws.String("a-profile"),
			},
			input: &CreateConfigurationInput{
				SecurityGroups:        []string{},
				IamInstanceProfileArn: "different-profile",
			},
			shouldDrift: true,
		},
		{
			launchConfig: &autoscaling.LaunchConfiguration{
				LaunchConfigurationName: aws.String("my-launch-config"),
				SecurityGroups:          aws.StringSlice([]string{"sg-1", "sg-2"}),
			},
			input: &CreateConfigurationInput{
				SecurityGroups: []string{"sg-1", "sg-3"},
			},
			shouldDrift: true,
		},
		{
			launchConfig: &autoscaling.LaunchConfiguration{
				LaunchConfigurationName: aws.String("my-launch-config"),
				SpotPrice:               aws.String("1.0"),
			},
			input: &CreateConfigurationInput{
				SecurityGroups: []string{},
				SpotPrice:      "1.1",
			},
			shouldDrift: true,
		},
		{
			launchConfig: &autoscaling.LaunchConfiguration{
				LaunchConfigurationName: aws.String("my-launch-config"),
				UserData:                aws.String("userdata"),
			},
			input: &CreateConfigurationInput{
				SecurityGroups: []string{},
				UserData:       "userdata2",
			},
			shouldDrift: true,
		},
		{
			launchConfig: &autoscaling.LaunchConfiguration{
				LaunchConfigurationName: aws.String("my-launch-config"),
				KeyName:                 aws.String("key"),
			},
			input: &CreateConfigurationInput{
				SecurityGroups: []string{},
				KeyName:        "key2",
			},
			shouldDrift: true,
		},
		{
			launchConfig: &autoscaling.LaunchConfiguration{
				LaunchConfigurationName: aws.String("my-launch-config"),
				BlockDeviceMappings: []*autoscaling.BlockDeviceMapping{
					{
						DeviceName: aws.String("/dev/xvda"),
						Ebs: &autoscaling.Ebs{
							VolumeType: aws.String("gp2"),
							VolumeSize: aws.Int64(32),
						},
					},
				},
			},
			input: &CreateConfigurationInput{
				SecurityGroups: []string{},
				Volumes: []v1alpha1.NodeVolume{
					{
						Name: "/dev/xvda",
						Type: "gp2",
						Size: 32,
					},
				},
			},
			shouldDrift: true,
		},
	}

	for i, tc := range tests {
		t.Logf("Test #%v", i)
		asgMock.LaunchConfigurations = []*autoscaling.LaunchConfiguration{tc.launchConfig}
		lc, err := NewLaunchConfiguration("", w, discoveryInput)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		result := lc.Drifted(tc.input)
		g.Expect(result).To(gomega.Equal(tc.shouldDrift))
	}
}
