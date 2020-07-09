# EKS Provisioner Features & API Reference

## API Reference

### EKSSpec

```yaml
spec:
  provisioner: eks
  eks:
    maxSize: <int64> : defines the auto scaling group's max instances (default 0)
    minSize: <int64> : defines the auto scaling group's min instances (default 0)
    configuration: <EKSConfiguration>
```

### EKSConfiguration

```yaml
spec:
  provisioner: eks
  eks:
    configuration:
      # Required minimal input
      clusterName: <string> : must match the name of the EKS cluster (required)
      keyPairName: <string> : must match the name of an EC2 Key Pair (required)
      image: <string> : must match the ID of an EKS AMI (required)
      instanceType: <string> : must match the type of an EC2 instance (required)
      securityGroups: <[]string> : must match existing security group IDs (required)
      subnets: <[]string> : must match existing subnet IDs (required)

      # customize EBS volumes
      volumes: <[]NodeVolume> : list of NodeVolume objects

      # suspend scaling processes, must be one of supported processes:
      # Launch
      # Terminate
      # AddToLoadBalancer
      # AlarmNotification
      # AZRebalance
      # HealthCheck
      # InstanceRefresh
      # ReplaceUnhealthy
      # ScheduledActions
      # All (will suspend all above processes)
      suspendProcesses: <[]string> : must match scaling process names to suspend

      bootstrapArguments: <string> : additional flags to pass to boostrap.sh script
      spotPrice: <string> : must be a decimal number represnting a minimal spot price

      # tags must be provided in the following format and will be applied to the scaling group with propogation
      # tags:
      # - key: tag-key
      #   value: tag-value
      tags: <[]map[string]string> : must be a list of maps with tag key-value

      # adds node lables via bootstrap arguments - make sure to not use restricted labels
      labels: <map[string]string> : must be a key-value map of labels

      # adds bootstrap taints via bootstrap arguments
      taints: <[]corev1.Taint> : must be a list of taint objects

      # provide a pre-created role in order to avoid granting the controller IAM access, if these fields are not provided an IAM role will be created by the controller.
      # only controller-created IAM roles will be deleted with the instance group.
      roleName: <string> : must match a name of an existing EKS node group role
      instanceProfileName: <string> : must match a name of the instance-profile of role referenced in roleName

      managedPolicies: <[]string> : must match list of existing managed policies to attach to the IAM role

      # enable metrics collection on the scaling group, must be one of supported metrics:
      # GroupMinSize
      # GroupMaxSize
      # GroupDesiredCapacity
      # GroupInServiceInstances
      # GroupPendingInstances
      # GroupStandbyInstances
      # GroupTerminatingInstances
      # GroupInServiceCapacity
      # GroupPendingCapacity
      # GroupTerminatingCapacity
      # GroupStandbyCapacity
      # GroupTotalInstances
      # GroupTotalCapacity
      # All (will enable all above metrics)
      metricsCollection: <[]string> : must be a list of metric names to enable collection for

      # customize UserData passed into launch configuration
      userData: <[]UserDataStage> : must be a list of UserDataStage
```

### UserDataStage

UserDataStage represents a custom userData script

```yaml
spec:
  provisioner: eks
  eks:
    configuration:
      userData:
      - stage: <string> : represents the stage of the script, allowed values are PreBootstrap, PostBootstrap
        data: <string> : represents the script payload to inject
```

### NodeVolume

NodeVolume represents a custom EBS volume

```yaml
spec:
  provisioner: eks
  eks:
    configuration:
      volumes:
      - name: <string> : represents the device name, e.g. /dev/xvda
        type: <string> : represents the type of volume, must be one of supported types "standard", "io1", "gp2", "st1", "sc1".
        size: <int64> : represents a volume size in gigabytes
```

### Taint

Uses Kubernetes CoreV1 standard taint, see more here: https://godoc.org/k8s.io/api/core/v1#Taint

```yaml
spec:
  provisioner: eks
  eks:
    configuration:
      taints:
      - key: <string> : the key of the taint
        value: <string> : the value of the taint
        effect: <string> : the effect of the taint
```

## Upgrade Strategies

An 'upgrade' is needed when a change is made to an instance-group which requires node rotation in order to take effect, for example the AMI has changed.
instance-manager currently supports two types of upgrade strategy, `rollingUpdate` and `crd`.

### Rolling Update Strategy

rollingUpdate is a basic implementation of a rolling instance replacement - you can define `maxUnavailable` to some number or percent that represents the desired capacity to be rotated at a time.

```yaml
apiVersion: instancemgr.keikoproj.io/v1alpha1
kind: InstanceGroup
metadata:
  name: hello-world
  namespace: instance-manager
spec:
  strategy:
    type: rollingUpdate
    rollingUpdate:
      maxUnavailable: 30%
```

### CRD Strategy

The second strategy is `crd` which allows for adding custom behavior via submission of custom resources.

There might be cases where you would want to implement custom behavior as part of your upgrade, this can be achieved by the `crd` strategy - it allows you to create any custom resource and watch it for a success condition.

