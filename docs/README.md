# Installation Reference Walkthrough

This guide will walk you through all the steps needed from the creation of the EKS Cluster to the creation of cluster-native worker nodes using instance-manager.

This cluster setup part of the walkthrough is meant to be an example and is not recommended to use in production to set up EKS clusters.

## Prerequisites

There are several prerquisites for installing instance-manager, currently instance-manager only supports provisioning on EKS, so this document will only refer to installation on EKS.
All examples provided are done using cloudformation templates, however you can use other tools such as `eksctl` or `awscli` to achieve the same result.
If you already have a running cluster you can **skip to the [Deploy instance-manager](#deploy-instance-manager) section** below.

### Create the control plane prerequisites

In order to create an EKS cluster, you first need a security group and a service role.
First, set some default variables in your shell:

```bash
export AWS_REGION=us-west-2  # or replace with different region
export VPC_ID=<TARGET VPC>   # replace this with your VPC

# Define stack names
export NODE_GROUP_STACK_NAME="eks-bootstrap-node-group"
export EKS_CONTROL_PLANE_STACK_NAME="eks-control-plane"

export EKS_SERVICE_ROLE_STACK_NAME="eks-service-role"
export EKS_SECURITY_GROUP_STACK_NAME="eks-security-group"
export NODE_GROUP_SERVICE_ROLE_STACK_NAME="eks-node-service-role"
export NODE_SECURITY_GROUP_STACK_NAME="eks-node-security-group"

cd <repository-root>  # cwd should be the root of the repository
```

#### Example

```bash
## creates a control plane service role and security group

# control plane service role
aws cloudformation create-stack \
--stack-name ${EKS_SERVICE_ROLE_STACK_NAME} \
--template-body file://docs/cloudformation/00_eks-cluster-service-role.yaml \
--capabilities CAPABILITY_IAM

aws cloudformation wait stack-create-complete --stack-name ${EKS_SERVICE_ROLE_STACK_NAME}

# get role arn
EKS_ROLE_ARN=$(aws cloudformation describe-stacks \
--stack-name ${EKS_SERVICE_ROLE_STACK_NAME} \
--query "Stacks[0].Outputs[?OutputKey=='RoleArn'].OutputValue" \
--output text)

# control plane security group
aws cloudformation create-stack \
--stack-name ${EKS_SECURITY_GROUP_STACK_NAME} \
--template-body file://docs/cloudformation/00_eks-cluster-security-group.yaml \
--parameters ParameterKey=VpcId,ParameterValue=${VPC_ID}

aws cloudformation wait stack-create-complete --stack-name ${EKS_SECURITY_GROUP_STACK_NAME}

# get security group id
EKS_SECURITY_GROUP_ID=$(aws cloudformation describe-stacks \
--stack-name ${EKS_SECURITY_GROUP_STACK_NAME} \
--query "Stacks[0].Outputs[?OutputKey=='SecurityGroup'].OutputValue" \
--output text)
```

### Create the EKS control plane

To create the EKS cluster, you can refer to the documentation provided by AWS on [creating eks clusters](https://docs.aws.amazon.com/eks/latest/userguide/create-cluster.html) or use the below example.

#### Example

```bash
## creates a control plane (eks cluster)

# export required parameters
export EKS_CLUSTER_NAME=my-eks-cluster  # EKS control plane cluster name
export VERSION=1.13  # EKS control plane kubernetes version

# Replace subnets with your control plane subnets (keep comma escaping)
export EKS_CLUSTER_SUBNETS='subnet-000000001\,subnet-000000002\,subnet-000000003'

# eks control plane (this one takes a while!)
aws cloudformation create-stack \
--stack-name ${EKS_CONTROL_PLANE_STACK_NAME} \
--template-body file://docs/cloudformation/01_eks-cluster.yaml \
--parameters ParameterKey=ClusterName,ParameterValue=${EKS_CLUSTER_NAME} \
ParameterKey=ServiceRoleArn,ParameterValue=${EKS_ROLE_ARN} \
ParameterKey=SecurityGroupIds,ParameterValue=${EKS_SECURITY_GROUP_ID} \
ParameterKey=Subnets,ParameterValue=${EKS_CLUSTER_SUBNETS} \
ParameterKey=Version,ParameterValue=${VERSION}

aws cloudformation wait stack-create-complete --stack-name ${EKS_CONTROL_PLANE_STACK_NAME}

# update kubeconfig
aws eks update-kubeconfig --name ${EKS_CLUSTER_NAME}
kubectl config set-context $(kubectl config current-context) --namespace instance-manager
```

At this point, the cluster should have no nodes in it.

```bash
kubectl get nodes
No resources found.
```

### Create the initial node group prerequisites

instance-manager requires some AWS permissions in order to create cloudformation templates.
These are the AWS policy actions that instance-manager requires:

```text
iam:GetRole
iam:PassRole
iam:CreateRole
iam:DeleteRole
iam:CreateInstanceProfile
iam:DeleteInstanceProfile
iam:AttachRolePolicy
iam:DetachRolePolicy
iam:RemoveRoleFromInstanceProfile
iam:AddRoleToInstanceProfile
ec2:Describe*
cloudformation:List*
cloudformation:Get*
cloudformation:Describe*
cloudformation:Create*
cloudformation:Delete*
cloudformation:Update*
autoscaling:Describe*
autoscaling:Create*
autoscaling:Delete*
autoscaling:Update*
autoscaling:Terminate*
autoscaling:Set*
eks:Describe*
```

You can chose to create the initial role group with these additional policies attached directly, or create a new role and use other solutions such as KIAM to assume it. You can refer to the documentation provided by KIAM [here](https://github.com/uswitch/kiam#overview).

#### Example

The instance role created in this example contains the above mentioned permissions required by instance-manager, plus the default required EKS node policies.

```bash
## creates a node service role and security group

# export required parameters
export NODE_AMI_ID=ami-03a55127c613349a7  # corresponds with standard eks ami for 1.13.7
export KEYPAIR_NAME=MyKeyPair
export NODE_GROUP_NAME=MyBootstrapNodeGroup

# Replace subnets with your worker node subnets (keep formatting)
export NODE_GROUP_SUBNETS='subnet-000000001\,subnet-000000002\,subnet-000000003'
export NODE_GROUP_SUBNET_LIST="['subnet-000000001','subnet-000000002','subnet-000000003']"

# create an SSH keypair under ~/.ssh
aws ec2 create-key-pair --key-name ${KEYPAIR_NAME} --query 'KeyMaterial' \
--output text > ~/.ssh/${KEYPAIR_NAME}.pem

# node service role
aws cloudformation create-stack \
--stack-name ${NODE_GROUP_SERVICE_ROLE_STACK_NAME} \
--capabilities CAPABILITY_IAM \
--template-body file://docs/cloudformation/02_node-group-service-role.yaml

aws cloudformation wait stack-create-complete --stack-name ${NODE_GROUP_SERVICE_ROLE_STACK_NAME}

# get service role arn
NODE_GROUP_ROLE_ARN=$(aws cloudformation describe-stacks \
--stack-name ${NODE_GROUP_SERVICE_ROLE_STACK_NAME} \
--query "Stacks[0].Outputs[?OutputKey=='NodeGroupRoleArn'].OutputValue" \
--output text)

# get service role name
NODE_GROUP_ROLE_NAME=$(aws cloudformation describe-stacks \
--stack-name ${NODE_GROUP_SERVICE_ROLE_STACK_NAME} \
--query "Stacks[0].Outputs[?OutputKey=='NodeGroupRoleName'].OutputValue" \
--output text)

# node security group
aws cloudformation create-stack \
--stack-name ${NODE_SECURITY_GROUP_STACK_NAME} \
--template-body file://docs/cloudformation/02_node-group-security-group.yaml \
--parameters ParameterKey=ClusterName,ParameterValue=${EKS_CLUSTER_NAME} \
ParameterKey=VpcId,ParameterValue=${VPC_ID} \
ParameterKey=ClusterControlPlaneSecurityGroup,ParameterValue=${EKS_SECURITY_GROUP_ID}

aws cloudformation wait stack-create-complete --stack-name ${NODE_SECURITY_GROUP_STACK_NAME}

# get security group id
NODE_SECURITY_GROUP=$(aws cloudformation describe-stacks \
--stack-name ${NODE_SECURITY_GROUP_STACK_NAME} \
--query "Stacks[0].Outputs[?OutputKey=='SecurityGroup'].OutputValue" \
--output text)
```

### Create an initial node group

Technically, you can run instance-manager locally to bootstrap the initial node group in your cluster, however we discourage this due to being a circular dependency on instance-manager, it's recommended to have at least one bootstrapped node group that is outside of instance-manager.

To create a basic node group manually, refer to the documentation provided by AWS on [launching worker nodes](https://docs.aws.amazon.com/eks/latest/userguide/launch-workers.html) or use the below example.

#### Example

```bash
## creates and bootstraps a node group

# node group (creates 3 x t3.medium nodes)
aws cloudformation create-stack \
--stack-name ${NODE_GROUP_STACK_NAME} \
--template-body file://docs/cloudformation/03_node-group.yaml \
--capabilities CAPABILITY_NAMED_IAM \
--tags Key=instancegroups.keikoproj.io/ClusterName,Value=${EKS_CLUSTER_NAME} \
--parameters ParameterKey=VpcId,ParameterValue=${VPC_ID} \
ParameterKey=NodeGroupName,ParameterValue=${NODE_GROUP_NAME} \
ParameterKey=ClusterControlPlaneSecurityGroup,ParameterValue=${EKS_SECURITY_GROUP_ID} \
ParameterKey=NodeSecurityGroup,ParameterValue=${NODE_SECURITY_GROUP} \
ParameterKey=Subnets,ParameterValue=${NODE_GROUP_SUBNETS} \
ParameterKey=ClusterName,ParameterValue=${EKS_CLUSTER_NAME} \
ParameterKey=KeyName,ParameterValue=${KEYPAIR_NAME} \
ParameterKey=NodeImageId,ParameterValue=${NODE_AMI_ID} \
ParameterKey=NodeInstanceRoleName,ParameterValue=${NODE_GROUP_ROLE_NAME} \
ParameterKey=NodeInstanceRoleArn,ParameterValue=${NODE_GROUP_ROLE_ARN}

aws cloudformation wait stack-create-complete --stack-name ${NODE_GROUP_STACK_NAME}

# update auth config map with arn
cat <<EOF | kubectl create -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: aws-auth
  namespace: kube-system
data:
  mapRoles: |
    - rolearn: $NODE_GROUP_ROLE_ARN
      username: system:node:{{EC2PrivateDNSName}}
      groups:
        - system:bootstrappers
        - system:nodes
EOF

# make sure the nodes have joined the cluster
kubectl get nodes
NAME                                           STATUS   ROLES    AGE     VERSION
ip-10-105-232-22.us-west-2.compute.internal    Ready    <none>   3m22s   v1.13.7-eks-c57ff8
ip-10-105-233-251.us-west-2.compute.internal   Ready    <none>   3m49s   v1.13.7-eks-c57ff8
ip-10-105-233-110.us-west-2.compute.internal   Ready    <none>   3m40s   v1.13.7-eks-c57ff8
```

#### Hybrid node groups

In this scenario, node groups which were manually bootstrapped (as above), and instance-manager managed instance groups can co-exist, you can add such node group's ARNs under `defaultArns` section of `controller.conf` if you want instance-manager to make sure it's added, however manual entries to aws-auth config map will not be modified/stripped out.

### Deploy instance-manager

Create the resources described in [04_instance-manager.yaml](04_instance-manager.yaml), in order to create the following resources:

- Namespace called "instance-manager"

- Custom Resource Definition for InstanceGroups API

- RBAC: Service Account, ClusterRole and ClusterRoleBinding

- ConfigMap: containing the instance-manager controller configuration

- Deployment: instance-manager controller

```bash
kubectl create -f ./docs/04_instance-manager.yaml
namespace/instance-manager created
customresourcedefinition.apiextensions.k8s.io/instancegroups.instancemgr.keikoproj.io created
serviceaccount/instance-manager created
clusterrole.rbac.authorization.k8s.io/instance-manager created
clusterrolebinding.rbac.authorization.k8s.io/instance-manager created
configmap/instance-manager created
deployment.extensions/instance-manager created

# test it
kubectl get instancegroups
No resources found.
```

#### instance-manager configmap

The instance-manager configmap contains two file which configure the behavior of instance-manager.

- `controller.conf` - allows to set some defaults, for example `defaultClusterName` and `defaultSubnets`, once you set these values you no longer need to provide them in the custom resource, doing so would override the defaults found in `controller.conf`. `defaultArns` can also be provided and instance-manager will make sure to always add those ARNs to the aws-auth config map.

- `cloudformation.template` - the actual cloudformation template that is used to create instance groups.
Several parts of the template contain golang templating blocks and should not be modified, other parts (such as tags) can be customized.

### Create an InstanceGroup object

Time to submit our first InstanceGroup.

There are several components to the custom resource:

- Provisioner - this defines the provisioner which will create the node group, use `eks-cf` to provision EKS worker nodes using cloudformation.

- Strategy - this defines the strategy of the node group upgrade, using `rollingUpdate` means that cloudformation's native rollingUpdate will be used for rotating the node, other supported types include `crd` which allows you to submit an arbitrary custom resource which implements some custom behaviour.

- Provisioner Configuration - defines the configuration of the actual node group with respect to the selected provisioner.

#### Example

This spec will provision a 3/6 node instance-group of type m3.medium

```bash
cat <<EOF | kubectl create -f -
apiVersion: instancemgr.keikoproj.io/v1alpha1
kind: InstanceGroup
metadata:
  name: hello-world
  namespace: instance-manager
spec:
  provisioner: eks-cf
  strategy:
      type: rollingUpdate
  eks-cf:
    maxSize: 6
    minSize: 3
    configuration:
      clusterName: $EKS_CLUSTER_NAME
      vpcId: $VPC_ID
      subnets: $NODE_GROUP_SUBNET_LIST
      keyPairName: $KEYPAIR_NAME
      image: $NODE_AMI_ID
      instanceType: m3.medium
      volSize: 20
      securityGroups:
      - $NODE_SECURITY_GROUP
      tags:
        - key: k8s.io/cluster-autoscaler/enabled
          value: "true"
        - key: hello
          value: world
EOF
```

Check that the instance group is in creating state

```bash
kubectl get instancegroups
NAME          STATE                MIN   MAX   GROUP NAME   PROVISIONER   AGE
hello-world   ReconcileModifying                            eks-cf        10s
```

Now simply wait for the reconcile to complete and for the node to join the cluster.

```bash
kubectl get instancegroups
NAME          STATE   MIN   MAX   GROUP NAME                                                                  PROVISIONER   AGE
hello-world   Ready   3     6     my-eks-cluster-instance-manager-hello-world-stack-NodeGroup-1JG6W6VL8WO8M   eks-cf        6m23s

kubectl get nodes
NAME                                           STATUS   ROLES         AGE     VERSION
ip-10-105-232-187.us-west-2.compute.internal   Ready    hello-world   9m2s    v1.13.7-eks-c57ff8
ip-10-105-232-22.us-west-2.compute.internal    Ready    <none>        3h56m   v1.13.7-eks-c57ff8
ip-10-105-233-227.us-west-2.compute.internal   Ready    hello-world   8m59s   v1.13.7-eks-c57ff8
ip-10-105-233-251.us-west-2.compute.internal   Ready    <none>        3h58m   v1.13.7-eks-c57ff8
ip-10-105-234-110.us-west-2.compute.internal   Ready    <none>        3h54m   v1.13.7-eks-c57ff8
ip-10-105-234-167.us-west-2.compute.internal   Ready    hello-world   8m59s   v1.13.7-eks-c57ff8
```

### Upgrade

Try upgrading the node group to a new AMI by changing the InstanceGroup resource. instance-manager considers any event where the launch configuration of the node group changes, an upgrade.

```bash
cat <<EOF | kubectl patch instancegroup hello-world --patch -
spec:
  eks-cf:
    configuration:
      image: ami-0355c210cb3f58aa2
EOF
```

instance group should now be modifying, since `rollingUpdate` was the selected upgrade strategy, it will use cloudformation native rolling update to replace the nodes one by one.

#### CRD Strategy

There might be cases where you would want to implement custom behavior as part of your upgrade, this can be achieved by the `crd` strategy - it allows you to create any custom resource and watch it for a success condition.

In [this](./examples/crd-argo.yaml) example, we submit an [argo](https://github.com/argoproj/argo) workflow as a response to an upgrade events.

#### Using spot instances

You can switch to spot instances in two ways:

- Manually set the `spec.eks-cf.configuration.spotPrice` to a spot price value, the downside is that if price becomes invalid (due to price changes) and instances gets pulled, you may end up with no instances until you modify the price again.

- Use a spot recommendation controller such as `minion-manager`, instance-manager will look at events with the following message format:

```json
{"apiVersion":"v1alpha1","spotPrice":"0.0067", "useSpot": true}
```

In addition, the event `involvedObject.name`, must be the name of the autoscaling group to switch, and the event `.reason` must be `SpotRecommendationGiven`.

When recommendations are not available (no events for an hour / recommendation controller is down), instance-group will retain the last provided configuration, until a human either changes back to on-demand (by setting `spotPrice: ""`) or until recommendation events are found again.

### Clean up

First, delete the instance group:

```bash
kubectl delete instancegroup hello-world
instancegroup.instancemgr.keikoproj.io "hello-world" deleted

# This will hang until finalizer is removed
# Finalizer makes sure the cloudformation template is deleted before the kubernetes resource is deleted.

kubectl get nodes
NAME                                           STATUS   ROLES    AGE     VERSION
ip-10-105-232-22.us-west-2.compute.internal    Ready    <none>   3h59m   v1.13.7-eks-c57ff8
ip-10-105-233-251.us-west-2.compute.internal   Ready    <none>   4h1m    v1.13.7-eks-c57ff8
ip-10-105-234-110.us-west-2.compute.internal   Ready    <none>   3h57m   v1.13.7-eks-c57ff8
```

Delete rest of the stacks created:

```bash
# Delete bootstrapped node group and EKS control plane first
aws cloudformation delete-stack --stack-name ${NODE_GROUP_STACK_NAME}
aws cloudformation delete-stack --stack-name ${EKS_CONTROL_PLANE_STACK_NAME}
aws cloudformation wait stack-delete-complete --stack-name ${NODE_GROUP_STACK_NAME}
aws cloudformation wait stack-delete-complete --stack-name ${EKS_CONTROL_PLANE_STACK_NAME}

# Delete service roles and security groups
aws cloudformation delete-stack --stack-name ${EKS_SERVICE_ROLE_STACK_NAME}
aws cloudformation delete-stack --stack-name ${EKS_SECURITY_GROUP_STACK_NAME}
aws cloudformation delete-stack --stack-name ${NODE_GROUP_SERVICE_ROLE_STACK_NAME}
aws cloudformation delete-stack --stack-name ${NODE_SECURITY_GROUP_STACK_NAME}
aws cloudformation wait stack-delete-complete --stack-name ${EKS_SERVICE_ROLE_STACK_NAME}
aws cloudformation wait stack-delete-complete --stack-name ${EKS_SECURITY_GROUP_STACK_NAME}
aws cloudformation wait stack-delete-complete --stack-name ${NODE_GROUP_SERVICE_ROLE_STACK_NAME}
aws cloudformation wait stack-delete-complete --stack-name ${NODE_SECURITY_GROUP_STACK_NAME}

# Unset cluster context from kubeconfig
CONTEXT=$(kubectl config current-context)
kubectl config unset current-context
kubectl config delete-cluster ${EKS_CLUSTER_NAME}
kubectl config delete-context ${CONTEXT}
```
