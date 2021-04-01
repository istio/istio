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
	"github.com/golang/protobuf/ptypes/any"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/secrets"
	authnmodel "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pkg/config/schema/gvk"
)

const (
	// GatewaySdsCaSuffix is the suffix of the sds resource name for root CA. All resource
	// names for gateway root certs end with "-cacert".
	GatewaySdsCaSuffix = "-cacert"
)

type SecretResource struct {
	Type         string
	Name         string
	Namespace    string
	ResourceName string
}

func (sr SecretResource) Key() string {
	return "sds://" + sr.ResourceName
}

func (sr SecretResource) DependentConfigs() []model.ConfigKey {
	return relatedConfigs(model.ConfigKey{Kind: gvk.Secret, Name: sr.Name, Namespace: sr.Namespace})
}

func (sr SecretResource) Cacheable() bool {
	return true
}

var _ model.XdsCacheEntry = SecretResource{}

func parseResourceName(resource, defaultNamespace string) (SecretResource, error) {
	sep := "/"
	if strings.HasPrefix(resource, authnmodel.KubernetesSecretTypeURI) {
		res := strings.TrimPrefix(resource, authnmodel.KubernetesSecretTypeURI)
		split := strings.Split(res, sep)
		namespace := defaultNamespace
		name := split[0]
		if len(split) > 1 {
			namespace = split[0]
			name = split[1]
		}
		return SecretResource{Type: authnmodel.KubernetesSecretType, Name: name, Namespace: namespace, ResourceName: resource}, nil
	}
	return SecretResource{}, fmt.Errorf("unknown resource type: %v", resource)
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

// Currently only same namespace is allowed. In the future this will be expanded.
func (s *SecretGen) proxyAuthorizedForSecret(proxy *model.Proxy, sr SecretResource) error {
	if proxy.ConfigNamespace != sr.Namespace {
		return fmt.Errorf("SDS is currently only supporting accessing secret within the same namespace. Secret namespace %q does not match proxy namespace %q",
			sr.Namespace, proxy.ConfigNamespace)
	}
	return nil
}

func (s *SecretGen) Generate(proxy *model.Proxy, push *model.PushContext, w *model.WatchedResource, req *model.PushRequest) (model.Resources, error) {
	if proxy.VerifiedIdentity == nil {
		adsLog.Warnf("proxy %v is not authorized to receive secrets. Ensure you are connecting over TLS port and are authenticated.", proxy.ID)
		return nil, nil
	}
	secrets, err := s.secrets.ForCluster(proxy.Metadata.ClusterID)
	if err != nil {
		adsLog.Warnf("proxy %v is from an unknown cluster, cannot retrieve certificates: %v", proxy.ID, err)
		return nil, nil
	}
	if err := secrets.Authorize(proxy.VerifiedIdentity.ServiceAccount, proxy.VerifiedIdentity.Namespace); err != nil {
		adsLog.Warnf("proxy %v is not authorized to receive secrets: %v", proxy.ID, err)
		return nil, nil
	}
	if req == nil || !needsUpdate(proxy, req.ConfigsUpdated) {
		return nil, nil
	}
	var updatedSecrets map[model.ConfigKey]struct{}
	if !req.Full {
		updatedSecrets = model.ConfigsOfKind(req.ConfigsUpdated, gvk.Secret)
	}
	results := model.Resources{}
	cached, regenerated := 0, 0
	for _, resource := range w.ResourceNames {
		sr, err := parseResourceName(resource, proxy.ConfigNamespace)
		if err != nil {
			adsLog.Warnf("error parsing resource name: %v", err)
			continue
		}

		if updatedSecrets != nil {
			if !containsAny(updatedSecrets, relatedConfigs(model.ConfigKey{Kind: gvk.Secret, Name: sr.Name, Namespace: sr.Namespace})) {
				// This is an incremental update, filter out secrets that are not updated.
				continue
			}
		}

		if err := s.proxyAuthorizedForSecret(proxy, sr); err != nil {
			adsLog.Warnf("requested secret %v not accessible for proxy %v: %v", sr.ResourceName, proxy.ID, err)
			continue
		}
		cachedItem, token, f := s.cache.Get(sr)
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
			secret := secrets.GetCaCert(sr.Name, sr.Namespace)
			if secret != nil {
				res := toEnvoyCaSecret(sr.ResourceName, secret)
				results = append(results, res)
				s.cache.Add(sr, token, res)
			} else {
				adsLog.Warnf("failed to fetch ca certificate for %v", sr.ResourceName)
			}
		} else {
			key, cert := secrets.GetKeyAndCert(sr.Name, sr.Namespace)
			if key != nil && cert != nil {
				res := toEnvoyKeyCertSecret(sr.ResourceName, key, cert)
				results = append(results, res)
				s.cache.Add(sr, token, res)
			} else {
				adsLog.Warnf("failed to fetch key and certificate for %v", sr.ResourceName)
			}
		}
	}
	adsLog.Infof("SDS: PUSH for node:%s resources:%d size:%s cached:%v/%v",
		proxy.ID, len(results), util.ByteCount(ResourceSize(results)), cached, cached+regenerated)
	return results, nil
}

func toEnvoyCaSecret(name string, cert []byte) *any.Any {
	return util.MessageToAny(&tls.Secret{
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
}

func toEnvoyKeyCertSecret(name string, key, cert []byte) *any.Any {
	return util.MessageToAny(&tls.Secret{
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
	cache model.XdsCache
}

var _ model.XdsResourceGenerator = &SecretGen{}

func NewSecretGen(sc secrets.MulticlusterController, cache model.XdsCache) *SecretGen {
	// TODO: Currently we only have a single secrets controller (Kubernetes). In the future, we will need a mapping
	// of resource type to secret controller (ie kubernetes:// -> KubernetesController, vault:// -> VaultController)
	return &SecretGen{
		secrets: sc,
		cache:   cache,
	}
}
