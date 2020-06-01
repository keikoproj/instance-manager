# Change Log

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](http://keepachangelog.com/)
and this project adheres to [Semantic Versioning](http://semver.org/).

## [v0.7.0-alpha2] - 2020-6-1

### Added

- Event publishing (#110)
- Metrics collection (#111)
- AWS API calls optimization (#123, #124, #126)
- AWS SDK caching (#127)
- Default role labels according to cluster version (#125)
- Node relabling & migration path to Kubernetes 1.16 (#129)

### Fixed

- Update examples to support Kubernetes 1.16 (#121)

### Migration notes

- This version includes support for Kubernetes 1.16, see further instructions in #129

## [v0.6.3-alpha2] - 2020-5-19

### Fixed

- Pagination & Launch config deletion fixes (#114)
- CR Spec validation fixes (#109)

## [v0.6.2-alpha2] - 2020-5-16

### Fixed

- Add retries/logging on AWS throttling (#106)
- Avoid modifications to desired instances (#108)

## [v0.6.1-alpha2] - 2020-5-13

### Fixed

- Bootstrapping of shared roles (#102)

## [v0.6.0-alpha2] - 2020-5-12

### Added

- **eks provisioner v2** (#83)

### Changed

- **eks-cf provisioner deprecated (#94)**
- functional tests improvements (#79)
- Removed vended code (#91)
- Use golang 1.13.10 & update SDKs (#90)

### Fixed

- General fixes, refactor, and code improvements (#93, #92, #75, #80)
- Documentation improvements (#96)

### BREAKING CHANGES

If you are migrating from 0.5.0 and lower, you MUST delete all instance groups, update CRD, RBAC and controller, and re-create your instance groups using the new `eks` API. make sure to review the new API spec [here](https://github.com/keikoproj/instance-manager#eks-sample-spec-alpha-2).

## [v0.5.0-alpha] - 2019-3-03

### Added

- Support for EKS managed node groups (#76)

### Changed

- Use latest aws-sdk (#67, #76)

### Fixed

- Documentation fixes (#74)

## [v0.4.2-alpha] - 2019-12-07

### Fixed

- Support scenarios where existing IAM role is different than existing instance profile (#64)

## [v0.4.1-alpha] - 2019-11-07

### Added

- Existing IAM role support (#62)

### Fixed

- Managed policy prefix including account id (#61)

## [v0.4.0-alpha] - 2019-10-30

### Added

- Basic spot-instance support (#36, #41)
- Additional options for rollingUpdate (#44)
- CFN stack prefixes (#46)
- Managed policy support (#55)
- MetricsCollection support (#57)

### Changed

- Switch to aws-auth library (#40, #43)
- Improve reconcile of error states (#42)

### Fixed

- Fix rollingUpdate defaults (#59)

## [v0.3.2-alpha] - 2019-08-28

### Changed

- Changed org name and all references
- Added tagging for KubernetesCluster

## [v0.3.0-alpha] - 2019-08-13

### Added

- CI integration & enhancments (#3, #4, #11, #12)
- CRD strategy concurrency (#6)

## [v0.2.0-alpha] - 2019-08-08

- Initial alpha release of instance-manager


## [v0.3.1-alpha] - 2019-08-20

### Fixed

- Bugfix: CRD strategy concurrency fix (#20)
- Bugfix: better management of aws-auth configmap (#23)

## [v0.3.0-alpha] - 2019-08-13

### Added

- CI integration & enhancments (#3, #4, #11, #12)
- CRD strategy concurrency (#6)

## [v0.2.0-alpha] - 2019-08-08

- Initial alpha release of instance-manager
