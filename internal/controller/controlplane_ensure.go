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
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	virtfoundryv1alpha1 "github.com/virtfoundry/vks/api/v1alpha1"
)

type controlPlaneReady struct {
	endpoint         string
	kubeconfigSecret string
}

// ensureControlPlane creates/updates the Kamaji TCP and waits until Ready (and LB VIP when needed).
// Non-zero result means requeue; nil ready + err is a hard failure.
func (r *VKSClusterReconciler) ensureControlPlane(
	ctx context.Context,
	cluster *virtfoundryv1alpha1.VKSCluster,
	tcpNS, tcpName string,
) (*controlPlaneReady, ctrl.Result, error) {
	log := logf.FromContext(ctx)

	serviceType := resolveServiceType(cluster.Spec.ControlPlane)
	port, err := resolveAPIPort(cluster.Spec.ControlPlane, serviceType, r.DefaultNodePort)
	if err != nil {
		_ = r.failControlPlane(ctx, cluster, "InvalidPort", err.Error(), virtfoundryv1alpha1.VKSClusterPhaseFailed)
		return nil, ctrl.Result{}, err
	}

	addr, err := r.resolveControlPlaneAddress(ctx, cluster, serviceType, tcpNS, tcpName)
	if err != nil {
		_ = r.failControlPlane(ctx, cluster, "MissingAddress", err.Error(), virtfoundryv1alpha1.VKSClusterPhaseFailed)
		return nil, ctrl.Result{}, err
	}

	ver := cluster.Spec.KubernetesVersion
	if !strings.HasPrefix(ver, "v") {
		ver = "v" + ver
	}

	tcp := &unstructured.Unstructured{}
	tcp.SetGroupVersionKind(tcpGVK)
	tcp.SetNamespace(tcpNS)
	tcp.SetName(tcpName)
	konnectivityPort := konnectivityServerPort(serviceType)

	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, tcp, func() error {
		return mutateTenantControlPlane(tcp, cluster, r.Scheme, serviceType, ver, addr, port, konnectivityPort)
	})
	if err != nil {
		log.Error(err, "ensure TenantControlPlane")
		return nil, ctrl.Result{}, err
	}

	if serviceType == serviceTypeLoadBalancer && addr == "" {
		result, err := r.waitLoadBalancerVIP(ctx, cluster, tcp, tcpNS, tcpName)
		if err != nil || !result.IsZero() {
			return nil, result, err
		}
	}

	if err := r.Get(ctx, types.NamespacedName{Namespace: tcpNS, Name: tcpName}, tcp); err != nil {
		return nil, ctrl.Result{}, err
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
		r.setPhase(cluster, virtfoundryv1alpha1.VKSClusterPhaseProvisioning)
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:    virtfoundryv1alpha1.ConditionControlPlaneReady,
			Status:  metav1.ConditionFalse,
			Reason:  "WaitingTCP",
			Message: fmt.Sprintf("TenantControlPlane %s/%s status=%q", tcpNS, tcpName, tcpStatus),
		})
		if err := r.Status().Update(ctx, cluster); err != nil {
			return nil, ctrl.Result{}, err
		}
		return nil, ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	return &controlPlaneReady{endpoint: endpoint, kubeconfigSecret: kcSecret}, ctrl.Result{}, nil
}

func (r *VKSClusterReconciler) failControlPlane(
	ctx context.Context,
	cluster *virtfoundryv1alpha1.VKSCluster,
	reason, message string,
	phase virtfoundryv1alpha1.VKSClusterPhase,
) error {
	r.setPhase(cluster, phase)
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:    virtfoundryv1alpha1.ConditionControlPlaneReady,
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
	return r.Status().Update(ctx, cluster)
}

