/*
Copyright 2026 The VirtFoundry Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	virtfoundryv1alpha1 "github.com/virtfoundry/vks/api/v1alpha1"
)

const (
	finalizerName = "vks.virtfoundry.io/finalizer"
	labelPartOf   = "app.kubernetes.io/part-of"
	labelManaged  = "vks.virtfoundry.io/cluster"
	labelWorker   = "vks.virtfoundry.io/worker"

	partOfValue = "virtfoundry-vks"
	labelTrue   = "true"
	defaultName = "default"
	fieldName   = "name"
)

var (
	tcpGVK = schema.GroupVersionKind{
		Group:   "kamaji.clastix.io",
		Version: "v1alpha1",
		Kind:    "TenantControlPlane",
	}
	instanceGVK = schema.GroupVersionKind{
		Group:   "virtfoundry.io",
		Version: "v1alpha1",
		Kind:    "Instance",
	}
)

// VKSClusterReconciler reconciles a VKSCluster object.
type VKSClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// DefaultNodeAddress is used when spec.controlPlane.address is empty.
	DefaultNodeAddress string
	// DefaultNodePort is used when spec.controlPlane.port is zero.
	DefaultNodePort int32
}

// +kubebuilder:rbac:groups=virtfoundry.io,resources=vksclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=virtfoundry.io,resources=vksclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=virtfoundry.io,resources=vksclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=virtfoundry.io,resources=instances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kamaji.clastix.io,resources=tenantcontrolplanes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

func (r *VKSClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var cluster virtfoundryv1alpha1.VKSCluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !cluster.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &cluster)
	}

	if !controllerutil.ContainsFinalizer(&cluster, finalizerName) {
		controllerutil.AddFinalizer(&cluster, finalizerName)
		if err := r.Update(ctx, &cluster); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	tcpNS := tcpNamespaceFor(&cluster)
	tcpName := cluster.Name

	if err := r.ensureNamespace(ctx, tcpNS, &cluster); err != nil {
		return ctrl.Result{}, err
	}

	addr := cluster.Spec.ControlPlane.Address
	if addr == "" {
		addr = r.DefaultNodeAddress
	}
	port := cluster.Spec.ControlPlane.Port
	if port == 0 {
		port = r.DefaultNodePort
	}
	if port == 0 {
		port = 30443
	}
	if addr == "" {
		r.setPhase(&cluster, virtfoundryv1alpha1.VKSClusterPhaseFailed)
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:    virtfoundryv1alpha1.ConditionControlPlaneReady,
			Status:  metav1.ConditionFalse,
			Reason:  "MissingAddress",
			Message: "spec.controlPlane.address or controller default --node-address is required for NodePort",
		})
		_ = r.Status().Update(ctx, &cluster)
		return ctrl.Result{}, fmt.Errorf("control plane address not configured")
	}

	ver := cluster.Spec.KubernetesVersion
	if !strings.HasPrefix(ver, "v") {
		ver = "v" + ver
	}

	tcp := &unstructured.Unstructured{}
	tcp.SetGroupVersionKind(tcpGVK)
	tcp.SetNamespace(tcpNS)
	tcp.SetName(tcpName)

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, tcp, func() error {
		labels := tcp.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels[labelPartOf] = partOfValue
		labels[labelManaged] = cluster.Namespace + "." + cluster.Name
		tcp.SetLabels(labels)

		_ = unstructured.SetNestedField(tcp.Object, defaultName, "spec", "dataStore")
		_ = unstructured.SetNestedField(tcp.Object, int64(1), "spec", "controlPlane", "deployment", "replicas")
		_ = unstructured.SetNestedField(tcp.Object, "NodePort", "spec", "controlPlane", "service", "serviceType")
		_ = unstructured.SetNestedField(tcp.Object, ver, "spec", "kubernetes", "version")
		_ = unstructured.SetNestedStringSlice(tcp.Object, []string{"InternalIP", "ExternalIP", "Hostname"}, "spec", "kubernetes", "kubelet", "preferredAddressTypes")
		_ = unstructured.SetNestedField(tcp.Object, "systemd", "spec", "kubernetes", "kubelet", "cgroupfs")
		_ = unstructured.SetNestedField(tcp.Object, addr, "spec", "networkProfile", "address")
		_ = unstructured.SetNestedField(tcp.Object, int64(port), "spec", "networkProfile", "port")
		_ = unstructured.SetNestedMap(tcp.Object, map[string]any{}, "spec", "addons", "coreDNS")
		_ = unstructured.SetNestedMap(tcp.Object, map[string]any{}, "spec", "addons", "kubeProxy")
		// Konnectivity shares the TCP Service; with serviceType=NodePort the
		// port becomes a nodePort and must be in 30000–32767 (not 8132).
		_ = unstructured.SetNestedField(tcp.Object, int64(30132), "spec", "addons", "konnectivity", "server", "port")

		if err := controllerutil.SetControllerReference(&cluster, tcp, r.Scheme); err != nil {
			// Cross-namespace owner refs are not allowed; annotate instead.
			ann := tcp.GetAnnotations()
			if ann == nil {
				ann = map[string]string{}
			}
			ann["vks.virtfoundry.io/owner-namespace"] = cluster.Namespace
			ann["vks.virtfoundry.io/owner-name"] = cluster.Name
			tcp.SetAnnotations(ann)
			tcp.SetOwnerReferences(nil)
		}
		return nil
	})
	if err != nil {
		log.Error(err, "ensure TenantControlPlane")
		return ctrl.Result{}, err
	}

	// Re-get TCP status
	if err := r.Get(ctx, types.NamespacedName{Namespace: tcpNS, Name: tcpName}, tcp); err != nil {
		return ctrl.Result{}, err
	}

	tcpStatus, _, _ := unstructured.NestedString(tcp.Object, "status", "kubernetesResources", "version", "status")
	endpoint, _, _ := unstructured.NestedString(tcp.Object, "status", "controlPlaneEndpoint")
	kcSecret, _, _ := unstructured.NestedString(tcp.Object, "status", "kubeconfig")
	if kcSecret == "" {
		kcSecret = tcpName + "-admin-kubeconfig"
	}

	cluster.Status.TCPNamespace = tcpNS
	cluster.Status.TCPName = tcpName
	cluster.Status.ControlPlaneEndpoint = endpoint
	cluster.Status.ObservedGeneration = cluster.Generation

	if tcpStatus != "Ready" || endpoint == "" {
		r.setPhase(&cluster, virtfoundryv1alpha1.VKSClusterPhaseProvisioning)
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:    virtfoundryv1alpha1.ConditionControlPlaneReady,
			Status:  metav1.ConditionFalse,
			Reason:  "WaitingTCP",
			Message: fmt.Sprintf("TenantControlPlane %s/%s status=%q", tcpNS, tcpName, tcpStatus),
		})
		if err := r.Status().Update(ctx, &cluster); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	if err := r.copyKubeconfig(ctx, &cluster, tcpNS, kcSecret); err != nil {
		log.Error(err, "copy kubeconfig")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, err
	}
	cluster.Status.KubeconfigSecretRef = cluster.Name + "-admin-kubeconfig"

	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:    virtfoundryv1alpha1.ConditionControlPlaneReady,
		Status:  metav1.ConditionTrue,
		Reason:  "TCPReady",
		Message: "TenantControlPlane Ready",
	})
	r.setPhase(&cluster, virtfoundryv1alpha1.VKSClusterPhaseControlPlaneReady)

	readyWorkers, err := r.ensureWorkers(ctx, &cluster, tcpNS, kcSecret, endpoint)
	if err != nil {
		log.Error(err, "ensure workers")
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:    virtfoundryv1alpha1.ConditionWorkersReady,
			Status:  metav1.ConditionFalse,
			Reason:  "WorkerError",
			Message: err.Error(),
		})
		_ = r.Status().Update(ctx, &cluster)
		return ctrl.Result{RequeueAfter: 15 * time.Second}, err
	}
	cluster.Status.ReadyWorkers = readyWorkers

	desired := cluster.Spec.Workers.Count
	if readyWorkers >= desired {
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:    virtfoundryv1alpha1.ConditionWorkersReady,
			Status:  metav1.ConditionTrue,
			Reason:  "WorkersReady",
			Message: fmt.Sprintf("%d/%d workers Ready", readyWorkers, desired),
		})
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:    virtfoundryv1alpha1.ConditionAvailable,
			Status:  metav1.ConditionTrue,
			Reason:  "Ready",
			Message: "control plane and workers ready",
		})
		r.setPhase(&cluster, virtfoundryv1alpha1.VKSClusterPhaseReady)
	} else {
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:    virtfoundryv1alpha1.ConditionWorkersReady,
			Status:  metav1.ConditionFalse,
			Reason:  "WaitingWorkers",
			Message: fmt.Sprintf("%d/%d workers Ready", readyWorkers, desired),
		})
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:    virtfoundryv1alpha1.ConditionAvailable,
			Status:  metav1.ConditionFalse,
			Reason:  "Provisioning",
			Message: "waiting for workers to join",
		})
	}

	if err := r.Status().Update(ctx, &cluster); err != nil {
		return ctrl.Result{}, err
	}
	if cluster.Status.Phase != virtfoundryv1alpha1.VKSClusterPhaseReady {
		return ctrl.Result{RequeueAfter: 20 * time.Second}, nil
	}
	return ctrl.Result{RequeueAfter: 2 * time.Minute}, nil
}

func (r *VKSClusterReconciler) reconcileDelete(ctx context.Context, cluster *virtfoundryv1alpha1.VKSCluster) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	r.setPhase(cluster, virtfoundryv1alpha1.VKSClusterPhaseDeleting)
	_ = r.Status().Update(ctx, cluster)

	// Delete worker Instances first.
	var list unstructured.UnstructuredList
	list.SetGroupVersionKind(schema.GroupVersionKind{Group: instanceGVK.Group, Version: instanceGVK.Version, Kind: instanceGVK.Kind + "List"})
	if err := r.List(ctx, &list, client.InNamespace(cluster.Namespace), client.MatchingLabels{
		labelManaged: cluster.Name,
		labelWorker:  labelTrue,
	}); err != nil {
		return ctrl.Result{}, err
	}
	for i := range list.Items {
		if err := r.Delete(ctx, &list.Items[i]); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}
	if len(list.Items) > 0 {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	tcpNS := cluster.Status.TCPNamespace
	if tcpNS == "" {
		tcpNS = tcpNamespaceFor(cluster)
	}
	tcpName := cluster.Status.TCPName
	if tcpName == "" {
		tcpName = cluster.Name
	}
	tcp := &unstructured.Unstructured{}
	tcp.SetGroupVersionKind(tcpGVK)
	tcp.SetNamespace(tcpNS)
	tcp.SetName(tcpName)
	if err := r.Delete(ctx, tcp); err != nil && !apierrors.IsNotFound(err) {
		log.Error(err, "delete TCP")
		return ctrl.Result{}, err
	}

	kc := &corev1.Secret{}
	kc.Name = cluster.Name + "-admin-kubeconfig"
	kc.Namespace = cluster.Namespace
	_ = r.Delete(ctx, kc)

	controllerutil.RemoveFinalizer(cluster, finalizerName)
	if err := r.Update(ctx, cluster); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *VKSClusterReconciler) ensureNamespace(ctx context.Context, name string, cluster *virtfoundryv1alpha1.VKSCluster) error {
	ns := &corev1.Namespace{}
	err := r.Get(ctx, types.NamespacedName{Name: name}, ns)
	if apierrors.IsNotFound(err) {
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
				Labels: map[string]string{
					labelPartOf:  partOfValue,
					labelManaged: cluster.Namespace + "." + cluster.Name,
				},
			},
		}
		return r.Create(ctx, ns)
	}
	return err
}

func (r *VKSClusterReconciler) copyKubeconfig(ctx context.Context, cluster *virtfoundryv1alpha1.VKSCluster, tcpNS, srcSecret string) error {
	src := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: tcpNS, Name: srcSecret}, src); err != nil {
		return err
	}
	dst := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name + "-admin-kubeconfig",
			Namespace: cluster.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, dst, func() error {
		dst.Labels = map[string]string{
			labelPartOf:  partOfValue,
			labelManaged: cluster.Name,
		}
		dst.Type = src.Type
		dst.Data = src.Data
		return controllerutil.SetControllerReference(cluster, dst, r.Scheme)
	})
	return err
}

func (r *VKSClusterReconciler) ensureWorkers(ctx context.Context, cluster *virtfoundryv1alpha1.VKSCluster, tcpNS, tcpKCSecret, endpoint string) (int32, error) {
	joinCmd, caHash, err := r.joinMaterial(ctx, cluster, tcpNS, tcpKCSecret, endpoint)
	if err != nil {
		return 0, err
	}

	desired := int(cluster.Spec.Workers.Count)
	for i := range desired {
		name := fmt.Sprintf("%s-worker-%d", cluster.Name, i)
		userData := cloudInitJoin(joinCmd)
		inst := &unstructured.Unstructured{}
		inst.SetGroupVersionKind(instanceGVK)
		inst.SetNamespace(cluster.Namespace)
		inst.SetName(name)

		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, inst, func() error {
			labels := inst.GetLabels()
			if labels == nil {
				labels = map[string]string{}
			}
			labels[labelPartOf] = partOfValue
			labels[labelManaged] = cluster.Name
			labels[labelWorker] = labelTrue
			inst.SetLabels(labels)

			_ = unstructured.SetNestedField(inst.Object, fmt.Sprintf("VKS %s worker %d", cluster.Name, i), "spec", "displayName")
			_ = unstructured.SetNestedField(inst.Object, cluster.Spec.Workers.TemplateRef.Name, "spec", "templateRef", fieldName)
			_ = unstructured.SetNestedField(inst.Object, cluster.Spec.Workers.OfferingRef.Name, "spec", "offeringRef", fieldName)
			_ = unstructured.SetNestedField(inst.Object, "Running", "spec", "powerState")
			// Only set join userdata on create — mutating cloud-init on Running VMs causes churn.
			existingUD, _, _ := unstructured.NestedString(inst.Object, "spec", "cloudInitUserData")
			if existingUD == "" {
				_ = unstructured.SetNestedField(inst.Object, userData, "spec", "cloudInitUserData")
			}

			nics := []any{
				map[string]any{
					fieldName: defaultName,
					"networkRef": map[string]any{
						fieldName: cluster.Spec.Workers.NetworkRef.Name,
					},
				},
			}
			_ = unstructured.SetNestedSlice(inst.Object, nics, "spec", "nics")

			if len(cluster.Spec.Workers.SSHKeyRefs) > 0 {
				refs := make([]any, 0, len(cluster.Spec.Workers.SSHKeyRefs))
				for _, ref := range cluster.Spec.Workers.SSHKeyRefs {
					refs = append(refs, map[string]any{fieldName: ref.Name})
				}
				_ = unstructured.SetNestedSlice(inst.Object, refs, "spec", "sshKeyRefs")
			}

			ann := inst.GetAnnotations()
			if ann == nil {
				ann = map[string]string{}
			}
			ann["vks.virtfoundry.io/join-ca-hash"] = caHash
			inst.SetAnnotations(ann)

			return controllerutil.SetControllerReference(cluster, inst, r.Scheme)
		})
		if err != nil {
			return 0, err
		}
	}

	return r.countReadyWorkers(ctx, tcpNS, tcpKCSecret)
}

func (r *VKSClusterReconciler) joinMaterial(ctx context.Context, cluster *virtfoundryv1alpha1.VKSCluster, tcpNS, tcpKCSecret, endpoint string) (joinCmd, caHash string, err error) {
	stored := &corev1.Secret{}
	err = r.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name + "-join"}, stored)
	if err == nil && len(stored.Data["join-command"]) > 0 && len(stored.Data["ca-hash"]) > 0 {
		return string(stored.Data["join-command"]), string(stored.Data["ca-hash"]), nil
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return "", "", err
	}

	joinCmd, caHash, err = r.buildJoinMaterial(ctx, tcpNS, tcpKCSecret, endpoint)
	if err != nil {
		return "", "", err
	}
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name + "-join",
			Namespace: cluster.Namespace,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, sec, func() error {
		sec.Labels = map[string]string{labelPartOf: partOfValue, labelManaged: cluster.Name}
		sec.Type = corev1.SecretTypeOpaque
		sec.Data = map[string][]byte{
			"join-command": []byte(joinCmd),
			"ca-hash":      []byte(caHash),
		}
		return controllerutil.SetControllerReference(cluster, sec, r.Scheme)
	})
	return joinCmd, caHash, err
}

func (r *VKSClusterReconciler) buildJoinMaterial(ctx context.Context, tcpNS, tcpKCSecret, endpoint string) (joinCmd, caHash string, err error) {
	src := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: tcpNS, Name: tcpKCSecret}, src); err != nil {
		return "", "", err
	}
	raw, ok := src.Data["admin.conf"]
	if !ok || len(raw) == 0 {
		return "", "", fmt.Errorf("admin.conf missing in %s/%s", tcpNS, tcpKCSecret)
	}

	restCfg, err := clientcmd.RESTConfigFromKubeConfig(raw)
	if err != nil {
		return "", "", err
	}
	// Prefer insecure for NodePort SANs quirks during bootstrap token create from host.
	restCfg.Insecure = true
	restCfg.CAData = nil
	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return "", "", err
	}

	caPEM := src.Data["ca.crt"]
	if len(caPEM) == 0 {
		// Fall back to TCP CA secret.
		caSec := &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: tcpNS, Name: strings.TrimSuffix(tcpKCSecret, "-admin-kubeconfig") + "-ca"}, caSec); err == nil {
			caPEM = caSec.Data["ca.crt"]
		}
	}
	caHash, err = caCertHash(caPEM)
	if err != nil {
		return "", "", err
	}

	tokenID, err := randomHex(3)
	if err != nil {
		return "", "", err
	}
	tokenSecret, err := randomHex(8)
	if err != nil {
		return "", "", err
	}
	token := tokenID + "." + tokenSecret
	exp := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)

	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bootstrap-token-" + tokenID,
			Namespace: "kube-system",
		},
		Type: "bootstrap.kubernetes.io/token",
		StringData: map[string]string{
			"description":                    "VKS worker join",
			"token-id":                       tokenID,
			"token-secret":                   tokenSecret,
			"expiration":                     exp,
			"usage-bootstrap-authentication": "true",
			"usage-bootstrap-signing":        "true",
			"auth-extra-groups":              "system:bootstrappers:kubeadm:default-node-token",
		},
	}
	_, err = cs.CoreV1().Secrets("kube-system").Create(ctx, sec, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return "", "", err
	}

	hostPort := endpoint
	if hostPort == "" {
		return "", "", fmt.Errorf("empty control plane endpoint")
	}
	joinCmd = fmt.Sprintf("kubeadm join %s --token %s --discovery-token-ca-cert-hash sha256:%s", hostPort, token, caHash)
	return joinCmd, caHash, nil
}

func (r *VKSClusterReconciler) countReadyWorkers(ctx context.Context, tcpNS, tcpKCSecret string) (int32, error) {
	src := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: tcpNS, Name: tcpKCSecret}, src); err != nil {
		return 0, err
	}
	raw := src.Data["admin.conf"]
	restCfg, err := clientcmd.RESTConfigFromKubeConfig(raw)
	if err != nil {
		return 0, err
	}
	restCfg.Insecure = true
	restCfg.CAData = nil
	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return 0, err
	}
	nodes, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, err
	}
	var ready int32
	for _, n := range nodes.Items {
		for _, c := range n.Status.Conditions {
			if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
				ready++
				break
			}
		}
	}
	return ready, nil
}

func cloudInitJoin(joinCmd string) string {
	return fmt.Sprintf(`#cloud-config
package_update: false
runcmd:
  - [ bash, -lc, "until command -v kubeadm >/dev/null; do sleep 2; done; %s" ]
`, joinCmd)
}

func tcpNamespaceFor(cluster *virtfoundryv1alpha1.VKSCluster) string {
	// Host namespace: vks-<tenant>-<name> truncated to 63.
	tenant := cluster.Namespace
	tenant = strings.TrimPrefix(tenant, "virtfoundry-tenant-")
	ns := fmt.Sprintf("vks-%s-%s", tenant, cluster.Name)
	ns = strings.ToLower(ns)
	ns = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, ns)
	if len(ns) > 63 {
		ns = ns[:63]
	}
	return strings.Trim(ns, "-")
}

func (r *VKSClusterReconciler) setPhase(cluster *virtfoundryv1alpha1.VKSCluster, phase virtfoundryv1alpha1.VKSClusterPhase) {
	cluster.Status.Phase = phase
}

func caCertHash(pemBytes []byte) (string, error) {
	if len(pemBytes) == 0 {
		return "", fmt.Errorf("empty CA certificate")
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return "", fmt.Errorf("failed to decode CA PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", err
	}
	der, err := x509.MarshalPKIXPublicKey(cert.PublicKey)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:]), nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VKSClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&virtfoundryv1alpha1.VKSCluster{}).
		Named("vkscluster").
		Complete(r)
}
