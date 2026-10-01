# VirtFoundry Kubernetes Service (VKS)

Hosted Kubernetes for VirtFoundry tenants — **EKS-style**: Kamaji control plane on the platform, workers as VirtFoundry Instances (KubeVirt containerDisk).

## Status

Phase 3 (operator façade). Phase 1 image + Phase 2 CAPI/Kamaji spike are done.

| Component | Role |
|-----------|------|
| `VKSCluster` CR | Tenant-facing API (`virtfoundry.io/v1alpha1`) |
| This operator | Ensures Kamaji `TenantControlPlane` + worker `Instance`s + kubeconfig Secret |
| IaaS operator | Turns Instances into KubeVirt VMs |
| Kamaji | Hosted kube-apiserver / etcd |

## Quick start (dev)

```bash
make generate manifests
make test
make run  # needs kubeconfig + Kamaji CRDs; set --node-address=<worker InternalIP>
```

Sample:

```bash
kubectl apply -f config/samples/virtfoundry_v1alpha1_vkscluster.yaml
kubectl get vksc -A
```

## Image

`ghcr.io/virtfoundry/vks` (digest-pinned via Argo after CI).

## License

Apache-2.0
