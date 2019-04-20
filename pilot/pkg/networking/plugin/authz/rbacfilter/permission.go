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

type PermissionGenerator struct {
	permissions []*rbac_config.Permission
}

func (pg *PermissionGenerator) IsEmpty() bool {
	return len(pg.permissions) == 0
}

func (pg *PermissionGenerator) Append(permission *rbac_config.Permission) {
	if permission == nil {
		return
	}
	pg.permissions = append(pg.permissions, permission)
}

func (pg *PermissionGenerator) AndPermissions() *rbac_config.Permission {
	if pg.IsEmpty() {
		return nil
	}

	return &rbac_config.Permission{
		Rule: &rbac_config.Permission_AndRules{
			AndRules: &rbac_config.Permission_Set{
				Rules: pg.permissions,
			},
		},
	}
}

func (pg *PermissionGenerator) OrPermissions() *rbac_config.Permission {
	if pg.IsEmpty() {
		return nil
	}

	return &rbac_config.Permission{
		Rule: &rbac_config.Permission_OrRules{
			OrRules: &rbac_config.Permission_Set{
				Rules: pg.permissions,
			},
		},
	}
}

func PermissionAny(any bool) *rbac_config.Permission {
	return &rbac_config.Permission{
		Rule: &rbac_config.Permission_Any{
			Any: any,
		},
	}
}

func PermissionNot(permission *rbac_config.Permission) *rbac_config.Permission {
	return &rbac_config.Permission{
		Rule: &rbac_config.Permission_NotRule{
			NotRule: permission,
		},
	}
}

func PermissionDestinationIP(cidr *core.CidrRange) *rbac_config.Permission {
	return &rbac_config.Permission{
		Rule: &rbac_config.Permission_DestinationIp{
			DestinationIp: cidr,
		},
	}
}

func PermissionDestinationPort(port uint32) *rbac_config.Permission {
	return &rbac_config.Permission{
		Rule: &rbac_config.Permission_DestinationPort{
			DestinationPort: port,
		},
	}
}

func PermissionRequestedServerName(name *envoy_matcher.StringMatcher) *rbac_config.Permission {
	return &rbac_config.Permission{
		Rule: &rbac_config.Permission_RequestedServerName{
			RequestedServerName: name,
		},
	}
}

func PermissionMetadata(metadata *envoy_matcher.MetadataMatcher) *rbac_config.Permission {
	return &rbac_config.Permission{
		Rule: &rbac_config.Permission_Metadata{
			Metadata: metadata,
		},
	}
}

func PermissionHeader(header *route.HeaderMatcher) *rbac_config.Permission {
	return &rbac_config.Permission{
		Rule: &rbac_config.Permission_Header{
			Header: header,
		},
	}
}
