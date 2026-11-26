#!/usr/bin/env bash
# E2E on a THROWAWAY Kind cluster: cert-manager + Kamaji (default DataStore) + the vks
# controller built from this checkout. A VKSCluster with a NodePort control plane must get
# a Ready TenantControlPlane, a kubeconfig Secret and its worker Instance.
# Scope: no KubeVirt, so the worker Instance is created but never becomes a VM.
#
# Env: HELM_CHARTS_DIR (virtfoundry/helm-charts checkout, source of the Instance CRD; required)
#      VKS_IMG         (image already loaded into Kind, default virtfoundry-vks:e2e)
# Safety: refuses any API server that is not on localhost.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

: "${HELM_CHARTS_DIR:?set HELM_CHARTS_DIR to a helm-charts checkout}"
VKS_IMG="${VKS_IMG:-virtfoundry-vks:e2e}"
CERT_MANAGER_VERSION="${CERT_MANAGER_VERSION:-v1.16.2}"
# The published Helm chart caps tenant Kubernetes at 1.30; the edge chart in git does not.
KAMAJI_REF="${KAMAJI_REF:-7a53df4a6db4e7feebe07e9856f3c173731bdfd5}"
KIND_NODE="${KIND_NODE:-e2e-control-plane}"
K8S_VERSION="${K8S_VERSION:-v1.36.5}"
NS=virtfoundry-system
TENANT_NS=virtfoundry-tenant-default
NAME=e2e

server="$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}')"
case "$server" in
  https://127.0.0.1:*|https://localhost:*|https://\[::1\]:*) ;;
  *) echo "REFUSING: API server $server is not local." >&2; exit 2 ;;
esac

ok()   { echo "OK: $*"; }
fail() { echo "FAIL: $*" >&2; diagnostics; exit 1; }
diagnostics() {
  set +e
  echo "::group::diagnostics"
  kubectl get pods -A -o wide
  kubectl get vksclusters,instances -A -o yaml | tail -120
  kubectl get tenantcontrolplanes,datastores -A
  kubectl get events -A --sort-by=.lastTimestamp | tail -40
  kubectl -n "$NS" logs deploy/virtfoundry-vks-controller-manager --tail=100
  kubectl -n kamaji-system logs deploy/kamaji --tail=60
  echo "::endgroup::"
}

# wait_for "<description>" <timeout-seconds> <command...>
wait_for() {
  local what=$1 timeout=$2; shift 2
  local end=$((SECONDS + timeout))
  until "$@" >/dev/null 2>&1; do
    [ "$SECONDS" -lt "$end" ] || fail "timeout waiting for $what"
    sleep 5
  done
  ok "$what"
}
# jp <ns> <kind> <name> <jsonpath>: re-evaluated on every call (use inside wait_for via bash -c)
jp() { kubectl -n "$1" get "$2" "$3" -o jsonpath="$4"; }
export -f jp

export KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}"

echo "==> cert-manager $CERT_MANAGER_VERSION"
kubectl apply -f "https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"
kubectl -n cert-manager wait --for=condition=Available deploy --all --timeout=300s

echo "==> Kamaji edge chart @ ${KAMAJI_REF:0:12}"
KAMAJI_SRC="$(mktemp -d)"
git clone -q https://github.com/clastix/kamaji.git "$KAMAJI_SRC"
git -C "$KAMAJI_SRC" checkout -q "$KAMAJI_REF"
helm dependency update "$KAMAJI_SRC/charts/kamaji" >/dev/null
helm upgrade --install kamaji "$KAMAJI_SRC/charts/kamaji" \
  --namespace kamaji-system --create-namespace --wait --timeout 10m
kubectl apply --server-side --force-conflicts -f "$KAMAJI_SRC/charts/kamaji/crds/"
wait_for "default DataStore" 300 kubectl get datastore default
kubectl get datastore

echo "==> CRDs: Instance (helm-charts) + VKSCluster (this checkout)"
kubectl apply --server-side -f "$HELM_CHARTS_DIR/charts/virtfoundry-crds/manifests/virtfoundry.io_instances.yaml"
kubectl apply --server-side -f config/crd/bases/virtfoundry.io_vksclusters.yaml

echo "==> vks controller"
kubectl create ns "$NS" --dry-run=client -o yaml | kubectl apply -f -
kubectl create ns "$TENANT_NS" --dry-run=client -o yaml | kubectl apply -f -
NODE_IP="$(docker inspect -f '{{ .NetworkSettings.Networks.kind.IPAddress }}' "$KIND_NODE")"
helm upgrade --install virtfoundry-vks charts/virtfoundry-vks --namespace "$NS" \
  --set image.repository="${VKS_IMG%:*}" --set image.tag="${VKS_IMG##*:}" \
  --set image.pullPolicy=Never --set nodeAddress="$NODE_IP" --wait --timeout 5m

echo "==> VKSCluster (NodePort control plane at $NODE_IP:30443)"
kubectl apply -f - <<YAML
apiVersion: virtfoundry.io/v1alpha1
kind: VKSCluster
metadata:
  name: $NAME
  namespace: $TENANT_NS
spec:
  kubernetesVersion: $K8S_VERSION
  controlPlane:
    serviceType: NodePort
    address: $NODE_IP
    port: 30443
  workers:
    count: 1
    templateRef: {name: ubuntu-node-1-36-5}
    offeringRef: {name: medium}
    networkRef: {name: default-default}
YAML

wait_for "TenantControlPlane Ready" 900 bash -c \
  "[ \"\$(kubectl get tcp -A -o jsonpath='{.items[0].status.kubernetesResources.version.status}')\" = Ready ]"
wait_for "controlPlaneEndpoint in status" 120 bash -c \
  "[ -n \"\$(jp $TENANT_NS vksclusters.virtfoundry.io $NAME '{.status.controlPlaneEndpoint}')\" ]"
wait_for "kubeconfig Secret copied to the tenant namespace" 120 \
  kubectl -n "$TENANT_NS" get secret "$NAME-admin-kubeconfig"
wait_for "worker Instance created" 300 kubectl -n "$TENANT_NS" get instances.virtfoundry.io "$NAME-worker-0"
[ "$(jp "$TENANT_NS" instances.virtfoundry.io "$NAME-worker-0" '{.spec.powerState}')" = Running ] \
  || fail "worker Instance powerState is not Running"
ok "worker Instance powerState Running"

echo "==> delete: worker Instance and TenantControlPlane are garbage collected"
kubectl -n "$TENANT_NS" delete vksclusters.virtfoundry.io "$NAME" --wait=false
wait_for "VKSCluster gone" 300 bash -c "! kubectl -n $TENANT_NS get vksclusters.virtfoundry.io $NAME"
[ -z "$(kubectl get tcp -A -o name)" ] || fail "TenantControlPlane left behind"
ok "TenantControlPlane removed"
echo "E2E PASSED"
