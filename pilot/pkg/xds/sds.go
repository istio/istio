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
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strconv"
	"strings"
	"time"

	xxhashv2 "github.com/cespare/xxhash/v2"
	cryptomb "github.com/envoyproxy/go-control-plane/contrib/envoy/extensions/private_key_providers/cryptomb/v3alpha"
	qat "github.com/envoyproxy/go-control-plane/contrib/envoy/extensions/private_key_providers/qat/v3alpha"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoytls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"

	mesh "istio.io/api/mesh/v1alpha1"
	credscontroller "istio.io/istio/pilot/pkg/credentials"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/credentials"
	securitymodel "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/util/sets"
)

// SecretResource wraps the authnmodel type with cache functions implemented
type SecretResource struct {
	credentials.SecretResource
	pkpConfHash string
}

var _ model.XdsCacheEntry = SecretResource{}

func (sr SecretResource) Type() string {
	return model.SDSType
}

func (sr SecretResource) Key() any {
	return sr.SecretResource.Key() + "/" + sr.pkpConfHash
}

func (sr SecretResource) DependentConfigs() []model.ConfigHash {
	configs := []model.ConfigHash{}
	for _, config := range relatedConfigs(model.ConfigKey{Kind: sr.ResourceKind, Name: sr.Name, Namespace: sr.Namespace}) {
		configs = append(configs, config.HashCode())
	}
	return configs
}

func (sr SecretResource) Cacheable() bool {
	return true
}

func sdsNeedsPush(forced bool, updates model.XdsUpdates) bool {
	if forced {
		return true
	}
	for update := range updates {
		switch update.Kind {
		case kind.Secret:
			return true
		case kind.ConfigMap:
			return true
		}
	}
	return false
}

// parseResources parses a list of resource names to SecretResource types, for a given proxy.
// Invalid resource names are ignored
func (s *SecretGen) parseResources(names []string, proxy *model.Proxy) []SecretResource {
	res := make([]SecretResource, 0, len(names))
	pkpConf := (*mesh.ProxyConfig)(proxy.Metadata.ProxyConfig).GetPrivateKeyProvider()
	pkpConfHashStr := ""
	if pkpConf != nil {
		pkpConfHashStr = strconv.FormatUint(xxhashv2.Sum64String(pkpConf.String()), 10)
	}
	for _, resource := range names {
		sr, err := credentials.ParseResourceName(resource, proxy.VerifiedIdentity.Namespace, proxy.Metadata.ClusterID, s.configCluster)
		if err != nil {
			pilotSDSCertificateErrors.Increment()
			log.Warnf("error parsing resource name: %v", err)
			continue
		}
		res = append(res, SecretResource{sr, pkpConfHashStr})
	}
	return res
}

func (s *SecretGen) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	if proxy.VerifiedIdentity == nil {
		log.Warnf("proxy %s is not authorized to receive credscontroller. Ensure you are connecting over TLS port and are authenticated.", proxy.ID)
		return nil, model.DefaultXdsLogDetails, nil
	}
	if req == nil || !sdsNeedsPush(req.Forced, req.ConfigsUpdated) {
		return nil, model.DefaultXdsLogDetails, nil
	}
	var updatedSecrets sets.Set[model.ConfigKey]
	if !req.Full {
		updatedSecrets = model.ConfigsOfKind(req.ConfigsUpdated, kind.Secret).Merge(model.ConfigsOfKind(req.ConfigsUpdated, kind.ConfigMap))
	}

	proxyClusterSecrets, err := s.secrets.ForCluster(proxy.Metadata.ClusterID)
	if err != nil {
		log.Warnf("proxy %s is from an unknown cluster, cannot retrieve certificates: %v", proxy.ID, err)
		pilotSDSCertificateErrors.Increment()
		return nil, model.DefaultXdsLogDetails, nil
	}
	configClusterSecrets, err := s.secrets.ForCluster(s.configCluster)
	if err != nil {
		log.Warnf("config cluster %s not found, cannot retrieve certificates: %v", s.configCluster, err)
		pilotSDSCertificateErrors.Increment()
		return nil, model.DefaultXdsLogDetails, nil
	}

	// Filter down to resources we can access. We do not return an error if they attempt to access a Secret
	// they cannot; instead we just exclude it. This ensures that a single bad reference does not break the whole
	// SDS flow. The pilotSDSCertificateErrors metric and logs handle visibility into invalid references.
	resources := filterAuthorizedResources(s.parseResources(w.ResourceNames.UnsortedList(), proxy), proxy, proxyClusterSecrets)

	var results model.Resources
	cached, regenerated := 0, 0
	for _, sr := range resources {
		if updatedSecrets != nil {
			if !containsAny(updatedSecrets, relatedConfigs(model.ConfigKey{Kind: sr.ResourceKind, Name: sr.Name, Namespace: sr.Namespace})) {
				// This is an incremental update, filter out secrets that are not updated.
				continue
			}
		}

		cachedItem := s.cache.Get(sr)
		if cachedItem != nil && !features.EnableUnsafeAssertions {
			// If it is in the Cache, add it and continue
			// We skip cache if assertions are enabled, so that the cache will assert our eviction logic is correct
			results = append(results, cachedItem)
			cached++
			continue
		}
		regenerated++
		res := s.generate(sr, configClusterSecrets, proxyClusterSecrets, proxy)
		if res != nil {
			s.cache.Add(sr, req, res)
			results = append(results, res)
		}
	}
	return results, model.XdsLogDetails{
		Incremental:    updatedSecrets != nil,
		AdditionalInfo: fmt.Sprintf("cached:%v/%v", cached, cached+regenerated),
	}, nil
}

