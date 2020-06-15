# instance-manager
[![Build Status](https://travis-ci.org/keikoproj/instance-manager.svg?branch=master)](https://travis-ci.org/keikoproj/instance-manager)
[![codecov](https://codecov.io/gh/keikoproj/instance-manager/branch/master/graph/badge.svg)](https://codecov.io/gh/keikoproj/instance-manager)
[![Go Report Card](https://goreportcard.com/badge/github.com/keikoproj/instance-manager)](https://goreportcard.com/report/github.com/keikoproj/instance-manager)
[![slack](https://img.shields.io/badge/slack-join%20the%20conversation-ff69b4.svg)][SlackUrl]
![version](https://img.shields.io/badge/version-0.7.0-blue.svg?cacheSeconds=2592000)
> Create and manage instance groups with Kubernetes.

**instance-manager** simplifies the creation of worker nodes from within a Kubernetes cluster and creates `InstanceGroup` objects in your cluster. Additionally, **instance-manager** will provision the actual machines and bootstrap them to the cluster.

![instance-manager](hack/instance-manager.png)

- [Installation](#installation)  
- [Usage Example](#usage-example)  
    * [Currently supported provisioners](#currently-supported-provisioners)
    * [EKS sample spec](#EKS-sample-spec)
    * [Submit and Verify](#submit-and-verify)
    * [Features](#features)
    * [Alpha-2 Version](#alpha-2-version)
- [Contributing](#contributing)  
- [Developer Guide](#developer-guide)  


Worker nodes in Kubernetes clusters work best if provisioned and managed using a logical grouping. Kops introduced the term “InstanceGroup” for this logical grouping. In AWS, an InstanceGroup maps to an AutoScalingGroup.

Given a particular cluster, there should be a way to create, read, upgrade and delete worker nodes from within the cluster itself. This enables use-cases where worker nodes can be created in response to Kubernetes events, InstanceGroups can be automatically assigned to namespaces for multi-tenancy, etc.

instance-manager provides this Kubernetes native mechanism for CRUD operations on worker nodes.

## Installation

You must first have atleast one instance group that was manually created, in order to host the instance-manager pod.

_For installation instructions and more examples of usage, please refer to the [Installation Reference Walkthrough][install]._

## Usage example

### Currently supported provisioners

| Provisioner | Description | 
| :---------- | :---------- | 
| eks         | provision nodes on EKS |
| eks-managed | provision managed node groups on EKS|
| eks-fargate | provision a cluster to run pods on EKS Fargate|

To create an instance group, submit an InstanceGroup custom resource in your cluster, and the controller will provision and bootstrap it to your cluster, and allow you to modify it from within the cluster.


### EKS sample spec

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


### Submit and Verify

```bash
$ kubectl create -f instance_group.yaml
instancegroup.instancemgr.keikoproj.io/hello-world created

$ kubectl get instancegroups
NAMESPACE          NAME         STATE                MIN   MAX  GROUP NAME    PROVISIONER   STRATEGY   LIFECYCLE   AGE
instance-manager   hello-world  ReconcileModifying   3     6    hello-world   eks           crd        normal      1m
```

some time later, once the cloudformation stacks are created

```bash
$ kubectl get instancegroups
NAMESPACE          NAME         STATE   MIN   MAX  GROUP NAME    PROVISIONER   STRATEGY   LIFECYCLE   AGE
instance-manager   hello-world  Ready   3     6    hello-world   eks           crd        normal      7m
```

At this point the new nodes should be joined as well

```bash
$ kubectl get nodes
NAME                                        STATUS   ROLES         AGE    VERSION
ip-10-10-10-10.us-west-2.compute.internal   Ready    system        2h     v1.14.6-eks-5047ed
ip-10-10-10-20.us-west-2.compute.internal   Ready    hello-world   32s    v1.14.6-eks-5047ed
ip-10-10-10-30.us-west-2.compute.internal   Ready    hello-world   32s    v1.14.6-eks-5047ed
ip-10-10-10-40.us-west-2.compute.internal   Ready    hello-world   32s    v1.14.6-eks-5047ed
```

### Features

#### Spot instance support

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

#### Upgrade Strategies

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

#### EKS Managed Node Group (alpha-1)

You can also provision EKS managed node groups by submitting a spec with a different provisioner.

```yaml
apiVersion: instancemgr.keikoproj.io/v1alpha1
kind: InstanceGroup
metadata:
  name: hello-world
  namespace: instance-manager
spec:
  strategy:
    type: managed
  provisioner: eks-managed
  eks-managed:
    maxSize: 6
    minSize: 3
    configuration:
      clusterName: my-eks-cluster
      labels:
        example.label.com/label: some-value
      volSize: 20
      nodeRole: arn:aws:iam::012345678910:role/basic-eks-role
      amiType: AL2_x86_64
      instanceType: m5.large
      keyPairName: my-ec2-key-pair
      securityGroups:
      - sg-04adb6343b07c7914
      subnets:
      - subnet-0bf9bc85fd80af561
      - subnet-0130025d2673de5e4
      - subnet-01a5c28e074c46580
      tags:
      - key: my-ec2-tag
        value: some-value
```

#### EKS Fargate
The purpose of the fargate provisioner is to enable the management of Fargate profiles.

By associating EKS clusters with a Fargate Profile, pods can be identified for execution through profile selectors. If a to-be-scheduled pod matches any of the selectors in the Fargate Profile, then that pod is scheduled on Fargate. 

An EKS cluster can have multiple Fargate Profiles. If a pod matches multiple Fargate Profiles, Amazon EKS picks one of the matches at random.  

EKS supports clusters with both local worker nodes and Fargate management.  If a pod is scheduled and matches a Fargate selector then Fargate manages the pod.  Otherwise the pod is scheduled on a worker node.  Clusters can be defined without any worker nodes (0) and completely rely upon Fargate for scheduling and running pods. 

More on [Fargate](https://docs.aws.amazon.com/eks/latest/userguide/fargate.html).

The provisioner will manage (create and delete) Fargate Profiles on any EKS cluster (within the account) regardless of whether the cluster was created via CloudFormation, the AWS CLI or the AWS API. 

Below is an example specification for the **eks-fargate** provisioner

```yaml
apiVersion: instancemgr.keikoproj.io/v1alpha1
kind: InstanceGroup
metadata:
  name: hello-world
spec:
  # provision for EKS using Fargate
  provisioner: eks-fargate
  strategy:
    type: managed
  # provisioner configuration
  eks-fargate:
    clusterName: "the-cluster-for-my-pods"
    podExecutionRoleArn: "arn:aws:iam::123456789012:role/MyPodRole"
    subnets:
    - subnet-1a2b3c4d
    - subnet-4d3c2b1a
    - subnet-0w9x8y7z
    selectors:
    - namespace1:
      labels:
        key1: "value1"
        key2: "value2"
    - namespace2:
      labels:
        key1: "value1"
        key2: "value2"
    tags:
      key1: "value1"
      key2: "value2"

```
Read more about the [Fargate Profile](https://docs.aws.amazon.com/eks/latest/userguide/fargate-profile.html).

Note that the eks-fargate provisioner does not accept a Fargate profile name.  Instead, the provisioner creates a unique profile name based upon the cluster name, instance group name and namespace.

If the above *podExecutionRoleArn* parameter is not specified, the provisioner will create a simple, limited role and policy that enables the pod to start but not access any AWS resources.  The role's name will be prefixed by the generated Fargate profile name from above.  That role and policy are shown below. 

```yaml
Type: 'AWS::IAM::Role'
Properties:
  AssumeRolePolicyDocument:
    Version: 2012-10-17
    Statement:
      - Effect: "Allow"
        Principal:
          Service: "eks-fargate-pods.amazonaws.com"
        Action: "sts:AssumeRole"
  ManagedPolicyArns:
  - "arn:aws:iam::aws:policy/AmazonEKSFargatePodExecutionRolePolicy"
  Path: /
  
```
Most likely an execution role with access to addtional AWS resources will be required.  In this case, the above IAM role can be used as the basis to create a new, custom role with the IAM policies specific to your pods. Create your new role and your pod specific policies and use the new role's ARN as the *podExecutionRoleArn* parameter value in eks-fargate spec. 

Here is an example of a role with an additional policy for S3 access.

```yaml
Type: 'AWS::IAM::Role'
Properties:
  AssumeRolePolicyDocument:
    Version: 2012-10-17
    Statement:
      - Effect: "Allow"
        Principal:
          Service: "eks-fargate-pods.amazonaws.com"
        Action: "sts:AssumeRole"
  ManagedPolicyArns:
  - "arn:aws:iam::aws:policy/AmazonEKSFargatePodExecutionRolePolicy"
  Path: /
  Policies:
    - PolicyName: "your chosen name"
      PolicyDocument:
        Version: 2012-10-17
        Statement:
          - Effect: Allow
            Action: 
              - 's3:*'
            Resource: '*'
```

AWS's Fargate Profiles are immutable.  Once one is created, it cannot be directly modified.  It first has to be deleted and then re-created with the desired change.

The **eks-fargate** provisioner is built on top of that immutability.  Therefore, if an attempt is made to modify an existing profile, the provisioner will return an error.  You first have to `delete` the profile and follow that with a `create`.

#### Bring your own role

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

### Alpha-2 Version

Please consider that this project is in alpha stages and breaking API changes may happen, we will do our best to not break backwards compatiblity without a deprecation period going further.

The previous eks-cf provisioner have been discontinued in favor of the Alpha-2 eks provisioner, which does not use cloudformation as a mechanism to provision the required resources.

In order to migrate instace-groups from versions <0.5.0, delete all instance groups, update the custom resource definition RBAC, and controller IAM role, and deploy new instance-groups with the new provisioner.

## Contributing

Please see [CONTRIBUTING.md](.github/CONTRIBUTING.md).

## Developer Guide

Please see [DEVELOPER.md](.github/DEVELOPER.md).

<!-- Markdown link -->
[install]: https://github.com/keikoproj/instance-manager/blob/master/docs/README.md
[SlackUrl]: https://keikoproj.slack.com/messages/instance-manager
