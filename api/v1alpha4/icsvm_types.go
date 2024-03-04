/*
Copyright 2024 The Kubernetes Authors.

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

package v1alpha4

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/errors"
)

const (
	// VMFinalizer allows the reconciler to clean up resources associated
	// with a ICSVM before removing it from the API Server.
	VMFinalizer = "icsvm.infrastructure.cluster.x-k8s.io"
)

// ICSVMSpec defines the desired state of ICSVM
type ICSVMSpec struct {
	VirtualMachineCloneSpec `json:",inline"`

	// BootstrapRef is a reference to a bootstrap provider-specific resource
	// that holds configuration details.
	// This field is optional in case no bootstrap data is required to create
	// a VM.
	// +optional
	BootstrapRef *corev1.ObjectReference `json:"bootstrapRef,omitempty"`

	// BiosUUID is the the VM's BIOS UUID that is assigned at runtime after
	// the VM has been created.
	// This field is required at runtime for other controllers that read
	// this CRD as unstructured data.
	// +optional
	BiosUUID string `json:"biosUUID,omitempty"`

	// UID is the the VM's ID that is assigned at runtime after
	// the VM has been created.
	// This field is required at runtime for other controllers that read
	// this CRD as unstructured data.
	// +optional
	UID string `json:"UID,omitempty"`
}

// ICSVMStatus defines the observed state of ICSVM
type ICSVMStatus struct {
	// Host describes the hostname or IP address of the infrastructure host
	// that the ICSVM is residing on.
	// +optional
	Host string `json:"host,omitempty"`

	// Ready is true when the provider resource is ready.
	// This field is required at runtime for other controllers that read
	// this CRD as unstructured data.
	// +optional
	Ready bool `json:"ready,omitempty"`

	// Addresses is a list of the VM's IP addresses.
	// This field is required at runtime for other controllers that read
	// this CRD as unstructured data.
	// +optional
	Addresses []string `json:"addresses,omitempty"`

	// CloneMode is the type of clone operation used to clone this VM. Since
	// LinkedMode is the default but fails gracefully if the source of the
	// clone has no snapshots, this field may be used to determine the actual
	// type of clone operation used to create this VM.
	// +optional
	CloneMode CloneMode `json:"cloneMode,omitempty"`

	// Snapshot is the name of the snapshot from which the VM was cloned if
	// LinkedMode is enabled.
	// +optional
	Snapshot string `json:"snapshot,omitempty"`

	// TaskRef is a managed object reference to a Task related to the machine.
	// This value is set automatically at runtime and should not be set or
	// modified by users.
	// +optional
	TaskRef string `json:"taskRef,omitempty"`

	// Network returns the network status for each of the machine's configured
	// network interfaces.
	// +optional
	Network []NetworkStatus `json:"network,omitempty"`

	// FailureReason will be set in the event that there is a terminal problem
	// reconciling the icsvm and will contain a succinct value suitable
	// for vm interpretation.
	//
	// This field should not be set for transitive errors that a controller
	// faces that are expected to be fixed automatically over
	// time (like service outages), but instead indicate that something is
	// fundamentally wrong with the vm.
	//
	// Any transient errors that occur during the reconciliation of icsvms
	// can be added as events to the icsvm object and/or logged in the
	// controller's output.
	// +optional
	FailureReason *errors.MachineStatusError `json:"failureReason,omitempty"`

	// FailureMessage will be set in the event that there is a terminal problem
	// reconciling the icsvm and will contain a more verbose string suitable
	// for logging and human consumption.
	//
	// This field should not be set for transitive errors that a controller
	// faces that are expected to be fixed automatically over
	// time (like service outages), but instead indicate that something is
	// fundamentally wrong with the vm.
	//
	// Any transient errors that occur during the reconciliation of icsvms
	// can be added as events to the icsvm object and/or logged in the
	// controller's output.
	// +optional
	FailureMessage *string `json:"failureMessage,omitempty"`

	// Conditions defines current service state of the ICSVM.
	// +optional
	Conditions clusterv1.Conditions `json:"conditions,omitempty"`

	// ModuleUUID is the unique identifier for the iCenter cluster module construct
	// which is used to configure anti-affinity. Objects with the same ModuleUUID
	// will be anti-affined, meaning that the iCenter DRS will best effort schedule
	// the VMs on separate hosts.
	// +optional
	ModuleUUID *string `json:"moduleUUID,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=icsvms,scope=Namespaced,categories=cluster-api
// +kubebuilder:subresource:status

// ICSVM is the Schema for the icsvms API
type ICSVM struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ICSVMSpec   `json:"spec,omitempty"`
	Status ICSVMStatus `json:"status,omitempty"`
}

func (r *ICSVM) GetConditions() clusterv1.Conditions {
	return r.Status.Conditions
}

func (r *ICSVM) SetConditions(conditions clusterv1.Conditions) {
	r.Status.Conditions = conditions
}

// +kubebuilder:object:root=true

// ICSVMList contains a list of ICSVM
type ICSVMList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ICSVM `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ICSVM{}, &ICSVMList{})
}
