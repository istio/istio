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

package gatewaycommon

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
	gatewayx "sigs.k8s.io/gateway-api/apisx/v1alpha1"

	creds "istio.io/istio/pilot/pkg/model/credentials"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	schematypes "istio.io/istio/pkg/config/schema/kubetypes"
	"istio.io/istio/pkg/kube/krt"
)

// ReferenceSet stores a variety of different types of resource, and allows looking them up as Gateway API references.
// This is merely a convenience to avoid needing to lookup up a bunch of types all over the place.
type ReferenceSet struct {
	ErasedCollections map[config.GroupVersionKind]func(ctx krt.HandlerContext, name, namespace string) (any, bool)
}

func (s ReferenceSet) LocalPolicyTargetRef(ctx krt.HandlerContext, ref gatewayv1.LocalPolicyTargetReference, localNamespace string) (any, error) {
	return s.internal(ctx, string(ref.Name), string(ref.Group), string(ref.Kind), localNamespace)
}

func (s ReferenceSet) XLocalPolicyTargetRef(ctx krt.HandlerContext, ref gatewayx.LocalPolicyTargetReference, localNamespace string) (any, error) {
	return s.internal(ctx, string(ref.Name), string(ref.Group), string(ref.Kind), localNamespace)
}

func (s ReferenceSet) LocalPolicyRef(ctx krt.HandlerContext, ref gatewayv1.LocalObjectReference, localNamespace string) (any, error) {
	return s.internal(ctx, string(ref.Name), string(ref.Group), string(ref.Kind), localNamespace)
}

func (s ReferenceSet) internal(ctx krt.HandlerContext, name, group, kind, localNamespace string) (any, error) {
	t := NormalizeReference(&group, &kind, config.GroupVersionKind{})
	lookup, f := s.ErasedCollections[t]
	if !f {
		return nil, fmt.Errorf("unsupported kind %v", kind)
	}
	if v, ok := lookup(ctx, name, localNamespace); ok {
		return v, nil
	}
	return nil, fmt.Errorf("reference %v/%v (of kind %v) not found", localNamespace, name, kind)
}

