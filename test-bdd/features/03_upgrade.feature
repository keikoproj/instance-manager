Feature: CRUD Upgrade
  In order to rotate an instance-groups
  As an EKS cluster operator
  I need to update the custom resource instance type

  Scenario: Resources can be upgraded
    Given an EKS cluster
    Then I update a resource instance-group.yaml with .spec.eks.configuration.instanceType set to t2.medium
    And I update a resource instance-group-crd.yaml with .spec.eks.configuration.instanceType set to t2.medium

  Scenario: Update an instance-group with rollingUpdate strategy
    Given an EKS cluster
    When I update a resource instance-group.yaml with .spec.eks.configuration.instanceType set to t2.medium
    Then 3 nodes should be ready with label beta.kubernetes.io/instance-type set to t2.medium
    And the resource should converge to selector .status.currentState=ready
    And the resource condition NodesReady should be true
    And 3 nodes should be ready

  Scenario: Update an instance-group with launch template
    Given an EKS cluster
    When I update a resource instance-group-launch-template.yaml with .spec.eks.configuration.instanceType set to t2.medium
    Then 3 nodes should be ready with label beta.kubernetes.io/instance-type set to t2.medium
    And the resource should converge to selector .status.currentState=ready
    And the resource condition NodesReady should be true
    And 3 nodes should be ready

  Scenario: Update an instance-group with launch template and mixed instances
    Given an EKS cluster
    When I update a resource instance-group-launch-template-mixed.yaml with .spec.eks.configuration.instanceType set to t2.medium
    Then 3 nodes should be ready with label beta.kubernetes.io/instance-type set to t2.medium
    And the resource should converge to selector .status.currentState=ready
    And the resource condition NodesReady should be true
    And 3 nodes should be ready

  Scenario: Update an instance-group with CRD strategy
    Given an EKS cluster
    When I update a resource instance-group-crd.yaml with .spec.eks.configuration.instanceType set to t2.medium
    Then 3 nodes should be ready with label beta.kubernetes.io/instance-type set to t2.medium
    And the resource should converge to selector .status.currentState=ready
    And the resource condition NodesReady should be true
    And 3 nodes should be ready
