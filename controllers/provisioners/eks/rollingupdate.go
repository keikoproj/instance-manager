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
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/go-logr/logr"
	"github.com/keikoproj/instance-manager/controllers/common"
	awsprovider "github.com/keikoproj/instance-manager/controllers/providers/aws"
	kubeprovider "github.com/keikoproj/instance-manager/controllers/providers/kubernetes"
	"github.com/keikoproj/instance-manager/controllers/provisioners/eks/scaling"
	"github.com/pkg/errors"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	drain "k8s.io/kubectl/pkg/drain"
)

const (
	RollingUpdateStrategyName = "rollingupdate"
)

var (
	DefaultWaitGroupTimeout  = time.Second * 3
	TransientLifecycleStates = []string{
		autoscaling.LifecycleStateDetaching,
		autoscaling.LifecycleStateEnteringStandby,
		autoscaling.LifecycleStatePending,
		autoscaling.LifecycleStatePendingProceed,
		autoscaling.LifecycleStatePendingWait,
		autoscaling.LifecycleStateTerminating,
		autoscaling.LifecycleStateTerminatingWait,
		autoscaling.LifecycleStateTerminatingProceed,
		autoscaling.LifecycleStateTerminated,
	}
)

type RollingUpdateRequest struct {
	logr.Logger
	AwsWorker        awsprovider.AwsWorker
	KubernetesClient kubeprovider.KubernetesClientSet
	ClusterNodes     *corev1.NodeList
	DrainGroup       *sync.WaitGroup
	ScalingGroup     *autoscaling.Group
	MaxUnavailable   int
	DesiredCapacity  int
	AllInstances     []string
	UpdateTargets    []string
}

func (ctx *EksInstanceGroupContext) NewRollingUpdateRequest() *RollingUpdateRequest {
	var (
		needsUpdate     []string
		allInstances    []string
		instanceGroup   = ctx.GetInstanceGroup()
		state           = ctx.GetDiscoveredState()
		scalingConfig   = state.GetScalingConfiguration()
		scalingResource = scalingConfig.Resource()
		scalingGroup    = state.GetScalingGroup()
		desiredCount    = int(aws.Int64Value(scalingGroup.DesiredCapacity))
		strategy        = instanceGroup.GetUpgradeStrategy().GetRollingUpdateType()
		maxUnavailable  = strategy.GetMaxUnavailable()
	)

	// Get all Autoscaling Instances that needs update
	for _, instance := range scalingGroup.Instances {
		var (
			instanceId = aws.StringValue(instance.InstanceId)
			lifecycle  = aws.StringValue(instance.LifecycleState)
		)

		if common.ContainsEqualFold(TransientLifecycleStates, lifecycle) {
			continue
		}

		allInstances = append(allInstances, aws.StringValue(instance.InstanceId))

		if awsprovider.IsUsingLaunchConfiguration(scalingGroup) {
			if instance.LaunchConfigurationName == nil {
				needsUpdate = append(needsUpdate, instanceId)
				continue
			}

			var (
				config       = aws.StringValue(instance.LaunchConfigurationName)
				activeConfig = aws.StringValue(scalingGroup.LaunchConfigurationName)
			)

			if !strings.EqualFold(config, activeConfig) {
				needsUpdate = append(needsUpdate, instanceId)
			}
		}

		if awsprovider.IsUsingLaunchTemplate(scalingGroup) {
			if instance.LaunchTemplate == nil {
				needsUpdate = append(needsUpdate, instanceId)
				continue
			}

			var (
				config           = aws.StringValue(instance.LaunchTemplate.LaunchTemplateName)
				version          = aws.StringValue(instance.LaunchTemplate.Version)
				launchTemplate   = scaling.ConvertToLaunchTemplate(scalingResource)
				activeConfig     = aws.StringValue(scalingGroup.LaunchTemplate.LaunchTemplateName)
				activeVersionNum = aws.Int64Value(launchTemplate.LatestVersionNumber)
				activeVersion    = common.Int64ToStr(activeVersionNum)
			)
			if !strings.EqualFold(config, activeConfig) || !strings.EqualFold(version, activeVersion) {
				needsUpdate = append(needsUpdate, instanceId)
			}
		}

		if awsprovider.IsUsingMixedInstances(scalingGroup) {
			if instance.LaunchTemplate == nil {
				needsUpdate = append(needsUpdate, instanceId)
				continue
			}

			var (
				config           = aws.StringValue(instance.LaunchTemplate.LaunchTemplateName)
				version          = aws.StringValue(instance.LaunchTemplate.Version)
				launchTemplate   = scaling.ConvertToLaunchTemplate(scalingResource)
				activeConfig     = aws.StringValue(scalingGroup.MixedInstancesPolicy.LaunchTemplate.LaunchTemplateSpecification.LaunchTemplateName)
				activeVersionNum = aws.Int64Value(launchTemplate.LatestVersionNumber)
				activeVersion    = common.Int64ToStr(activeVersionNum)
			)

			if !strings.EqualFold(config, activeConfig) || !strings.EqualFold(version, activeVersion) {
				needsUpdate = append(needsUpdate, instanceId)
			}
		}

	}
	allCount := len(allInstances)

	var unavailableInt int
	if maxUnavailable.Type == intstr.String {
		unavailableInt, _ = intstr.GetValueFromIntOrPercent(maxUnavailable, allCount, true)
	} else {
		unavailableInt = maxUnavailable.IntValue()
	}

	if unavailableInt == 0 {
		unavailableInt = 1
	}

	sort.Strings(needsUpdate)

	return &RollingUpdateRequest{
		Logger:           ctx.Log,
		KubernetesClient: ctx.KubernetesClient,
		AwsWorker:        ctx.AwsWorker,
		DrainGroup:       ctx.DrainGroup,
		ClusterNodes:     state.GetClusterNodes(),
		MaxUnavailable:   unavailableInt,
		DesiredCapacity:  desiredCount,
		AllInstances:     allInstances,
		UpdateTargets:    needsUpdate,
		ScalingGroup:     scalingGroup,
	}
}

