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
	"time"

	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/collections"
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

	peerAuthentications map[string][]Config

	rootNamespace string
}

// initAuthenticationPolicies creates a new AuthenticationPolicies struct and populates with the
// authentication policies in the mesh environment.
func initAuthenticationPolicies(env *Environment) (*AuthenticationPolicies, error) {
	policy := &AuthenticationPolicies{
		requestAuthentications: map[string][]Config{},
		peerAuthentications:    map[string][]Config{},
		rootNamespace:          env.Mesh().GetRootNamespace(),
	}

	if configs, err := env.List(
		collections.IstioSecurityV1Beta1Requestauthentications.Resource().GroupVersionKind(), NamespaceAll); err == nil {
		sortConfigByCreationTime(configs)
		policy.addRequestAuthentication(configs)
	} else {
		return nil, err
	}

	if configs, err := env.List(
		collections.IstioSecurityV1Beta1Peerauthentications.Resource().GroupVersionKind(), NamespaceAll); err == nil {
		sortConfigByCreationTime(configs)
		policy.addPeerAuthentication(configs)
	} else {
		return nil, err
	}

	return policy, nil
}

func (policy *AuthenticationPolicies) addRequestAuthentication(configs []Config) {
	for _, config := range configs {
		policy.requestAuthentications[config.Namespace] =
			append(policy.requestAuthentications[config.Namespace], config)
	}
}

func (policy *AuthenticationPolicies) addPeerAuthentication(configs []Config) {
	for _, config := range configs {
		policy.peerAuthentications[config.Namespace] =
			append(policy.peerAuthentications[config.Namespace], config)
	}
}

// GetJwtPoliciesForWorkload returns a list of JWT policies matching to labels.
func (policy *AuthenticationPolicies) GetJwtPoliciesForWorkload(namespace string,
	workloadLabels labels.Collection) []*Config {
	return getConfigsForWorkload(policy.requestAuthentications, policy.rootNamespace, namespace, workloadLabels)
}

// GetPeerAuthenticationsForWorkload returns a list of peer authentication policies matching to labels.
func (policy *AuthenticationPolicies) GetPeerAuthenticationsForWorkload(namespace string,
	workloadLabels labels.Collection) []*Config {
	return getConfigsForWorkload(policy.peerAuthentications, policy.rootNamespace, namespace, workloadLabels)
}

// GetRootNamespace return root namespace that is tracked by the policy object.
func (policy *AuthenticationPolicies) GetRootNamespace() string {
	return policy.rootNamespace
}

func getConfigsForWorkload(configsByNamespace map[string][]Config,
	rootNamespace string,
	namespace string,
	workloadLabels labels.Collection) []*Config {
	configs := make([]*Config, 0)
	lookupInNamespaces := []string{namespace}
	if namespace != rootNamespace {
		// Only check the root namespace if the (workload) namespace is not already the root namespace
		// to avoid double inclusion.
		lookupInNamespaces = append(lookupInNamespaces, rootNamespace)
	}
	for _, ns := range lookupInNamespaces {
		if nsConfig, ok := configsByNamespace[ns]; ok {
			for idx := range nsConfig {
				cfg := &nsConfig[idx]
				if ns != cfg.Namespace {
					// Should never come here. Log warning just in case.
					log.Warnf("Seeing config %s with namespace %s in map entry for %s. Ignored", cfg.Name, cfg.Namespace, ns)
					continue
				}
				var selector labels.Instance
				switch cfg.Type {
				case collections.IstioSecurityV1Beta1Requestauthentications.Resource().Kind():
					selector = labels.Instance(cfg.Spec.(*v1beta1.RequestAuthentication).GetSelector().GetMatchLabels())
				case collections.IstioSecurityV1Beta1Peerauthentications.Resource().Kind():
					selector = labels.Instance(cfg.Spec.(*v1beta1.PeerAuthentication).GetSelector().GetMatchLabels())
				default:
					log.Warnf("Not support authentication type %q", cfg.Type)
					continue
				}
				if workloadLabels.IsSupersetOf(selector) {
					configs = append(configs, cfg)
				}
			}
		}
	}

	return configs
}


// getMutualTLSMode returns the MutualTLSMode enum corresponding peer MutualTLS settings.
// Input cannot be nil.
func getMutualTLSMode(mtls *v1beta1.PeerAuthentication_MutualTLS) MutualTLSMode {
	switch mtls.Mode {
	case v1beta1.PeerAuthentication_MutualTLS_DISABLE:
		return MTLSDisable
	case v1beta1.PeerAuthentication_MutualTLS_PERMISSIVE:
		return MTLSPermissive
	case v1beta1.PeerAuthentication_MutualTLS_STRICT:
		return MTLSStrict
	default:
		return MTLSUnknown
	}
}

