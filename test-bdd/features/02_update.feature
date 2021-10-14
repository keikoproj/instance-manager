Feature: CRUD Update
  In order to update an instance-groups
  As an EKS cluster operator
  I need to update the custom resource

  Scenario: Resources can be updated
    Given an EKS cluster
    Then I update a resource instance-group.yaml with .spec.eks.minSize set to 3
    And I update a resource instance-group-crd.yaml with .spec.eks.minSize set to 3
    And I update a resource instance-group-wp.yaml with .spec.eks.minSize set to 3
    And I update a resource instance-group-crd-wp.yaml with .spec.eks.minSize set to 3
    And I update a resource instance-group-launch-template.yaml with .spec.eks.minSize set to 3
    And I update a resource instance-group-launch-template-mixed.yaml with .spec.eks.minSize set to 3
    And I update a resource instance-group-managed.yaml with .spec.eks-managed.minSize set to 3
    And I update a resource instance-group-latest-locked.yaml with .spec.eks.minSize set to 3

  Scenario: Update an instance-group with rollingUpdate strategy
    Given an EKS cluster
    When I update a resource instance-group.yaml with .spec.eks.minSize set to 3
    Then the resource should converge to selector .status.currentState=ready
    And the resource condition NodesReady should be true
    And 3 nodes should be ready

  Scenario: Update an instance-group with CRD strategy
    Given an EKS cluster
    When I update a resource instance-group-crd.yaml with .spec.eks.minSize set to 3
    Then the resource should converge to selector .status.currentState=ready
    And the resource condition NodesReady should be true
    And 3 nodes should be ready

  Scenario: Update an instance-group with rollingUpdate strategy and warm pools configured
    Given an EKS cluster
    When I update a resource instance-group-wp.yaml with .spec.eks.minSize set to 3
    Then the resource should converge to selector .status.currentState=ready
    And the resource condition NodesReady should be true
    And 3 nodes should be ready

  Scenario: Update an instance-group with CRD strategy and warm pools configured
    Given an EKS cluster
    When I update a resource instance-group-crd-wp.yaml with .spec.eks.minSize set to 3
    Then the resource should converge to selector .status.currentState=ready
    And the resource condition NodesReady should be true
    And 3 nodes should be ready

  Scenario: Update an instance-group with launch template
    Given an EKS cluster
    When I update a resource instance-group-launch-template.yaml with .spec.eks.minSize set to 3
    Then the resource should converge to selector .status.currentState=ready
    And the resource condition NodesReady should be true
    And 3 nodes should be ready

  Scenario: Update an instance-group with launch template and mixed instances
    Given an EKS cluster
    When I update a resource instance-group-launch-template-mixed.yaml with .spec.eks.minSize set to 3
    Then the resource should converge to selector .status.currentState=ready
    And the resource condition NodesReady should be true
    And 3 nodes should be ready

  Scenario: Update an instance-group with managed node-group
    Given an EKS cluster
    When I update a resource instance-group-managed.yaml with .spec.eks-managed.minSize set to 3
    Then the resource should converge to selector .status.currentState=ready
    And 3 nodes should be ready

  Scenario: Update an instance-group with latest ami
    Given an EKS cluster
    When I update a resource instance-group-latest-locked.yaml with .spec.eks.minSize set to 3
    Then the resource should converge to selector .status.currentState=ready
    And 3 nodes should be ready