In the below example, we submit an [argo](https://github.com/argoproj/argo) workflow as a response to an upgrade events, but you can submit any kind of resource you wish, such as [upgrade-manager](https://github.com/keikoproj/upgrade-manager)'s RollingUpgrade.

```yaml
apiVersion: instancemgr.keikoproj.io/v1alpha1
kind: InstanceGroup
metadata:
  name: hello-world
  namespace: instance-manager
spec:
  strategy:
    type: crd
    crd:
      crdName: workflows
      statusJSONPath: .status.phase
      statusSuccessString: Succeeded
      statusFailureString: Failed
      concurrencyPolicy: Forbid
      spec: |
        apiVersion: argoproj.io/v1alpha1
        kind: Workflow
        metadata:
          generateName: hello-world-parameters-
        spec:
          entrypoint: whalesay
          arguments:
            parameters:
            - name: message
              value: hello world
          templates:
          - name: whalesay
            inputs:
              parameters:
              - name: message
            container:
              image: docker/whalesay
              command: [cowsay]
              args: ["echo", "{{ .InstanceGroup.Status.ActiveScalingGroupName }}"]
```

## Spot instances

You can switch to spot instances in two ways:

- Manually set the `spec.eks.configuration.spotPrice` to a spot price value, if the price is available, the instances will rotate, if the price is no longer available, it's up to you to change it to a different value.

- Use a spot recommendation controller such as [minion-manager](https://github.com/keikoproj/minion-manager), instance-manager will look at events published with the following message format:

```json
{"apiVersion":"v1alpha1","spotPrice":"0.0067", "useSpot": true}
```

In addition, the event `involvedObject.name`, must be the name of the autoscaling group to switch, and the event `.reason` must be `SpotRecommendationGiven`.

When recommendations are not available (no events for an hour / recommendation controller is down), instance-group will retain the last provided configuration, until a human either changes back to on-demand (by setting `spotPrice: ""`) or until recommendation events are found again.

## Customize Scaling Group

You can customize specific attributes of the scaling group

### EBS Volumes

You can customize EBS volumes as follows

```yaml
apiVersion: instancemgr.keikoproj.io/v1alpha1
kind: InstanceGroup
metadata:
  name: hello-world
  namespace: instance-manager
spec:
  provisioner: eks
  eks:
    configuration:
      volumes:
      - name: /dev/xvda
        type: gp2
        size: 50
      - name: /dev/xvdb
        type: gp2
        size: 100
```

You can customize scaling group's collected metrics as follows

```yaml
apiVersion: instancemgr.keikoproj.io/v1alpha1
kind: InstanceGroup
metadata:
  name: hello-world
  namespace: instance-manager
spec:
  provisioner: eks
  eks:
    configuration:
      metricsCollection:
      - GroupMinSize
      - GroupMaxSize
      # you can also reference "All" to collect all metrics
```

You can customize scaling group's suspended processes as follows

```yaml
apiVersion: instancemgr.keikoproj.io/v1alpha1
kind: InstanceGroup
metadata:
  name: hello-world
  namespace: instance-manager
spec:
  provisioner: eks
  eks:
    configuration:
      suspendProcesses:
      - AZRebalance
      # you can also reference "All" to suspend all processes
```

## GitOps/Platform support, boundaries and default values

In order to support use-cases around GitOps or platform management, the controller allows operators to define 'boundaries' of configurations into `restricted` and `shared` configurations, along with the default values to enforce.

For example, if you'd like your cluster tenants to only be able to control certain fields such as `minSize`, `maxSize`, `instanceType`, `labels` and `tags` while the rest is controlled via the operator you could achieve this by creating a config-map the defines these boundaries.

By default the controller watches configmaps with the name `instance-manager` in the namespaace `instance-manager`, namespace can be customized via a controller flag `--config-namespace`.

- Configurations without a boundary means it must be provided in the custom resource.
- Configurations with a shared boundary means the controller will try to merge a default value with the custom resource provided value.
- Configurations with a restricted boundary means the controller will give first priority to the default value, and will fall back on a custom resource provided value if the default is missing.

For enforcing the above described conditions, the configmap should look like this:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: instance-manager
  namespace: instance-manager
data:
  boundaries: |
    shared:
    - spec.eks.configuration.tags
    - spec.eks.configuration.labels
    restricted:
    - spec.eks.configuration.clusterName
    - spec.eks.configuration.image
    - spec.eks.configuration.roleName
    - spec.eks.configuration.instanceProfileName
    - spec.eks.configuration.keyPairName
    - spec.eks.configuration.securityGroups
    - spec.eks.configuration.subnets
    - spec.eks.configuration.taints
  defaults: |
    spec:
      provisioner: eks
      eks:
        configuration:
          clusterName: a-managed-cluster
          subnets:
          - subnet-1234
          - subnet-2345
          - subnet-3456
          keyPairName: a-managed-keypair
          image: ami-1234
          labels:
            a-default-label: "true"
          taints:
          - key: default-taint
            value: a-taint
            effect: NoSchedule
          roleName: a-managed-role
          instanceProfileName: a-managed-profile
          tags:
          - key: a-default-tag
            value: a-default-value
          securityGroups:
          - sg-1234
```

The user (or GitOps) can now submit a shortened custom resource that looks like this:

```yaml
apiVersion: instancemgr.keikoproj.io/v1alpha1
kind: InstanceGroup
metadata:
  name: bdd-test-gitops
  namespace: instance-manager
spec:
  eks:
    minSize: 2
    maxSize: 4
    configuration:
      labels:
        custom-resource-label: "true"
      instanceType: t2.small
      tags:
      - key: a-customresource-key
        value: a-customresource-value
```

The resulting scaling group will be a result of merging the shared values, and prefering the restricted values.

Any update to the configmap will trigger a reconcile for instancegroups which are aligned with a non-matching configuration.

This is enforced via the `status.configMD5` field, which has an MD5 hash of the last seen configmap data, this guarantees consistency with the values defined in the configmap.

This also makes upgrades easier across a managed cluster, an operator can now simply modify the default value for `image` and trigger an upgrade across all instance groups.
