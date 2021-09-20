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

package xds

import (
	"fmt"
	"strings"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/credentials"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/secrets"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
)

const (
	// GatewaySdsCaSuffix is the suffix of the sds resource name for root CA. All resource
	// names for gateway root certs end with "-cacert".
	GatewaySdsCaSuffix = "-cacert"
)

// SecretResource wraps the authnmodel type with cache functions implemented
type SecretResource struct {
	credentials.SecretResource
}

var _ model.XdsCacheEntry = SecretResource{}

// DependentTypes is not needed; we know exactly which configs impact SDS, so we can scope at DependentConfigs level
func (sr SecretResource) DependentTypes() []config.GroupVersionKind {
	return nil
}

func (sr SecretResource) DependentConfigs() []model.ConfigKey {
	return relatedConfigs(model.ConfigKey{Kind: gvk.Secret, Name: sr.Name, Namespace: sr.Namespace})
}

func (sr SecretResource) Cacheable() bool {
	return true
}

func needsUpdate(proxy *model.Proxy, updates model.XdsUpdates) bool {
	if proxy.Type != model.Router {
		return false
	}
	if len(updates) == 0 {
		return true
	}
	if len(model.ConfigNamesOfKind(updates, gvk.Secret)) > 0 {
		return true
	}
	return false
}

// parseResources parses a list of resource names to SecretResource types, for a given proxy.
// Invalid resource names are ignored
func (s *SecretGen) parseResources(names []string, proxy *model.Proxy) []SecretResource {
	res := make([]SecretResource, 0, len(names))
	for _, resource := range names {
		sr, err := credentials.ParseResourceName(resource, proxy.VerifiedIdentity.Namespace, proxy.Metadata.ClusterID, s.configCluster)
		if err != nil {
			pilotSDSCertificateErrors.Increment()
			log.Warnf("error parsing resource name: %v", err)
			continue
		}
		res = append(res, SecretResource{sr})
	}
	return res
}