func (s *SecretGen) generate(sr SecretResource, configClusterSecrets, proxyClusterSecrets credscontroller.Controller, proxy *model.Proxy) *discovery.Resource {
	// Fetch the appropriate cluster's secret, based on the credential type
	var secretController credscontroller.Controller
	switch sr.ResourceType {
	case credentials.KubernetesGatewaySecretType, credentials.KubernetesConfigMapType:
		secretController = configClusterSecrets
	default:
		secretController = proxyClusterSecrets
	}

	isCAOnlySecret := strings.HasSuffix(sr.Name, securitymodel.SdsCaSuffix)
	if sr.ResourceType == credentials.KubernetesConfigMapType {
		caCertInfo, err := secretController.GetConfigMapCaCert(sr.Name, sr.Namespace)
		if err != nil {
			pilotSDSCertificateErrors.Increment()
			log.Warnf("failed to fetch ca certificate for %s: %v", sr.ResourceName, err)
			return nil
		}
		if err := ValidateCertificate(caCertInfo.Cert); err != nil {
			recordInvalidCertificate(sr.ResourceName, err)
		}
		res := toEnvoyCaSecret(sr.ResourceName, caCertInfo)
		return res
	} else if isCAOnlySecret {
		caCertInfo, err := secretController.GetCaCert(sr.Name, sr.Namespace)
		if err != nil {
			pilotSDSCertificateErrors.Increment()
			log.Warnf("failed to fetch ca certificate for %s: %v", sr.ResourceName, err)
			return nil
		}
		if err := ValidateCertificate(caCertInfo.Cert); err != nil {
			recordInvalidCertificate(sr.ResourceName, err)
		}
		res := toEnvoyCaSecret(sr.ResourceName, caCertInfo)
		return res
	}
	certInfo, err := secretController.GetCertInfo(sr.Name, sr.Namespace)
	if err != nil {
		pilotSDSCertificateErrors.Increment()
		log.Warnf("failed to fetch key and certificate for %s: %v", sr.ResourceName, err)
		return nil
	}
	if err := ValidateCertificate(certInfo.Cert); err != nil {
		recordInvalidCertificate(sr.ResourceName, err)
	}
	res := toEnvoyTLSSecret(sr.ResourceName, certInfo, proxy, s.meshConfig)
	return res
}

func ValidateCertificate(data []byte) error {
	block, _ := pem.Decode(data)
	if block == nil {
		return fmt.Errorf("pem decode failed")
	}
	certs, err := x509.ParseCertificates(block.Bytes)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, cert := range certs {
		// check if the certificate has expired
		if now.After(cert.NotAfter) || now.Before(cert.NotBefore) {
			return fmt.Errorf("certificate is expired or not yet valid")
		}
	}
	return nil
}

func recordInvalidCertificate(name string, err error) {
	pilotSDSCertificateErrors.Increment()
	log.Warnf("invalid certificates: %q: %v", name, err)
}

