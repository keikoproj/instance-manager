# EKS Provisioner Features & API Reference

## API Reference

### EKSSpec

```yaml
spec:
  provisioner: eks
  eks:
    maxSize: <int64> : defines the auto scaling group's max instances (default 0)
    minSize: <int64> : defines the auto scaling group's min instances (default 0)
    configuration: <EKSConfiguration> : the scaling group configuration
    type: <ScalingConfigurationType> : defines the type of scaling group, either LaunchTemplate or LaunchConfiguration (default)
    warmPool: <WarmPoolSpec> : defines the spec of the auto scaling group's warm pool
```
### WarmPoolSpec
```yaml
spec:
  provisioner: eks
  eks:
    warmPool:
      maxSize: <int64> : defines the maximum size of the warm pool, use -1 to match to autoscaling group's max (default 0)
      minSize: <int64> : defines the minimum size of the warm pool (default 0)
```

### EKSConfiguration

```yaml
spec:
  provisioner: eks
  eks:
    configuration:
      # required minimal input
      clusterName: <string> : must match the name of the EKS cluster (required)
      keyPairName: <string> : must match the name of an EC2 Key Pair (required)
      image: <string> : must match the ID of an EKS AMI (required)
      instanceType: <string> : must match the type of an EC2 instance (required)
      securityGroups: <[]string> : must match existing security group IDs or Name (by value of tag "Name") (required)
      subnets: <[]string> : must match existing subnet IDs or Name (by value of tag "Name") (required)

      # Launch Template options
      mixedInstancesPolicy: <MixedInstancesPolicySpec> : defines the mixed instances policy, this can only be used when spec.eks.type is LaunchTemplate

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

      bootstrapOptions:
        containerRuntime: <string> : one of "dockerd" or "containerd". Specifies which container runtime to use. Currently only available on Amazon Linux 2
        maxPods: <int> : maximum number of pods that can be run per-node in this IG.
                 

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

      # add LifecycleHooks to be created as part of the scaling group
      lifecycleHooks: <[]LifecycleHookSpec> : must be a list of LifecycleHookSpec

      # add Placement information
      licenseSpecifications: <[]string> : must be a list of strings containing ARNs to Dedicated host license specifications
      placement: <PlacementSpec> : placement information for EC2 instances.
```

### LifecycleHookSpec

LifecycleHookSpec represents an autoscaling group lifecycle hook

```yaml
spec:
  provisioner: eks
  eks:
    configuration:
      lifecycleHooks:
      - name: <string> : name of the hook (required)
        lifecycle: <string> : represents the transition to create a hook for, can either be "launch" or "terminate" (required)
        defaultResult: <string> : represents the default result when timeout expires, can either be "abandon" or "continue" (defaults to "abandon")
        heartbeatTimeout: <int64> : represents the required interval for sending a heartbeat in seconds (defaults to 300)
        notificationArn: <string> : if non-empty, must be a valid IAM ARN belonging to an SNS or SQS queue (optional)
        roleArn: <string> : if non-empty, must be a valid IAM Role ARN providing access to publish messages (optional)
        metadata: <string> : additional metadata to add to notification payload
```

### MixedInstancesPolicySpec

MixedInstancesPolicySpec represents launch template options for mixed instances

```yaml
spec:
  provisioner: eks
  eks:
    type: LaunchTemplate
    configuration:
      mixedInstancesPolicy:
        strategy: <string> : represents the strategy for choosing an instance pool, must be either CapacityOptimized or LowestPrice (default CapacityOptimized)
        spotPools: <int64> : represents number of spot pools to choose from - can only be used when strategy is LowestPrice (default 2)
        baseCapacity: <int64> : the base on-demand capacity that must always be present (default 0)
        spotRatio: <IntOrStr> : the percent value defining the ratio of spot instances on top of baseCapacity (default 0)
        instancePool: <string> : defines pools that can be used to automatically derive the instance types to use, SubFamilyFlexible supported only, required if instanceTypes not provided.
        instanceTypes: <[]InstanceTypeSpec> : represents specific instance types to use, required if instancePool not provided.
```

