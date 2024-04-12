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
	securityclient "istio.io/client-go/pkg/apis/security/v1beta1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/workloadapi/security"
)

func PolicyCollections(
	AuthzPolicies krt.Collection[*securityclient.AuthorizationPolicy],
	PeerAuths krt.Collection[*securityclient.PeerAuthentication],
	MeshConfig krt.Singleton[MeshConfig],
) (krt.Collection[model.WorkloadAuthorization], krt.Collection[model.WorkloadAuthorization]) {
	AuthzDerivedPolicies := krt.NewCollection(AuthzPolicies, func(ctx krt.HandlerContext, i *securityclient.AuthorizationPolicy) *model.WorkloadAuthorization {
		meshCfg := krt.FetchOne(ctx, MeshConfig.AsCollection())
		pol := convertAuthorizationPolicy(meshCfg.GetRootNamespace(), i)
		if pol == nil {
			return nil
		}
		return &model.WorkloadAuthorization{Authorization: pol, LabelSelector: model.NewSelector(i.Spec.GetSelector().GetMatchLabels())}
	}, krt.WithName("AuthzDerivedPolicies"))
	PeerAuthDerivedPolicies := krt.NewCollection(PeerAuths, func(ctx krt.HandlerContext, i *securityclient.PeerAuthentication) *model.WorkloadAuthorization {
		meshCfg := krt.FetchOne(ctx, MeshConfig.AsCollection())
		pol := convertPeerAuthentication(meshCfg.GetRootNamespace(), i)
		if pol == nil {
			return nil
		}
		return &model.WorkloadAuthorization{
			Authorization: pol,
			LabelSelector: model.NewSelector(i.Spec.GetSelector().GetMatchLabels()),
		}
	}, krt.WithName("PeerAuthDerivedPolicies"))
	DefaultPolicy := krt.NewSingleton[model.WorkloadAuthorization](func(ctx krt.HandlerContext) *model.WorkloadAuthorization {
		if len(krt.Fetch(ctx, PeerAuths)) == 0 {
			return nil
		}
		meshCfg := krt.FetchOne(ctx, MeshConfig.AsCollection())
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
	}, krt.WithName("Policies"))
	return AuthzDerivedPolicies, Policies
}