// filterAuthorizedResources takes a list of SecretResource and filters out resources that proxy cannot access
func filterAuthorizedResources(resources []SecretResource, proxy *model.Proxy, secrets credscontroller.Controller) []SecretResource {
	var authzResult *bool
	var authzError error
	// isAuthorized is a small wrapper around credscontroller.Authorize so we only call it once instead of each time in the loop
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
	deniedResources := make([]string, 0)
	for _, r := range resources {
		sameNamespace := r.Namespace == proxy.VerifiedIdentity.Namespace
		verified := proxy.MergedGateway != nil && proxy.MergedGateway.VerifiedCertificateReferences.Contains(r.ResourceName)
		switch r.ResourceType {
		case credentials.KubernetesGatewaySecretType:
			// For KubernetesGateway, we only allow VerifiedCertificateReferences.
			// This means a Secret in the same namespace as the Gateway (which also must be in the same namespace
			// as the proxy), or a ReferencePolicy allowing the reference.
			if verified {
				allowedResources = append(allowedResources, r)
			} else {
				deniedResources = append(deniedResources, r.Name)
			}
		case credentials.KubernetesConfigMapType:
			// Current, we allow any configmap references. We only expose the ca.crt field, which should not be 'private'.
			// We cannot do a namespace check, as the client is the one reading the configmap of the server, so cross-namespace
			// lookups are expected.
			// We could do a check that the ConfigMap is referenced by a DestinationRule, but given the lack of impact here
			// this seems over-complicated.
			allowedResources = append(allowedResources, r)

		case credentials.KubernetesSecretType:
			// CA Certs are public information, so we allows allow these to be accessed (from the same namespace)
			isCAOnlySecret := strings.HasSuffix(r.Name, securitymodel.SdsCaSuffix)
			// For Kubernetes, we require the secret to be in the same namespace as the proxy and for it to be
			// authorized for access.
			if sameNamespace && (isCAOnlySecret || isAuthorized()) {
				// if sameNamespace && isAuthorized() {
				allowedResources = append(allowedResources, r)
			} else {
				deniedResources = append(deniedResources, r.Name)
			}
		case credentials.InvalidSecretType:
			// Do nothing. We return nothing, and logs for why an invalid resource was generated are handled elsewhere.
		default:
			// Should never happen
			log.Warnf("unknown credential type %q", r.Type())
			pilotSDSCertificateErrors.Increment()
		}
	}

	// If we filtered any out, report an error. We aggregate errors in one place here, rather than in the loop,
	// to avoid excessive logs.
	if len(deniedResources) > 0 {
		errMessage := authzError
		if errMessage == nil {
			errMessage = fmt.Errorf("cross namespace secret reference requires ReferencePolicy")
		}
		log.Warnf("proxy %s attempted to access unauthorized certificates %s: %v", proxy.ID, atMostNJoin(deniedResources, 3), errMessage)
		pilotSDSCertificateErrors.Increment()
	}

	return allowedResources
}

func toEnvoyCaSecret(name string, certInfo *credscontroller.CertInfo) *discovery.Resource {
	validationContext := &envoytls.CertificateValidationContext{
		TrustedCa: &core.DataSource{
			Specifier: &core.DataSource_InlineBytes{
				InlineBytes: certInfo.Cert,
			},
		},
	}
	if certInfo.CRL != nil {
		validationContext.Crl = &core.DataSource{
			Specifier: &core.DataSource_InlineBytes{
				InlineBytes: certInfo.CRL,
			},
		}
	}
	res := protoconv.MessageToAny(&envoytls.Secret{
		Name: name,
		Type: &envoytls.Secret_ValidationContext{
			ValidationContext: validationContext,
		},
	})
	return &discovery.Resource{
		Name:     name,
		Resource: res,
	}
}

