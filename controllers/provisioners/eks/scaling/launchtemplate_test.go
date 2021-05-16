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
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"

	"github.com/onsi/gomega"
)

func MockLaunchTemplateScalingInstance(id, name, version string) *autoscaling.Instance {
	return &autoscaling.Instance{
		InstanceId: aws.String(id),
		LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
			LaunchTemplateName: aws.String(name),
			Version:            aws.String(version),
		},
	}
}

func MockLaunchTemplateVersion() *ec2.LaunchTemplateVersion {
	return &ec2.LaunchTemplateVersion{
		LaunchTemplateData: &ec2.ResponseLaunchTemplateData{
			IamInstanceProfile: &ec2.LaunchTemplateIamInstanceProfileSpecification{
				Arn: aws.String(""),
			},
			InstanceType:     aws.String(""),
			SecurityGroupIds: aws.StringSlice([]string{}),
			ImageId:          aws.String(""),
			KeyName:          aws.String(""),
			UserData:         aws.String(""),
		},
	}
}

func MockLaunchTemplate(name string) *ec2.LaunchTemplate {
	return &ec2.LaunchTemplate{
		LaunchTemplateName: aws.String(name),
	}
}

type MockEc2Client struct {
	ec2iface.EC2API
	DescribeLaunchTemplatesErr            error
	CreateLaunchTemplateErr               error
	CreateLaunchTemplateVersionErr        error
	DeleteLaunchTemplateErr               error
	DeletedLaunchTemplateVersionCount     int
	DeleteLaunchTemplateVersionsCallCount int
	CreateLaunchTemplateCallCount         int
	CreateLaunchTemplateVersionCallCount  int
	ModifyLaunchTemplateCallCount         int
	DeleteLaunchTemplateCallCount         int
	LaunchTemplates                       []*ec2.LaunchTemplate
	LaunchTemplateVersions                []*ec2.LaunchTemplateVersion
}

func (c *MockEc2Client) CreateLaunchTemplate(input *ec2.CreateLaunchTemplateInput) (*ec2.CreateLaunchTemplateOutput, error) {
	c.CreateLaunchTemplateCallCount++
	return &ec2.CreateLaunchTemplateOutput{}, c.CreateLaunchTemplateErr
}

func (c *MockEc2Client) DeleteLaunchTemplateVersions(input *ec2.DeleteLaunchTemplateVersionsInput) (*ec2.DeleteLaunchTemplateVersionsOutput, error) {
	c.DeletedLaunchTemplateVersionCount = len(input.Versions)
	c.DeleteLaunchTemplateVersionsCallCount++
	return &ec2.DeleteLaunchTemplateVersionsOutput{}, nil
}

func (c *MockEc2Client) DeleteLaunchTemplate(input *ec2.DeleteLaunchTemplateInput) (*ec2.DeleteLaunchTemplateOutput, error) {
	c.DeleteLaunchTemplateCallCount++
	return &ec2.DeleteLaunchTemplateOutput{}, c.DeleteLaunchTemplateErr
}

func (c *MockEc2Client) ModifyLaunchTemplate(input *ec2.ModifyLaunchTemplateInput) (*ec2.ModifyLaunchTemplateOutput, error) {
	c.ModifyLaunchTemplateCallCount++
	out := &ec2.ModifyLaunchTemplateOutput{
		LaunchTemplate: MockLaunchTemplate(*input.LaunchTemplateName),
	}
	return out, nil
}

func (c *MockEc2Client) CreateLaunchTemplateVersion(input *ec2.CreateLaunchTemplateVersionInput) (*ec2.CreateLaunchTemplateVersionOutput, error) {
	c.CreateLaunchTemplateVersionCallCount++
	out := &ec2.CreateLaunchTemplateVersionOutput{
		LaunchTemplateVersion: &ec2.LaunchTemplateVersion{
			VersionNumber: aws.Int64(1),
		},
	}

	return out, c.CreateLaunchTemplateVersionErr
}

func (c *MockEc2Client) DescribeLaunchTemplateVersionsPages(input *ec2.DescribeLaunchTemplateVersionsInput, callback func(*ec2.DescribeLaunchTemplateVersionsOutput, bool) bool) error {
	page, err := c.DescribeLaunchTemplateVersions(input)
	if err != nil {
		return err
	}
	callback(page, false)
	return nil
}

