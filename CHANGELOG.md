# Change Log

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](http://keepachangelog.com/)
and this project adheres to [Semantic Versioning](http://semver.org/).

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
