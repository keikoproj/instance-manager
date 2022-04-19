Feature: CRUD Create
  In order to create instance-groups
  As an EKS cluster operator
  I need to submit the custom resource

  Scenario: Resources can be submitted
    Given an EKS cluster
    Then I create a resource namespace.yaml
    And I create a resource namespace-gitops.yaml
    And I create a resource instance-group.yaml
    And I create a resource instance-group-crd.yaml
    And I create a resource instance-group-wp.yaml
    And I create a resource instance-group-crd-wp.yaml
    And I create a resource instance-group-managed.yaml
    And I create a resource instance-group-fargate.yaml
    And I create a resource instance-group-launch-template.yaml
    And I create a resource instance-group-launch-template-mixed.yaml
    And I create a resource manager-configmap.yaml
    And I create a resource instance-group-gitops.yaml
    And I create a resource instance-group-latest-locked.yaml

  Scenario: Create an instance-group with rollingUpdate strategy
    Given an EKS cluster
    When I create a resource instance-group.yaml
    Then the resource should be created
    And the resource should converge to selector .status.currentState=ready
    And the resource condition NodesReady should be true
    And 2 nodes should be ready

  Scenario: Create an instance-group with CRD strategy
    Given an EKS cluster
    When I create a resource instance-group-crd.yaml
    Then the resource should be created
    And the resource should converge to selector .status.currentState=ready
    And the resource condition NodesReady should be true
    And 2 nodes should be ready

  Scenario: Create an instance-group with rollingUpdate strategy and warm pools configured
    Given an EKS cluster
    When I create a resource instance-group-wp.yaml
    Then the resource should be created
    And the resource should converge to selector .status.currentState=ready
    And the resource condition NodesReady should be true
    And 2 nodes should be ready

  Scenario: Create an instance-group with CRD strategy and warm pools configured
    Given an EKS cluster
    When I create a resource instance-group-crd-wp.yaml
    Then the resource should be created
    And the resource should converge to selector .status.currentState=ready
    And the resource condition NodesReady should be true
    And 2 nodes should be ready

  Scenario: Create an instance-group with launch template
    Given an EKS cluster
    When I create a resource instance-group-launch-template.yaml
    Then the resource should be created
    And the resource should converge to selector .status.currentState=ready
    And the resource condition NodesReady should be true
    And 2 nodes should be ready

  Scenario: Create an instance-group with launch template and mixed instances
    Given an EKS cluster
    When I create a resource instance-group-launch-template-mixed.yaml
    Then the resource should be created
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
    And the resource should converge to selector .status.currentState=ready

  Scenario: Create an instance-group with shortened resource
    Given an EKS cluster
    When I create a resource instance-group-gitops.yaml
    Then the resource should be created
    And the resource should converge to selector .status.currentState=ready
    And the resource condition NodesReady should be true
    And 2 nodes should be ready
  
  Scenario: Create an instance-group with latest ami
    Given an EKS cluster
    When I create a resource instance-group-latest-locked.yaml
    Then the resource should be created
    And the resource should converge to selector .status.currentState=ready
    And the resource condition NodesReady should be true
    And 2 nodes should be ready
