# Development reference

This document will walk you through setting up a basic testing environment, running unit tests or e2e functional tests.

## Setup EKS cluster

- Make sure you have access and valid credentials to an AWS account

- Run [setup.sh](../tests-bdd/setup/setup.sh) with appropriate flags.

### Example

```bash
$ ./setup.sh --region us-west-2 \
--vpc-id vpc-EXAMPLE6c75dd646 \
--cluster-name my-eks-cluster \
--ami-id ami-03a55127c613349a7 \
--cluster-subnets subnet-EXAMPLE2673de5e4,subnet-EXAMPLE4ad6eb0f5,subnet-EXAMPLE3ce858b2a \
--node-subnets subnet-EXAMPLEe074c46580,subnet-EXAMPLEd2673de5e4,subnet-EXAMPLE5fd80af561 \
--keypair-name MyKeyPair \
--template-path /absolute/path/to/instance-manager/docs/cloudformation \
create
```

## Running locally

Using the `Makefile` you can use `make run` to run instance-manager locally on your machine, and it will try to reconcile InstanceGroups in the cluster.
Make sure the context you are in is correct and that you scaled down the actual instance-manager controller pod.

### Example

```bash
$ kubectl scale deployment instance-manager --replicas 0
deployment.extensions/instance-manager scaled

$ make run
/Users/eibissror/go/bin/controller-gen object:headerFile=./hack/boilerplate.go.txt paths=./api/...
go fmt ./...
go vet ./...
go run ./main.go
2019-07-15T10:07:57.650-0700	INFO	controller-runtime.controller	Starting EventSource	{"controller": "instancegroup", "source": "kind source: /, Kind="}
2019-07-15T10:07:57.650-0700	INFO	setup	starting manager
2019-07-15T10:07:57.751-0700	INFO	controller-runtime.controller	Starting Controller	{"controller": "instancegroup"}
2019-07-15T10:07:57.851-0700	INFO	controller-runtime.controller	Starting workers	{"controller": "instancegroup", "worker count": 1}
```

## Running unit tests

Using the `Makefile` you can run basic unit tests.

### Example

```bash
$ make test

?       github.com/keikoproj/instance-manager/controllers    [no test files]
?       github.com/keikoproj/instance-manager/controllers/common [no test files]
?       github.com/keikoproj/instance-manager/controllers/providers/aws  [no test files]
=== RUN   TestStateDiscoveryInitUpdate
--- PASS: TestStateDiscoveryInitUpdate (0.00s)
=== RUN   TestStateDiscoveryReconcileModifying
--- PASS: TestStateDiscoveryReconcileModifying (0.00s)
=== RUN   TestStateDiscoveryReconcileInitCreate
--- PASS: TestStateDiscoveryReconcileInitCreate (0.00s)
=== RUN   TestStateDiscoveryInitDeleting
--- PASS: TestStateDiscoveryInitDeleting (0.00s)
=== RUN   TestStateDiscoveryReconcileDelete
--- PASS: TestStateDiscoveryReconcileDelete (0.00s)
=== RUN   TestStateDiscoveryDeleted
--- PASS: TestStateDiscoveryDeleted (0.00s)
=== RUN   TestStateDiscoveryDeletedExist
--- PASS: TestStateDiscoveryDeletedExist (0.00s)
=== RUN   TestStateDiscoveryUpdateRecoverableError
--- PASS: TestStateDiscoveryUpdateRecoverableError (0.00s)
=== RUN   TestStateDiscoveryUnrecoverableError
--- PASS: TestStateDiscoveryUnrecoverableError (0.00s)
=== RUN   TestStateDiscoveryUnrecoverableErrorDelete
--- PASS: TestStateDiscoveryUnrecoverableErrorDelete (0.00s)
=== RUN   TestNodeBootstrappingCreateConfigMap
--- PASS: TestNodeBootstrappingCreateConfigMap (0.00s)
=== RUN   TestNodeBootstrappingUpdateConfigMap
--- PASS: TestNodeBootstrappingUpdateConfigMap (0.00s)
=== RUN   TestNodeBootstrappingUpdateConfigMapWithExistingMembers
--- PASS: TestNodeBootstrappingUpdateConfigMapWithExistingMembers (0.00s)
=== RUN   TestNodeBootstrappingRemoveMembers
--- PASS: TestNodeBootstrappingRemoveMembers (0.00s)
=== RUN   TestCrdStrategyCRExist
--- PASS: TestCrdStrategyCRExist (0.00s)
PASS
coverage: 72.6% of statements
ok      github.com/keikoproj/instance-manager/controllers/provisioners/ekscloudformation 5.352s  coverage: 72.6% of statements
```

You can use `make vtest` to run test with verbose logging, you can also run `make coverage` to generate a coverage report.

## Running BDD tests

Export some variables and run `make bdd` to run a functional e2e test.

### Example

```bash
export EKS_CLUSTER=my-eks-cluster
export AWS_REGION=us-west-2
export KUBECONFIG=~/.kube/config
export KEYPAIR_NAME=MyKeyPair
export VPC_ID=vpc-EXAMPLE23dk9
export STABLE_AMI=ami-EXAMPLEdk93
export LATEST_AMI=ami-EXAMPLE2e09
export SECURITY_GROUPS=sg-EXAMPLE2323,sg-EXAMPLE4433
export SUBNETS=subnet-EXAMPLE223d,subnet-EXAMPLEdkkf,subnet-EXAMPLEkkr9

$ make bdd

=== RUN   TestE2e
Running Suite: InstanceGroup Type Suite
=======================================
Random Seed: 1563208279
Will run 4 of 4 specs

...
...

Ran 4 of 4 Specs in 635.904 seconds
SUCCESS! -- 4 Passed | 0 Failed | 0 Pending | 0 Skipped
--- PASS: TestE2e (635.90s)
PASS
ok      github.com/keikoproj/instance-manager/test-bdd   636.198s
```
