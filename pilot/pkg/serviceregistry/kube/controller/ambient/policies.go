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
	"strings"

	securityclient "istio.io/client-go/pkg/apis/security/v1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/workloadapi/security"
)

func PolicyCollections(
	authzPolicies krt.Collection[*securityclient.AuthorizationPolicy],
	peerAuths krt.Collection[*securityclient.PeerAuthentication],
	meshConfig krt.Singleton[MeshConfig],
	waypoints krt.Collection[Waypoint],
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
			Binding: model.WorkloadAuthorizationBindingStatus{
				ResourceName: string(model.Ztunnel),
				Error:        status,
				Warn:         []string{},
			},
		}
	}, krt.WithName("AuthzDerivedPolicies"))

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
	}, krt.WithName("PeerAuthDerivedPolicies"))

	ImplicitWaypointPolicies := krt.NewCollection(waypoints, func(ctx krt.HandlerContext, waypoint Waypoint) *model.WorkloadAuthorization {
		return implicitWaypointPolicy(ctx, meshConfig, waypoint)
	}, krt.WithName("DefaultAllowFromWaypointPolicies"))

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
	}, krt.WithName("DefaultPolicy"))

	// Policies contains all of the policies we will send down to clients
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
