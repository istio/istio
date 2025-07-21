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
	gotls "crypto/tls"
	"strings"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/credentials"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/log"
	pm "istio.io/istio/pkg/model"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/spiffe"
)

const (
	// SDSClusterName is the name of the cluster for SDS connections
	SDSClusterName = pm.SDSClusterName

	// SDSDefaultResourceName is the default name in sdsconfig, used for fetching normal key/cert.
	SDSDefaultResourceName = pm.SDSDefaultResourceName

	// SDSRootResourceName is the sdsconfig name for root CA, used for fetching root cert.
	SDSRootResourceName = pm.SDSRootResourceName

	// ThirdPartyJwtPath is the token volume mount file name for k8s trustworthy jwt token.
	ThirdPartyJwtPath = "/var/run/secrets/tokens/istio-token"

	// SdsCaSuffix is the suffix of the sds resource name for root CA.
	SdsCaSuffix = credentials.SdsCaSuffix

	// EnvoyJwtFilterName is the name of the Envoy JWT filter. This should be the same as the name defined
	// in https://github.com/envoyproxy/envoy/blob/v1.9.1/source/extensions/filters/http/well_known_names.h#L48
	EnvoyJwtFilterName = "envoy.filters.http.jwt_authn"
)

var SDSAdsConfig = &core.ConfigSource{
	ConfigSourceSpecifier: &core.ConfigSource_Ads{
		Ads: &core.AggregatedConfigSource{},
	},
	// We intentionally do *not* set InitialFetchTimeout to 0s here, as this is used for
	// credentialName SDS which may refer to secrets which do not exist. We do not want to block the
	// entire listener/cluster in these cases.
	ResourceApiVersion: core.ApiVersion_V3,
}

// ConstructSdsSecretConfigForCredential constructs SDS secret configuration used
// from certificates referenced by credentialName in DestinationRule or Gateway.
func ConstructSdsSecretConfigForCredential(name string, credentialSocketExist bool, push *model.PushContext) *tls.SdsSecretConfig {
	if name == "" {
		return nil
	}
	if name == credentials.BuiltinGatewaySecretTypeURI {
		return ConstructSdsSecretConfig(SDSDefaultResourceName)
	}
	if name == credentials.BuiltinGatewaySecretTypeURI+SdsCaSuffix {
		return ConstructSdsSecretConfig(SDSRootResourceName)
	}
	if strings.HasPrefix(name, security.SDSExternalCredentialPrefix) {
		// Check if External SDS is a provider and use its cluster name
		if sdsName, ok := strings.CutPrefix(name, security.SDSExternalCredentialPrefix); ok {
			for _, provider := range push.Mesh.ExtensionProviders {
				if provider.GetSds() != nil && provider.GetSds().Name == sdsName {
					_, cluster, err := model.LookupCluster(push, provider.GetSds().Service, int(provider.GetSds().Port))
					if err != nil {
						model.IncLookupClusterFailures("externalSds")
						log.Errorf("could not find cluster for external sds provider %q: %v", provider.GetSds(), err)
						return nil
					}
					return ConstructSdsSecretConfigForCredentialSocket(name, cluster)
				}
			}
		}
	}
	// if credentialSocketExist exists and credentialName is using SDSExternalCredentialPrefix
	// SDS will be served via SDSExternalClusterName
	if credentialSocketExist && strings.HasPrefix(name, security.SDSExternalCredentialPrefix) {
		return ConstructSdsSecretConfigForCredentialSocket(name, security.SDSExternalClusterName)
	}

	return &tls.SdsSecretConfig{
		Name:      credentials.ToResourceName(name),
		SdsConfig: SDSAdsConfig,
	}
}

// ConstructSdsSecretConfigForCredentialSocket constructs SDS Secret Configuration based on CredentialNameSocketPath
func ConstructSdsSecretConfigForCredentialSocket(name string, clusterName string) *tls.SdsSecretConfig {
	return &tls.SdsSecretConfig{
		Name: name,
		SdsConfig: &core.ConfigSource{
			ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
				ApiConfigSource: &core.ApiConfigSource{
					ApiType:                   core.ApiConfigSource_GRPC,
					SetNodeOnFirstMessageOnly: true,
					TransportApiVersion:       core.ApiVersion_V3,
					GrpcServices: []*core.GrpcService{
						{
							TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
								EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: clusterName},
							},
						},
					},
				},
			},
			ResourceApiVersion: core.ApiVersion_V3,
		},
	}
}

