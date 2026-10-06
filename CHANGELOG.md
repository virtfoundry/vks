# Changelog

All notable changes to the **VirtFoundry VKS operator** are documented here.

Format based on [Keep a Changelog](https://keepachangelog.com/). Versioning aligned with [virtfoundry/helm-charts](https://github.com/virtfoundry/helm-charts/blob/main/docs/project/versioning.md): the chart and image share the product version.

## [Unreleased]

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
