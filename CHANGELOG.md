# Changelog

All notable changes to the **VirtFoundry VKS operator** are documented here.

Format based on [Keep a Changelog](https://keepachangelog.com/). Versioning aligned with [virtfoundry/helm-charts](https://github.com/virtfoundry/helm-charts/blob/main/docs/project/versioning.md): the chart and image share the product version.

## [Unreleased]

## [0.11.2] - 2026-10-07

### Changed

- Chart / image pins aligned with core **0.11.2**. No code changes.

## [0.11.1] - 2026-10-07

### Changed

- Dependency updates: Go modules, builder image Go 1.27, GitHub Actions.
- Chart / image pins aligned with core **0.11.1**.

## [0.11.0] - 2026-10-07

### Changed

- **Breaking:** the `virtfoundry-vks` chart no longer ships the `VKSCluster` CRD. It moves to the `virtfoundry-crds` chart in [helm-charts](https://github.com/virtfoundry/helm-charts), which syncs it from `config/crd/bases` (the source of truth, unchanged). Install `virtfoundry-crds` first. See [CRDs and upgrades](https://virtfoundry.github.io/helm-charts/docs/guide/crds/).

### Added

- Release workflow publishes the `virtfoundry-vks` chart as an OCI artifact (`oci://ghcr.io/virtfoundry/charts`) on tags, so the platform umbrella chart can depend on it.

## [0.10.0] - 2026-10-06

First tagged release. The chart version moves from the untagged `0.1.0` to the product line so it matches core and operator.

### Added

- `VKSCluster` CRD (`virtfoundry.io/v1alpha1`) and operator: Kamaji `TenantControlPlane`, worker `Instance`s and kubeconfig Secret.
- Control plane defaults to `LoadBalancer`; `spec.controlPlane.addressPool` overrides the global `--load-balancer-address-pool` per cluster.
- Chart `virtfoundry-vks` (CRD, RBAC, Deployment).

### Fixed

- No NodePorts allocated for `LoadBalancer` control planes.
- CNI reconcile requeues on `Forbidden` instead of an error storm.
- Konnectivity port uses a NodePort-valid value.

### Security

- Container images signed with cosign keyless; CodeQL, Scorecard, Dependabot (grouped monthly) and dependency review.