// ConstructSdsSecretConfig constructs SDS Secret Configuration for workload proxy.
func ConstructSdsSecretConfig(name string) *tls.SdsSecretConfig {
	return pm.ConstructSdsSecretConfig(name)
}

func AppendURIPrefixToTrustDomain(trustDomainAliases []string) []string {
	res := make([]string, 0, len(trustDomainAliases))
	for _, td := range trustDomainAliases {
		res = append(res, spiffe.URIPrefix+td+"/")
	}
	return res
}

// ApplyToCommonTLSContext completes the commonTlsContext
func ApplyToCommonTLSContext(tlsContext *tls.CommonTlsContext, proxy *model.Proxy,
	subjectAltNames []string, crl string, trustDomainAliases []string, validateClient bool,
	tlsCertificates []*networking.ServerTLSSettings_TLSCertificate,
) {
	sdsSecretConfigs := make([]*tls.SdsSecretConfig, 0)
	customFileSDSServer := proxy.Metadata.Raw[security.CredentialFileMetaDataName] == "true"
	// Envoy does not support client validation using multiple CA certificates when multiple certificates are provided.
	// So we only use what's provided in the ServerTLSSettings.
	caCert := proxy.Metadata.TLSServerRootCert

	// These are certs being mounted from within the pod. Rather than reading directly in Envoy,
	// which does not support rotation, we will serve them over SDS by reading the files.
	// We should check if these certs have values, if yes we should use them or otherwise fall back to defaults.
	if len(tlsCertificates) > 0 {
		for _, cert := range tlsCertificates {
			res := security.SdsCertificateConfig{
				CertificatePath:   cert.ServerCertificate,
				PrivateKeyPath:    cert.PrivateKey,
				CaCertificatePath: caCert,
			}
			sdsSecretConfigs = append(sdsSecretConfigs, constructSdsSecretConfig(res.GetResourceName(), SDSDefaultResourceName, customFileSDSServer))
		}
	} else {
		res := security.SdsCertificateConfig{
			CertificatePath:   proxy.Metadata.TLSServerCertChain,
			PrivateKeyPath:    proxy.Metadata.TLSServerKey,
			CaCertificatePath: caCert,
		}
		sdsSecretConfigs = append(sdsSecretConfigs, constructSdsSecretConfig(res.GetResourceName(), SDSDefaultResourceName, customFileSDSServer))
	}
	// TODO: if subjectAltName ends with *, create a prefix match as well.
	// TODO: if user explicitly specifies SANs - should we alter his explicit config by adding all spifee aliases?
	matchSAN := util.StringToExactMatch(subjectAltNames)
	if len(trustDomainAliases) > 0 {
		matchSAN = append(matchSAN, util.StringToPrefixMatch(AppendURIPrefixToTrustDomain(trustDomainAliases))...)
	}

	// configure server listeners with SDS.
	tlsContext.TlsCertificateSdsSecretConfigs = sdsSecretConfigs

	// configure client validation context with SDS if enabled.
	if validateClient {
		defaultValidationContext := &tls.CertificateValidationContext{
			MatchSubjectAltNames: matchSAN,
		}
		if crl != "" {
			defaultValidationContext.Crl = &core.DataSource{
				Specifier: &core.DataSource_Filename{
					Filename: crl,
				},
			}
		}
		caRes := security.SdsCertificateConfig{
			CaCertificatePath: caCert,
		}
		tlsContext.ValidationContextType = &tls.CommonTlsContext_CombinedValidationContext{
			CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
				DefaultValidationContext:         defaultValidationContext,
				ValidationContextSdsSecretConfig: constructSdsSecretConfig(caRes.GetRootResourceName(), SDSRootResourceName, customFileSDSServer),
			},
		}
	}
}

// constructSdsSecretConfig allows passing a file name and a fallback.
// If the filename is set, it is used and the customFileSDSServer flag is respected.
func constructSdsSecretConfig(maybeFileName string, fallbackName string, customFileSDSServer bool) *tls.SdsSecretConfig {
	if maybeFileName != "" && customFileSDSServer {
		return pm.ConstructSdsFilesSecretConfig(maybeFileName)
	}
	return pm.ConstructSdsSecretConfig(model.GetOrDefault(maybeFileName, fallbackName))
}