func NewReferenceSet(opts ...func(r *ReferenceSet)) *ReferenceSet {
	r := &ReferenceSet{ErasedCollections: make(map[config.GroupVersionKind]func(ctx krt.HandlerContext, name, namespace string) (any, bool))}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func AddReference[T runtime.Object](c krt.Collection[T]) func(r *ReferenceSet) {
	return func(r *ReferenceSet) {
		g := schematypes.MustGVKFromType[T]()
		r.ErasedCollections[g] = func(ctx krt.HandlerContext, name, namespace string) (any, bool) {
			o := krt.FetchOne(ctx, c, krt.FilterKey(namespace+"/"+name))
			if o == nil {
				return nil, false
			}
			return *o, true
		}
	}
}

// NormalizeReference takes a generic Group/Kind (the API uses a few variations) and converts to a known GroupVersionKind.
// Defaults for the group/kind are also passed.
func NormalizeReference[G ~string, K ~string](group *G, kind *K, def config.GroupVersionKind) config.GroupVersionKind {
	k := def.Kind
	if kind != nil {
		k = string(*kind)
	}
	g := def.Group
	if group != nil {
		g = string(*group)
	}
	gk := config.GroupVersionKind{
		Group: g,
		Kind:  k,
	}
	s, f := collections.All.FindByGroupKind(gk)
	if f {
		return s.GroupVersionKind()
	}
	return gk
}

// Reference stores a reference to a namespaced GVK, as used by ReferenceGrant
type Reference struct {
	Kind      config.GroupVersionKind
	Namespace gatewayv1.Namespace
}

func (refs Reference) String() string {
	return refs.Kind.String() + "/" + string(refs.Namespace)
}

// ReferencePair holds a from-to pair of references.
type ReferencePair struct {
	To, From Reference
}

func (r ReferencePair) String() string {
	return fmt.Sprintf("%s->%s", r.From, r.To)
}

// ReferenceGrants holds a collection and index for ReferenceGrant objects.
type ReferenceGrants struct {
	Collection krt.Collection[ReferenceGrant]
	Index      krt.Index[ReferencePair, ReferenceGrant]
}

// ReferenceGrantsCollection builds a krt collection from ReferenceGrant resources.
func ReferenceGrantsCollection(referenceGrants krt.Collection[*gatewayv1beta1.ReferenceGrant], opts krt.OptionsBuilder) krt.Collection[ReferenceGrant] {
	return krt.NewManyCollection(referenceGrants, func(ctx krt.HandlerContext, obj *gatewayv1beta1.ReferenceGrant) []ReferenceGrant {
		rp := obj.Spec
		results := make([]ReferenceGrant, 0, len(rp.From)*len(rp.To))
		for _, from := range rp.From {
			fromKey := Reference{
				Namespace: from.Namespace,
			}
			ref := NormalizeReference(&from.Group, &from.Kind, config.GroupVersionKind{})
			switch ref {
			case gvk.KubernetesGateway, gvk.HTTPRoute, gvk.GRPCRoute, gvk.TLSRoute, gvk.TCPRoute, gvk.ListenerSet:
				fromKey.Kind = ref
			default:
				// Not supported type. Not an error; may be for another controller
				continue
			}
			for _, to := range rp.To {
				toKey := Reference{
					Namespace: gatewayv1.Namespace(obj.Namespace),
				}

				ref := NormalizeReference(&to.Group, &to.Kind, config.GroupVersionKind{})
				switch ref {
				case gvk.ConfigMap, gvk.Secret, gvk.Service, gvk.InferencePool:
					toKey.Kind = ref
				default:
					continue
				}
				rg := ReferenceGrant{
					Source:      config.NamespacedName(obj),
					From:        fromKey,
					To:          toKey,
					AllowAll:    false,
					AllowedName: "",
				}
				if to.Name != nil {
					rg.AllowedName = string(*to.Name)
				} else {
					rg.AllowAll = true
				}
				results = append(results, rg)
			}
		}
		return results
	}, opts.WithName("ReferenceGrants")...)
}

// BuildReferenceGrants builds a ReferenceGrants with an index from a collection.
func BuildReferenceGrants(collection krt.Collection[ReferenceGrant]) ReferenceGrants {
	idx := krt.NewIndex(collection, "toFrom", func(o ReferenceGrant) []ReferencePair {
		return []ReferencePair{{
			To:   o.To,
			From: o.From,
		}}
	})
	return ReferenceGrants{
		Collection: collection,
		Index:      idx,
	}
}

// ReferenceGrant represents a parsed ReferenceGrant resource.
type ReferenceGrant struct {
	Source      types.NamespacedName
	From        Reference
	To          Reference
	AllowAll    bool
	AllowedName string
}

func (g ReferenceGrant) ResourceName() string {
	nameKey := "*"
	if !g.AllowAll {
		nameKey = g.AllowedName
	}
	return g.Source.String() + "/" + g.From.String() + "/" + g.To.String() + "/" + nameKey
}

// SecretAllowed checks if a secret reference is allowed.
func (refs ReferenceGrants) SecretAllowed(ctx krt.HandlerContext, kind config.GroupVersionKind, resourceName string, namespace string) bool {
	p, err := creds.ParseResourceName(resourceName, "", "", "")
	if err != nil {
		log.Warnf("failed to parse resource name %q: %v", resourceName, err)
		return false
	}
	resourceKind := config.GroupVersionKind{Kind: p.ResourceKind.String()}
	resourceSchema, resourceSchemaFound := collections.All.FindByGroupKind(resourceKind)
	if resourceSchemaFound {
		resourceKind = resourceSchema.GroupVersionKind()
	}
	from := Reference{Kind: kind, Namespace: gatewayv1.Namespace(namespace)}
	to := Reference{Kind: resourceKind, Namespace: gatewayv1.Namespace(p.Namespace)}
	pair := ReferencePair{From: from, To: to}
	grants := krt.FetchOrList(ctx, refs.Collection, krt.FilterIndex(refs.Index, pair))
	for _, g := range grants {
		if g.AllowAll || g.AllowedName == p.Name {
			return true
		}
	}
	return false
}

// BackendAllowed checks if a backend reference is allowed.
func (refs ReferenceGrants) BackendAllowed(ctx krt.HandlerContext,
	k config.GroupVersionKind,
	toGVK config.GroupVersionKind,
	backendName gatewayv1.ObjectName,
	backendNamespace gatewayv1.Namespace,
	routeNamespace string,
) bool {
	from := Reference{Kind: k, Namespace: gatewayv1.Namespace(routeNamespace)}
	to := Reference{Kind: toGVK, Namespace: backendNamespace}
	pair := ReferencePair{From: from, To: to}
	grants := krt.Fetch(ctx, refs.Collection, krt.FilterIndex(refs.Index, pair))
	for _, g := range grants {
		if g.AllowAll || g.AllowedName == string(backendName) {
			return true
		}
	}
	return false
}
