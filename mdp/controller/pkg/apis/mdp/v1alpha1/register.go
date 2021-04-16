// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package v1alpha1 contains API Schema definitions for the istio v1alpha1 API group
// +k8s:deepcopy-gen=package,register
// +groupName=mdp.istio.io
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// MDPConfigGVK is GVK for MDPConfig
	MDPConfigGVK = schema.GroupVersionKind{
		Version: "v1alpha1",
		Group:   "mdp.istio.io",
		Kind:    "MDPConfig",
	}

	// SchemeGroupVersion is group version used to register these objects
	SchemeGroupVersion = schema.GroupVersion{Group: "mdp.istio.io", Version: "v1alpha1"}

	// SchemeGroupKind is group version used to register these objects
	SchemeGroupKind = schema.GroupKind{Group: "mdp.istio.io", Kind: "MDPConfig"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme
	SchemeBuilder = &scheme.Builder{GroupVersion: SchemeGroupVersion}
)

// Register the MDPConfig and MDPConfigList API kind
func init() {
	SchemeBuilder.Register(&MDPConfig{}, &MDPConfigList{})
}