func (s *SecretGen) Generate(proxy *model.Proxy, push *model.PushContext, w *model.WatchedResource,
	req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	if proxy.VerifiedIdentity == nil {
		log.Warnf("proxy %v is not authorized to receive secrets. Ensure you are connecting over TLS port and are authenticated.", proxy.ID)
		return nil, model.DefaultXdsLogDetails, nil
	}
	if req == nil || !needsUpdate(proxy, req.ConfigsUpdated) {
		return nil, model.DefaultXdsLogDetails, nil
	}
	var updatedSecrets map[model.ConfigKey]struct{}
	if !req.Full {
		updatedSecrets = model.ConfigsOfKind(req.ConfigsUpdated, gvk.Secret)
	}

	// TODO: For the new gateway-api, we should always search the config namespace and stop reading across all clusters
	proxyClusterSecrets, err := s.secrets.ForCluster(proxy.Metadata.ClusterID)
	if err != nil {
		log.Warnf("proxy %v is from an unknown cluster, cannot retrieve certificates: %v", proxy.ID, err)
		pilotSDSCertificateErrors.Increment()
		return nil, model.DefaultXdsLogDetails, nil
	}
	configClusterSecrets, err := s.secrets.ForCluster(s.configCluster)
	if err != nil {
		log.Warnf("proxy %v is from an unknown cluster, cannot retrieve certificates: %v", proxy.ID, err)
		pilotSDSCertificateErrors.Increment()
		return nil, model.DefaultXdsLogDetails, nil
	}

	// Filter down to resources we can access. We do not return an error if they attempt to access a Secret
	// they cannot; instead we just exclude it. This ensures that a single bad reference does not break the whole
	// SDS flow. The pilotSDSCertificateErrors metric and logs handle visibility into invalid references.
	resources := filterAuthorizedResources(s.parseResources(w.ResourceNames, proxy), proxy, proxyClusterSecrets)

	results := model.Resources{}
	cached, regenerated := 0, 0
	for _, sr := range resources {
		if updatedSecrets != nil {
			if !containsAny(updatedSecrets, relatedConfigs(model.ConfigKey{Kind: gvk.Secret, Name: sr.Name, Namespace: sr.Namespace})) {
				// This is an incremental update, filter out secrets that are not updated.
				continue
			}
		}

		// Fetch the appropriate cluster's secrets, based on the credential type
		var secretController secrets.Controller
		switch sr.Type {
		case credentials.KubernetesGatewaySecretType:
			secretController = configClusterSecrets
		default:
			secretController = proxyClusterSecrets
		}

		cachedItem, f := s.cache.Get(sr)
		if f && !features.EnableUnsafeAssertions {
			// If it is in the Cache, add it and continue
			// We skip cache if assertions are enabled, so that the cache will assert our eviction logic is correct
			results = append(results, cachedItem)
			cached++
			continue
		}
		regenerated++

		isCAOnlySecret := strings.HasSuffix(sr.Name, GatewaySdsCaSuffix)
		if isCAOnlySecret {
			secret, err := secretController.GetCaCert(sr.Name, sr.Namespace)
			if err != nil {
				pilotSDSCertificateErrors.Increment()
				log.Warnf("failed to fetch ca certificate for %v: %v", sr.ResourceName, err)
			} else {
				res := toEnvoyCaSecret(sr.ResourceName, secret)
				results = append(results, res)
				s.cache.Add(sr, req, res)
			}
		} else {
			key, cert, err := secretController.GetKeyAndCert(sr.Name, sr.Namespace)
			if err != nil {
				pilotSDSCertificateErrors.Increment()
				log.Warnf("failed to fetch key and certificate for %v: %v", sr.ResourceName, err)
			} else {
				res := toEnvoyKeyCertSecret(sr.ResourceName, key, cert)
				results = append(results, res)
				s.cache.Add(sr, req, res)
			}
		}
	}
	return results, model.XdsLogDetails{AdditionalInfo: fmt.Sprintf("cached:%v/%v", cached, cached+regenerated)}, nil
}

// filterAuthorizedResources takes a list of SecretResource and filters out resources that proxy cannot access
func filterAuthorizedResources(resources []SecretResource, proxy *model.Proxy, secrets secrets.Controller) []SecretResource {
	var authzResult *bool
	var authzError error
	// isAuthorized is a small wrapper around secrets.Authorize so we only call it once instead of each time in the loop
	isAuthorized := func() bool {
		if authzResult != nil {
			return *authzResult
		}
		res := false
		if err := secrets.Authorize(proxy.VerifiedIdentity.ServiceAccount, proxy.VerifiedIdentity.Namespace); err == nil {
			res = true
		} else {
			authzError = err
		}
		authzResult = &res
		return res
	}

	// There are 4 cases of secret reference
	// Verified cross namespace (by ReferencePolicy). No Authz needed.
	// Verified same namespace (implicit). No Authz needed.
	// Unverified cross namespace. Never allowed.
	// Unverified same namespace. Allowed if authorized.
	allowedResources := make([]SecretResource, 0, len(resources))
	for _, r := range resources {
		sameNamespace := r.Namespace == proxy.VerifiedIdentity.Namespace
		verified := proxy.MergedGateway != nil && proxy.MergedGateway.VerifiedCertificateReferences.Contains(r.ResourceName)
		switch r.Type {
		case credentials.KubernetesGatewaySecretType:
			// For KubernetesGateway, we only allow VerifiedCertificateReferences.
			// This means a Secret in the same namespace as the Gateway (which also must be in the same namespace
			// as the proxy), or a ReferencePolicy allowing the reference.
			if verified {
				allowedResources = append(allowedResources, r)
			}
		case credentials.KubernetesSecretType:
			// For Kubernetes, we require the secret to be in the same namespace as the proxy and for it to be
			// authorized for access.
			if sameNamespace && isAuthorized() {
				allowedResources = append(allowedResources, r)
			}
		default:
			// Should never happen
			log.Warnf("unknown credential type %q", r.Type)
			pilotSDSCertificateErrors.Increment()
		}
	}

	// If we filtered any out, report an error. We aggregate errors in one place here, rather than in the loop,
	// to avoid excessive logs.
	if len(allowedResources) != len(resources) {
		errMessage := authzError
		if errMessage == nil {
			errMessage = fmt.Errorf("cross namespace secret reference requires ReferencePolicy")
		}
		log.Warnf("proxy %v attempted to access %d unauthorized certificates: %v", proxy.ID, len(resources)-len(allowedResources), errMessage)
		pilotSDSCertificateErrors.Increment()
	}

	return allowedResources
}

