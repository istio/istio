/*

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
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ToySpec defines the desired state of Toy
type ToySpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:ExclusiveMinimum=true
	Power float32 `json:"power,omitempty"`

	Bricks int32 `json:"bricks,omitempty"`

	// +kubebuilder:validation:MaxLength=15
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name,omitempty"`

	// This is a comment on an array field.
	// +kubebuilder:validation:MaxItems=500
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:UniqueItems=false
	Knights []string `json:"knights,omitempty"`

	// This is a comment on a boolean field.
	Winner bool `json:"winner,omitempty"`

	// +kubebuilder:validation:Enum=Lion,Wolf,Dragon
	Alias string `json:"alias,omitempty"`

	// +kubebuilder:validation:Enum=1,2,3
	Rank int `json:"rank"`

	Comment []byte `json:"comment,omitempty"`

	// This is a comment on an object field.
	Template v1.PodTemplateSpec `json:"template"`

	Claim v1.PersistentVolumeClaim `json:"claim,omitempty"`

	//This is a dummy comment.
	// Just checking if the multi-line comments are working or not.
	Replicas *int32 `json:"replicas"`

	// This is a newly added field.
	// Using this for testing purpose.
	Rook *intstr.IntOrString `json:"rook"`

	// This is a comment on a map field.
	Location map[string]string `json:"location"`
}

// ToyStatus defines the observed state of Toy
type ToyStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// It tracks the number of replicas.
	Replicas int32 `json:"replicas"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Toy is the Schema for the toys API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=
// +kubebuilder:printcolumn:name="toy",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description="descr1",format="date",priority=3
// +kubebuilder:printcolumn:name="abc",type="integer",JSONPath="status",description="descr2",format="int32",priority=1
// +kubebuilder:printcolumn:name="service",type="string",JSONPath=".status.conditions.ready",description="descr3",format="byte",priority=2
// +kubebuilder:resource:path=services,shortName=ty
type Toy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ToySpec   `json:"spec,omitempty"`
	Status ToyStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ToyList contains a list of Toy
type ToyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Toy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Toy{}, &ToyList{})
}
