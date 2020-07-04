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
	"strings"
	"time"

	"istio.io/api/security/v1beta1"

	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
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

const (
	ResourceSeparator = "~"
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

	// namespaceMutualTLSMode is the MutualTLSMode correspoinding to the namespace-level PeerAuthentication.
	// All namespace-level policies, and only them, are added to this map. If the policy mTLS mode is set
	// to UNSET, it will be resolved to the value set by mesh policy if exist (i.e not UNKNOWN), or MTLSPermissive
	// otherwise.
	namespaceMutualTLSMode map[string]MutualTLSMode

	// globalMutualTLSMode is the MutualTLSMode corresponding to the mesh-level PeerAuthentication.
	// This value can be MTLSUnknown, if there is no mesh-level policy.
	globalMutualTLSMode MutualTLSMode

	rootNamespace string
}

// initAuthenticationPolicies creates a new AuthenticationPolicies struct and populates with the
// authentication policies in the mesh environment.
func initAuthenticationPolicies(env *Environment) (*AuthenticationPolicies, error) {
	policy := &AuthenticationPolicies{
		requestAuthentications: map[string][]Config{},
		peerAuthentications:    map[string][]Config{},
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

func (policy *AuthenticationPolicies) addRequestAuthentication(configs []Config) {
	for _, config := range configs {
		reqPolicy := config.Spec.(*v1beta1.RequestAuthentication)
		// Follow OIDC discovery to resolve JwksURI if need to.
		GetJwtKeyResolver().ResolveJwksURI(reqPolicy)
		policy.requestAuthentications[config.Namespace] =
			append(policy.requestAuthentications[config.Namespace], config)
	}
}

// TODO(diemtvu): refactor this function to share with policy-applier pkg.
func apiModeToMutualTLSMode(mode v1beta1.PeerAuthentication_MutualTLS_Mode) MutualTLSMode {
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

func (policy *AuthenticationPolicies) addPeerAuthentication(configs []Config) {
	// Sort configs in ascending order by their creation time.
	sortConfigByCreationTime(configs)

	foundNamespaceMTLS := make(map[string]v1beta1.PeerAuthentication_MutualTLS_Mode)
	// Track which namespace/mesh level policy seen so far to make sure the oldest one is used.
	seenNamespaceOrMeshConfig := make(map[string]time.Time)

	for _, config := range configs {
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
					policy.globalMutualTLSMode = apiModeToMutualTLSMode(mode)
				}
			} else {
				// For regular namespace, just add to the intemediate map.
				foundNamespaceMTLS[config.Namespace] = mode
			}
		}

		// Add the config to the map by namespace for future look up. This is done after namespace/mesh
		// singleton check so there should be at most one namespace/mesh config is added to the map.
		policy.peerAuthentications[config.Namespace] =
			append(policy.peerAuthentications[config.Namespace], config)
	}

	// Process found namespace-level policy.
	policy.namespaceMutualTLSMode = make(map[string]MutualTLSMode, len(foundNamespaceMTLS))

	inheritedMTLSMode := policy.globalMutualTLSMode
	if inheritedMTLSMode == MTLSUnknown {
		// If the mesh policy is not explicitly presented, use default valude MTLSPermissive.
		inheritedMTLSMode = MTLSPermissive
	}
	for ns, mtlsMode := range foundNamespaceMTLS {
		if mtlsMode == v1beta1.PeerAuthentication_MutualTLS_UNSET {
			policy.namespaceMutualTLSMode[ns] = inheritedMTLSMode
		} else {
			policy.namespaceMutualTLSMode[ns] = apiModeToMutualTLSMode(mtlsMode)
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
				switch cfg.GroupVersionKind {
				case collections.IstioSecurityV1Beta1Requestauthentications.Resource().GroupVersionKind():
					selector = cfg.Spec.(*v1beta1.RequestAuthentication).GetSelector().GetMatchLabels()
				case collections.IstioSecurityV1Beta1Peerauthentications.Resource().GroupVersionKind():
					selector = cfg.Spec.(*v1beta1.PeerAuthentication).GetSelector().GetMatchLabels()
				default:
					log.Warnf("Not support authentication type %q", cfg.GroupVersionKind)
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

// SdsCertificateConfig holds TLS certs needed to build SDS TLS context.
type SdsCertificateConfig struct {
	CertificatePath   string
	PrivateKeyPath    string
	CaCertificatePath string
}

// GetResourceName converts a SdsCertificateConfig to a string to be used as an SDS resource name
func (s SdsCertificateConfig) GetResourceName() string {
	if s.IsKeyCertificate() {
		return "file-cert:" + s.CertificatePath + ResourceSeparator + s.PrivateKeyPath // Format: file-cert:%s~%s
	}
	return ""
}

// GetRootResourceName converts a SdsCertificateConfig to a string to be used as an SDS resource name for the root
func (s SdsCertificateConfig) GetRootResourceName() string {
	if s.IsRootCertificate() {
		return "file-root:" + s.CaCertificatePath // Format: file-root:%s
	}
	return ""
}

// IsRootCertificate returns true if this config represents a root certificate config.
func (s SdsCertificateConfig) IsRootCertificate() bool {
	return s.CaCertificatePath != ""
}

// IsKeyCertificate returns true if this config represents key certificate config.
func (s SdsCertificateConfig) IsKeyCertificate() bool {
	return s.CertificatePath != "" && s.PrivateKeyPath != ""
}

// SdsCertificateConfigFromResourceName converts the provided resource name into a SdsCertificateConfig
// If the resource name is not valid, false is returned.
func SdsCertificateConfigFromResourceName(resource string) (SdsCertificateConfig, bool) {
	if strings.HasPrefix(resource, "file-cert:") {
		filesString := strings.TrimPrefix(resource, "file-cert:")
		split := strings.Split(filesString, ResourceSeparator)
		if len(split) != 2 {
			return SdsCertificateConfig{}, false
		}
		return SdsCertificateConfig{split[0], split[1], ""}, true
	} else if strings.HasPrefix(resource, "file-root:") {
		filesString := strings.TrimPrefix(resource, "file-root:")
		split := strings.Split(filesString, ResourceSeparator)
		if len(split) != 1 {
			return SdsCertificateConfig{}, false
		}
		return SdsCertificateConfig{"", "", split[0]}, true
	} else {
		return SdsCertificateConfig{}, false
	}
}
