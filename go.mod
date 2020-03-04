module github.com/keikoproj/instance-manager

go 1.12

require (
	github.com/aws/aws-sdk-go v1.29.14
	github.com/ghodss/yaml v1.0.0
	github.com/go-logr/logr v0.1.0
	github.com/keikoproj/aws-auth v0.0.0-20190910182258-c705a9d52f92
	github.com/onsi/ginkgo v1.8.0
	github.com/onsi/gomega v1.6.0
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.4.2
	golang.org/x/net v0.0.0-20200202094626-16171245cfb2
	gopkg.in/yaml.v2 v2.2.2
	k8s.io/api v0.0.0-20190409021203-6e4e0e4f393b
	k8s.io/apiextensions-apiserver v0.0.0-20190409022649-727a075fdec8
	k8s.io/apimachinery v0.0.0-20190404173353-6a84e37a896d
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	sigs.k8s.io/controller-runtime v0.2.0-beta.2
	sigs.k8s.io/controller-tools v0.2.0-beta.2 // indirect
	sigs.k8s.io/yaml v1.1.0
)
