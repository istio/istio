package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// IstioControlPlaneSpec defines the desired state of IstioControlPlane
// +k8s:openapi-gen=true
type IstioControlPlaneSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book.kubebuilder.io/beyond_basics/generating_crd.html
}

// IstioControlPlaneStatus defines the observed state of IstioControlPlane
// +k8s:openapi-gen=true
type IstioControlPlaneStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book.kubebuilder.io/beyond_basics/generating_crd.html
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// IstioControlPlane is the Schema for the istiocontrolplanes API
// +k8s:openapi-gen=true
type IstioControlPlane struct {
	Spec   IstioControlPlaneSpec   `json:"spec,omitempty"`
	Status IstioControlPlaneStatus `json:"status,omitempty"`

	metav1.ObjectMeta `json:"metadata,omitempty"`
	metav1.TypeMeta   `json:",inline"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// IstioControlPlaneList contains a list of IstioControlPlane
type IstioControlPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IstioControlPlane `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IstioControlPlane{}, &IstioControlPlaneList{})
}
