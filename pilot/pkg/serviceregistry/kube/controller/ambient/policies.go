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

// nolint: gocritic
package ambient

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	securityclient "istio.io/client-go/pkg/apis/security/v1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/workloadapi/security"
)

func WaypointPolicyStatusCollection(
	authzPolicies krt.Collection[*securityclient.AuthorizationPolicy],
	waypoints krt.Collection[Waypoint],
	services krt.Collection[*corev1.Service],
	serviceEntries krt.Collection[*networkingclient.ServiceEntry],
	namespaces krt.Collection[*corev1.Namespace],
	withDebug krt.CollectionOption,
) krt.Collection[model.WaypointPolicyStatus] {
	return krt.NewCollection(authzPolicies,
		func(ctx krt.HandlerContext, i *securityclient.AuthorizationPolicy) *model.WaypointPolicyStatus {
			targetRefs := model.GetTargetRefs(&i.Spec)
			if len(targetRefs) == 0 {
				return nil // targetRef is required for binding to waypoint
			}

			var conditions []model.PolicyBindingStatus

			for _, target := range targetRefs {
				namespace := i.GetNamespace()
				if n := target.GetNamespace(); n != "" {
					namespace = n
				}
				key := namespace + "/" + target.GetName()
				message := "not bound"
				reason := "unknown"
				bound := false
				switch target.GetKind() {
				case gvk.KubernetesGateway.Kind:
					fetchedWaypoints := krt.Fetch(ctx, waypoints, krt.FilterKey(key))
					if len(fetchedWaypoints) == 1 {
						bound = true
						reason = model.WaypointPolicyReasonAccepted
						message = fmt.Sprintf("bound to %s", fetchedWaypoints[0].ResourceName())
					} else {
						reason = model.WaypointPolicyReasonTargetNotFound
					}
				case gvk.Service.Kind:
					fetchedServices := krt.Fetch(ctx, services, krt.FilterKey(key))
					if len(fetchedServices) == 1 {
						w, _ := fetchWaypointForService(ctx, waypoints, namespaces, fetchedServices[0].ObjectMeta)
						if w != nil {
							bound = true
							reason = model.WaypointPolicyReasonAccepted
							message = fmt.Sprintf("bound to %s", w.ResourceName())
						} else {
							message = fmt.Sprintf("Service %s is not bound to a waypoint", key)
							reason = model.WaypointPolicyReasonAncestorNotBound
						}
					} else {
						message = fmt.Sprintf("Service %s was not found", key)
						reason = model.WaypointPolicyReasonTargetNotFound
					}
				case gvk.ServiceEntry.Kind:
					fetchedServiceEntries := krt.Fetch(ctx, serviceEntries, krt.FilterKey(key))
					if len(fetchedServiceEntries) == 1 {
						w, _ := fetchWaypointForService(ctx, waypoints, namespaces, fetchedServiceEntries[0].ObjectMeta)
						if w != nil {
							bound = true
							reason = model.WaypointPolicyReasonAccepted
							message = fmt.Sprintf("bound to %s", w.ResourceName())
						} else {
							message = fmt.Sprintf("ServiceEntry %s is not bound to a waypoint", key)
							reason = model.WaypointPolicyReasonAncestorNotBound
						}
					} else {
						message = fmt.Sprintf("ServiceEntry %s was not found", key)
						reason = model.WaypointPolicyReasonTargetNotFound
					}
				}
				targetGroup := target.GetGroup()
				if targetGroup == "" {
					targetGroup = "core"
				}
				conditions = append(conditions, model.PolicyBindingStatus{
					ObservedGeneration: i.GetGeneration(),
					Status: &model.StatusMessage{
						Reason:  reason,
						Message: message,
					},
					Ancestor: target.GetKind() + "." + targetGroup + ":" + key,
					Bound:    bound,
				})
			}

			return &model.WaypointPolicyStatus{
				Source:     MakeSource(i),
				Conditions: conditions,
			}
		}, krt.WithName("WaypointPolicyStatuses"), withDebug)
}

