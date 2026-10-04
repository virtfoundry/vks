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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	virtfoundryv1alpha1 "github.com/virtfoundry/vks/api/v1alpha1"
)

const (
	testK8sVersion = "v1.36.5"
	testPoolMgmt   = "homelab-mgmt"
	testPoolPublic = "homelab-public"
)

func TestResolveServiceType(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{"", serviceTypeLoadBalancer},
		{serviceTypeLoadBalancer, serviceTypeLoadBalancer},
		{serviceTypeNodePort, serviceTypeNodePort},
		{"ClusterIP", serviceTypeLoadBalancer},
	}
	for _, tc := range cases {
		got := resolveServiceType(virtfoundryv1alpha1.VKSControlPlaneSpec{ServiceType: tc.in})
		if got != tc.want {
			t.Fatalf("serviceType %q: got %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolveAPIPort(t *testing.T) {
	t.Parallel()
	port, err := resolveAPIPort(virtfoundryv1alpha1.VKSControlPlaneSpec{}, serviceTypeLoadBalancer, 30443)
	if err != nil || port != 443 {
		t.Fatalf("LB default: port=%d err=%v", port, err)
	}
	port, err = resolveAPIPort(virtfoundryv1alpha1.VKSControlPlaneSpec{}, serviceTypeNodePort, 0)
	if err != nil || port != 30443 {
		t.Fatalf("NodePort default: port=%d err=%v", port, err)
	}
	port, err = resolveAPIPort(virtfoundryv1alpha1.VKSControlPlaneSpec{Port: 30555}, serviceTypeNodePort, 30443)
	if err != nil || port != 30555 {
		t.Fatalf("NodePort explicit: port=%d err=%v", port, err)
	}
	_, err = resolveAPIPort(virtfoundryv1alpha1.VKSControlPlaneSpec{Port: 443}, serviceTypeNodePort, 30443)
	if err == nil {
		t.Fatal("expected error for NodePort 443")
	}
	port, err = resolveAPIPort(virtfoundryv1alpha1.VKSControlPlaneSpec{Port: 6443}, serviceTypeLoadBalancer, 30443)
	if err != nil || port != 6443 {
		t.Fatalf("LB explicit: port=%d err=%v", port, err)
	}
}

func TestKonnectivityServerPort(t *testing.T) {
	t.Parallel()
	if p := konnectivityServerPort(serviceTypeLoadBalancer); p != 8132 {
		t.Fatalf("LB konnectivity=%d", p)
	}
	if p := konnectivityServerPort(serviceTypeNodePort); p != 30132 {
		t.Fatalf("NodePort konnectivity=%d", p)
	}
}

func TestLoadBalancerVIP(t *testing.T) {
	t.Parallel()
	if loadBalancerVIP(nil) != "" {
		t.Fatal("nil service")
	}
	svc := &corev1.Service{}
	if loadBalancerVIP(svc) != "" {
		t.Fatal("empty ingress")
	}
	svc.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{IP: "10.0.50.104"}}
	if got := loadBalancerVIP(svc); got != "10.0.50.104" {
		t.Fatalf("got %q", got)
	}
	svc.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{Hostname: "vip.example"}}
	if got := loadBalancerVIP(svc); got != "vip.example" {
		t.Fatalf("got %q", got)
	}
}

func TestMutateTenantControlPlane_LBDoesNotAllocateNodePorts(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	if err := virtfoundryv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	cluster := &virtfoundryv1alpha1.VKSCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "tenant-a"},
		Spec:       virtfoundryv1alpha1.VKSClusterSpec{KubernetesVersion: testK8sVersion},
	}
	tcp := &unstructured.Unstructured{Object: map[string]any{}}
	tcp.SetGroupVersionKind(tcpGVK)
	tcp.SetName("demo")
	tcp.SetNamespace("kamaji")

	if err := mutateTenantControlPlane(tcp, cluster, scheme, serviceTypeLoadBalancer, testK8sVersion, "10.0.50.110", 443, 8132, ""); err != nil {
		t.Fatal(err)
	}
	alloc, found, err := unstructured.NestedBool(tcp.Object, "spec", "controlPlane", "service", "allocateLoadBalancerNodePorts")
	if err != nil || !found || alloc {
		t.Fatalf("allocateLoadBalancerNodePorts=%v found=%v err=%v", alloc, found, err)
	}
	st, _, _ := unstructured.NestedString(tcp.Object, "spec", "controlPlane", "service", "serviceType")
	if st != serviceTypeLoadBalancer {
		t.Fatalf("serviceType=%q", st)
	}

	tcpNP := &unstructured.Unstructured{Object: map[string]any{}}
	tcpNP.SetGroupVersionKind(tcpGVK)
	if err := mutateTenantControlPlane(tcpNP, cluster, scheme, serviceTypeNodePort, testK8sVersion, "10.0.30.250", 30443, 30132, ""); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := unstructured.NestedBool(tcpNP.Object, "spec", "controlPlane", "service", "allocateLoadBalancerNodePorts"); found {
		t.Fatal("NodePort must not set allocateLoadBalancerNodePorts (Kamaji CEL)")
	}
}

func TestResolveAddressPool(t *testing.T) {
	t.Parallel()
	cases := []struct {
		cr, flag, want string
	}{
		{"", "", ""},
		{"", testPoolMgmt, testPoolMgmt},
		{testPoolPublic, testPoolMgmt, testPoolPublic},
		{"  " + testPoolPublic + "  ", testPoolMgmt, testPoolPublic},
	}
	for _, tc := range cases {
		got := resolveAddressPool(virtfoundryv1alpha1.VKSControlPlaneSpec{AddressPool: tc.cr}, tc.flag)
		if got != tc.want {
			t.Fatalf("cr=%q flag=%q: got %q want %q", tc.cr, tc.flag, got, tc.want)
		}
	}
}

func TestMutateTenantControlPlane_LBPinsAddressPool(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	if err := virtfoundryv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	cluster := &virtfoundryv1alpha1.VKSCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "tenant-a"},
		Spec:       virtfoundryv1alpha1.VKSClusterSpec{KubernetesVersion: testK8sVersion},
	}
	tcp := &unstructured.Unstructured{Object: map[string]any{}}
	tcp.SetGroupVersionKind(tcpGVK)
	if err := mutateTenantControlPlane(tcp, cluster, scheme, serviceTypeLoadBalancer, testK8sVersion, "", 443, 8132, testPoolPublic); err != nil {
		t.Fatal(err)
	}
	ann, found, err := unstructured.NestedStringMap(tcp.Object, "spec", "controlPlane", "service", "additionalMetadata", "annotations")
	if err != nil || !found || ann["metallb.universe.tf/address-pool"] != testPoolPublic {
		t.Fatalf("pool annotation=%v found=%v err=%v", ann, found, err)
	}

	tcpEmpty := &unstructured.Unstructured{Object: map[string]any{}}
	tcpEmpty.SetGroupVersionKind(tcpGVK)
	if err := mutateTenantControlPlane(tcpEmpty, cluster, scheme, serviceTypeLoadBalancer, testK8sVersion, "", 443, 8132, ""); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := unstructured.NestedStringMap(tcpEmpty.Object, "spec", "controlPlane", "service", "additionalMetadata", "annotations"); found {
		t.Fatal("empty pool must not pin metallb annotation")
	}
}
