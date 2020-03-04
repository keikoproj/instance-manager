Feature: EKSCF Create
  In order to create instance-groups
  As an EKS cluster operator
  I need to submit the custom resource

  Scenario: Create an instance-group with rollingUpdate strategy
    Given an EKS cluster
    When I create a resource instance-group.yaml
    Then the resource should be created
    And the resource should converge to selector .status.currentState=success
    And 2 nodes should be ready

  Scenario: Create an instance-group with CRD strategy
    Given an EKS cluster
    When I create a resource instance-group-crd.yaml
    Then the resource should be created
    And the resource should converge to selector .status.currentState=success
    And 2 nodes should be ready
