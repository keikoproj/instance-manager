# Change Log

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](http://keepachangelog.com/)
and this project adheres to [Semantic Versioning](http://semver.org/).

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
