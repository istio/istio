// Copyright 2018 Istio Authors
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

package schema

import (
	"fmt"

	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	sc "k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/galley/pkg/source/kube/dynamic/converter"
)

// ResourceSpec represents a known crd. It is used to drive the K8s-related machinery, and to map to
// the proto format.
type ResourceSpec struct {

	// Singular name of the K8s resource
	Singular string

	// Plural name of the K8s resource
	Plural string

	// Group name of the K8s resource
	Group string

	// Version of the K8s resource
	Version string

	// Kind of the K8s resource
	Kind string

	// ListKind of the K8s resource
	ListKind string

	// Target resource type of the resource
	Target resource.Info

	// The converter to use
	Converter converter.Fn

	// Indicates that the resource is not required to be present
	Optional bool
}

// APIResource generated from this type.
func (i *ResourceSpec) APIResource() *metaV1.APIResource {
	return &metaV1.APIResource{
		Name:         i.Plural,
		SingularName: i.Singular,
		Kind:         i.Kind,
		Version:      i.Version,
		Group:        i.Group,
		Namespaced:   true,
	}
}

// GroupVersion returns the GroupVersion of this type.
func (i *ResourceSpec) GroupVersion() sc.GroupVersion {
	return sc.GroupVersion{
		Group:   i.Group,
		Version: i.Version,
	}
}

// CanonicalResourceName of the resource.
func (i *ResourceSpec) CanonicalResourceName() string {
	return fmt.Sprintf("%s.%s/%s", i.Plural, i.Group, i.Version)
}
