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
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/api/annotation"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	securityclient "istio.io/client-go/pkg/apis/security/v1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/workloadapi/security"
)

func WaypointPolicyStatusCollection(
	authzPolicies krt.Collection[*securityclient.AuthorizationPolicy],
	waypoints krt.Collection[Waypoint],
	services krt.Collection[*corev1.Service],
	serviceEntries krt.Collection[*networkingclient.ServiceEntry],
	gatewayClasses krt.Collection[*v1beta1.GatewayClass],
	meshConfig krt.Singleton[MeshConfig],
	namespaces krt.Collection[*corev1.Namespace],
	opts krt.OptionsBuilder,
) krt.Collection[model.WaypointPolicyStatus] {
	return krt.NewCollection(authzPolicies,
		func(ctx krt.HandlerContext, i *securityclient.AuthorizationPolicy) *model.WaypointPolicyStatus {
			targetRefs := model.GetTargetRefs(&i.Spec)
			if len(targetRefs) == 0 {
				return nil // targetRef is required for binding to waypoint
			}

			var (
				conditions []model.PolicyBindingStatus
				rootNs     string
			)

			if meshConfig.Get() != nil {
				rootNs = meshConfig.Get().MeshConfig.RootNamespace
			}

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
				case gvk.GatewayClass_v1.Kind:
					// first verify the AP is in the root namespace, if not it's ignored
					if namespace != rootNs {
						reason = model.WaypointPolicyReasonInvalid
						message = fmt.Sprintf("AuthorizationPolicy must be in the root namespace `%s` when referencing a GatewayClass", rootNs)
						break
					}

					fetchedGatewayClass := ptr.Flatten(krt.FetchOne(ctx, gatewayClasses, krt.FilterKey(target.GetName())))
					if fetchedGatewayClass == nil {
						reason = model.WaypointPolicyReasonTargetNotFound
					} else {
						// verify GatewayClass is for waypoint
						if fetchedGatewayClass.Spec.ControllerName != constants.ManagedGatewayMeshController {
							reason = model.WaypointPolicyReasonInvalid
							message = fmt.Sprintf("GatewayClass must use controller name `%s` for waypoints", constants.ManagedGatewayMeshController)
						} else {
							bound = true
							reason = model.WaypointPolicyReasonAccepted
							message = fmt.Sprintf("bound to %s", fetchedGatewayClass.GetName())
						}
					}
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
		}, opts.WithName("WaypointPolicyStatuses")...)
}

