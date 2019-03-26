package v1alpha3

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ServiceEntry is a specification for a ServiceEntry resource
type ServiceEntry struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Status ServiceEntryStatus `json:"status,omitempty"`

	Spec ServiceEntrySpec `json:"spec"`
}

// ServiceEntrySpec is the spec for a ServiceEntry resource
type ServiceEntrySpec struct {
	Hosts          []string    `json:"hosts,omitempty"`
	Addresses      []string    `json:"addresses,omitempty"`
	Ports          []*Port     `json:"ports,omitempty"`
	Location       string      `json:"location,omitempty"`
	Resolution     string      `json:"resolution,omitempty"`
	Endpoints      []*Endpoint `json:"endpoints,omitempty"`
	ExportTo       []string    `json:"exportTo,omitempty"`
	SubjectAltName []string    `json:"subject_alt_names,omitempty"`
}

// Port is the spec for a Port resource
type Port struct {
	Number   uint32 `json:"number"`
	Protocol string `json:"protocol"`
	Name     string `json:"name,omitempty"`
}

// Endpoint is the spec for a Endpoint resource
type Endpoint struct {
	Address  string            `json:"address"`
	Ports    map[string]uint32 `json:"ports,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
	Network  string            `json:"network"`
	Locality string            `json:"locality"`
	Weight   uint32            `json:"weight"`
}

// custom status
type ServiceEntryStatus struct {
	Name string
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// no client needed for list as it's been created in above
type ServiceEntryList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `son:"metadata,omitempty"`

	Items []ServiceEntry `json:"items"`
}
