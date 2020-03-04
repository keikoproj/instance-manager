Feature: EKSCF Update
  In order to update an instance-groups
  As an EKS cluster operator
  I need to update the custom resource

  Scenario: Update an instance-group with rollingUpdate strategy
    Given an EKS cluster
    When I update a resource instance-group.yaml with .spec.eks-cf.minSize set to 3
    Then 3 nodes should be ready
    And the resource should converge to selector .status.currentState=ready

  Scenario: Update an instance-group with CRD strategy
    Given an EKS cluster
    When I update a resource instance-group-crd.yaml with .spec.eks-cf.minSize set to 3
    Then 3 nodes should be ready
    And the resource should converge to selector .status.currentState=ready
