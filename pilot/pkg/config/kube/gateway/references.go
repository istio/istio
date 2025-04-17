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

package gateway

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayalpha "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"istio.io/istio/pkg/config"
	schematypes "istio.io/istio/pkg/config/schema/kubetypes"
	"istio.io/istio/pkg/kube/krt"
)

// ReferenceSet stores a variety of different types of resource, and allows looking them up as Gateway API references.
// This is merely a convenience to avoid needing to lookup up a bunch of types all over the place.
type ReferenceSet struct {
	erasedCollections map[config.GroupVersionKind]func(name, namespace string) (any, bool)
}

func (s ReferenceSet) LocalPolicyTargetRef(ref gatewayalpha.LocalPolicyTargetReference, localNamespace string) (any, error) {
	return s.internal(string(ref.Name), string(ref.Group), string(ref.Kind), localNamespace)
}

func (s ReferenceSet) LocalPolicyRef(ref gatewayv1.LocalObjectReference, localNamespace string) (any, error) {
	return s.internal(string(ref.Name), string(ref.Group), string(ref.Kind), localNamespace)
}

func (s ReferenceSet) internal(name, group, kind, localNamespace string) (any, error) {
	t := normalizeReference(&group, &kind, config.GroupVersionKind{})
	lookup, f := s.erasedCollections[t]
	if !f {
		return nil, fmt.Errorf("unsupported kind %v", kind)
	}
	if v, ok := lookup(name, localNamespace); ok {
		return v, nil
	}
	return nil, fmt.Errorf("reference %v/%v (of kind %v) not found", localNamespace, name, kind)
}

func NewReferenceSet(opts ...func(r *ReferenceSet)) *ReferenceSet {
	r := &ReferenceSet{erasedCollections: make(map[config.GroupVersionKind]func(name, namespace string) (any, bool))}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func AddReference[T runtime.Object](c krt.Collection[T]) func(r *ReferenceSet) {
	return func(r *ReferenceSet) {
		g := schematypes.MustGVKFromType[T]()
		r.erasedCollections[g] = func(name, namespace string) (any, bool) {
			o := c.GetKey(namespace + "/" + name)
			if o == nil {
				return nil, false
			}
			return *o, true
		}
	}
}
