# instance-manager
[![Build Status](https://travis-ci.org/keikoproj/instance-manager.svg?branch=master)](https://travis-ci.org/keikoproj/instance-manager)
[![codecov](https://codecov.io/gh/keikoproj/instance-manager/branch/master/graph/badge.svg)](https://codecov.io/gh/keikoproj/instance-manager)
[![Go Report Card](https://goreportcard.com/badge/github.com/keikoproj/instance-manager)](https://goreportcard.com/report/github.com/keikoproj/instance-manager)
[![slack](https://img.shields.io/badge/slack-join%20the%20conversation-ff69b4.svg)][SlackUrl]
![version](https://img.shields.io/badge/version-0.4.0-blue.svg?cacheSeconds=2592000)
> Create and manage instance groups with Kubernetes.

instance-manager simplifies the creation of worker nodes from within a Kubernetes cluster, create `InstanceGroup` objects in your cluster and instance-manager will provision the actual machines and bootstrap them to the cluster.

![instance-manager](hack/instance-manager.png)

Worker nodes in Kubernetes clusters work best if provisioned and managed using a logical grouping. Kops introduced the term “InstanceGroup” for this logical grouping. In AWS, an InstanceGroup maps to an AutoScalingGroup.

Given a particular cluster, there should be a way to create, read, upgrade and delete worker nodes from within the cluster itself. This enables use-cases where worker nodes can be created in response to Kubernetes events, InstanceGroups can be automatically assigned to namespaces for multi-tenancy, etc.

instance-manager provides this Kubernetes native mechanism for CRUD operations on worker nodes.

## Installation

You must first have atleast one instance group that was manually created, in order to host the instance-manager pod.

```sh
kubectl create namespace instance-manager
kubectl apply -n instance-manager -f https://raw.githubusercontent.com/keikoproj/instance-manager/master/docs/04_instance-manager.yaml
```

_For more examples and usage, please refer to the [Installation Reference Walkthrough][install]._

## Usage example

### Currently supported provisioners

| Provisioner | Description | Supported?
| :--- | :--- | :---: |
| eks-cf | provision nodes on EKS using cloudformation | ✅ |
| eks-tf | provision nodes on EKS using terraform | ⚠️🔜 |
| ec2-kops | provision nodes on AWS using Kops | ⚠️🔜 |

To create an instance group, submit an InstanceGroup spec to the controller.

### Sample spec

```yaml
apiVersion: instancemgr.keikoproj.io/v1alpha1
kind: InstanceGroup
metadata:
  name: hello-world
spec:
  # provision for EKS using Cloudformation
  provisioner: eks-cf
  # upgrade strategy
  strategy:
    type: rollingUpdate
  # provisioner configuration
  eks-cf:
    # autoscaling group size
    minSize: 1
    maxSize: 3
    configuration:
      # extra kubelet arguments for taints / labels
      bootstrapArguments: --register-with-taints=my-taint-key=my-taint-value:NoSchedule --node-labels=my-label=true
      # an existing ec2 keypair name
      keyPairName: my-key
      # an EKS compatible AMI
      image: ami-089d3b6350c1769a6
      instanceType: m5.large
      volSize: 20
      # the name of the EKS cluster
      clusterName: my-eks-cluster
      subnets:
      - subnet-1a2b3c4d
      - subnet-4d3c2b1a
      - subnet-0w9x8y7z
      securityGroups:
      - sg-q1w2e3r4t5y6u7i8
      # tags to append to all created resources
      tags:
        - key: mykey
          value: myvalue
```

### Submit & Verify

```sh
$ kubectl create -f instance_group.yaml
instancegroup.instancemgr.keikoproj.io/hello-world created

$ kubectl get instancegroups
NAME          STATE                MIN   MAX   GROUP NAME   PROVISIONER   AGE
hello-world   ReconcileModifying   1     3                  eks-cf        10s
```

some time later, once the cloudformation stacks are created

```sh
$ kubectl get instancegroups
NAME          STATE   MIN   MAX   GROUP NAME              PROVISIONER   AGE
hello-world   Ready   1     3     autoscaling-group-name  eks-cf        4m

$ kubectl get nodes
NAME                                        STATUS   ROLES         AGE    VERSION
ip-10-10-10-10.us-west-2.compute.internal   Ready    system        2h     v1.12.7
ip-10-10-10-20.us-west-2.compute.internal   Ready    hello-world   32s    v1.12.7
```

_For more examples and usage, please refer to the [Installation Reference Walkthrough][install]._

## ❤ Contributing ❤

Please see [CONTRIBUTING.md](.github/CONTRIBUTING.md).

## Developer Guide

Please see [DEVELOPER.md](.github/DEVELOPER.md).

<!-- Markdown link -->
[install]: https://github.com/keikoproj/instance-manager/blob/master/docs/README.md
[SlackUrl]: https://keikoproj.slack.com/messages/instance-manager