func (c *MockEc2Client) DescribeLaunchTemplateVersions(input *ec2.DescribeLaunchTemplateVersionsInput) (*ec2.DescribeLaunchTemplateVersionsOutput, error) {
	return &ec2.DescribeLaunchTemplateVersionsOutput{LaunchTemplateVersions: c.LaunchTemplateVersions}, nil
}

func (c *MockEc2Client) DescribeLaunchTemplatesPages(input *ec2.DescribeLaunchTemplatesInput, callback func(*ec2.DescribeLaunchTemplatesOutput, bool) bool) error {
	page, err := c.DescribeLaunchTemplates(input)
	if err != nil {
		return err
	}
	callback(page, false)
	return nil
}

func (c *MockEc2Client) DescribeLaunchTemplates(input *ec2.DescribeLaunchTemplatesInput) (*ec2.DescribeLaunchTemplatesOutput, error) {
	return &ec2.DescribeLaunchTemplatesOutput{LaunchTemplates: c.LaunchTemplates}, c.DescribeLaunchTemplatesErr
}

func TestLaunchTemplateDiscover(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		asgMock = &MockAutoScalingClient{}
		ec2Mock = &MockEc2Client{}
	)

	w := awsprovider.AwsWorker{
		AsgClient: asgMock,
		Ec2Client: ec2Mock,
	}

	discoveryInput := &DiscoverConfigurationInput{
		ScalingGroup: &autoscaling.Group{
			AutoScalingGroupName: aws.String("my-asg"),
			LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
				LaunchTemplateName: aws.String("my-launch-template"),
			},
		},
	}

	targetResource := &ec2.LaunchTemplate{
		LaunchTemplateName: aws.String("my-launch-template"),
	}

	resourceList := []*ec2.LaunchTemplate{
		targetResource,
		{
			LaunchTemplateName: aws.String("other-launch-config"),
		},
	}

	ec2Mock.LaunchTemplates = resourceList
	lt, err := NewLaunchTemplate("", w, discoveryInput)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(lt.TargetResource).To(gomega.Equal(targetResource))
	g.Expect(lt.ResourceList).To(gomega.Equal(resourceList))
	g.Expect(lt.Provisioned()).To(gomega.BeTrue())
	g.Expect(lt.Resource().(*ec2.LaunchTemplate)).To(gomega.Equal(targetResource))
	g.Expect(lt.Name()).To(gomega.Equal("my-launch-template"))

	ec2Mock.LaunchTemplates = []*ec2.LaunchTemplate{}
	lt, err = NewLaunchTemplate("", w, discoveryInput)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(lt.TargetResource).To(gomega.BeNil())
	g.Expect(lt.ResourceList).To(gomega.Equal([]*ec2.LaunchTemplate{}))
	g.Expect(lt.Provisioned()).To(gomega.BeFalse())
	g.Expect(lt.Resource().(*ec2.LaunchTemplate)).To(gomega.BeNil())
	g.Expect(lt.Name()).To(gomega.BeEmpty())

	ec2Mock.LaunchTemplates = resourceList
	ec2Mock.DescribeLaunchTemplatesErr = errors.New("some-error")
	lt, err = NewLaunchTemplate("", w, discoveryInput)
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(lt.TargetResource).To(gomega.BeNil())
	g.Expect(lt.ResourceList).To(gomega.BeNil())
	g.Expect(lt.Provisioned()).To(gomega.BeFalse())
	g.Expect(lt.Resource().(*ec2.LaunchTemplate)).To(gomega.BeNil())
	g.Expect(lt.Name()).To(gomega.BeEmpty())
	ec2Mock.DescribeLaunchTemplatesErr = nil

	discoveryInput.ScalingGroup.LaunchTemplate.LaunchTemplateName = aws.String("different-launch-template")
	lt, err = NewLaunchTemplate("", w, discoveryInput)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(lt.TargetResource).To(gomega.BeNil())
	g.Expect(lt.ResourceList).To(gomega.Equal(resourceList))
	g.Expect(lt.Provisioned()).To(gomega.BeFalse())
	g.Expect(lt.Resource().(*ec2.LaunchTemplate)).To(gomega.BeNil())
	g.Expect(lt.Name()).To(gomega.BeEmpty())

	discoveryInput.ScalingGroup = nil
	lt, err = NewLaunchTemplate("", w, discoveryInput)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(lt.TargetResource).To(gomega.BeNil())
	g.Expect(lt.ResourceList).To(gomega.Equal(resourceList))
	g.Expect(lt.Provisioned()).To(gomega.BeFalse())
	g.Expect(lt.Resource().(*ec2.LaunchTemplate)).To(gomega.BeNil())
	g.Expect(lt.Name()).To(gomega.BeEmpty())
}