func PolicyCollections(
	authzPolicies krt.Collection[*securityclient.AuthorizationPolicy],
	peerAuths krt.Collection[*securityclient.PeerAuthentication],
	meshConfig krt.Singleton[MeshConfig],
	waypoints krt.Collection[Waypoint],
	withDebug krt.CollectionOption,
) (krt.Collection[model.WorkloadAuthorization], krt.Collection[model.WorkloadAuthorization]) {
	AuthzDerivedPolicies := krt.NewCollection(authzPolicies, func(ctx krt.HandlerContext, i *securityclient.AuthorizationPolicy) *model.WorkloadAuthorization {
		meshCfg := krt.FetchOne(ctx, meshConfig.AsCollection())
		pol, status := convertAuthorizationPolicy(meshCfg.GetRootNamespace(), i)
		if status == nil && pol == nil {
			return nil
		}

		return &model.WorkloadAuthorization{
			Authorization: pol,
			LabelSelector: model.NewSelector(i.Spec.GetSelector().GetMatchLabels()),
			Source:        MakeSource(i),
			Binding: model.PolicyBindingStatus{
				ObservedGeneration: i.GetGeneration(),
				Ancestor:           string(model.Ztunnel),
				Status:             status,
				Bound:              pol != nil,
			},
		}
	}, krt.WithName("AuthzDerivedPolicies"), withDebug)

	PeerAuthDerivedPolicies := krt.NewCollection(peerAuths, func(ctx krt.HandlerContext, i *securityclient.PeerAuthentication) *model.WorkloadAuthorization {
		meshCfg := krt.FetchOne(ctx, meshConfig.AsCollection())
		pol := convertPeerAuthentication(meshCfg.GetRootNamespace(), i)
		if pol == nil {
			return nil
		}
		return &model.WorkloadAuthorization{
			Authorization: pol,
			LabelSelector: model.NewSelector(i.Spec.GetSelector().GetMatchLabels()),
		}
	}, krt.WithName("PeerAuthDerivedPolicies"), withDebug)

	ImplicitWaypointPolicies := krt.NewCollection(waypoints, func(ctx krt.HandlerContext, waypoint Waypoint) *model.WorkloadAuthorization {
		return implicitWaypointPolicy(ctx, meshConfig, waypoint)
	}, krt.WithName("DefaultAllowFromWaypointPolicies"), withDebug)

	DefaultPolicy := krt.NewSingleton[model.WorkloadAuthorization](func(ctx krt.HandlerContext) *model.WorkloadAuthorization {
		if len(krt.Fetch(ctx, peerAuths)) == 0 {
			return nil
		}
		meshCfg := krt.FetchOne(ctx, meshConfig.AsCollection())
		// If there are any PeerAuthentications in our cache, send our static STRICT policy
		return &model.WorkloadAuthorization{
			LabelSelector: model.LabelSelector{},
			Authorization: &security.Authorization{
				Name:      staticStrictPolicyName,
				Namespace: meshCfg.GetRootNamespace(),
				Scope:     security.Scope_WORKLOAD_SELECTOR,
				Action:    security.Action_DENY,
				Groups: []*security.Group{
					{
						Rules: []*security.Rules{
							{
								Matches: []*security.Match{
									{
										NotPrincipals: []*security.StringMatch{
											{
												MatchType: &security.StringMatch_Presence{},
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
	}, krt.WithName("DefaultPolicy"), withDebug)

	// Policies contains all of the policies we will send down to clients
	// No need to add withDebug on join since it is trivial
	Policies := krt.JoinCollection([]krt.Collection[model.WorkloadAuthorization]{
		AuthzDerivedPolicies,
		PeerAuthDerivedPolicies,
		DefaultPolicy.AsCollection(),
		ImplicitWaypointPolicies,
	}, krt.WithName("Policies"))
	return AuthzDerivedPolicies, Policies
}

func implicitWaypointPolicyName(waypoint *Waypoint) string {
	if !features.DefaultAllowFromWaypoint || waypoint == nil || len(waypoint.ServiceAccounts) == 0 {
		return ""
	}
	// use '_' character since those are illegal in k8s names
	return "istio_allow_waypoint_" + waypoint.Namespace + "_" + waypoint.Name
}

func implicitWaypointPolicy(ctx krt.HandlerContext, MeshConfig krt.Singleton[MeshConfig], waypoint Waypoint) *model.WorkloadAuthorization {
	if !features.DefaultAllowFromWaypoint || len(waypoint.ServiceAccounts) == 0 {
		return nil
	}
	meshCfg := krt.FetchOne(ctx, MeshConfig.AsCollection())
	return &model.WorkloadAuthorization{
		Authorization: &security.Authorization{
			Name:      implicitWaypointPolicyName(&waypoint),
			Namespace: waypoint.Namespace,
			// note: we don't actually use label selection; the names have an internally well-known format
			// workload generation will append a reference to this
			Scope:  security.Scope_WORKLOAD_SELECTOR,
			Action: security.Action_ALLOW,
			Groups: []*security.Group{{
				Rules: []*security.Rules{
					{
						Matches: []*security.Match{
							{
								Principals: slices.Map(waypoint.ServiceAccounts, func(sa string) *security.StringMatch {
									return &security.StringMatch{MatchType: &security.StringMatch_Exact{
										Exact: strings.TrimPrefix(spiffe.MustGenSpiffeURI(meshCfg.MeshConfig, waypoint.Namespace, sa), spiffe.URIPrefix),
									}}
								}),
							},
						},
					},
				},
			}},
		},
	}
}
