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

package rbacfilter

import (
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	rbac_config "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2alpha"
	envoy_matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher"
)

type PrincipalGenerator struct {
	principals []*rbac_config.Principal
}

func (pg *PrincipalGenerator) IsEmpty() bool {
	return len(pg.principals) == 0
}

func (pg *PrincipalGenerator) Append(principal *rbac_config.Principal) {
	if principal == nil {
		return
	}
	pg.principals = append(pg.principals, principal)
}

func (pg *PrincipalGenerator) AndPrincipals() *rbac_config.Principal {
	if pg.IsEmpty() {
		return nil
	}

	return &rbac_config.Principal{
		Identifier: &rbac_config.Principal_AndIds{
			AndIds: &rbac_config.Principal_Set{
				Ids: pg.principals,
			},
		},
	}
}

func (pg *PrincipalGenerator) OrPrincipals() *rbac_config.Principal {
	if pg.IsEmpty() {
		return nil
	}

	return &rbac_config.Principal{
		Identifier: &rbac_config.Principal_OrIds{
			OrIds: &rbac_config.Principal_Set{
				Ids: pg.principals,
			},
		},
	}
}

func PrincipalAny(any bool) *rbac_config.Principal {
	return &rbac_config.Principal{
		Identifier: &rbac_config.Principal_Any{
			Any: any,
		},
	}
}

func PrincipalNot(principal *rbac_config.Principal) *rbac_config.Principal {
	return &rbac_config.Principal{
		Identifier: &rbac_config.Principal_NotId{
			NotId: principal,
		},
	}
}

func PrincipalAuthenticated(name *envoy_matcher.StringMatcher) *rbac_config.Principal {
	return &rbac_config.Principal{
		Identifier: &rbac_config.Principal_Authenticated_{
			Authenticated: &rbac_config.Principal_Authenticated{
				PrincipalName: name,
			},
		},
	}
}

func PrincipalSourceIP(cidr *core.CidrRange) *rbac_config.Principal {
	return &rbac_config.Principal{
		Identifier: &rbac_config.Principal_SourceIp{
			SourceIp: cidr,
		},
	}
}

func PrincipalMetadata(metadata *envoy_matcher.MetadataMatcher) *rbac_config.Principal {
	return &rbac_config.Principal{
		Identifier: &rbac_config.Principal_Metadata{
			Metadata: metadata,
		},
	}
}

func PrincipalHeader(header *route.HeaderMatcher) *rbac_config.Principal {
	return &rbac_config.Principal{
		Identifier: &rbac_config.Principal_Header{
			Header: header,
		},
	}
}
