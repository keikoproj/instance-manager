# Development reference

This document will walk you through setting up a basic testing environment, running unit tests or e2e functional tests.

## Running locally

Using the `Makefile` you can use `make run` to run instance-manager locally on your machine, and it will try to reconcile InstanceGroups in the cluster - if you do this, make sure another controller is not running in your cluster already to avoid conflict.

Make sure you have AWS credentials and a region exported so that your local instance-manager controller can make the required API calls.

### Example

```bash
$ kubectl scale deployment instance-manager --replicas 0
deployment.extensions/instance-manager scaled

$ make run
go fmt ./...
go vet ./...
go run ./main.go
2020-05-12T01:43:05.970-0700	INFO	controller-runtime.metrics	metrics server is starting to listen	{"addr": ":8080"}
2020-05-12T01:43:06.047-0700	INFO	setup	starting manager
2020-05-12T01:43:06.047-0700	INFO	controller-runtime.manager	starting metrics server	{"path": "/metrics"}
2020-05-12T01:43:06.047-0700	INFO	controller-runtime.controller	Starting EventSource	{"controller": "instancegroup", "source": "kind source: /, Kind="}
2020-05-12T01:43:06.150-0700	INFO	controller-runtime.controller	Starting EventSource	{"controller": "instancegroup", "source": "kind source: /, Kind="}
2020-05-12T01:43:06.255-0700	INFO	controller-runtime.controller	Starting Controller	{"controller": "instancegroup"}
2020-05-12T01:43:06.255-0700	INFO	controller-runtime.controller	Starting workers	{"controller": "instancegroup", "worker count": 5}
```

## Running unit tests

Using the `Makefile` you can run basic unit tests.

### Example

```bash
$ make test
go fmt ./...
go vet ./...
/Users/eibissror/go/bin/controller-gen "crd:trivialVersions=true" rbac:roleName=instance-manager webhook paths="./api/...;./controllers/..." output:crd:artifacts:config=config/crd/bases
go test -v ./controllers/... -coverprofile coverage.txt
?       github.com/keikoproj/instance-manager/controllers    [no test files]
?       github.com/keikoproj/instance-manager/controllers/common    [no test files]
?       github.com/keikoproj/instance-manager/controllers/providers/aws [no test files]
?       github.com/keikoproj/instance-manager/controllers/providers/kubernetes  [no test files]
?       github.com/keikoproj/instance-manager/controllers/provisioners  [no test files]
PASS
coverage: 86.5% of statements
ok      github.com/keikoproj/instance-manager/controllers/provisioners/eks  0.472s  coverage: 86.5% of statements
coverage: 81.0% of statements
ok      github.com/keikoproj/instance-manager/controllers/provisioners/eksmanaged   0.785s  coverage: 81.0% of statements
```

You can also run `make coverage` to generate a coverage report.

## Running BDD tests

Export some variables and run `make bdd` to run a functional e2e test.

### Example

```bash
export AWS_REGION=us-west-2
export KUBECONFIG=~/.kube/config

export EKS_CLUSTER=my-eks-cluster
export KEYPAIR_NAME=MyKeyPair
export AMI_ID=ami-EXAMPLEdk93
export SECURITY_GROUPS=sg-EXAMPLE2323,sg-EXAMPLE4433
export NODE_SUBNETS=subnet-EXAMPLE223d,subnet-EXAMPLEdkkf,subnet-EXAMPLEkkr9

# an existing role for nodes
export NODE_ROLE_ARN=arn:aws:iam::123456789012:role/basic-eks-role
export NODE_ROLE=basic-eks-role

$ make bdd

Feature: CRUD Create
  In order to create instance-groups
  As an EKS cluster operator
  I need to submit the custom resource

  Scenario: Resources can be submitted                  # features/01_create.feature:6
    Given an EKS cluster                                # main_test.go:125 -> *FunctionalTest
    Then I create a resource instance-group.yaml        # main_test.go:165 -> *FunctionalTest
    And I create a resource instance-group-crd.yaml     # main_test.go:165 -> *FunctionalTest
    And I create a resource instance-group-managed.yaml # main_test.go:165 -> *FunctionalTest

...
...

15 scenarios (15 passed)
72 steps (72 passed)
22m40.347700419s
testing: warning: no tests to run
PASS
ok  	github.com/keikoproj/instance-manager/test-bdd	1362.336s [no tests to run]
```