func (r *RollingUpdateRequest) ProcessRollingUpgradeStrategy() (bool, error) {

	var (
		scalingGroupName = aws.StringValue(r.ScalingGroup.AutoScalingGroupName)
	)

	r.Info("starting rolling update", "scalinggroup", scalingGroupName, "targets", r.UpdateTargets, "maxunavailable", r.MaxUnavailable)

	if len(r.UpdateTargets) == 0 {
		r.Info("no updatable instances", "scalinggroup", scalingGroupName)
		return true, nil
	}

	// cannot rotate if maxUnavailable is greater than number of desired
	if r.MaxUnavailable > r.DesiredCapacity {
		r.Info("maxUnavailable exceeds desired capacity, setting maxUnavailable match desired",
			"scalinggroup", scalingGroupName, "maxunavailable", r.MaxUnavailable, "desiredcapacity", r.DesiredCapacity)
		r.MaxUnavailable = r.DesiredCapacity
	}

	ok := awsprovider.IsDesiredInService(r.ScalingGroup)

	if !ok {
		r.Info("desired instances are not in service", "scalinggroup", scalingGroupName)
		return false, nil
	}

	ok, err := kubeprovider.IsMinNodesReady(r.ClusterNodes, r.AllInstances, r.MaxUnavailable)
	if err != nil {
		return false, err
	}

	if !ok {
		r.Info("desired nodes are not ready", "scalinggroup", scalingGroupName)
		return false, nil
	}

	var terminateTargets []string
	if r.MaxUnavailable <= len(r.UpdateTargets) {
		terminateTargets = r.UpdateTargets[:r.MaxUnavailable]
	} else {
		terminateTargets = r.UpdateTargets
	}

	targetNodes := kubeprovider.GetNodesByInstance(terminateTargets, r.ClusterNodes)

	targetNames := make([]string, 0)
	for _, node := range targetNodes.Items {
		targetNames = append(targetNames, node.GetName())
	}

	r.Info("starting rotation on target nodes", "targets", targetNames)

	// Only create new threads if waitgroup is empty
	drainErrs := make(chan error)
	if !reflect.DeepEqual(r.DrainGroup, sync.WaitGroup{}) {
		for _, node := range targetNodes.Items {

			nodeName := node.GetName()
			r.Info("creating drainer goroutine for node", "node", nodeName)

			drainOpts := &drain.Helper{
				DeleteLocalData:     true,
				Force:               true,
				IgnoreAllDaemonSets: true,
				GracePeriodSeconds:  -1,
				Client:              r.KubernetesClient.Kubernetes,
				Out:                 os.Stdout,
				ErrOut:              os.Stderr,
			}

			r.DrainGroup.Add(1)
			n := node

			go func() {
				defer r.DrainGroup.Done()
				r.Info("cordoning node", "node", nodeName)
				err := drain.RunCordonOrUncordon(drainOpts, &n, true)
				if err != nil {
					errors.Wrap(err, "cordon node failed")
					drainErrs <- err
				}
				r.Info("draining node", "node", nodeName)
				err = drain.RunNodeDrain(drainOpts, nodeName)
				if err != nil {
					// If drain has failed, try to uncordon
					drain.RunCordonOrUncordon(drainOpts, &n, false)
					errors.Wrap(err, "drain node failed")
					drainErrs <- err
				}
			}()
		}
	}

	timeout := make(chan struct{})
	go func() {
		defer close(timeout)
		r.DrainGroup.Wait()
	}()

	select {
	case err := <-drainErrs:
		r.Info("failed to cordon/drain targets", "scalinggroup", scalingGroupName, "targets", terminateTargets)
		return false, err
	case <-timeout:
		// goroutines completed, terminate and requeue
		r.Info("targets drained successfully, terminating", "scalinggroup", scalingGroupName, "targets", terminateTargets)
		if err := r.AwsWorker.TerminateScalingInstances(terminateTargets); err != nil {
			// terminate failures are retryable
			r.Info("failed to terminate targets", "reason", err.Error(), "scalinggroup", scalingGroupName, "targets", terminateTargets)
		}
		return false, nil

	case <-time.After(DefaultWaitGroupTimeout):
		// goroutines timed out - requeue
		r.Info("targets still draining", "scalinggroup", scalingGroupName, "targets", terminateTargets)
		return false, nil
	}
}