func TestLaunchTemplateCreate(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		asgMock = &MockAutoScalingClient{}
		ec2Mock = &MockEc2Client{}
	)

	w := awsprovider.AwsWorker{
		AsgClient: asgMock,
		Ec2Client: ec2Mock,
	}

	discoveryInput := &DiscoverConfigurationInput{
		ScalingGroup: &autoscaling.Group{
			AutoScalingGroupName: aws.String("my-asg"),
			LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
				LaunchTemplateName: aws.String("my-launch-template"),
			},
		},
	}

	resourceList := []*ec2.LaunchTemplate{
		{
			LaunchTemplateName: aws.String("other-launch-template"),
		},
	}

	ec2Mock.LaunchTemplates = resourceList

	lt, err := NewLaunchTemplate("", w, discoveryInput)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(lt.TargetResource).To(gomega.BeNil())
	g.Expect(lt.ResourceList).To(gomega.Equal(resourceList))

	err = lt.Create(&CreateConfigurationInput{
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

	resourceList = append(resourceList, &ec2.LaunchTemplate{
		LaunchTemplateName: aws.String("my-launch-template"),
	})
	ec2Mock.LaunchTemplates = resourceList

	lt, err = NewLaunchTemplate("", w, discoveryInput)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(lt.TargetResource).To(gomega.Equal(&ec2.LaunchTemplate{
		LaunchTemplateName: aws.String("my-launch-template"),
	}))
	g.Expect(lt.ResourceList).To(gomega.Equal(resourceList))

	err = lt.Create(&CreateConfigurationInput{
		Name:      "some-config",
		SpotPrice: "1.0",
	})
	lt.LatestVersion = nil
	g.Expect(err).NotTo(gomega.HaveOccurred())

	ec2Mock.CreateLaunchTemplateVersionErr = errors.New("some-error")
	err = lt.Create(&CreateConfigurationInput{
		Name:      "some-config",
		SpotPrice: "1.0",
	})
	g.Expect(err).To(gomega.HaveOccurred())
	ec2Mock.CreateLaunchTemplateVersionErr = nil

	ec2Mock.CreateLaunchTemplateErr = errors.New("some-error")
	ec2Mock.LaunchTemplates = []*ec2.LaunchTemplate{}
	lt, err = NewLaunchTemplate("", w, discoveryInput)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(lt.TargetResource).To(gomega.BeNil())
	g.Expect(lt.ResourceList).To(gomega.Equal([]*ec2.LaunchTemplate{}))
	err = lt.Create(&CreateConfigurationInput{
		Name:      "some-config",
		SpotPrice: "1.0",
	})
	g.Expect(err).To(gomega.HaveOccurred())
	ec2Mock.CreateLaunchTemplateErr = nil

	discoveryInput.ScalingGroup = nil
	ec2Mock.LaunchTemplates = resourceList
	lt, err = NewLaunchTemplate("", w, discoveryInput)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(lt.TargetResource).To(gomega.BeNil())
	g.Expect(lt.ResourceList).To(gomega.Equal(resourceList))
	err = lt.Create(&CreateConfigurationInput{
		Name:      "some-config",
		SpotPrice: "1.0",
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())
}

func TestLaunchTemplateDelete(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		asgMock = &MockAutoScalingClient{}
		ec2Mock = &MockEc2Client{}
	)

	w := awsprovider.AwsWorker{
		AsgClient: asgMock,
		Ec2Client: ec2Mock,
	}

	discoveryInput := &DiscoverConfigurationInput{
		ScalingGroup: &autoscaling.Group{
			AutoScalingGroupName: aws.String("my-asg"),
			LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
				LaunchTemplateName: aws.String("prefix-my-launch-template"),
				Version:            aws.String("6"),
			},
		},
	}

	now := time.Now()

	resourceList := []*ec2.LaunchTemplateVersion{
		{
			LaunchTemplateName: aws.String("prefix-my-launch-template"),
			VersionNumber:      aws.Int64(1),
			CreateTime:         aws.Time(now.Add(time.Duration(-10) * time.Minute)),
		},
		{
			LaunchTemplateName: aws.String("prefix-my-launch-template"),
			VersionNumber:      aws.Int64(2),
			CreateTime:         aws.Time(now.Add(time.Duration(-8) * time.Minute)),
		},
		{
			LaunchTemplateName: aws.String("prefix-my-launch-template"),
			VersionNumber:      aws.Int64(3),
			CreateTime:         aws.Time(now.Add(time.Duration(-7) * time.Minute)),
		},
		{
			LaunchTemplateName: aws.String("prefix-my-launch-template"),
			VersionNumber:      aws.Int64(4),
			CreateTime:         aws.Time(now.Add(time.Duration(-5) * time.Minute)),
		},
		{
			LaunchTemplateName: aws.String("prefix-my-launch-template"),
			VersionNumber:      aws.Int64(5),
			CreateTime:         aws.Time(now.Add(time.Duration(-3) * time.Minute)),
		},
	}

	ec2Mock.LaunchTemplateVersions = resourceList
	ec2Mock.LaunchTemplates = []*ec2.LaunchTemplate{
		{
			LaunchTemplateName: aws.String("prefix-my-launch-template"),
		},
		{
			LaunchTemplateName: aws.String("prefix-old-launch-template"),
		},
	}

	lt, err := NewLaunchTemplate("", w, discoveryInput)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(lt.TargetResource).To(gomega.Equal(&ec2.LaunchTemplate{
		LaunchTemplateName: aws.String("prefix-my-launch-template"),
	}))
	g.Expect(lt.TargetVersions).To(gomega.Equal(resourceList))

	err = lt.Delete(&DeleteConfigurationInput{
		Name:           "prefix-my-launch-template",
		Prefix:         "prefix-",
		RetainVersions: 2,
		DeleteAll:      false,
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(ec2Mock.DeleteLaunchTemplateVersionsCallCount).To(gomega.Equal(1))
	g.Expect(ec2Mock.DeletedLaunchTemplateVersionCount).To(gomega.Equal(3))
	ec2Mock.DeletedLaunchTemplateVersionCount = 0
	ec2Mock.DeleteLaunchTemplateVersionsCallCount = 0

	err = lt.Delete(&DeleteConfigurationInput{
		Name:           "prefix-my-launch-template",
		Prefix:         "prefix-",
		RetainVersions: 1,
		DeleteAll:      false,
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(ec2Mock.DeleteLaunchTemplateVersionsCallCount).To(gomega.Equal(1))
	g.Expect(ec2Mock.DeletedLaunchTemplateVersionCount).To(gomega.Equal(4))
	ec2Mock.DeletedLaunchTemplateVersionCount = 0
	ec2Mock.DeleteLaunchTemplateVersionsCallCount = 0

	err = lt.Delete(&DeleteConfigurationInput{
		Name:      "prefix-my-launch-template",
		Prefix:    "prefix-",
		DeleteAll: true,
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(ec2Mock.DeleteLaunchTemplateVersionsCallCount).To(gomega.Equal(0))
	g.Expect(ec2Mock.DeletedLaunchTemplateVersionCount).To(gomega.Equal(0))
	g.Expect(ec2Mock.DeleteLaunchTemplateCallCount).To(gomega.Equal(1))
	ec2Mock.DeleteLaunchTemplateCallCount = 0

	ec2Mock.DeleteLaunchTemplateErr = awserr.New(ec2.LaunchTemplateErrorCodeLaunchTemplateNameDoesNotExist, "not found", errors.New("an error occured"))
	err = lt.Delete(&DeleteConfigurationInput{
		Name:      "prefix-my-launch-template",
		Prefix:    "prefix-",
		DeleteAll: true,
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(ec2Mock.DeleteLaunchTemplateCallCount).To(gomega.Equal(1))
	ec2Mock.DeleteLaunchTemplateErr = nil
}

func TestLaunchTemplateRotationNeeded(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		asgMock = &MockAutoScalingClient{}
		ec2Mock = &MockEc2Client{}
	)

	w := awsprovider.AwsWorker{
		AsgClient: asgMock,
		Ec2Client: ec2Mock,
	}

	tests := []struct {
		scalingInstances []*autoscaling.Instance
		latestVersion    string
		rotationNeeded   bool
	}{
		{scalingInstances: []*autoscaling.Instance{}, latestVersion: "6", rotationNeeded: false},
		{scalingInstances: []*autoscaling.Instance{MockLaunchTemplateScalingInstance("i-1234", "my-launch-template", "6"), MockLaunchTemplateScalingInstance("i-2222", "my-launch-template", "6")}, latestVersion: "6", rotationNeeded: false},
		{scalingInstances: []*autoscaling.Instance{MockLaunchTemplateScalingInstance("i-1234", "my-launch-template", "6"), MockLaunchTemplateScalingInstance("i-2222", "my-launch-template", "5")}, latestVersion: "6", rotationNeeded: true},
		{scalingInstances: []*autoscaling.Instance{MockLaunchTemplateScalingInstance("i-1234", "my-launch-template", "6"), MockLaunchTemplateScalingInstance("i-2222", "other-launch-template", "6")}, latestVersion: "6", rotationNeeded: true},
	}

	for i, tc := range tests {
		t.Logf("Test #%v", i)
		discoveryInput := &DiscoverConfigurationInput{
			ScalingGroup: &autoscaling.Group{
				Instances:            tc.scalingInstances,
				AutoScalingGroupName: aws.String("my-asg"),
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
					LaunchTemplateName: aws.String("my-launch-template"),
					Version:            aws.String(tc.latestVersion),
				},
			},
		}

		ec2Mock.LaunchTemplates = []*ec2.LaunchTemplate{
			{
				LaunchTemplateName: aws.String("my-launch-template"),
			},
		}

		lt, err := NewLaunchTemplate("", w, discoveryInput)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		n, err := strconv.ParseInt(tc.latestVersion, 10, 64)
		lt.LatestVersion = &ec2.LaunchTemplateVersion{
			VersionNumber: aws.Int64(n),
		}

		result := lt.RotationNeeded(discoveryInput)
		g.Expect(result).To(gomega.Equal(tc.rotationNeeded))
	}

}

func TestLaunchTemplateDrifted(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		asgMock = &MockAutoScalingClient{}
		ec2Mock = &MockEc2Client{}
	)

	w := awsprovider.AwsWorker{
		AsgClient: asgMock,
		Ec2Client: ec2Mock,
	}

	discoveryInput := &DiscoverConfigurationInput{
		ScalingGroup: &autoscaling.Group{
			AutoScalingGroupName: aws.String("my-asg"),
			LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
				LaunchTemplateName: aws.String("prefix-my-launch-template"),
				Version:            aws.String("6"),
			},
		},
	}
	tests := []struct {
		launchTemplate *ec2.LaunchTemplate
		latestVersion  *ec2.LaunchTemplateVersion
		input          *CreateConfigurationInput
		shouldDrift    bool
	}{
		{
			launchTemplate: MockLaunchTemplate("my-launch-template"),
			latestVersion:  MockLaunchTemplateVersion(),
			input:          &CreateConfigurationInput{},
			shouldDrift:    false,
		},
		{
			launchTemplate: MockLaunchTemplate("my-launch-template"),
			latestVersion:  nil,
			input:          &CreateConfigurationInput{},
			shouldDrift:    true,
		},
		{
			launchTemplate: MockLaunchTemplate("my-launch-template"),
			latestVersion:  MockLaunchTemplateVersion(),
			input: &CreateConfigurationInput{
				ImageId: "ami-123456",
			},
			shouldDrift: true,
		},
		{
			launchTemplate: MockLaunchTemplate("my-launch-template"),
			latestVersion:  MockLaunchTemplateVersion(),
			input: &CreateConfigurationInput{
				InstanceType: "m5.large",
			},
			shouldDrift: true,
		},
		{
			launchTemplate: MockLaunchTemplate("my-launch-template"),
			latestVersion:  MockLaunchTemplateVersion(),
			input: &CreateConfigurationInput{
				IamInstanceProfileArn: "arn::aws:other-role",
			},
			shouldDrift: true,
		},
		{
			launchTemplate: MockLaunchTemplate("my-launch-template"),
			latestVersion:  MockLaunchTemplateVersion(),
			input: &CreateConfigurationInput{
				SecurityGroups: []string{"sg-1"},
			},
			shouldDrift: true,
		},
		{
			launchTemplate: MockLaunchTemplate("my-launch-template"),
			latestVersion:  MockLaunchTemplateVersion(),
			input: &CreateConfigurationInput{
				KeyName: "my-key",
			},
			shouldDrift: true,
		},
		{
			launchTemplate: MockLaunchTemplate("my-launch-template"),
			latestVersion:  MockLaunchTemplateVersion(),
			input: &CreateConfigurationInput{
				UserData: "test",
			},
			shouldDrift: true,
		},
		{
			launchTemplate: MockLaunchTemplate("my-launch-template"),
			latestVersion:  MockLaunchTemplateVersion(),
			input: &CreateConfigurationInput{
				Volumes: []v1alpha1.NodeVolume{
					{
						Name: "/dev/xvda",
						Type: "gp2",
						Size: 100,
					},
				},
			},
			shouldDrift: true,
		},
		{
			launchTemplate: MockLaunchTemplate("my-launch-template"),
			latestVersion:  MockLaunchTemplateVersion(),
			input: &CreateConfigurationInput{
				LicenseSpecifications: []string{"arn:aws:license-manager:us-west-2:1234456789:license-configuration:lic-1111122223333344444111112222333"},
			},
			shouldDrift: true,
		},
		{
			launchTemplate: MockLaunchTemplate("my-launch-template"),
			latestVersion:  MockLaunchTemplateVersion(),
			input: &CreateConfigurationInput{
				Placement: &v1alpha1.PlacementSpec{
					AvailabilityZone:     "us-west-2a",
					HostResourceGroupArn: "arn:aws:resource-groups:us-west-2:1234456789:group/host-group-name",
					Tenancy:              "host",
				},
			},
			shouldDrift: true,
		},
		{
			launchTemplate: MockLaunchTemplate("my-launch-template"),
			latestVersion:  MockLaunchTemplateVersion(),
			input: &CreateConfigurationInput{
				MetadataOptions: &v1alpha1.MetadataOptions{
					HttpEndpoint: "Enabled",
				},
			},
			shouldDrift: true,
		},
	}

	for i, tc := range tests {
		t.Logf("Test #%v", i)
		if len(tc.input.SecurityGroups) == 0 {
			tc.input.SecurityGroups = []string{}
		}
		ec2Mock.LaunchTemplates = []*ec2.LaunchTemplate{tc.launchTemplate}
		lt, err := NewLaunchTemplate("", w, discoveryInput)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		lt.LatestVersion = tc.latestVersion
		result := lt.Drifted(tc.input)
		g.Expect(result).To(gomega.Equal(tc.shouldDrift))
	}
}

func TestLaunchTemplatePlacementRequest(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		asgMock = &MockAutoScalingClient{}
		ec2Mock = &MockEc2Client{}
	)

	w := awsprovider.AwsWorker{
		AsgClient: asgMock,
		Ec2Client: ec2Mock,
	}

	discoveryInput := &DiscoverConfigurationInput{
		ScalingGroup: &autoscaling.Group{
			AutoScalingGroupName: aws.String("my-asg"),
			LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
				LaunchTemplateName: aws.String("prefix-my-launch-template"),
				Version:            aws.String("6"),
			},
		},
	}

	tests := []struct {
		name     string
		input    *v1alpha1.PlacementSpec
		expected *ec2.LaunchTemplatePlacementRequest
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: &ec2.LaunchTemplatePlacementRequest{},
		},
		{
			name: "valid input",
			input: &v1alpha1.PlacementSpec{
				AvailabilityZone:     "us-west-2a",
				HostResourceGroupArn: "arn:aws:resource-groups:us-west-2:1234456789:group/host-group-name",
				Tenancy:              "host",
			},
			expected: &ec2.LaunchTemplatePlacementRequest{
				AvailabilityZone:     aws.String("us-west-2a"),
				HostResourceGroupArn: aws.String("arn:aws:resource-groups:us-west-2:1234456789:group/host-group-name"),
				Tenancy:              aws.String("host"),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lt, err := NewLaunchTemplate("", w, discoveryInput)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			output := lt.launchTemplatePlacementRequest(tc.input)

			g.Expect(output).To(gomega.Equal(tc.expected))
		})
	}
}

func TestLaunchTemplatePlacement(t *testing.T) {
	var (
		g       = gomega.NewGomegaWithT(t)
		asgMock = &MockAutoScalingClient{}
		ec2Mock = &MockEc2Client{}
	)

	w := awsprovider.AwsWorker{
		AsgClient: asgMock,
		Ec2Client: ec2Mock,
	}

	discoveryInput := &DiscoverConfigurationInput{
		ScalingGroup: &autoscaling.Group{
			AutoScalingGroupName: aws.String("my-asg"),
			LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
				LaunchTemplateName: aws.String("prefix-my-launch-template"),
				Version:            aws.String("6"),
			},
		},
	}

	tests := []struct {
		name     string
		input    *v1alpha1.PlacementSpec
		expected *ec2.LaunchTemplatePlacement
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: &ec2.LaunchTemplatePlacement{},
		},
		{
			name: "valid input",
			input: &v1alpha1.PlacementSpec{
				AvailabilityZone:     "us-west-2a",
				HostResourceGroupArn: "arn:aws:resource-groups:us-west-2:1234456789:group/host-group-name",
				Tenancy:              "host",
			},
			expected: &ec2.LaunchTemplatePlacement{
				AvailabilityZone:     aws.String("us-west-2a"),
				HostResourceGroupArn: aws.String("arn:aws:resource-groups:us-west-2:1234456789:group/host-group-name"),
				Tenancy:              aws.String("host"),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lt, err := NewLaunchTemplate("", w, discoveryInput)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			output := lt.launchTemplatePlacement(tc.input)

			g.Expect(output).To(gomega.Equal(tc.expected))
		})
	}
}

