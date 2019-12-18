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

package model

import (
	"istio.io/api/security/v1beta1"

	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schemas"
)

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

// String converts MutualTLSMode to human readable string for debugging.
func (mode MutualTLSMode) String() string {
	switch mode {
	case MTLSDisable:
		return "DISABLE"
	case MTLSPermissive:
		return "PERMISSIVE"
	case MTLSStrict:
		return "STRICT"
	default:
		return "UNKNOWN"
	}
}

// AuthenticationPolicies organizes authentication (mTLS + JWT) policies by namespace.
type AuthenticationPolicies struct {
	// Maps from namespace to the v1beta1 authentication policies.
	requestAuthentications map[string][]Config

	// Maps from namespace to mTLS mode.
	namespaceMTLSMode map[string]MutualTLSMode

	rootNamespace string
}

// initAuthenticationPolicies creates a new AuthenticationPolicies struct and populates with the
// authentication policies in the mesh environment.
func initAuthenticationPolicies(env *Environment) *AuthenticationPolicies {
	policy := &AuthenticationPolicies{
		requestAuthentications: map[string][]Config{},
		namespaceMTLSMode:      map[string]MutualTLSMode{},
		rootNamespace:          env.Mesh().GetRootNamespace(),
	}

	if configs, err := env.List(schemas.RequestAuthentication.Type, NamespaceAll); err == nil {
		sortConfigByCreationTime(configs)
		policy.addRequestAuthentication(configs)
	}

	// TODO(diemtvu): populate mTLS mode from mesh config and namespace labels.
	return policy
}

func (policy *AuthenticationPolicies) addRequestAuthentication(configs []Config) {
	for _, config := range configs {
		if config.Namespace == policy.rootNamespace && config.Name != "default" {
			// Validation should prevent non-singleton global policy, but it's ok to check and log just in case.
			log.Warnf("Ignore non default RequestAuthentication config in root namespace: %s/%s", config.Namespace, config.Name)
			continue
		}
		policy.requestAuthentications[config.Namespace] =
			append(policy.requestAuthentications[config.Namespace], config)
	}
}

// GetJwtPoliciesForWorkload returns a list of JWT policies matching to labels.
func (policy *AuthenticationPolicies) GetJwtPoliciesForWorkload(namespace string,
	workloadLabels labels.Collection) []*Config {
	configs := make([]*Config, 0)
	if nsConfig, ok := policy.requestAuthentications[namespace]; ok {
		for idx := range nsConfig {
			cfg := &nsConfig[idx]
			if namespace != cfg.Namespace {
				// Should never come here. Log warning just in case.
				log.Warnf("Seeing config %s with namespace %s in map entry for %s. Ignored", cfg.Name, cfg.Namespace, namespace)
				continue
			}
			spec := cfg.Spec.(*v1beta1.RequestAuthentication)
			selector := labels.Instance(spec.GetSelector().GetMatchLabels())
			if workloadLabels.IsSupersetOf(selector) {
				configs = append(configs, cfg)
			}
		}
	}

	if rootConfig, ok := policy.requestAuthentications[policy.rootNamespace]; ok {
		for idx := range rootConfig {
			configs = append(configs, &rootConfig[idx])
		}
	}
	return configs
}
