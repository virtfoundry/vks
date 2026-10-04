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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// LocalObjectRef names an object in the same namespace (or cluster-scoped catalog).
type LocalObjectRef struct {
	// Name of the referent.
	Name string `json:"name"`
}

// VKSWorkersSpec describes worker VMs for the tenant cluster.
type VKSWorkersSpec struct {
	// Count of worker Instances to create (MVP: 1–3).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=3
	// +kubebuilder:default=1
	Count int32 `json:"count"`

	// TemplateRef names the VirtFoundry Template (containerDisk node image).
	TemplateRef LocalObjectRef `json:"templateRef"`

	// OfferingRef names the VirtFoundry Offering (CPU/RAM).
	OfferingRef LocalObjectRef `json:"offeringRef"`

	// NetworkRef names the VirtFoundry Network (Multus NAD).
	NetworkRef LocalObjectRef `json:"networkRef"`

	// SSHKeyRefs are injected into worker cloud-init (and merge with join userdata).
	// +optional
	SSHKeyRefs []LocalObjectRef `json:"sshKeyRefs,omitempty"`
}

// VKSControlPlaneSpec configures the hosted Kamaji TenantControlPlane.
type VKSControlPlaneSpec struct {
	// ServiceType for the TCP API Service.
	// LoadBalancer (default): VIP from cluster LB (MetalLB autoAssign when unset — no pool pin).
	// NodePort: lab/dev; requires address (spec or --node-address).
	// +kubebuilder:validation:Enum=LoadBalancer;NodePort
	// +kubebuilder:default=LoadBalancer
	// +optional
	ServiceType string `json:"serviceType,omitempty"`

	// Address advertised on the TCP.
	// NodePort: node InternalIP (empty = controller --node-address).
	// LoadBalancer: omit; operator fills from Service status.loadBalancer.ingress.
	// +optional
	Address string `json:"address,omitempty"`

	// Port for the API Service.
	// LoadBalancer default 443; NodePort default 30443 (must be 30000–32767).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Port int32 `json:"port,omitempty"`

	// AddressPool is a MetalLB IPAddressPool name for LoadBalancer CPs
	// (annotation metallb.universe.tf/address-pool). Overrides the controller
	// --load-balancer-address-pool flag. Empty = flag, then cluster autoAssign.
	// +optional
	AddressPool string `json:"addressPool,omitempty"`
}

// VKSClusterSpec defines the desired state of VKSCluster.
type VKSClusterSpec struct {
	// KubernetesVersion for the TenantControlPlane (e.g. v1.36.5).
	// +kubebuilder:validation:Pattern=`^v?1\.(3[1-9]|[4-9][0-9])\.[0-9]+$`
	KubernetesVersion string `json:"kubernetesVersion"`

	// ControlPlane hosts the Kamaji TCP settings.
	// +optional
	ControlPlane VKSControlPlaneSpec `json:"controlPlane,omitempty"`

	// Workers are VirtFoundry Instances that join the tenant cluster.
	Workers VKSWorkersSpec `json:"workers"`
}

// VKSClusterPhase is a coarse status for UI/CLI.
// +kubebuilder:validation:Enum=Pending;Provisioning;ControlPlaneReady;Ready;Failed;Deleting
type VKSClusterPhase string

const (
	VKSClusterPhasePending           VKSClusterPhase = "Pending"
	VKSClusterPhaseProvisioning      VKSClusterPhase = "Provisioning"
	VKSClusterPhaseControlPlaneReady VKSClusterPhase = "ControlPlaneReady"
	VKSClusterPhaseReady             VKSClusterPhase = "Ready"
	VKSClusterPhaseFailed            VKSClusterPhase = "Failed"
	VKSClusterPhaseDeleting          VKSClusterPhase = "Deleting"
)

// Condition types for VKSCluster.
const (
	ConditionControlPlaneReady = "ControlPlaneReady"
	ConditionWorkersReady      = "WorkersReady"
	ConditionAvailable         = "Available"
)

// VKSClusterStatus defines the observed state of VKSCluster.
type VKSClusterStatus struct {
	// Phase is a high-level lifecycle marker.
	// +optional
	Phase VKSClusterPhase `json:"phase,omitempty"`

	// ControlPlaneEndpoint is host:port for the tenant API.
	// +optional
	ControlPlaneEndpoint string `json:"controlPlaneEndpoint,omitempty"`

	// KubeconfigSecretRef is the Secret (same namespace) with admin kubeconfig key admin.conf.
	// +optional
	KubeconfigSecretRef string `json:"kubeconfigSecretRef,omitempty"`

	// TCPNamespace is the host namespace of the TenantControlPlane.
	// +optional
	TCPNamespace string `json:"tcpNamespace,omitempty"`

	// TCPName is the TenantControlPlane name.
	// +optional
	TCPName string `json:"tcpName,omitempty"`

	// ReadyWorkers is how many workers report Ready in the tenant cluster.
	// +optional
	ReadyWorkers int32 `json:"readyWorkers,omitempty"`

	// ObservedGeneration is the generation last processed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the VKSCluster resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=vksc
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.kubernetesVersion`
// +kubebuilder:printcolumn:name="Endpoint",type=string,JSONPath=`.status.controlPlaneEndpoint`
// +kubebuilder:printcolumn:name="Workers",type=string,JSONPath=`.status.readyWorkers`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// VKSCluster is a managed Kubernetes cluster (Kamaji CP + VirtFoundry worker VMs).
type VKSCluster struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of VKSCluster
	// +required
	Spec VKSClusterSpec `json:"spec"`

	// status defines the observed state of VKSCluster
	// +optional
	Status VKSClusterStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// VKSClusterList contains a list of VKSCluster
type VKSClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []VKSCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &VKSCluster{}, &VKSClusterList{})
		return nil
	})
}