func PolicyCollections(
	authzPolicies krt.Collection[*securityclient.AuthorizationPolicy],
	peerAuths krt.Collection[*securityclient.PeerAuthentication],
	meshConfig krt.Singleton[MeshConfig],
	waypoints krt.Collection[Waypoint],
	opts krt.OptionsBuilder,
	flags FeatureFlags,
) (krt.Collection[model.WorkloadAuthorization], krt.Collection[model.WorkloadAuthorization]) {
	AuthzDerivedPolicies := krt.NewCollection(authzPolicies, func(ctx krt.HandlerContext, i *securityclient.AuthorizationPolicy) *model.WorkloadAuthorization {
		dryRun, _ := strconv.ParseBool(i.Annotations[annotation.IoIstioDryRun.Name])
		if dryRun {
			return nil
		}
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
	}, opts.WithName("AuthzDerivedPolicies")...)

	PeerAuthByNamespace := krt.NewIndex(peerAuths, "namespaceWithSelector", func(p *securityclient.PeerAuthentication) []string {
		if p.Spec.GetSelector() == nil {
			return []string{p.GetNamespace()}
		}
		return nil
	})

	// Our derived PeerAuthentication policies are the effective (i.e. potentially merged) set of policies we will send down to ztunnel
	// A policy is sent iff (if and only if):
	// 1. the PeerAuthentication has a workload selector
	// 2. The PeerAuthentication is NOT in the root namespace
	// 3. There is a portLevelMtls policy (technically implied by 1)
	// 4. If the top-level mode is PERMISSIVE or DISABLE, there is at least one portLevelMtls policy with mode STRICT
	//
	// STRICT policies that don't have portLevelMtls will be
	// handled when the Workload xDS resource is pushed (a static STRICT-equivalent policy will always be pushed)
	//
	// As a corollary, if the effective top-level policy is STRICT, the workload policy.mode is UNSET
	// and the portLevelMtls policy.mode is STRICT, we will merge the portLevelMtls policy with our static strict policy
	// (which basically just looks like setting the workload policy.mode to STRICT). This is because our precedence order for policy
	// requires that traffic matching *any* DENY policy is blocked, so attaching 2 polciies (the static strict policy + an exception)
	// does not work (the traffic will be blocked despite the exception)
	PeerAuthDerivedPolicies := krt.NewCollection(peerAuths, func(ctx krt.HandlerContext, i *securityclient.PeerAuthentication) *model.WorkloadAuthorization {
		meshCfg := krt.FetchOne(ctx, meshConfig.AsCollection())
		// violates case #1, #2, or #3
		if i.Namespace == meshCfg.GetRootNamespace() || i.Spec.GetSelector() == nil || len(i.Spec.PortLevelMtls) == 0 {
			log.Debugf("skipping PeerAuthentication %s/%s for ambient since it isn't a workload policy with port level mTLS", i.Namespace, i.Name)
			return nil
		}

		var nsPol, rootPol *securityclient.PeerAuthentication
		nsPols := PeerAuthByNamespace.Lookup(i.GetNamespace())
		rootPols := PeerAuthByNamespace.Lookup(meshCfg.GetRootNamespace())

		switch len(nsPols) {
		case 0:
			nsPol = nil
		case 1:
			nsPol = nsPols[0]
		default:
			nsPol = getOldestPeerAuthn(nsPols)
		}

		switch len(rootPols) {
		case 0:
			rootPol = nil
		case 1:
			rootPol = rootPols[0]
		default:
			rootPol = getOldestPeerAuthn(rootPols)
		}

		pol := convertPeerAuthentication(meshCfg.GetRootNamespace(), i, nsPol, rootPol)
		if pol == nil {
			return nil
		}
		return &model.WorkloadAuthorization{
			Authorization: pol,
			LabelSelector: model.NewSelector(i.Spec.GetSelector().GetMatchLabels()),
		}
	}, opts.WithName("PeerAuthDerivedPolicies")...)

	ImplicitWaypointPolicies := krt.NewCollection(waypoints, func(ctx krt.HandlerContext, waypoint Waypoint) *model.WorkloadAuthorization {
		return implicitWaypointPolicy(flags, ctx, meshConfig, waypoint)
	}, opts.WithName("DefaultAllowFromWaypointPolicies")...)

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
	}, opts.WithName("DefaultPolicy")...)

	// Policies contains all of the policies we will send down to clients
	// No need to add withDebug on join since it is trivial
	Policies := krt.JoinCollection([]krt.Collection[model.WorkloadAuthorization]{
		AuthzDerivedPolicies,
		PeerAuthDerivedPolicies,
		DefaultPolicy.AsCollection(),
		ImplicitWaypointPolicies,
	}, opts.WithName("Policies")...)
	return AuthzDerivedPolicies, Policies
}

func implicitWaypointPolicyName(flags FeatureFlags, waypoint *Waypoint) string {
	if !flags.DefaultAllowFromWaypoint || waypoint == nil || len(waypoint.ServiceAccounts) == 0 {
		return ""
	}
	// use '_' character since those are illegal in k8s names
	return "istio_allow_waypoint_" + waypoint.Namespace + "_" + waypoint.Name
}

func implicitWaypointPolicy(
	flags FeatureFlags,
	ctx krt.HandlerContext,
	MeshConfig krt.Singleton[MeshConfig],
	waypoint Waypoint,
) *model.WorkloadAuthorization {
	if !flags.DefaultAllowFromWaypoint || len(waypoint.ServiceAccounts) == 0 {
		return nil
	}
	meshCfg := krt.FetchOne(ctx, MeshConfig.AsCollection())
	return &model.WorkloadAuthorization{
		Authorization: &security.Authorization{
			Name:      implicitWaypointPolicyName(flags, &waypoint),
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
