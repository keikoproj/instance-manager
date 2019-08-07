# Frequently Asked Questions

**What Kubernetes versions has instance-manager being tested against?**

> instance-manager has been tested with kubernetes `1.11.x` to `1.13.x` mainly over EKS using Cloudformation. since `eks-cf` is currently the only supported provisioner we are currently only testing those scenarios.

**Are all regions supported by instance-manager?**

> This is a provisioner-specific question, currently `eks-cf` supports all the regions which EKS supports, which currently are `us-west-2`, `us-east-1`, and `us-east-2`.

**What would happen if the instance-manager pod dies while cloudformation is mid-reconcile?**

> Like most Kubernetes controllers and CRDs, instance-manager is built to be resilient to controller failures and idempotent. In this scenario, once a new pod is Running it will continue reconciling in-flight InstanceGroups, and existing ones will not be affected.

**How do I set the default values for resource parameters?**

> Some of the custom resource configuration parameters can be defined in the `controller.conf` section of the instance-manager config map, supported keys are `defaultClusterName` which replaces the need to specify `clusterName` in the resource, and `defaultSubnets` which replace the need to specify `subnets`.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: instance-manager
  namespace: instance-manager
data:
  controller.conf: |
    defaultArns:
    - arn:aws:iam::1234567890:role/my-eks-cluster-system-node-group
    defaultClusterName: my-eks-cluster
    defaultSubnets:
    - subnet-12345678
    - subnet-23456789
    - subnet-34567890
  cloudformation.template: |
    ...
    <- cloudformation template content ->
    ...
```

**Does instance-manager "watch" resources in all namespaces?**

> InstanceGroups are namespaced resources, a controller needs to be running in the namespace where InstanceGroups are created in order for them to reconcile.

**What happens when AWS resources are modified out-of-band of instance-manager?**

> If you modify an InstanceGroup stack outside of instance-manager and the resource that owns the stack, it will be changed back via Cloudformation as soon as the controller notices this change, this will most likely be when the custom resource is modified in some way, or when the instance-manager controller pod is restarted.

**How do Kubernetes version ugprades work with instance-manager?**

> There are several upgrade strategies currently available. The most basic one is `rollingUpdate` which uses the native cloudformation mechanism to rotate instances. For example, if you patch your InstanceGroup and change it's `image` value, the Cloudformation template will be updated and the instances will rotate one by one by the same mechanism. Another supported strategy is `crd` which allows submitting an arbitrary custom resource when ever an InstanceGroup's launch configuration has been updated - this allows implementing custom upgrade behavior if needed.
