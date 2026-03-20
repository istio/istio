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

package agentgateway

import (
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

// RouteParentReference holds information about a route's parent reference
type RouteParentReference struct {
	// InternalName refers to the internal name of the parent we can reference it by. For example "my-ns/my-gateway"
	InternalName string
	// InternalKind is the Group/Kind of the Parent
	InternalKind schema.GroupVersionKind
	// DeniedReason, if present, indicates why the reference was not valid
	DeniedReason *ParentError
	// OriginalReference contains the original reference
	OriginalReference gatewayv1.ParentReference
	// Hostname is the hostname match of the Parent, if any
	Hostname        string
	BannedHostnames sets.Set[string]
	ParentKey       AgwParentKey
	ParentSection   gatewayv1.SectionName
	Accepted        bool
	ParentGateway   types.NamespacedName
}

var allowedParentReferences = sets.New(
	gvk.KubernetesGateway,
	gvk.Service,
	gvk.ServiceEntry,
	gvk.ListenerSet,
)

func defaultString[T ~string](s *T, def string) string {
	if s == nil {
		return def
	}
	return string(*s)
}

func toInternalParentReference(p gatewayv1.ParentReference, localNamespace string) (AgwParentKey, error) {
	ref := normalizeReference(p.Group, p.Kind, gvk.KubernetesGateway)
	if !allowedParentReferences.Contains(ref) {
		return AgwParentKey{}, fmt.Errorf("unsupported parent: %v/%v", p.Group, p.Kind)
	}
	return AgwParentKey{
		Kind: ref.Kubernetes(),
		Name: string(p.Name),
		// Unset namespace means "same namespace"
		Namespace: defaultString(p.Namespace, localNamespace),
	}, nil
}

// ReferenceAllowed validates if a route can reference a specified parent based on rules like section, port, and hostnames.
// Returns a *ParentError if the reference violates any constraints or is disallowed.
// Returns nil if the reference is valid and permitted for the given route and ParentInfo.
func ReferenceAllowed(
	ctx RouteContext,
	parent *AgwParentInfo,
	routeKind schema.GroupVersionKind,
	parentRef ParentReference,
	hostnames []gatewayv1.Hostname,
	localNamespace string,
) *ParentError {
	if parentRef.Kind == gvk.Service.Kubernetes() {
		key := parentRef.Namespace + "/" + parentRef.Name
		svc := ptr.Flatten(krt.FetchOne(ctx.Krt, ctx.Services, krt.FilterKey(key)))

		// check that the referenced svc exists
		if svc == nil {
			return &ParentError{
				Reason:  ParentErrorNotAccepted,
				Message: fmt.Sprintf("parent service: %q not found", parentRef.Name),
			}
		}
	} else if parentRef.Kind == gvk.ServiceEntry.Kubernetes() {
		// check that the referenced svc entry exists
		key := parentRef.Namespace + "/" + parentRef.Name
		svcEntry := ptr.Flatten(krt.FetchOne(ctx.Krt, ctx.ServiceEntries, krt.FilterKey(key)))
		if svcEntry == nil {
			return &ParentError{
				Reason:  ParentErrorNotAccepted,
				Message: fmt.Sprintf("parent service entry: %q not found", parentRef.Name),
			}
		}
	} else {
		// First, check section and port apply. This must come first
		if parentRef.Port != 0 && parentRef.Port != parent.Port {
			return &ParentError{
				Reason:  ParentErrorNotAccepted,
				Message: fmt.Sprintf("port %v not found", parentRef.Port),
			}
		}
		if len(parentRef.SectionName) > 0 && parentRef.SectionName != parent.SectionName {
			return &ParentError{
				Reason:  ParentErrorNotAccepted,
				Message: fmt.Sprintf("sectionName %q not found", parentRef.SectionName),
			}
		}

		// Next check the hostnames are a match. This is a bi-directional wildcard match. Only one route
		// hostname must match for it to be allowed (but the others will be filtered at runtime)
		// If either is empty its treated as a wildcard which always matches

		if len(hostnames) == 0 {
			hostnames = []gatewayv1.Hostname{"*"}
		}
		if len(parent.Hostnames) > 0 {
			matched := false
			hostMatched := false
		out:
			for _, routeHostname := range hostnames {
				for _, parentHostNamespace := range parent.Hostnames {
					var parentNamespace, parentHostname string
					if strings.Contains(parentHostNamespace, "/") {
						spl := strings.Split(parentHostNamespace, "/")
						parentNamespace, parentHostname = spl[0], spl[1]
					} else {
						parentNamespace, parentHostname = "*", parentHostNamespace
					}

					hostnameMatch := host.Name(parentHostname).Matches(host.Name(routeHostname))
					namespaceMatch := parentNamespace == "*" || parentNamespace == localNamespace

					hostMatched = hostMatched || hostnameMatch
					if hostnameMatch && namespaceMatch {
						matched = true
						break out
					}
				}
			}
			if !matched {
				if hostMatched {
					return &ParentError{
						Reason: ParentErrorNotAllowed,
						Message: fmt.Sprintf(
							"hostnames matched parent hostname %q, but namespace %q is not allowed by the parent",
							parent.OriginalHostname, localNamespace,
						),
					}
				}
				return &ParentError{
					Reason: ParentErrorNoHostname,
					Message: fmt.Sprintf(
						"no hostnames matched parent hostname %q",
						parent.OriginalHostname,
					),
				}
			}
		}
	}
	// Also make sure this route kind is allowed
	matched := false
	for _, ak := range parent.AllowedKinds {
		if string(ak.Kind) == routeKind.Kind && ptr.OrDefault((*string)(ak.Group), gvk.GatewayClass.Group) == routeKind.Group {
			matched = true
			break
		}
	}
	if !matched {
		return &ParentError{
			Reason:  ParentErrorNotAllowed,
			Message: fmt.Sprintf("kind %v is not allowed", routeKind),
		}
	}
	return nil
}

// parentRefString creates a string representation of a ParentReference for consistent ordering and comparison in tests and logging
// example output: "gateway.networking.k8s.io/KubernetesGateway/my-gateway/sectionName/8080/ns1"
func parentRefString(ref gatewayv1.ParentReference) string {
	return fmt.Sprintf("%s/%s/%s/%s/%d.%s",
		defaultString(ref.Group, gvk.KubernetesGateway.Group),
		defaultString(ref.Kind, gvk.KubernetesGateway.Kind),
		ref.Name,
		ptr.OrEmpty(ref.SectionName),
		ptr.OrEmpty(ref.Port),
		ptr.OrEmpty(ref.Namespace))
}

// extractParentReferenceInfo extracts the parent reference information for a given route, including any errors related to invalid references
func extractParentReferenceInfo(ctx RouteContext, parents RouteParents, obj controllers.Object) []RouteParentReference {
	routeRefs, hostnames, kind := GetCommonRouteInfo(obj)
	localNamespace := obj.GetNamespace()
	var parentRefs []RouteParentReference
	for _, ref := range routeRefs {
		ir, err := toInternalParentReference(ref, localNamespace)
		if err != nil {
			continue
		}
		pk := ParentReference{
			AgwParentKey: ir,
			SectionName:  ptr.OrEmpty(ref.SectionName),
			Port:         ptr.OrEmpty(ref.Port),
		}
		gk := ir
		currentParents := parents.fetch(ctx.Krt, gk)
		appendParent := func(pr *AgwParentInfo, pk ParentReference) {
			bannedHostnames := sets.New[string]()
			for _, gw := range currentParents {
				if gw == pr {
					continue // do not ban ourself
				}
				if gw.Port != pr.Port {
					continue
				}
				if gw.Protocol != pr.Protocol {
					continue
				}
				bannedHostnames.Insert(gw.OriginalHostname)
			}
			deniedReason := ReferenceAllowed(ctx, pr, kind.Kubernetes(), pk, hostnames, localNamespace)

			rpi := RouteParentReference{
				ParentGateway:     pr.ParentGateway,
				InternalName:      pr.InternalName,
				InternalKind:      ir.Kind,
				Hostname:          pr.OriginalHostname,
				DeniedReason:      deniedReason,
				OriginalReference: ref,
				BannedHostnames:   bannedHostnames.Copy().Delete(pr.OriginalHostname),
				ParentKey:         ir,
				ParentSection:     pr.SectionName,
				Accepted:          deniedReason == nil,
			}
			parentRefs = append(parentRefs, rpi)
		}
		for _, gw := range currentParents {
			appendParent(gw, pk)
		}
	}
	// Ensure stable order
	slices.SortBy(parentRefs, func(a RouteParentReference) string {
		return parentRefString(a.OriginalReference)
	})
	return parentRefs
}

// FilteredReferences filters out references that are not accepted by the Parent.
func FilteredReferences(parents []RouteParentReference) []RouteParentReference {
	ret := make([]RouteParentReference, 0, len(parents))
	for _, p := range parents {
		if p.DeniedReason != nil {
			// We should filter this out
			continue
		}
		ret = append(ret, p)
	}
	// To ensure deterministic order, sort them
	sort.Slice(ret, func(i, j int) bool {
		return ret[i].InternalName < ret[j].InternalName
	})
	return ret
}
