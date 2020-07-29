# Installation Reference Walkthrough

This guide will walk you through all the steps needed from the creation of the EKS Cluster to the creation of cluster-native worker nodes using instance-manager.

The cluster setup part of the walkthrough is meant to be an example and is not recommended to use in production to set up EKS clusters.

## Prerequisites

There are several prerquisites for installing instance-manager, this document will only refer to installation on EKS.

You will need a functional EKS cluster, with at least one bootstrapped node (that can run the instance-manager controller pod).

If you already have a running cluster you can **skip to the [Deploy instance-manager](#deploy-instance-manager) section** below.

Alternatively, you can also refer to the AWS provided [documentation](https://docs.aws.amazon.com/eks/latest/userguide/create-cluster.html).

### Create the control plane prerequisites

In order to create an EKS cluster, you first need a VPC, a security group and a service role, you can either use eksctl, or AWS console to do this if you follow the [docs](https://docs.aws.amazon.com/eks/latest/userguide/getting-started.html).

Once you've created the prereqs, you can create the EKS cluster using the AWS CLI.

```bash
export IAMROLE=arn:aws:iam::111122223333:role/eks-service-role
export SUBNETS=subnet-a9189fe2,subnet-50432629
export SECURITYGROUPS=sg-f5c54184

aws eks --region region-code create-cluster \
   --name devel --kubernetes-version 1.16 \
   --role-arn $IAMROLE \
   --resources-vpc-config
   subnetIds=$SUBNETS,securityGroupIds=$SECURITYGROUPS
```

### Control Plane

At this point, you should have a cluster without any nodes.

```bash
$ kubectl get nodes
No resources found.
```

### Bootstrap node group

In order to run the controller you need atleast one node that is created manually, in addition, the instance-manager controller requires IAM permissions in order to create the required resources in your account.

The below are the minimum required policies needed to run the basic operations of provisioners.

```text
iam:GetRole
iam:GetInstanceProfile
iam:PassRole
autoscaling:DescribeAutoScalingGroups
autoscaling:UpdateAutoScalingGroup
autoscaling:TerminateInstanceInAutoScalingGroup
autoscaling:CreateLaunchConfiguration
autoscaling:DeleteLaunchConfiguration
autoscaling:DeleteAutoScalingGroup
autoscaling:CreateAutoScalingGroup
eks:CreateNodegroup
eks:DescribeNodegroup
eks:DeleteNodegroup
eks:UpdateNodegroupConfig
eks:DescribeCluster
```

The following are also required if you want the controller to be creating IAM roles for your instance groups, otherwise you can omit this and provide an existing role in the custom resource.

```text
iam:CreateInstanceProfile
iam:RemoveRoleFromInstanceProfile
iam:CreateRole
iam:AttachRolePolicy
iam:AddRoleToInstanceProfile
iam:DetachRolePolicy
iam:ListAttachedRolePolicies
iam:DeleteInstanceProfile
iam:DeleteRole
autoscaling:DescribeLaunchConfigurations
```

You can choose to create the initial instance-manager IAM role with these additional policies attached directly, or create a new role and use other solutions such as KIAM to assume it. You can refer to the documentation provided by KIAM [here](https://github.com/uswitch/kiam#overview).

To create a basic node group manually, refer to the documentation provided by AWS on [launching worker nodes](https://docs.aws.amazon.com/eks/latest/userguide/launch-workers.html) or use the below example.

```bash
$ kubectl get nodes
NAME                                           STATUS   ROLES    AGE     VERSION
ip-10-105-232-22.us-west-2.compute.internal    Ready    <none>   3m22s   v1.15.11-eks-af3caf
ip-10-105-233-251.us-west-2.compute.internal   Ready    <none>   3m49s   v1.15.11-eks-af3caf
ip-10-105-233-110.us-west-2.compute.internal   Ready    <none>   3m40s   v1.15.11-eks-af3caf
```

#### Hybrid node groups

In this scenario, node groups which were manually bootstrapped (as above), and instance-manager managed instance groups can co-exist, while the controller modifies the shared `aws-auth` configmap, it does this using an upsert/delete in order to not affect existing permissions. Read more on how we [manage the aws-auth](https://github.com/keikoproj/aws-auth) configmap.

### Deploy instance-manager

Create the following resources

- Namespace (or use kube-system)

- [Custom Resource Definition](https://github.com/keikoproj/instance-manager/blob/master/config/crd/bases/instancemgr.keikoproj.io_instancegroups.yaml) for InstanceGroups API

- [Service Account](https://github.com/keikoproj/instance-manager/blob/master/config/rbac/service_account.yaml), [ClusterRole](https://github.com/keikoproj/instance-manager/blob/master/config/rbac/role.yaml) and [ClusterRoleBinding](https://github.com/keikoproj/instance-manager/blob/master/config/rbac/role_binding.yaml)

- [Deployment](https://github.com/keikoproj/instance-manager/blob/master/config/crd/bases/instance-manager-deployment.yaml) - the instance-manager controller

```bash
kubectl apply -f ./manifests.yaml
namespace/instance-manager created
customresourcedefinition.apiextensions.k8s.io/instancegroups.instancemgr.keikoproj.io created
serviceaccount/instance-manager created
clusterrole.rbac.authorization.k8s.io/instance-manager created
clusterrolebinding.rbac.authorization.k8s.io/instance-manager created
deployment.extensions/instance-manager created
```

### Create an InstanceGroup object

Time to submit our first instancegroup.

There are several components to the custom resource:

- Provisioner - this defines the provisioner which will create the node group, use `eks` to provision a scaling group of worker nodes, or the `eks-managed` provisioner to provision a managed node group.

- Strategy - this defines the upgrade strategy of the instancegroup, `rollingUpdate` is a basic form of rolling update of nodes managed by the controller, other supported types include `crd` which allows you to submit an arbitrary custom resource which implements some custom behaviour.

- Provisioner Configuration - defines the configuration of the actual node group with respect to the selected provisioner.

#### Example

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

check that the instance group is in creating state.

```bash
$ kubectl create -f instance_group.yaml
instancegroup.instancemgr.keikoproj.io/hello-world created

$ kubectl get instancegroups
NAMESPACE          NAME         STATE                MIN   MAX  GROUP NAME    PROVISIONER   STRATEGY   LIFECYCLE   AGE
instance-manager   hello-world  ReconcileModifying   3     6    hello-world   eks           crd        normal      1m
```

some time later, once the cloudformation stacks are created.

```bash
$ kubectl get instancegroups
NAMESPACE          NAME         STATE   MIN   MAX  GROUP NAME    PROVISIONER   STRATEGY   LIFECYCLE   AGE
instance-manager   hello-world  Ready   3     6    hello-world   eks           crd        normal      7m
```

Also, the new nodes should be joined as well.

```bash
$ kubectl get nodes
NAME                                           STATUS   ROLES         AGE      VERSION
ip-10-105-232-22.us-west-2.compute.internal    Ready    <none>        38m22s   v1.15.11-eks-af3caf
ip-10-105-233-251.us-west-2.compute.internal   Ready    <none>        38m49s   v1.15.11-eks-af3caf
ip-10-105-233-110.us-west-2.compute.internal   Ready    <none>        38m40s   v1.15.11-eks-af3caf
ip-10-10-10-20.us-west-2.compute.internal      Ready    hello-world   32s      v1.15.11-eks-af3caf
ip-10-10-10-30.us-west-2.compute.internal      Ready    hello-world   32s      v1.15.11-eks-af3caf
ip-10-10-10-40.us-west-2.compute.internal      Ready    hello-world   32s      v1.15.11-eks-af3caf
```

### Upgrade

Try upgrading the node group to a new AMI by changing the InstanceGroup resource. instance-manager considers any event where the launch configuration of the node group changes, an upgrade.

```bash
cat <<EOF | kubectl patch instancegroup hello-world --patch -
spec:
  eks:
    configuration:
      image: ami-065418523a44331e5
EOF
```

instance group should now be modifying, since `rollingUpdate` was the selected upgrade strategy, the controller will start rotating out the nodes according to `maxUnavailable`
