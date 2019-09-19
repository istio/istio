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

package v1alpha1

import (
	authn "istio.io/api/authentication/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	authn_model "istio.io/istio/pilot/pkg/security/model"
)

// GetConsolidateAuthenticationPolicy returns the v1alpha1 authentication policy for workload specified by
// hostname (or label selector if specified) and port, if defined.
// It also tries to resolve JWKS URI if necessary.
func GetConsolidateAuthenticationPolicy(store model.IstioConfigStore, serviceInstance *model.ServiceInstance) *authn.Policy {
	service := serviceInstance.Service
	port := serviceInstance.Endpoint.ServicePort
	labels := serviceInstance.Labels

	config := store.AuthenticationPolicyForWorkload(service, labels, port)
	if config != nil {
		policy := config.Spec.(*authn.Policy)
		if err := authn_model.JwtKeyResolver.SetAuthenticationPolicyJwksURIs(policy); err == nil {
			return policy
		}
	}

	return nil
}

// MutualTLSMode is the mutule TLS mode specified by authentication policy.
type MutualTLSMode int

const (
	// MTLSUnknown is used to indicate the variable hasn't been initialized correctly (with the authentication policy).
	MTLSUnknown MutualTLSMode = iota

	// MTLSDisable if authentication policy disable mTLS.
	MTLSDisable

	// MTLSPermissive if authentication policy enable mTLS in permissive mode.
	MTLSPermissive

	// MTLSStrict if authentication policy enable mTLS in strict mode.
	MTLSStrict
)

// GetServiceMutualTLSMode returns the mTLS mode for given service-port.
func GetServiceMutualTLSMode(store model.IstioConfigStore, service *model.Service, port *model.Port) MutualTLSMode {
	// TODO(diemtvu) when authentication poicy changes to workload-selector model, this should be changed to
	// iterate over all service instances to examine the mTLS mode. May also cache this to avoid
	// querying config store and process policy everytime.
	if config := store.AuthenticationPolicyForWorkload(service, nil, port); config != nil {
		return getMutualTLSMode(config.Spec.(*authn.Policy))
	}
	return MTLSDisable
}

func getMutualTLSMode(policy *authn.Policy) MutualTLSMode {
	if mTLSSetting := GetMutualTLS(policy); mTLSSetting != nil {
		if mTLSSetting.GetMode() == authn.MutualTls_STRICT {
			return MTLSStrict
		}
		return MTLSPermissive
	}
	return MTLSDisable
}
