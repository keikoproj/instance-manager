### EKS Fargate

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