### InstanceTypeSpec

InstanceTypeSpec represents the additional instances for MixedInstancesPolicy and their weight

```yaml
spec:
  provisioner: eks
  eks:
    type: LaunchTemplate
    configuration:
      mixedInstancesPolicy:
        instanceTypes:
        - type: <string> : an AWS instance type (required)
          weight: <int64> : a weight representing the scaling index for the instance type (default 1)
```

### UserDataStage

UserDataStage represents a custom userData script

```yaml
spec:
  provisioner: eks
  eks:
    configuration:
      userData:
      - name: <string> : name of the stage
        stage: <string> : represents the stage of the script, allowed values are PreBootstrap, PostBootstrap (required)
        data: <string> : represents the script payload to inject in plain text or base64 (required)
```

### NodeVolume

NodeVolume represents a custom EBS volume

```yaml
spec:
  provisioner: eks
  eks:
    configuration:
      volumes:
      - name: <string> : represents the device name, e.g. /dev/xvda (required)
        type: <string> : represents the type of volume, must be one of supported types "standard", "io1", "gp2", "st1", "sc1" (required)
        size: <int64> : represents a volume size in gigabytes, cannot be used with snapshotId
        snapshotId : <string> : represents a snapshot ID to use, cannot be used with size
        iops: <int64> : represents number of IOPS to provision volume with (min 100)
        deleteOnTermination : <bool> : delete the EBS volume when the instance is terminated (defaults to true)
        encrypted: <bool> : encrypt the EBS volume with a KMS key
        mountOptions: <MountOptions> : auto-mount options for additional volumes
```

### MountOptions

MountOptions helps with formatting & mounting an EBS volume by injecting commands to userdata.
This should not be used for the root volume that is defined by the AMI.

```yaml
spec:
  provisioner: eks
  eks:
    configuration:
      volumes:
      - mountOptions:
          fileSystem: <string> : the type of file system to format volume to, must be one of the supported types "xfs" or "ext4"
          mount: <string> : the mount path to mount the volume to
          persistance: <bool> : make mount persist after reboot by adding it to /etc/fstab (default true)
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

### PlacementSpec

Represents the EC2 Placement information for your EC2 instances.
All fields are supported for Launch Templates; Launch Configuration's only support setting `tenancy`.

```yaml
spec:
  provisioner: eks
  eks:
    type: LaunchTemplate
    configuration:
      placement:
        availabiltyZone: "us-west-2a"
        hostResourceGroupArn: "arn:aws:resource-groups:us-west-2:1234456789:group/host-group-name"
        tenancy: "host"
```

```yaml
spec:
  provisioner: eks
  eks:
    type: LaunchConfiguration # can be omitted as the default
    configuration:
      placement:
        tenancy: "host"
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

#### Recovering from Upgrade Failures

You can configure whether you want the controller to retry by setting the `spec.strategy.crd.maxRetries` field - if left unset it will default to 3, max retries can also be set to -1 which would retry until success.
When the submitted resource fails, the controller will delete/recreate the resource up to configured amount of times, once the max retries are met, the instance-group will enter an error state and requeue with exponential backoff.
In order to manually retry, you must delete the failed custom resource and either wait for the next reconcile, or trigger a reconcile by making a modifications to the instance group or restarting the controller.

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

## Warm Pools for Auto Scaling