func toEnvoyCaSecret(name string, cert []byte) *discovery.Resource {
	res := util.MessageToAny(&tls.Secret{
		Name: name,
		Type: &tls.Secret_ValidationContext{
			ValidationContext: &tls.CertificateValidationContext{
				TrustedCa: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{
						InlineBytes: cert,
					},
				},
			},
		},
	})
	return &discovery.Resource{
		Name:     name,
		Resource: res,
	}
}

func toEnvoyKeyCertSecret(name string, key, cert []byte) *discovery.Resource {
	res := util.MessageToAny(&tls.Secret{
		Name: name,
		Type: &tls.Secret_TlsCertificate{
			TlsCertificate: &tls.TlsCertificate{
				CertificateChain: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{
						InlineBytes: cert,
					},
				},
				PrivateKey: &core.DataSource{
					Specifier: &core.DataSource_InlineBytes{
						InlineBytes: key,
					},
				},
			},
		},
	})
	return &discovery.Resource{
		Name:     name,
		Resource: res,
	}
}

func containsAny(mp map[model.ConfigKey]struct{}, keys []model.ConfigKey) bool {
	for _, k := range keys {
		if _, f := mp[k]; f {
			return true
		}
	}
	return false
}

// relatedConfigs maps a single resource to a list of relevant resources. This is used for cache invalidation
// and push skipping. This is because an secret potentially has a dependency on the same secret with or without
// the -cacert suffix. By including this dependency we ensure we do not miss any updates.
// This is important for cases where we have a compound secret. In this case, the `foo` secret may update,
// but we need to push both the `foo` and `foo-cacert` resource name, or they will fall out of sync.
func relatedConfigs(k model.ConfigKey) []model.ConfigKey {
	related := []model.ConfigKey{k}
	// For secrets without -cacert suffix, add the suffix
	if !strings.HasSuffix(k.Name, GatewaySdsCaSuffix) {
		withSuffix := k
		withSuffix.Name += GatewaySdsCaSuffix
		related = append(related, withSuffix)
	}
	// For secrets with -cacert suffix, remove the suffix
	if strings.HasSuffix(k.Name, GatewaySdsCaSuffix) {
		withoutSuffix := k
		withoutSuffix.Name = strings.TrimSuffix(withoutSuffix.Name, GatewaySdsCaSuffix)
		related = append(related, withoutSuffix)
	}
	return related
}

type SecretGen struct {
	secrets secrets.MulticlusterController
	// Cache for XDS resources
	cache         model.XdsCache
	configCluster cluster.ID
}

var _ model.XdsResourceGenerator = &SecretGen{}

func NewSecretGen(sc secrets.MulticlusterController, cache model.XdsCache, configCluster cluster.ID) *SecretGen {
	// TODO: Currently we only have a single secrets controller (Kubernetes). In the future, we will need a mapping
	// of resource type to secret controller (ie kubernetes:// -> KubernetesController, vault:// -> VaultController)
	return &SecretGen{
		secrets:       sc,
		cache:         cache,
		configCluster: configCluster,
	}
}
