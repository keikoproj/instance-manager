Feature: CRUD Delete
  In order to delete instance-groups
  As an EKS cluster operator
  I need to delete the custom resource

  Scenario: Resources can be deleted
    Given an EKS cluster
    Then I delete a resource instance-group.yaml
    And I delete a resource instance-group-crd.yaml
    And I delete a resource instance-group-managed.yaml
    And I delete a resource instance-group-fargate.yaml

  Scenario: Delete an instance-group with rollingUpdate strategy
    Given an EKS cluster
    When I delete a resource instance-group.yaml
    Then 0 nodes should be found
    And the resource should be deleted

  Scenario: Delete an instance-group with CRD strategy
    Given an EKS cluster
    When I delete a resource instance-group-crd.yaml
    Then 0 nodes should be found
    And the resource should be deleted

  Scenario: Delete an instance-group with managed node-group
    Given an EKS cluster
    When I delete a resource instance-group-managed.yaml
    Then 0 nodes should be found
    And the resource should be deleted

  Scenario: Delete a fargate profile
    Given an EKS cluster
    Then I delete a resource instance-group-fargate.yaml
    And the fargate profile should be not found