func TestSortLicenseSpecifications(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	tests := []struct {
		input    []*ec2.LaunchTemplateLicenseConfiguration
		expected []*ec2.LaunchTemplateLicenseConfiguration
	}{
		{
			input: []*ec2.LaunchTemplateLicenseConfiguration{
				&ec2.LaunchTemplateLicenseConfiguration{
					LicenseConfigurationArn: aws.String("arn:aws:license-manager:us-west-2:1234456789:license-configuration:lic-2"),
				},
				&ec2.LaunchTemplateLicenseConfiguration{
					LicenseConfigurationArn: aws.String("arn:aws:license-manager:us-west-2:1234456789:license-configuration:lic-3"),
				},
				&ec2.LaunchTemplateLicenseConfiguration{
					LicenseConfigurationArn: aws.String("arn:aws:license-manager:us-west-2:1234456789:license-configuration:lic-1"),
				},
			},
			expected: []*ec2.LaunchTemplateLicenseConfiguration{
				&ec2.LaunchTemplateLicenseConfiguration{
					LicenseConfigurationArn: aws.String("arn:aws:license-manager:us-west-2:1234456789:license-configuration:lic-1"),
				},
				&ec2.LaunchTemplateLicenseConfiguration{
					LicenseConfigurationArn: aws.String("arn:aws:license-manager:us-west-2:1234456789:license-configuration:lic-2"),
				},
				&ec2.LaunchTemplateLicenseConfiguration{
					LicenseConfigurationArn: aws.String("arn:aws:license-manager:us-west-2:1234456789:license-configuration:lic-3"),
				},
			},
		},
	}

	for _, tc := range tests {
		output := sortLicenseSpecifications(tc.input)
		g.Expect(output).To(gomega.Equal(tc.expected))
	}

}
