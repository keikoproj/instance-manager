module github.com/keikoproj/instance-manager

go 1.15

require (
	github.com/Masterminds/semver v1.5.0
	github.com/aws/aws-sdk-go v1.35.22
	github.com/cucumber/godog v0.8.1
	github.com/evanphx/json-patch v4.9.0+incompatible
	github.com/ghodss/yaml v1.0.0
	github.com/go-logr/logr v0.3.0
	github.com/keikoproj/aws-auth v0.0.0-20210105225553-36322b72224f
	github.com/keikoproj/aws-sdk-go-cache v0.0.0-20201118182730-f6f418a4e2df
	github.com/onsi/gomega v1.10.2
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.6.0
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d // indirect
	golang.org/x/tools v0.0.0-20200616195046-dc31b401abb5 // indirect
	k8s.io/api v0.19.6
	k8s.io/apimachinery v0.19.6
	k8s.io/client-go v0.19.6
	k8s.io/kubectl v0.19.6
	sigs.k8s.io/controller-runtime v0.7.0
)