func (r *VKSClusterReconciler) resolveControlPlaneAddress(
	ctx context.Context,
	cluster *virtfoundryv1alpha1.VKSCluster,
	serviceType, tcpNS, tcpName string,
) (string, error) {
	addr := cluster.Spec.ControlPlane.Address
	if serviceType == serviceTypeNodePort {
		if addr == "" {
			addr = r.DefaultNodeAddress
		}
		if addr == "" {
			return "", fmt.Errorf("spec.controlPlane.address or controller default --node-address is required for NodePort")
		}
		return addr, nil
	}
	// LoadBalancer: prefer explicit pin; else VIP from Service (no MetalLB annots).
	if addr != "" {
		return addr, nil
	}
	svc := &corev1.Service{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: tcpNS, Name: tcpName}, svc); err == nil {
		return loadBalancerVIP(svc), nil
	}
	return "", nil
}

func (r *VKSClusterReconciler) waitLoadBalancerVIP(
	ctx context.Context,
	cluster *virtfoundryv1alpha1.VKSCluster,
	tcp *unstructured.Unstructured,
	tcpNS, tcpName string,
) (ctrl.Result, error) {
	svc := &corev1.Service{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: tcpNS, Name: tcpName}, svc); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}
	addr := loadBalancerVIP(svc)
	if addr == "" {
		r.setPhase(cluster, virtfoundryv1alpha1.VKSClusterPhaseProvisioning)
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:    virtfoundryv1alpha1.ConditionControlPlaneReady,
			Status:  metav1.ConditionFalse,
			Reason:  "LoadBalancerPending",
			Message: fmt.Sprintf("waiting for LoadBalancer VIP on Service %s/%s", tcpNS, tcpName),
		})
		cluster.Status.TCPNamespace = tcpNS
		cluster.Status.TCPName = tcpName
		cluster.Status.ObservedGeneration = cluster.Generation
		if err := r.Status().Update(ctx, cluster); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	if err := r.Get(ctx, types.NamespacedName{Namespace: tcpNS, Name: tcpName}, tcp); err != nil {
		return ctrl.Result{}, err
	}
	curAddr, _, _ := unstructured.NestedString(tcp.Object, "spec", "networkProfile", "address")
	if curAddr != addr {
		_ = unstructured.SetNestedField(tcp.Object, addr, "spec", "networkProfile", "address")
		if err := r.Update(ctx, tcp); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func mutateTenantControlPlane(
	tcp *unstructured.Unstructured,
	cluster *virtfoundryv1alpha1.VKSCluster,
	scheme *runtime.Scheme,
	serviceType, ver, addr string,
	port, konnectivityPort int32,
) error {
	labels := tcp.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[labelPartOf] = partOfValue
	labels[labelManaged] = cluster.Namespace + "." + cluster.Name
	tcp.SetLabels(labels)

	_ = unstructured.SetNestedField(tcp.Object, defaultName, "spec", "dataStore")
	_ = unstructured.SetNestedField(tcp.Object, int64(1), "spec", "controlPlane", "deployment", "replicas")
	_ = unstructured.SetNestedField(tcp.Object, serviceType, "spec", "controlPlane", "service", "serviceType")
	// Intentionally no metallb.io/* annotations — unset = cluster default LB pool
	_ = unstructured.SetNestedField(tcp.Object, ver, "spec", "kubernetes", "version")
	_ = unstructured.SetNestedStringSlice(tcp.Object, []string{"InternalIP", "ExternalIP", "Hostname"}, "spec", "kubernetes", "kubelet", "preferredAddressTypes")
	_ = unstructured.SetNestedField(tcp.Object, "systemd", "spec", "kubernetes", "kubelet", "cgroupfs")
	if addr != "" {
		_ = unstructured.SetNestedField(tcp.Object, addr, "spec", "networkProfile", "address")
	}
	_ = unstructured.SetNestedField(tcp.Object, int64(port), "spec", "networkProfile", "port")
	_ = unstructured.SetNestedMap(tcp.Object, map[string]any{}, "spec", "addons", "coreDNS")
	_ = unstructured.SetNestedMap(tcp.Object, map[string]any{}, "spec", "addons", "kubeProxy")
	_ = unstructured.SetNestedField(tcp.Object, int64(konnectivityPort), "spec", "addons", "konnectivity", "server", "port")

	if err := controllerutil.SetControllerReference(cluster, tcp, scheme); err != nil {
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
}
