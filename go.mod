module github.com/keikoproj/instance-manager

go 1.13

require (
	github.com/aws/aws-sdk-go v1.30.24
	github.com/cucumber/godog v0.8.1
	github.com/ghodss/yaml v1.0.0
	github.com/go-logr/logr v0.1.0
	github.com/keikoproj/aws-auth v0.0.0-20190910182258-c705a9d52f92
	github.com/onsi/gomega v1.9.0
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.4.2
	golang.org/x/crypto v0.0.0-20190820162420-60c769a6c586 // indirect
	golang.org/x/sys v0.0.0-20191026070338-33540a1f6037 // indirect
	gopkg.in/yaml.v2 v2.2.8
	k8s.io/api v0.0.0-20190918155943-95b840bb6a1f
	k8s.io/apimachinery v0.0.0-20190913080033-27d36303b655
	k8s.io/client-go v11.0.0+incompatible
	k8s.io/utils v0.0.0-20191114184206-e782cd3c129f // indirect
	sigs.k8s.io/controller-runtime v0.4.0
)

replace (
	k8s.io/api => k8s.io/api v0.0.0-20190918155943-95b840bb6a1f
	k8s.io/apimachinery => k8s.io/apimachinery v0.0.0-20190913080033-27d36303b655
	k8s.io/client-go => k8s.io/client-go v0.0.0-20190918160344-1fbdaa4c8d90
)