// composePeerAuthentication returns the effective PeerAuthentication given the list of applicable
// configs. This list should contains at most 1 mesh-level and 1 namespace-level configs.
// Workload-level configs should not be in root namespace (this should be guaranteed by the caller,
// though they will be safely ignored in this function). If the input config list is empty, returns
// nil which can be used to indicate no applicable (beta) policy exist in order to trigger fallback
// to alpha policy. This can be simplified once we deprecate alpha policy.
// If there is at least one applicable config, returns should be not nil, and is a combined policy
// based on following rules:
// - It should have the setting from the most narrow scope (i.e workload-level is  preferred over
// namespace-level, which is preferred over mesh-level).
// - When there are more than one policy in the same scope (i.e workload-level), the oldest one
// win.
// - UNSET will be replaced with the setting from the parrent. I.e UNSET port-level config will be
// replaced with config from workload-level, UNSET in workload-level config will be replaced with
// one in namespace-level and so on.
func composePeerAuthentication(rootNamespace string, configs []*Config) *v1beta1.PeerAuthentication {
	var meshPolicy, namespacePolicy, workloadPolicy *v1beta1.PeerAuthentication
	// Creation time associate with the selected workloadPolicy above. Initially set to max time.
	workloadPolicyCreationTime := time.Unix(1<<63-1, 0)

	for _, cfg := range configs {
		spec := cfg.Spec.(*v1beta1.PeerAuthentication)
		if cfg.Namespace == rootNamespace && spec.Selector == nil {
			meshPolicy = spec
		} else if spec.Selector == nil {
			namespacePolicy = spec
		} else if cfg.Namespace != rootNamespace {
			// Assign to the (selected) workloadPolicy, if it is not set or have a newer timestamp.
			if workloadPolicy == nil || cfg.CreationTimestamp.Before(workloadPolicyCreationTime) {
				workloadPolicy = spec
				workloadPolicyCreationTime = cfg.CreationTimestamp
			}
		}
	}

	if meshPolicy == nil && namespacePolicy == nil && workloadPolicy == nil {
		// Return nil so that caller can fallback to apply alpha policy. Once we deprecate alpha API,
		// this special case can be removed.
		return nil
	}

	// Initial outputPolicy is set to a PERMISSIVE.
	outputPolicy := v1beta1.PeerAuthentication{
		Mtls: &v1beta1.PeerAuthentication_MutualTLS{
			Mode: v1beta1.PeerAuthentication_MutualTLS_PERMISSIVE,
		},
	}

	// Process in mesh, namespace, workload order to resolve inheritance (UNSET)

	if meshPolicy != nil && !isMtlsModeUnset(meshPolicy.Mtls) {
		// If mesh policy is defined, update parentPolicy to mesh policy.
		outputPolicy.Mtls = meshPolicy.Mtls
	}

	if namespacePolicy != nil && !isMtlsModeUnset(namespacePolicy.Mtls) {
		// If namespace policy is defined, update output policy to namespace policy. This means namespace
		// policy overwrite mesh policy.
		outputPolicy.Mtls = namespacePolicy.Mtls
	}

	if workloadPolicy != nil && !isMtlsModeUnset(workloadPolicy.Mtls) {
		// If workload policy is defined, update parent policy to workload policy.
		outputPolicy.Mtls = workloadPolicy.Mtls
	}

	if workloadPolicy != nil && workloadPolicy.PortLevelMtls != nil {
		outputPolicy.PortLevelMtls = make(map[uint32]*v1beta1.PeerAuthentication_MutualTLS, len(workloadPolicy.PortLevelMtls))
		for port, mtls := range workloadPolicy.PortLevelMtls {
			if isMtlsModeUnset(mtls) {
				// Inherit from workload level.
				outputPolicy.PortLevelMtls[port] = outputPolicy.Mtls
			} else {
				outputPolicy.PortLevelMtls[port] = mtls
			}
		}
	}

	return &outputPolicy
}

func isMtlsModeUnset(mtls *v1beta1.PeerAuthentication_MutualTLS) bool {
	return mtls == nil || mtls.Mode == v1beta1.PeerAuthentication_MutualTLS_UNSET
}