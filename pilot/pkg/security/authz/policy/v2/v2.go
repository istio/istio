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

package v2

import (
	http_config "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/rbac/v2"
	envoy_rbac "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2"

	istiolog "istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	authz_model "istio.io/istio/pilot/pkg/security/authz/model"
	"istio.io/istio/pilot/pkg/security/authz/policy"
)

var (
	rbacLog = istiolog.RegisterScope("rbac", "rbac debugging", 0)
)

type v2Generator struct {
	serviceMetadata           *authz_model.ServiceMetadata
	authzPolicies             *model.AuthorizationPolicies
	isGlobalPermissiveEnabled bool
}

func NewGenerator(
	serviceMetadata *authz_model.ServiceMetadata,
	authzPolicies *model.AuthorizationPolicies,
	isGlobalPermissiveEnabled bool) policy.Generator {
	return &v2Generator{
		serviceMetadata:           serviceMetadata,
		authzPolicies:             authzPolicies,
		isGlobalPermissiveEnabled: isGlobalPermissiveEnabled,
	}
}

func (b *v2Generator) Generate(forTCPFilter bool) *http_config.RBAC {
	rbacLog.Debugf("building v2 policy")

	rbac := &envoy_rbac.RBAC{
		Action:   envoy_rbac.RBAC_ALLOW,
		Policies: map[string]*envoy_rbac.Policy{},
	}

	if b.isGlobalPermissiveEnabled {
		// TODO(pitlv2109): Handle permissive mode in the future.
		rbacLog.Errorf("ignored global permissive mode: not implemented for v2 policy.")
	}

	serviceMetadata := b.serviceMetadata
	authzPolicies := b.authzPolicies

	namespace := serviceMetadata.GetNamespace()
	if authzPolicies.NamespaceToAuthorizationConfigV2 == nil {
		return &http_config.RBAC{Rules: rbac}
	}
	var _, present = authzPolicies.NamespaceToAuthorizationConfigV2[namespace]
	if !present {
		return &http_config.RBAC{Rules: rbac}
	}
	// TODO(yangminzhu): Implement the new authorization v1beta1 policy.
	return &http_config.RBAC{Rules: rbac}
}
