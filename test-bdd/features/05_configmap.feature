Feature: GitOps / ConfigMap Defaults
  In order to use GitOps for instance groups
  As an EKS cluster operator
  I need to create a configmap which defines configuration boundaries and defaults

  Scenario: Resources can be submitted
    Given an EKS cluster
    Then I create a resource manager-configmap.yaml
    And I create a resource instance-group-gitops.yaml

  Scenario: Create an instance-group with shortened resource
    Given an EKS cluster
    When I create a resource instance-group-gitops.yaml
    Then the resource should be created
    And the resource should converge to selector .status.currentState=ready
    And the resource condition NodesReady should be true
    And 2 nodes should be ready

  Scenario: Delete an instance-group with shortened resource
    Given an EKS cluster
    When I delete a resource instance-group-gitops.yaml
    Then 0 nodes should be found
    And the resource should be deleted
    And I delete a resource manager-configmap.yaml
