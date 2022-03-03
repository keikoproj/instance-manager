# EKS Usage examples

- [EKS Usage examples](#eks-usage-examples)
  - [EKS sample spec](#eks-sample-spec)
  - [Features](#features)
    - [Spot instance support](#spot-instance-support)
    - [Upgrade Strategies](#upgrade-strategies)
    - [EC2 Instance placement](#ec2-instance-placement)
    - [Bring your own role](#bring-your-own-role)
    - [Auto-Updating AWS EKS Optimised AMI ID](#auto-updating-aws-eks-optimised-ami-id)

## EKS sample spec

```yaml
apiVersion: instancemgr.keikoproj.io/v1alpha1
kind: InstanceGroup
metadata:
  name: hello-world
  namespace: instance-manager
spec:
  provisioner: eks
  strategy:
    type: rollingUpdate
    rollingUpdate:
      maxUnavailable: 1
  eks:
    minSize: 3
    maxSize: 6
    configuration:
      clusterName: my-eks-cluster
      bootstrapArguments: <.. eks bootstrap arguments ..>
      taints:
      - key: node-role.kubernetes.io/some-taint
        value: some-value
        effect: NoSchedule
      labels:
        example.label.com/label: some-value
      keyPairName: my-ec2-key-pair
      image: ami-076c743acc3ec4159
      instanceType: m5.large
      volumes:
      - name: /dev/xvda
        type: gp2
        size: 32
      subnets:
      - subnet-0bf9bc85fd80af561
      - subnet-0130025d2673de5e4
      - subnet-01a5c28e074c46580
      securityGroups:
      - sg-04adb6343b07c7914
      tags:
      - key: my-ec2-tag
        value: some-value
```

## Features

### Spot instance support

You can manually specify a spot price directly in the spec of an instance group

```yaml
apiVersion: instancemgr.keikoproj.io/v1alpha1
kind: InstanceGroup
metadata:
  name: hello-world
  namespace: instance-manager
spec:
  strategy: <...>
  provisioner: eks
  eks:
    configuration:
      spotPrice: "0.67"
```

instance-manager will switch the instances to spot for you if that price is available.

You can also use [minion-manager](https://github.com/keikoproj/minion-manager/issues/50) in `--events-only` mode to provide spot recommendations for instance-manager.

### Upgrade Strategies

instance-manager supports multiple upgrade strategies, a basic one called `rollingUpdate` which rotates instances according to `maxUnavailable`.

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
  provisioner: eks
  eks:
    configuration: <...>
```

In this case, 30% of the capacity will terminate at a time, once all desired nodes are in ready state, the next batch will terminate.

You can also use custom controllers with instance manager using the `CRD strategy`.

```yaml
apiVersion: instancemgr.keikoproj.io/v1alpha1
kind: InstanceGroup
metadata:
  name: hello-world
  namespace: instance-manager
spec:
  strategy:
    provisioner: eks
    crd:
      crdName: rollingupgrades
      statusJSONPath: .status.currentStatus
      statusSuccessString: completed
      statusFailureString: error
      spec: |
        apiVersion: upgrademgr.keikoproj.io/v1alpha1
        kind: RollingUpgrade
        metadata:
          name: rollup-nodes
          namespace: instance-manager
        spec:
          postDrainDelaySeconds: 30
          nodeIntervalSeconds: 30
          asgName: {{ .InstanceGroup.Status.ActiveScalingGroupName }}
          strategy:
            mode: eager
            type: randomUpdate
            maxUnavailable: 30%
            drainTimeout: 120
```

In this strategy you can create resources as a response for a pending upgrade, you can use [upgrade-manager](https://github.com/keikoproj/upgrade-manager) and `RollingUpgrade` custom resources in order to rotate your instance-groups with more fine grained strategies. You can template any required parameter right form the instance group spec, in this case we are taking the scaling group name from the instance group's `.status.activeScalingGroupName` and putting it in the `.spec.asgName` of the `RollingUpgrade` custom resource. We are also telling instance-manager how to know if this custom-resource is successful or not by looking at the resource's jsonpath for specific strings.

_For more examples and usage, please refer to the [Installation Reference Walkthrough][install]._

### EC2 Instance placement

When using Launch Templates, you can provide EC2 placement information to take advantage on EC2 dedicated hosts.

```yaml
apiVersion: instancemgr.keikoproj.io/v1alpha1
kind: InstanceGroup
metadata:
  name: hello-world
  namespace: instance-manager
spec:
  strategy: <...>
  provisioner: eks
  eks:
    type: LaunchTemplate
    configuration:
      placement:
        availabiltyZone: "us-west-2a"
        hostResourceGroupArn: "arn:aws:resource-groups:us-west-2:1234456789:group/host-group-name"
        tenancy: "host"
```

### Bring your own role

You can choose to provide an IAM role that is managed externally to instance-manager by providing the name of the instance profile and the role name.

```yaml
apiVersion: instancemgr.keikoproj.io/v1alpha1
kind: InstanceGroup
metadata:
  name: hello-world
  namespace: instance-manager
spec:
  strategy: <...>
  provisioner: eks
  eks:
    configuration:
      roleName: my-eks-role-name
      instanceProfileName: my-eks-instance-profile-name
```

if you do not provide these fields, a role will be created for your instance-group by the controller (will require IAM access).


### Auto-Updating AWS EKS Optimised AMI ID

Amazon provides a EKS optimised AMIs for AmazonLinux2, Bottlerocket and Windows - https://docs.aws.amazon.com/eks/latest/userguide/eks-optimized-ami.html
Instance-manager can periodically query the latest AMI ID from AWS SSM parameter store, and automatically update the image used in your instance groups, ensuring your nodes are always patched and kept up-to-date. This feature pairs well with customised upgrade strategies to ensure nodes are safely upgraded when and how you expect.

note: The controller reconciles `InstanceGroups` every 10 hours by default, so changes may take up to 11 hours (sync period + SSM GetParameter cache TTL) to occur.
Using the locking annotation `instancemgr.keikoproj.io/lock-upgrades` will allow more control of upgrades. Locking/Unlocking `InstanceGroups` will trigger a reconcile.

```yaml
apiVersion: instancemgr.keikoproj.io/v1alpha1
kind: InstanceGroup
metadata:
  name: hello-world
  namespace: instance-manager
  annotations:
    instancemgr.keikoproj.io/os-family: "amazonlinux2" #Default if not provided
spec:
  strategy: <...>
  provisioner: eks
  eks:
    configuration:
      image: latest
```


## SSM-based versioned AMI

It might not be acceptable to pin your `InstanceGroup` to the latest AMI. If you want to pin to a specific version,
you can specify the relevant version information using syntax like `ssm://version`. An example for the EKS-optimized Linux AMI might look like:

```yaml
apiVersion: instancemgr.keikoproj.io/v1alpha1
kind: InstanceGroup
metadata:
  name: hello-world
  namespace: instance-manager
  annotations:
    instancemgr.keikoproj.io/os-family: "amazonlinux2" #Default if not provided
spec:
  strategy: <...>
  provisioner: eks
  eks:
    configuration:
      image: ssm://amazon-eks-node-1.18-v20220226 # resolves to value in /aws/service/eks/optimized-ami/1.18/amazon-linux-2/amazon-eks-node-1.18-v20220226/image_id
```

Using this type of image reference over a raw ami ID has a few benefits:
- The same image reference can be used across regions, since it's not a direct reference to the AMI.
- The image is more readable - for users leveraging GitOps, the changes will be much easier to review.