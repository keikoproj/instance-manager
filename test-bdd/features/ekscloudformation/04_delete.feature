Feature: EKSCF Delete
  In order to delete instance-groups
  As an EKS cluster operator
  I need to delete the custom resource

  Scenario: Delete an instance-group with rollingUpdate strategy
    Given an EKS cluster
    When I delete a resource instance-group.yaml
    Then the resource should converge to selector .status.currentState=deleting
    And 0 nodes should be found
    And the resource should be deleted

  Scenario: Delete an instance-group with CRD strategy
    Given an EKS cluster
    When I delete a resource instance-group-crd.yaml
    Then the resource should converge to selector .status.currentState=deleting
    And 0 nodes should be found
    And the resource should be deleted