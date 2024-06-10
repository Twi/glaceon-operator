/*
Copyright 2024.

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
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// MachineProxySpec defines the desired state of MachineProxy
type MachineProxySpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	// Org is the Fly.io organization that this proxy will be associated with
	// +kubebuilder:default=personal
	Org string `json:"org,omitempty"`

	// Region is the Fly.io region that this proxy will be terminated in
	//
	// See https://fly.io/docs/reference/regions/ for the list of available regions
	// +kubebuilder:validation:Required
	Region string `json:"region,omitempty"`

	// Target is the URL to proxy to
	// +kubebuilder:validation:Required
	Target string `json:"target,omitempty"`

	// Port is the port to use inside the kubernetes cluster
	// +kubebuilder:default=80
	Port int32 `json:"port,omitempty"`
}

// MachineProxyStatus defines the observed state of MachineProxy
type MachineProxyStatus struct {
	// Represents the observations of a MachineProxy's current state.
	// MachineProxy.status.conditions.type are: "Available", "Progressing", and "Degraded"
	// MachineProxy.status.conditions.status are one of True, False, Unknown.
	// MachineProxy.status.conditions.reason the value should be a CamelCase string and producers of specific
	// condition types may define expected values and meanings for this field, and whether the values
	// are considered a guaranteed API.
	// MachineProxy.status.conditions.Message is a human readable message indicating details about the transition.
	// For further information see: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// MachineProxy is the Schema for the machineproxies API
type MachineProxy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MachineProxySpec   `json:"spec,omitempty"`
	Status MachineProxyStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// MachineProxyList contains a list of MachineProxy
type MachineProxyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MachineProxy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MachineProxy{}, &MachineProxyList{})
}
