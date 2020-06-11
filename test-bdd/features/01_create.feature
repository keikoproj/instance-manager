Feature: CRUD Create
  In order to create instance-groups
  As an EKS cluster operator
  I need to submit the custom resource

  Scenario: Resources can be submitted
    Given an EKS cluster
    Then I create a resource instance-group.yaml
    And I create a resource instance-group-crd.yaml
    And I create a resource instance-group-managed.yaml

  Scenario: Create an instance-group with rollingUpdate strategy
    Given an EKS cluster
    When I create a resource instance-group.yaml
    Then the resource should be created
    And the resource should converge to selector .status.lifecycle=spot
    And the resource should converge to selector .status.currentState=ready
    And the resource condition NodesReady should be true
    And 2 nodes should be ready

  Scenario: Create an instance-group with CRD strategy
    Given an EKS cluster
    When I create a resource instance-group-crd.yaml
    Then the resource should be created
    And the resource should converge to selector .status.lifecycle=spot
    And the resource should converge to selector .status.currentState=ready
    And the resource condition NodesReady should be true
    And 2 nodes should be ready

  Scenario: Create an instance-group with managed node group
    Given an EKS cluster
    When I create a resource instance-group-managed.yaml
    Then the resource should be created
    And the resource should converge to selector .status.currentState=ready
    And 2 nodes should be ready

  Scenario: Create a fargate profile with default execution role 
    Given an EKS cluster
    Then I create a resource instance-group-fargate.yaml
    And the resource should be created
    And the fargate profile should be found

