Feature: EKSCF Upgrade
  In order to rotate an instance-groups
  As an EKS cluster operator
  I need to update the custom resource instance type

  Scenario: Update an instance-group with rollingUpdate strategy
    Given an EKS cluster
    When I update a resource instance-group.yaml with .spec.eks-cf.configuration.instanceType set to t2.medium
    Then 3 nodes should be ready with label kubernetes.io/instance-type set to t2.medium
    And the resource should converge to selector .status.currentState=ready

  Scenario: Update an instance-group with CRD strategy
    Given an EKS cluster
    When I update a resource instance-group.yaml with .spec.eks-cf.configuration.instanceType set to t2.medium
    Then 3 nodes should be ready with label kubernetes.io/instance-type set to t2.medium
    And the resource should converge to selector .status.currentState=ready
