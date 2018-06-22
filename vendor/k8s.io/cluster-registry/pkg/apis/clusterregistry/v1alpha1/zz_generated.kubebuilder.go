/*
Copyright YEAR The Kubernetes Authors.

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
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// SchemeGroupVersion is group version used to register these objects
var SchemeGroupVersion = schema.GroupVersion{Group: "clusterregistry.k8s.io", Version: "v1alpha1"}

// Kind takes an unqualified kind and returns back a Group qualified GroupKind
func Kind(kind string) schema.GroupKind {
	return SchemeGroupVersion.WithKind(kind).GroupKind()
}

// Resource takes an unqualified resource and returns a Group qualified GroupResource
func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}

var (
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)
	AddToScheme   = SchemeBuilder.AddToScheme
)

// Adds the list of known types to Scheme.
func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&Cluster{},
		&ClusterList{},
	)
	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
	return nil
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cluster `json:"items"`
}

// CRD Generation
func getFloat(f float64) *float64 {
	return &f
}

func getInt(i int64) *int64 {
	return &i
}

var (
	// Define CRDs for resources
	ClusterCRD = v1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "clusters.clusterregistry.k8s.io",
		},
		Spec: v1beta1.CustomResourceDefinitionSpec{
			Group:   "clusterregistry.k8s.io",
			Version: "v1alpha1",
			Names: v1beta1.CustomResourceDefinitionNames{
				Kind:   "Cluster",
				Plural: "clusters",
			},
			Scope: "Namespaced",
			Validation: &v1beta1.CustomResourceValidation{
				OpenAPIV3Schema: &v1beta1.JSONSchemaProps{
					Type: "object",
					Properties: map[string]v1beta1.JSONSchemaProps{
						"apiVersion": {
							Type: "string",
						},
						"kind": {
							Type: "string",
						},
						"metadata": {
							Type: "object",
						},
						"spec": {
							Type: "object",
							Properties: map[string]v1beta1.JSONSchemaProps{
								"authInfo": {
									Type: "object",
									Properties: map[string]v1beta1.JSONSchemaProps{
										"controller": {
											Type: "object",
											Properties: map[string]v1beta1.JSONSchemaProps{
												"kind": {
													Type: "string",
												},
												"name": {
													Type: "string",
												},
												"namespace": {
													Type: "string",
												},
											},
										},
										"user": {
											Type: "object",
											Properties: map[string]v1beta1.JSONSchemaProps{
												"kind": {
													Type: "string",
												},
												"name": {
													Type: "string",
												},
												"namespace": {
													Type: "string",
												},
											},
										},
									},
								},
								"kubernetesApiEndpoints": {
									Type: "object",
									Properties: map[string]v1beta1.JSONSchemaProps{
										"caBundle": {
											Type: "array",
											Items: &v1beta1.JSONSchemaPropsOrArray{
												Schema: &v1beta1.JSONSchemaProps{
													Type: "byte",
												},
											},
										},
										"serverEndpoints": {
											Type: "array",
											Items: &v1beta1.JSONSchemaPropsOrArray{
												Schema: &v1beta1.JSONSchemaProps{
													Type: "object",
													Properties: map[string]v1beta1.JSONSchemaProps{
														"clientCIDR": {
															Type: "string",
														},
														"serverAddress": {
															Type: "string",
														},
													},
												},
											},
										},
									},
								},
							},
						},
						"status": {
							Type: "object",
							Properties: map[string]v1beta1.JSONSchemaProps{
								"conditions": {
									Type: "array",
									Items: &v1beta1.JSONSchemaPropsOrArray{
										Schema: &v1beta1.JSONSchemaProps{
											Type: "object",
											Properties: map[string]v1beta1.JSONSchemaProps{
												"lastHeartbeatTime": {
													Type:   "string",
													Format: "date-time",
												},
												"lastTransitionTime": {
													Type:   "string",
													Format: "date-time",
												},
												"message": {
													Type: "string",
												},
												"reason": {
													Type: "string",
												},
												"status": {
													Type: "string",
												},
												"type": {
													Type: "string",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
)
