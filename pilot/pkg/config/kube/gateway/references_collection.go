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

	"k8s.io/apimachinery/pkg/types"
	gateway "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	creds "istio.io/istio/pilot/pkg/model/credentials"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/krt"
)

// Reference stores a reference to a namespaced GVK, as used by ReferenceGrant
type Reference struct {
	Kind      config.GroupVersionKind
	Namespace gateway.Namespace
}

func (refs Reference) String() string {
	return refs.Kind.String() + "/" + string(refs.Namespace)
}

type ReferencePair struct {
	To, From Reference
}

func (r ReferencePair) String() string {
	return fmt.Sprintf("%s->%s", r.From, r.To)
}

type ReferenceGrants struct {
	collection krt.Collection[ReferenceGrant]
	index      krt.Index[ReferencePair, ReferenceGrant]
}

func ReferenceGrantsCollection(referenceGrants krt.Collection[*gatewayv1beta1.ReferenceGrant], opts krt.OptionsBuilder) krt.Collection[ReferenceGrant] {
	return krt.NewManyCollection(referenceGrants, func(ctx krt.HandlerContext, obj *gatewayv1beta1.ReferenceGrant) []ReferenceGrant {
		rp := obj.Spec
		results := make([]ReferenceGrant, 0, len(rp.From)*len(rp.To))
		for _, from := range rp.From {
			fromKey := Reference{
				Namespace: from.Namespace,
			}
			ref := normalizeReference(&from.Group, &from.Kind, config.GroupVersionKind{})
			switch ref {
			case gvk.KubernetesGateway, gvk.HTTPRoute, gvk.GRPCRoute, gvk.TLSRoute, gvk.TCPRoute, gvk.XListenerSet:
				fromKey.Kind = ref
			default:
				// Not supported type. Not an error; may be for another controller
				continue
			}
			for _, to := range rp.To {
				toKey := Reference{
					Namespace: gateway.Namespace(obj.Namespace),
				}

				ref := normalizeReference(&to.Group, &to.Kind, config.GroupVersionKind{})
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

func BuildReferenceGrants(collection krt.Collection[ReferenceGrant]) ReferenceGrants {
	idx := krt.NewIndex(collection, "toFrom", func(o ReferenceGrant) []ReferencePair {
		return []ReferencePair{{
			To:   o.To,
			From: o.From,
		}}
	})
	return ReferenceGrants{
		collection: collection,
		index:      idx,
	}
}

type ReferenceGrant struct {
	Source      types.NamespacedName
	From        Reference
	To          Reference
	AllowAll    bool
	AllowedName string
}

func (g ReferenceGrant) ResourceName() string {
	return g.Source.String() + "/" + g.From.String() + "/" + g.To.String()
}

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
	from := Reference{Kind: kind, Namespace: gateway.Namespace(namespace)}
	to := Reference{Kind: resourceKind, Namespace: gateway.Namespace(p.Namespace)}
	pair := ReferencePair{From: from, To: to}
	grants := krt.FetchOrList(ctx, refs.collection, krt.FilterIndex(refs.index, pair))
	for _, g := range grants {
		if g.AllowAll || g.AllowedName == p.Name {
			return true
		}
	}
	return false
}

func (refs ReferenceGrants) BackendAllowed(ctx krt.HandlerContext,
	k config.GroupVersionKind,
	toGVK config.GroupVersionKind,
	backendName gateway.ObjectName,
	backendNamespace gateway.Namespace,
	routeNamespace string,
) bool {
	from := Reference{Kind: k, Namespace: gateway.Namespace(routeNamespace)}
	to := Reference{Kind: toGVK, Namespace: backendNamespace}
	pair := ReferencePair{From: from, To: to}
	grants := krt.Fetch(ctx, refs.collection, krt.FilterIndex(refs.index, pair))
	for _, g := range grants {
		if g.AllowAll || g.AllowedName == string(backendName) {
			return true
		}
	}
	return false
}