// ApplyCustomSDSToClientCommonTLSContext applies the customized sds to CommonTlsContext
func ApplyCustomSDSToClientCommonTLSContext(tlsContext *tls.CommonTlsContext,
	tlsOpts *networking.ClientTLSSettings, credentialSocketExist bool,
) {
	if tlsOpts.CredentialName != "" {
		// If credential name is specified at Destination Rule config and originating node is egress gateway, create
		// SDS config for egress gateway to fetch key/cert at gateway agent.
		tlsContext.TlsCertificateSdsSecretConfigs = append(tlsContext.TlsCertificateSdsSecretConfigs,
			ConstructSdsSecretConfigForCredential(tlsOpts.CredentialName, credentialSocketExist, nil))
	}

	// If the InsecureSkipVerify is true, there is no need to configure CA Cert and SAN.
	if tlsOpts.GetInsecureSkipVerify().GetValue() {
		return
	}

	// create SDS config for gateway to fetch certificate validation context
	// at gateway agent.
	defaultValidationContext := &tls.CertificateValidationContext{
		MatchSubjectAltNames: util.StringToExactMatch(tlsOpts.SubjectAltNames),
	}
	tlsContext.ValidationContextType = &tls.CommonTlsContext_CombinedValidationContext{
		CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
			DefaultValidationContext: defaultValidationContext,
			ValidationContextSdsSecretConfig: ConstructSdsSecretConfigForCredential(
				tlsOpts.CredentialName+SdsCaSuffix, credentialSocketExist, nil),
		},
	}
}

// ApplyCredentialSDSToServerCommonTLSContext applies the credentialName sds (Gateway/DestinationRule) to CommonTlsContext
// Used for building both gateway/sidecar TLS context
func ApplyCredentialSDSToServerCommonTLSContext(tlsContext *tls.CommonTlsContext,
	tlsOpts *networking.ServerTLSSettings, credentialSocketExist bool, push *model.PushContext,
) {
	// create SDS config for gateway/sidecar to fetch key/cert from agent.
	caCert := tlsOpts.CredentialName + SdsCaSuffix
	if len(tlsOpts.CredentialNames) > 0 {
		// Handle multiple certificates for RSA and ECDSA
		tlsContext.TlsCertificateSdsSecretConfigs = make([]*tls.SdsSecretConfig, len(tlsOpts.CredentialNames))
		for i, name := range tlsOpts.CredentialNames {
			tlsContext.TlsCertificateSdsSecretConfigs[i] = ConstructSdsSecretConfigForCredential(name, credentialSocketExist, push)
		}
		// If MUTUAL, we only support one CA certificate for all credentialNames. Thus we use the first one as CA.
		caCert = tlsOpts.CredentialNames[0] + SdsCaSuffix
	} else {
		// Handle single certificate
		tlsContext.TlsCertificateSdsSecretConfigs = []*tls.SdsSecretConfig{
			ConstructSdsSecretConfigForCredential(tlsOpts.CredentialName, credentialSocketExist, push),
		}
	}

	// If tls mode is MUTUAL/OPTIONAL_MUTUAL, create SDS config for gateway/sidecar to fetch certificate validation context
	// at gateway agent. Otherwise, use the static certificate validation context config.
	if tlsOpts.Mode == networking.ServerTLSSettings_MUTUAL || tlsOpts.Mode == networking.ServerTLSSettings_OPTIONAL_MUTUAL {
		defaultValidationContext := &tls.CertificateValidationContext{
			MatchSubjectAltNames:  util.StringToExactMatch(tlsOpts.SubjectAltNames),
			VerifyCertificateSpki: tlsOpts.VerifyCertificateSpki,
			VerifyCertificateHash: tlsOpts.VerifyCertificateHash,
		}
		tlsContext.ValidationContextType = &tls.CommonTlsContext_CombinedValidationContext{
			CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
				DefaultValidationContext:         defaultValidationContext,
				ValidationContextSdsSecretConfig: ConstructSdsSecretConfigForCredential(caCert, credentialSocketExist, push),
			},
		}
	} else if len(tlsOpts.SubjectAltNames) > 0 {
		tlsContext.ValidationContextType = &tls.CommonTlsContext_ValidationContext{
			ValidationContext: &tls.CertificateValidationContext{
				MatchSubjectAltNames: util.StringToExactMatch(tlsOpts.SubjectAltNames),
			},
		}
	}
}

func EnforceGoCompliance(ctx *gotls.Config) {
	pm.EnforceGoCompliance(ctx)
}

func EnforceCompliance(ctx *tls.CommonTlsContext) {
	pm.EnforceCompliance(ctx)
}
