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

package model

import (
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
)

func permissionAny() *rbacpb.Permission {
	return &rbacpb.Permission{
		Rule: &rbacpb.Permission_Any{
			Any: true,
		},
	}
}

func permissionAnd(permission []*rbacpb.Permission) *rbacpb.Permission {
	return &rbacpb.Permission{
		Rule: &rbacpb.Permission_AndRules{
			AndRules: &rbacpb.Permission_Set{
				Rules: permission,
			},
		},
	}
}

func permissionOr(permission []*rbacpb.Permission) *rbacpb.Permission {
	return &rbacpb.Permission{
		Rule: &rbacpb.Permission_OrRules{
			OrRules: &rbacpb.Permission_Set{
				Rules: permission,
			},
		},
	}
}

func permissionNot(permission *rbacpb.Permission) *rbacpb.Permission {
	return &rbacpb.Permission{
		Rule: &rbacpb.Permission_NotRule{
			NotRule: permission,
		},
	}
}

func permissionDestinationIP(cidr *core.CidrRange) *rbacpb.Permission {
	return &rbacpb.Permission{
		Rule: &rbacpb.Permission_DestinationIp{
			DestinationIp: cidr,
		},
	}
}

func permissionDestinationPort(port uint32) *rbacpb.Permission {
	return &rbacpb.Permission{
		Rule: &rbacpb.Permission_DestinationPort{
			DestinationPort: port,
		},
	}
}

func permissionRequestedServerName(name *matcherpb.StringMatcher) *rbacpb.Permission {
	return &rbacpb.Permission{
		Rule: &rbacpb.Permission_RequestedServerName{
			RequestedServerName: name,
		},
	}
}

func permissionMetadata(metadata *matcherpb.MetadataMatcher) *rbacpb.Permission {
	return &rbacpb.Permission{
		Rule: &rbacpb.Permission_Metadata{
			Metadata: metadata,
		},
	}
}

func permissionHeader(header *routepb.HeaderMatcher) *rbacpb.Permission {
	return &rbacpb.Permission{
		Rule: &rbacpb.Permission_Header{
			Header: header,
		},
	}
}

func permissionPath(path *matcherpb.PathMatcher) *rbacpb.Permission {
	return &rbacpb.Permission{
		Rule: &rbacpb.Permission_UrlPath{
			UrlPath: path,
		},
	}
}
