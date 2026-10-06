# Security Policy

## Supported versions

| Version | Supported |
|---------|-----------|
| `main` branch | yes |
| tagged releases | best effort |

## Reporting a vulnerability

**Do not open public GitHub issues for security vulnerabilities.**

Report via a **private GitHub security advisory** on this repository or [virtfoundry/core](https://github.com/virtfoundry/core/security/advisories). Primary contact: **Matheus Thurler** ([@Matheus-Thurler](https://github.com/Matheus-Thurler)) — see [MAINTAINERS.md](MAINTAINERS.md).

Include the affected component (the `VKSCluster` CRD, the controller, and its Helm chart), impact, reproduction steps, and a suggested fix if any.

We aim to acknowledge within **7 days**.

## Secure deployment

- Pin the controller image by digest in production overlays; do not deploy `:latest`.
- The controller reconciles control-plane Services and tenant Instances; report any path that lets one tenant affect another tenant's cluster.
