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
	"crypto/md5"
	"fmt"
	"strings"
	"time"

	"istio.io/api/security/v1beta1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
)

// MutualTLSMode is the mutual TLS mode specified by authentication policy.
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

// ConvertToMutualTLSMode converts from peer authn MTLS mode (`PeerAuthentication_MutualTLS_Mode`)
// to the MTLS mode specified by authn policy.
func ConvertToMutualTLSMode(mode v1beta1.PeerAuthentication_MutualTLS_Mode) MutualTLSMode {
	switch mode {
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

// AuthenticationPolicies organizes authentication (mTLS + JWT) policies by namespace.
type AuthenticationPolicies struct {
	// Maps from namespace to the v1beta1 authentication policies.
	requestAuthentications map[string][]config.Config

	peerAuthentications map[string][]config.Config

	// namespaceMutualTLSMode is the MutualTLSMode corresponding to the namespace-level PeerAuthentication.
	// All namespace-level policies, and only them, are added to this map. If the policy mTLS mode is set
	// to UNSET, it will be resolved to the value set by mesh policy if exist (i.e not UNKNOWN), or MTLSPermissive
	// otherwise.
	namespaceMutualTLSMode map[string]MutualTLSMode

	// globalMutualTLSMode is the MutualTLSMode corresponding to the mesh-level PeerAuthentication.
	// This value can be MTLSUnknown, if there is no mesh-level policy.
	globalMutualTLSMode MutualTLSMode

	rootNamespace string

	// aggregateVersion contains the versions of all peer authentications.
	aggregateVersion string
}

// initAuthenticationPolicies creates a new AuthenticationPolicies struct and populates with the
// authentication policies in the mesh environment.
func initAuthenticationPolicies(env *Environment) (*AuthenticationPolicies, error) {
	policy := &AuthenticationPolicies{
		requestAuthentications: map[string][]config.Config{},
		peerAuthentications:    map[string][]config.Config{},
		globalMutualTLSMode:    MTLSUnknown,
		rootNamespace:          env.Mesh().GetRootNamespace(),
	}

	if configs, err := env.List(
		gvk.RequestAuthentication, NamespaceAll); err == nil {
		sortConfigByCreationTime(configs)
		policy.addRequestAuthentication(configs)
	} else {
		return nil, err
	}

	if configs, err := env.List(
		gvk.PeerAuthentication, NamespaceAll); err == nil {
		policy.addPeerAuthentication(configs)
	} else {
		return nil, err
	}

	return policy, nil
}

func (policy *AuthenticationPolicies) addRequestAuthentication(configs []config.Config) {
	for _, config := range configs {
		policy.requestAuthentications[config.Namespace] = append(policy.requestAuthentications[config.Namespace], config)
	}
}

func (policy *AuthenticationPolicies) addPeerAuthentication(configs []config.Config) {
	// Sort configs in ascending order by their creation time.
	sortConfigByCreationTime(configs)

	foundNamespaceMTLS := make(map[string]v1beta1.PeerAuthentication_MutualTLS_Mode)
	// Track which namespace/mesh level policy seen so far to make sure the oldest one is used.
	seenNamespaceOrMeshConfig := make(map[string]time.Time)
	versions := []string{}

	for _, config := range configs {
		versions = append(versions, config.UID+"."+config.ResourceVersion)
		// Mesh & namespace level policy are those that have empty selector.
		spec := config.Spec.(*v1beta1.PeerAuthentication)
		if spec.Selector == nil || len(spec.Selector.MatchLabels) == 0 {
			if t, ok := seenNamespaceOrMeshConfig[config.Namespace]; ok {
				log.Warnf(
					"Namespace/mesh-level PeerAuthentication is already defined for %q at time %v. Ignore %q which was created at time %v",
					config.Namespace, t, config.Name, config.CreationTimestamp)
				continue
			}
			seenNamespaceOrMeshConfig[config.Namespace] = config.CreationTimestamp

			mode := v1beta1.PeerAuthentication_MutualTLS_UNSET
			if spec.Mtls != nil {
				mode = spec.Mtls.Mode
			}
			if config.Namespace == policy.rootNamespace {
				// This is mesh-level policy. UNSET is treated as permissive for mesh-policy.
				if mode == v1beta1.PeerAuthentication_MutualTLS_UNSET {
					policy.globalMutualTLSMode = MTLSPermissive
				} else {
					policy.globalMutualTLSMode = ConvertToMutualTLSMode(mode)
				}
			} else {
				// For regular namespace, just add to the intermediate map.
				foundNamespaceMTLS[config.Namespace] = mode
			}
		}

		// Add the config to the map by namespace for future look up. This is done after namespace/mesh
		// singleton check so there should be at most one namespace/mesh config is added to the map.
		policy.peerAuthentications[config.Namespace] = append(policy.peerAuthentications[config.Namespace], config)
	}

	policy.aggregateVersion = fmt.Sprintf("%x", md5.Sum([]byte(strings.Join(versions, ";"))))

	// Process found namespace-level policy.
	policy.namespaceMutualTLSMode = make(map[string]MutualTLSMode, len(foundNamespaceMTLS))

	inheritedMTLSMode := policy.globalMutualTLSMode
	if inheritedMTLSMode == MTLSUnknown {
		// If the mesh policy is not explicitly presented, use default value MTLSPermissive.
		inheritedMTLSMode = MTLSPermissive
	}
	for ns, mtlsMode := range foundNamespaceMTLS {
		if mtlsMode == v1beta1.PeerAuthentication_MutualTLS_UNSET {
			policy.namespaceMutualTLSMode[ns] = inheritedMTLSMode
		} else {
			policy.namespaceMutualTLSMode[ns] = ConvertToMutualTLSMode(mtlsMode)
		}
	}
}

// GetNamespaceMutualTLSMode returns the MutualTLSMode as defined by a namespace or mesh level
// PeerAuthentication. The return value could be `MTLSUnknown` if there is no mesh nor namespace
// PeerAuthentication policy for the given namespace.
func (policy *AuthenticationPolicies) GetNamespaceMutualTLSMode(namespace string) MutualTLSMode {
	if mode, ok := policy.namespaceMutualTLSMode[namespace]; ok {
		return mode
	}
	return policy.globalMutualTLSMode
}

// GetJwtPoliciesForWorkload returns a list of JWT policies matching to labels.
func (policy *AuthenticationPolicies) GetJwtPoliciesForWorkload(namespace string,
	workloadLabels labels.Instance) []*config.Config {
	return getConfigsForWorkload(policy.requestAuthentications, policy.rootNamespace, namespace, workloadLabels)
}

// GetPeerAuthenticationsForWorkload returns a list of peer authentication policies matching to labels.
func (policy *AuthenticationPolicies) GetPeerAuthenticationsForWorkload(namespace string,
	workloadLabels labels.Instance) []*config.Config {
	return getConfigsForWorkload(policy.peerAuthentications, policy.rootNamespace, namespace, workloadLabels)
}

// GetRootNamespace return root namespace that is tracked by the policy object.
func (policy *AuthenticationPolicies) GetRootNamespace() string {
	return policy.rootNamespace
}

// GetVersion return versions of all peer authentications..
func (policy *AuthenticationPolicies) GetVersion() string {
	return policy.aggregateVersion
}

func getConfigsForWorkload(configsByNamespace map[string][]config.Config,
	rootNamespace string,
	namespace string,
	workloadLabels labels.Instance) []*config.Config {
	configs := make([]*config.Config, 0)
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
				switch cfg.GroupVersionKind {
				case collections.IstioSecurityV1Beta1Requestauthentications.Resource().GroupVersionKind():
					selector = cfg.Spec.(*v1beta1.RequestAuthentication).GetSelector().GetMatchLabels()
				case collections.IstioSecurityV1Beta1Peerauthentications.Resource().GroupVersionKind():
					selector = cfg.Spec.(*v1beta1.PeerAuthentication).GetSelector().GetMatchLabels()
				default:
					log.Warnf("Not support authentication type %q", cfg.GroupVersionKind)
					continue
				}
				if selector.SubsetOf(workloadLabels) {
					configs = append(configs, cfg)
				}
			}
		}
	}

	return configs
}
