# Change Log

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](http://keepachangelog.com/)
and this project adheres to [Semantic Versioning](http://semver.org/).
## [v0.15.0-alpha2] - 2023-4-17

* fix: functional test by @vgunapati and @garomonegro in (#383) (#382)
* chore: Update functional-test.yaml ubuntu-20.04 by @garomonegro in (#381)
* fix: Not honoring gp3 volume iops and throughput values by @vgunapati in (#380)
* chore(deps): Bump golang.org/x/net (#377) 
* chore(deps): Bump github.com/prometheus/client_golang (#375)
* fix: Replace ARN format validation procedure by @srosenberg-apptio in (#374)
* fix: remove spot dependency from functional test by @eytan-avisror in (#360)
* feat: add support for container runtime on windows AMIs by @backjo in (#359)
* chore: Add CodeQL analysis by @backjo in (#358)
* feat: use IMDS for autoscaling information by @backjo in (#357)

## [v0.14.1-alpha2] - 2022-4-6
* fix: use non-deprecated CRD api by @backjo in https://github.com/keikoproj/instance-manager/pull/355
* chore: upgrade dependencies by @eytan-avisror in https://github.com/keikoproj/instance-manager/pull/353
* feat: allow resolving EKS AMIs by version/slug for better readability by @backjo in https://github.com/keikoproj/instance-manager/pull/351
* fix: Add override instance type for running instances in MixedInstancePolicy by @eytan-avisror in https://github.com/keikoproj/instance-manager/pull/350

## [v0.14.0-alpha2] - 2022-2-14
* feat: add support for calculating max pods when prefix assignment is … by @backjo in https://github.com/keikoproj/instance-manager/pull/326
* fix: check architecture when considering similar instance types in the same family by @backjo in https://github.com/keikoproj/instance-manager/pull/332
* feat: automated ami upgrades + upgrade locking by @preflightsiren in https://github.com/keikoproj/instance-manager/pull/327
* chore: Add missing bdd templates by @preflightsiren in https://github.com/keikoproj/instance-manager/pull/334
* fix: Stop passing in ClusterCA and Endpoint information for Windows nodes by @backjo in https://github.com/keikoproj/instance-manager/pull/337
* fix: don't set max pods above kubernetes recommended limit by @backjo in https://github.com/keikoproj/instance-manager/pull/342
* chore: update go, improve cross-compilation in docker by @backjo in https://github.com/keikoproj/instance-manager/pull/343
* fix: Deduplicate Subnets by @eytan-avisror in https://github.com/keikoproj/instance-manager/pull/344
* fix: Enforce Restricted Fields usage when no default value by @eytan-avisror in https://github.com/keikoproj/instance-manager/pull/345
* fix: Avoid launch template creation loop when switching from launch config and failing by @sahilbadla in https://github.com/keikoproj/instance-manager/pull/346

## [v0.13.0-alpha2] - 2021-7-21
- feat: add support for specifying container runtime (#323)
- fix: calculate cluster dns correctly for non /16 blocks (#319)
- fix: add throughput parameter for volumes, allow iops to be set for any provisioned iops-supported volume (#316)
- fix: add addl tags for scale to 0 support for windows nodes (#315)
- fix: evaluate controller-added tags (#314)
- feat: Pass API Endpoint & CA Cert directly to bootstrap

## [v0.12.1-alpha2] - 2021-6-4
- fix: Set InstanceTypes before provisioned early exit (#312) 
- refactor: awsprovider file restructure (#296)
- feat: add support for instance metadata options (#305)
- fix: Ignore mixedInstancePolicy if type is LaunchConfig (#303)
- fix: add sorting when resolving securitygroups/subnets (#302)

## [v0.12.0-alpha2] - 2021-5-7

### Added

- Warm Pool Support (#286, #290)

### Fixed

- Fixes for conditionals logic (#287)
- OsFamily validation (#289)

## [v0.11.1-alpha2] - 2021-4-29

### Fixed

- Only set restricted field to default value if none of the conditionals set it (#284)

## [v0.11.0-alpha2] - 2021-4-28

### Fixed

- Functional-test fixes (#272, #276)
- Add build stage to Github Actions (#281)
- Add locking to Namespaces map access (#279)

### Added

- annotation for IRSA enabled nodes (#271)
- add image default label to nodes (#270) 
- prometheus metrics integration (#274)
- conditional default values (#273)

## [v0.10.1-alpha2] - 2021-4-01

### Fixed

- Documentation fixes (#257, #260)
- All logging entries should reference namespaced name (#266)
- Allow kubeconfig chaining in local-mode (#268)
- Add validation for provisioner spec (#267)
- Fix memory leak on caching pagination (#265)

## [v0.10.0-alpha2] - 2021-2-10

### Added

- Launch template placement support (#199)
- Support bootstrap options / maxPods (#216)
- Automatic caluclation of custom networking maxPods (#244)
- Support new volume types (#233)
- Pre/Post bootstrap userdata for Windows Nodes (#220)
- Pre/Post bootstrap userdata for Bottlerocket Nodes (#227)
- ConfigMap Namespace Exclusion (#221)
- Build ARM compatible images (#224)
- Add MaxRetries to CRD Strategy (#249)

### Changes

- Move BDD to Github Actions (#228, #229, #231, #232)
- Readme improvements (#226)
- Update dependency versions (#222)
- Move to Go 1.15 (#246)

### Fixed

- Avoid launch template creation loop (#225)
- Remove confighash on excluded namespace (#236)
- Remove handling of upgrade resource name conflicts (#243, #252)
- Guard cast of launchtemplate with type check (#254)
- BDD cleanup timing (#245)
- Set launch template version after creation (#239)
- Add missing namespace watch permissions (#242)
- Remove whitespace in userdata (#248)

## [v0.9.2-alpha2] - 2020-12-1

### Fixed

- Fix premature upgrade completion (#211)
- LaunchID omitted when switching between configuration types (#208)
- Caching improvements for DescribeInstanceTypes/Offerings (#206)
- NPE when switching between configuration types (#205)

## [v0.9.1-alpha2] - 2020-11-17

### Fixed

- rollingUpdate does not trigger an upgrade when using LaunchTemplate (#200)
- Handle length exceeded error for IAM role creation (#202)

## [v0.9.0-alpha2] - 2020-11-13

### Added

- Launch Template support (#179)
- Support windows and bottlerocket images (#188, #186)
- Cluster Autoscaler configuration support (#183)
- Lifecycle Hooks support (#176)
- Add status fields for Strategy & Provisioner (#171)
- Support additional volume options (#169)
- Support base64 payload in userData (#168)

### Changed

- Make scaling config retention configurable (#172)
- Refactor: scaling configuration abstraction (#165)

## [v0.8.0-alpha2] - 2020-8-7

### Added

- Basic GitOps/Platform Support (#157)
- Suspend Processes (#132, #136)
- "Replace" concurrency policy (#138)
- Configurable AWS API retries (#139)
- UserData support (#146, #160)
- Node Label for Lifecycle State (#152)
- Additional NodeVolume options (#161)
- CRD Validation on Upgrade (#133)

### Changed

- Uniform finalizer (#153)
- Accept "Name" tag for SecurityGroups / Subnets (#159)

### Fixed

- Documentation/RBAC/Logging fixes (#134, #154)
- BDD Fixes (#140, #148)
- Allow upgrade if nodes NotReady (#147)

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

### Upgrade Notes

- This version includes support for Kubernetes 1.16, see further instructions in #129
- RBAC has been added for patching node objects
- This version includes CRD changes

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
