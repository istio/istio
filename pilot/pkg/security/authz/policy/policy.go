// Copyright 2019 Istio Authors
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

package policy

import (
	envoyRbacHttpPb "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/rbac/v2"
	envoyRbacPb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2"
)

type Generator interface {
	Generate(forTCPFilter bool) (denyConfig *envoyRbacHttpPb.RBAC, allowConfig *envoyRbacHttpPb.RBAC)
}

// DefaultDenyAllConfig returns a default RBAC filter config that denies all requests.
func DefaultDenyAllConfig(name string) *envoyRbacHttpPb.RBAC {
	return &envoyRbacHttpPb.RBAC{
		Rules: &envoyRbacPb.RBAC{
			Action: envoyRbacPb.RBAC_DENY,
			Policies: map[string]*envoyRbacPb.Policy{
				name: {
					Permissions: []*envoyRbacPb.Permission{
						{
							Rule: &envoyRbacPb.Permission_Any{Any: true},
						},
					},
					Principals: []*envoyRbacPb.Principal{
						{
							Identifier: &envoyRbacPb.Principal_Any{Any: true},
						},
					},
				},
			},
		},
	}
}