You can configure your scaling group to use [AWS Warm Pools for Auto Scaling](https://docs.aws.amazon.com/autoscaling/ec2/userguide/ec2-auto-scaling-warm-pools.html), which allows you to keep a capacity separate pool of stopped instances have already run any pre-bootstrap userdata - using warm pools can reduce the time it takes for nodes to join the cluster.

Warm Pools is not officially supported with EKS, hence the following requirements exist in order to use it with EKS:
- For Amazon Linux 2 instances, your AMI must have `awscli` & `jq`, you can also install it in pre-bootstrap userdata.
- For Windows instances, your AMI must have the `Get-ASAutoScalingInstance` cmdlet installed.
- If you use your own IAM role for the instance group, you must make sure it has access to `DescribeAutoScalingInstances`, this is required in order to figure out the current lifecycle state within userdata. If you are provisioning your IAM role through the controller, simply be aware that the controller will add the managed policy `AutoScalingReadOnlyAccess` to the role it creates.
- This is currently only supported for AmazonLinux2 and Windows based AMIs.

We hope to get rid of these requirements in the future once AWS offers native support for Warm Pools in EKS, these are currently required in order to avoid warming instances to join the cluster briefly while they are provisioned, we are able to avoid this problem by checking if the instances is in the Warmed* lifecycle, and skipping bootstrapping in that case. Skipping on these requirements and using warm pools might mean having nodes join the cluster when the are being warmed, and cause unneeded scheduling of pods on nodes that are about to shutdown.

You can enable warm pools with the following spec:
```yaml
spec:
  provisioner: eks
  eks:
    maxSize: 6
    minSize: 3
      warmPool:
        maxSize: -1
        minSize: 0    
```

Using `-1` means "Equal to the Auto Scaling group's maximum capacity", so effectively it will change according to scaling group's `maxSize`.

## GitOps/Platform support, boundaries, default and conditional values

In order to support use-cases around GitOps or platform management, the controller allows operators to define 'boundaries' of configurations into `restricted` and `shared` configurations, along with the default values to enforce.

`shared` fields can be referenced with a `merge`, `mergeOverride` or `replace` directive, which provide controls over how default values should be combined with the resource values. `merge` will provide a simple merge, if there is a conflict between the default value and the resource value, the resource value will not be allowed to override the default, for example consider the following default / resource values.

```yaml
# default values
tags:
- key: tag-A
  value: value-A
- key: tag-B
  value: value-B

# resource values
tags:
- key: tag-A
  value: value-D
- key: tag-C
  value: value-C
```

If `spec.eks.configuration.tags` was referenced as a `merge` field the result would be:

```yaml
tags:
- key: tag-A
  value: value-A
- key: tag-B
  value: value-B
- key: tag-C
  value: value-C
```

`mergeOverride` allows the CR to override the default will have a different result:

```yaml
tags:
- key: tag-A
  value: value-D
- key: tag-B
  value: value-B
- key: tag-C
  value: value-C
```

`replace` would allow the CR to completely replace the default values:

```yaml
tags:
- key: tag-A
  value: value-D
- key: tag-C
  value: value-C
```

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
      merge:
      - spec.eks.configuration.labels
      mergeOverride:
      - spec.eks.configuration.tags
      replace:
      - spec.eks.strategy
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
  name: my-instance-group
  namespace: instance-manager
spec:
  eks:
    minSize: 2
    maxSize: 4
    configuration:
      labels:
        test-label: "true"
      instanceType: t2.small
      tags:
      - key: tag-key
        value: tag-value
```

The resulting scaling group will be a result of merging the shared values, and prefering the restricted values.

Any update to the configmap will trigger a reconcile for instancegroups which are aligned with a non-matching configuration.

This is enforced via the `status.configMD5` field, which has an MD5 hash of the last seen configmap data, this guarantees consistency with the values defined in the configmap.

This also makes upgrades easier across a managed cluster, an operator can now simply modify the default value for `image` and trigger an upgrade across all instance groups.

Individual namespaces can opt-out by adding the annotation `instancemgr.keikoproj.io/config-excluded=true`, this is useful for system namespaces which may need to override a global restrictive configuration, e.g. subnet, while keeping the boundary as is for other namespaces - adding this annotation to a namespace will opt-out all instancegroups under the namespace from using the cluster configuration.


### Conditional defaults
For more complex setups, such as clusters that have InstanceGroups that have different architectures, operating systems, etc - it might be 
desirable to conditionally apply default values. Conditional default values can be added, as seen in the example below:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: instance-manager
  namespace: instance-manager
data:
  boundaries: |
    shared:
      merge:
      - spec.eks.configuration.labels
      mergeOverride:
      - spec.eks.configuration.tags
      replace:
      - spec.eks.strategy
    restricted:
    - spec.eks.configuration.image
  defaults: |
    spec:
      provisioner: eks
      eks:
        configuration:
          image: ami-x86linux
  conditionals: |
    - annotationSelector: 'instancemgr.keikoproj.io/os-family = windows'
      defaults:
       spec:
         eks:
           configuration:
             image: ami-windows
    - annotationSelector: 'instancemgr.keikoproj.io/arch = arm64,instancemgr.keikoproj.io/os-family = bottlerocket'
      defaults:
       spec:
         eks:
           configuration:
             image: ami-bottlerocketArm
```

The `annotationSelector` field accepts a selector string, which follows the semantics of the Kubernetes [field selector](https://kubernetes.io/docs/concepts/overview/working-with-objects/field-selectors/). 
This selector is applied against the annotations of InstanceGroup objects. When a selector matches an InstanceGroup, the `defaults` associated with it are applied to the InstangeGroup, taking precedence over the non-conditional 
default values. Conditional rules are applied in-order from the beginning of the list to the end - meaning that the last applicable rule will apply in case of conflicts. 

The following operators are supported:
```
<selector-syntax>         ::= <requirement> | <requirement> "," <selector-syntax>
<requirement>             ::= [!] KEY [ <set-based-restriction> | <exact-match-restriction> ]
<set-based-restriction>   ::= "" | <inclusion-exclusion> <value-set>
<inclusion-exclusion>     ::= <inclusion> | <exclusion>
<exclusion>               ::= "notin"
<inclusion>               ::= "in"
<value-set>               ::= "(" <values> ")"
<values>                  ::= VALUE | VALUE "," <values>
<exact-match-restriction> ::= ["="|"=="|"!="] VALUE
```

## Annotations

| Annotation Key | Object | Annotation Value | Purpose |
|:--------------:|:------:|:----------------:|:-------:|
|instancemgr.keikoproj.io/config-excluded|Namespace|"true"|settings this annotation on a namespace will allow opt-out from a configuration configmap, all instancegroups under such namespace will not use configmap boundaries and default values|
|instancemgr.keikoproj.io/cluster-autoscaler-enabled|InstanceGroup|"true"|setting this annotation to true will add the relevant cluster-autoscaler EC2 tags according to cluster name, taints, and labels|
|instancemgr.keikoproj.io/irsa-enabled|InstanceGroup|"true"|setting this annotation to true will remove the AmazonEKS_CNI_Policy from the default managed policies attached to the node role, this should only be used when nodes are using IAM Roles for Service Accounts (IRSA) and the aws-node daemonset is using an IRSA role which contains this policy|
|instancemgr.keikoproj.io/os-family|InstanceGroup|either "windows", "bottlerocket", or "amazonlinux2" (default)|this is required if you are running a windows or bottlerocket based AMI, by default the controller will try to bootstrap an amazonlinux2 AMI|
|instancemgr.keikoproj.io/default-labels|InstanceGroup|comma-seprarated key-value string e.g. "label1=value1,label2=value2"|allows overriding the default node labels added by the controller, by default the role label is added depending on the cluster version|
|instancemgr.keikoproj.io/custom-networking-enabled|InstanceGroup|"true"|setting this annotation to true will automatically calculate the correct setting for max pods and pass it to the kubelet|
|instancemgr.keikoproj.io/custom-networking-prefix-assignment-enabled|InstanceGroup|"true"|setting this annotation to true will change the max pod calculations to reflect the pod density supported by vpc prefix assignment. Supported in AWS VPC CNI versions 1.9.0 and above - see [AWS VPC CNI 1.9.0](https://github.com/aws/amazon-vpc-cni-k8s/releases/tag/v1.9.0) for more information.|
|instancemgr.keikoproj.io/custom-networking-host-pods|InstanceGroup|"2"|setting this annotation increases the number of max pods on nodes with custom networking, due to the fact that hostNetwork pods do not use an additional IP address |
