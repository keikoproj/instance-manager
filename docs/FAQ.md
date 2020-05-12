# Frequently Asked Questions

**What Kubernetes versions has instance-manager being tested against?**

> instance-manager has been tested with kubernetes `1.12.x` to `1.16.x` using EKS with the `eks` provisioner and the `eks-managed` provisioner for managed node groups.

**What would happen if the instance-manager pod dies during reconcile?**

> Like most Kubernetes controllers and CRDs, instance-manager is built to be resilient to controller failures and idempotent. In this scenario, once a new pod is Running it will continue reconciling in-flight instancegroups, and existing ones will not be affected, instance-manager is not in the data path and even if the controller is down, existing nodes will continue to run (however modifications will not be processed).

**Does instance-manager "watch" resources in all namespaces?**

> instancegroups are namespaced resources, a controller is usually run centrally in a cluster and will reconcile all the instancegroups for that cluster across multiple namespaces.

**What happens when AWS resources are modified out-of-band of instance-manager?**

> If you modify an instancegroup's autoscaling group / launch-configuration outside of instance-manager it will be changed back by the controller as soon as it notices the diff, this will most likely be when the custom resource is modified in some way, when the instance-manager controller pod is restarted, or when the default watch sync period expires.

**How do Kubernetes version ugprades work with instance-manager?**

> There are several upgrade strategies currently available. The most basic one is `rollingUpdate` which uses a basic form of node replacement. For example, if you patch your instancegroup and change it's `image` value, the controller will detect this drift, create a new launch configuration, and the instances will beging rotating according to the specified mechanism. Another supported strategy is `crd` which allows submitting an arbitrary custom resource for rotating the nodes - this allows implementing custom upgrade behavior if needed, or use other controllers such as `upgrade-manager`.