func toEnvoyTLSSecret(name string, certInfo *credscontroller.CertInfo, proxy *model.Proxy, meshConfig *mesh.MeshConfig) *discovery.Resource {
	var res *anypb.Any
	pkpConf := proxy.Metadata.ProxyConfigOrDefault(meshConfig.GetDefaultConfig()).GetPrivateKeyProvider()
	switch pkpConf.GetProvider().(type) {
	case *mesh.PrivateKeyProvider_Cryptomb:
		crypto := pkpConf.GetCryptomb()
		msg := protoconv.MessageToAny(&cryptomb.CryptoMbPrivateKeyMethodConfig{
			PollDelay: durationpb.New(time.Duration(crypto.GetPollDelay().Nanos)),
			PrivateKey: &core.DataSource{
				Specifier: &core.DataSource_InlineBytes{
					InlineBytes: certInfo.Key,
				},
			},
		})
		res = protoconv.MessageToAny(&envoytls.Secret{
			Name: name,
			Type: &envoytls.Secret_TlsCertificate{
				TlsCertificate: &envoytls.TlsCertificate{
					CertificateChain: &core.DataSource{
						Specifier: &core.DataSource_InlineBytes{
							InlineBytes: certInfo.Cert,
						},
					},
					PrivateKeyProvider: &envoytls.PrivateKeyProvider{
						ProviderName: "cryptomb",
						ConfigType: &envoytls.PrivateKeyProvider_TypedConfig{
							TypedConfig: msg,
						},
						Fallback: crypto.GetFallback().GetValue(),
					},
				},
			},
		})
	case *mesh.PrivateKeyProvider_Qat:
		qatConf := pkpConf.GetQat()
		msg := protoconv.MessageToAny(&qat.QatPrivateKeyMethodConfig{
			PollDelay: durationpb.New(time.Duration(qatConf.GetPollDelay().Nanos)),
			PrivateKey: &core.DataSource{
				Specifier: &core.DataSource_InlineBytes{
					InlineBytes: certInfo.Key,
				},
			},
		})
		res = protoconv.MessageToAny(&envoytls.Secret{
			Name: name,
			Type: &envoytls.Secret_TlsCertificate{
				TlsCertificate: &envoytls.TlsCertificate{
					CertificateChain: &core.DataSource{
						Specifier: &core.DataSource_InlineBytes{
							InlineBytes: certInfo.Cert,
						},
					},
					PrivateKeyProvider: &envoytls.PrivateKeyProvider{
						ProviderName: "qat",
						ConfigType: &envoytls.PrivateKeyProvider_TypedConfig{
							TypedConfig: msg,
						},
						Fallback: qatConf.GetFallback().GetValue(),
					},
				},
			},
		})
	default:
		tlsCertificate := &envoytls.TlsCertificate{
			CertificateChain: &core.DataSource{
				Specifier: &core.DataSource_InlineBytes{
					InlineBytes: certInfo.Cert,
				},
			},
			PrivateKey: &core.DataSource{
				Specifier: &core.DataSource_InlineBytes{
					InlineBytes: certInfo.Key,
				},
			},
		}
		if certInfo.Staple != nil {
			tlsCertificate.OcspStaple = &core.DataSource{
				Specifier: &core.DataSource_InlineBytes{
					InlineBytes: certInfo.Staple,
				},
			}
		}
		res = protoconv.MessageToAny(&envoytls.Secret{
			Name: name,
			Type: &envoytls.Secret_TlsCertificate{
				TlsCertificate: tlsCertificate,
			},
		})
	}
	return &discovery.Resource{
		Name:     name,
		Resource: res,
	}
}

func containsAny(mp sets.Set[model.ConfigKey], keys []model.ConfigKey) bool {
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
	// For secret without -cacert suffix, add the suffix
	if !strings.HasSuffix(k.Name, securitymodel.SdsCaSuffix) {
		k.Name += securitymodel.SdsCaSuffix
		related = append(related, k)
	} else {
		// For secret with -cacert suffix, remove the suffix
		k.Name = strings.TrimSuffix(k.Name, securitymodel.SdsCaSuffix)
		related = append(related, k)
	}
	return related
}

type SecretGen struct {
	secrets credscontroller.MulticlusterController
	// Cache for XDS resources
	cache         model.XdsCache
	configCluster cluster.ID
	meshConfig    *mesh.MeshConfig
}

var _ model.XdsResourceGenerator = &SecretGen{}

func NewSecretGen(sc credscontroller.MulticlusterController, cache model.XdsCache, configCluster cluster.ID,
	meshConfig *mesh.MeshConfig,
) *SecretGen {
	// TODO: Currently we only have a single credentials controller (Kubernetes). In the future, we will need a mapping
	// of resource type to secret controller (ie kubernetes:// -> KubernetesController, vault:// -> VaultController)
	return &SecretGen{
		secrets:       sc,
		cache:         cache,
		configCluster: configCluster,
		meshConfig:    meshConfig,
	}
}
