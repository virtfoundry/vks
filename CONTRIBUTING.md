# Contributing to VKS

Thank you for helping grow VirtFoundry. This repository lives under the [virtfoundry](https://github.com/virtfoundry) organization.

This repository ships the `VKSCluster` CRD and the controller that provisions managed Kubernetes clusters (Kamaji control plane plus worker Instances) on VirtFoundry.

## Before you start

- Read the project [governance](GOVERNANCE.md) and [code of conduct](CODE_OF_CONDUCT.md)
- Search [existing issues](https://github.com/virtfoundry/vks/issues) before opening a duplicate

## Language

- **Commits**: English only, [Conventional Commits](https://www.conventionalcommits.org/), subject under 72 characters, no trailing period
- **Documentation** and PR descriptions: English

### Commit examples

```
feat(vks): allow MetalLB pool override per VKSCluster
fix(cni): requeue on Forbidden instead of error storm
docs(readme): document the VIP contract
```

## Development setup

```bash
make manifests generate
make test
make lint
```

## Branch workflow

**Do not commit directly to `main`.** Use:

1. Branch from `main`: `feat/<name>`, `fix/<name>`, `docs/<name>`, or `chore/<name>`
2. Open a PR against `main` with a Summary and a Test plan
3. Wait for CI (`make test`, lint) before merge; squash merge after approval

## Reporting security issues

Do not open a public issue. See [SECURITY.md](SECURITY.md).
