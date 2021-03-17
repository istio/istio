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
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/golang/protobuf/ptypes"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/spiffe"
)

const (
	// SDSStatPrefix is the human readable prefix to use when emitting statistics for the SDS service.
	SDSStatPrefix = "sdsstat"

	// SDSClusterName is the name of the cluster for SDS connections
	SDSClusterName = "sds-grpc"

	// SDSDefaultResourceName is the default name in sdsconfig, used for fetching normal key/cert.
	SDSDefaultResourceName = "default"

	// SDSRootResourceName is the sdsconfig name for root CA, used for fetching root cert.
	SDSRootResourceName = "ROOTCA"

	// K8sSAJwtFileName is the token volume mount file name for k8s jwt token.
	K8sSAJwtFileName = "/var/run/secrets/kubernetes.io/serviceaccount/token"

	// K8sSATrustworthyJwtFileName is the token volume mount file name for k8s trustworthy jwt token.
	K8sSATrustworthyJwtFileName = "/var/run/secrets/tokens/istio-token"

	// K8sSAJwtTokenHeaderKey is the request header key for k8s jwt token.
	// Binary header name must has suffix "-bin", according to https://github.com/grpc/grpc/blob/master/doc/PROTOCOL-HTTP2.md.
	K8sSAJwtTokenHeaderKey = "istio_sds_credentials_header-bin"

	// SdsCaSuffix is the suffix of the sds resource name for root CA.
	SdsCaSuffix = "-cacert"

	// EnvoyJwtFilterName is the name of the Envoy JWT filter. This should be the same as the name defined
	// in https://github.com/envoyproxy/envoy/blob/v1.9.1/source/extensions/filters/http/well_known_names.h#L48
	EnvoyJwtFilterName = "envoy.filters.http.jwt_authn"

	// AuthnFilterName is the name for the Istio AuthN filter. This should be the same
	// as the name defined in
	// https://github.com/istio/proxy/blob/master/src/envoy/http/authn/http_filter_factory.cc#L30
	AuthnFilterName = "istio_authn"

	// KubernetesSecretType is the name of a SDS secret stored in Kubernetes
	KubernetesSecretType    = "kubernetes"
	KubernetesSecretTypeURI = KubernetesSecretType + "://"
)

var SDSAdsConfig = &core.ConfigSource{
	ConfigSourceSpecifier: &core.ConfigSource_Ads{
		Ads: &core.AggregatedConfigSource{},
	},
	ResourceApiVersion: core.ApiVersion_V3,
}

// ConstructSdsSecretConfigForCredential constructs SDS secret configuration used
// from certificates referenced by credentialName in DestinationRule or Gateway.
// Currently this is served by a local SDS server, but in the future replaced by
// Istiod SDS server.
func ConstructSdsSecretConfigForCredential(name string) *tls.SdsSecretConfig {
	if name == "" {
		return nil
	}

	return &tls.SdsSecretConfig{
		Name:      KubernetesSecretTypeURI + name,
		SdsConfig: SDSAdsConfig,
	}
}

// Preconfigured SDS configs to avoid excessive memory allocations
var (
	// set the fetch timeout to 0 here in legacyDefaultSDSConfig and rootSDSConfig
	// because workload certs are guaranteed exist.
	legacyDefaultSDSConfig = &tls.SdsSecretConfig{
		Name: SDSDefaultResourceName,
		SdsConfig: &core.ConfigSource{
			ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
				ApiConfigSource: &core.ApiConfigSource{
					ApiType:             core.ApiConfigSource_GRPC,
					TransportApiVersion: core.ApiVersion_V3,
					GrpcServices: []*core.GrpcService{
						{
							TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
								EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
							},
						},
					},
				},
			},
			ResourceApiVersion:  core.ApiVersion_V3,
			InitialFetchTimeout: ptypes.DurationProto(time.Second * 0),
		},
	}
	legacyRootSDSConfig = &tls.SdsSecretConfig{
		Name: SDSRootResourceName,
		SdsConfig: &core.ConfigSource{
			ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
				ApiConfigSource: &core.ApiConfigSource{
					ApiType:             core.ApiConfigSource_GRPC,
					TransportApiVersion: core.ApiVersion_V3,
					GrpcServices: []*core.GrpcService{
						{
							TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
								EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
							},
						},
					},
				},
			},
			ResourceApiVersion:  core.ApiVersion_V3,
			InitialFetchTimeout: ptypes.DurationProto(time.Second * 0),
		},
	}
	defaultSDSConfig = &tls.SdsSecretConfig{
		Name: SDSDefaultResourceName,
		SdsConfig: &core.ConfigSource{
			ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
				ApiConfigSource: &core.ApiConfigSource{
					ApiType:                   core.ApiConfigSource_GRPC,
					SetNodeOnFirstMessageOnly: true,
					TransportApiVersion:       core.ApiVersion_V3,
					GrpcServices: []*core.GrpcService{
						{
							TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
								EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
							},
						},
					},
				},
			},
			ResourceApiVersion:  core.ApiVersion_V3,
			InitialFetchTimeout: ptypes.DurationProto(time.Second * 0),
		},
	}
	rootSDSConfig = &tls.SdsSecretConfig{
		Name: SDSRootResourceName,
		SdsConfig: &core.ConfigSource{
			ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
				ApiConfigSource: &core.ApiConfigSource{
					ApiType:                   core.ApiConfigSource_GRPC,
					SetNodeOnFirstMessageOnly: true,
					TransportApiVersion:       core.ApiVersion_V3,
					GrpcServices: []*core.GrpcService{
						{
							TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
								EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
							},
						},
					},
				},
			},
			ResourceApiVersion:  core.ApiVersion_V3,
			InitialFetchTimeout: ptypes.DurationProto(time.Second * 0),
		},
	}
)

// ConstructSdsSecretConfig constructs SDS Secret Configuration for workload proxy.
func ConstructSdsSecretConfig(name string, node *model.Proxy) *tls.SdsSecretConfig {
	if name == "" {
		return nil
	}

	if name == SDSDefaultResourceName {
		if util.IsIstioVersionGE19(node) {
			return defaultSDSConfig
		}
		return legacyDefaultSDSConfig
	}
	if name == SDSRootResourceName {
		if util.IsIstioVersionGE19(node) {
			return rootSDSConfig
		}
		return legacyRootSDSConfig
	}

	cfg := &tls.SdsSecretConfig{
		Name: name,
		SdsConfig: &core.ConfigSource{
			ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
				ApiConfigSource: &core.ApiConfigSource{
					ApiType:             core.ApiConfigSource_GRPC,
					TransportApiVersion: core.ApiVersion_V3,
					GrpcServices: []*core.GrpcService{
						{
							TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
								EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
							},
						},
					},
				},
			},
			ResourceApiVersion: core.ApiVersion_V3,
		},
	}

	if util.IsIstioVersionGE19(node) {
		cfg.SdsConfig.GetApiConfigSource().SetNodeOnFirstMessageOnly = true
	}
	return cfg
}

// ConstructValidationContext constructs ValidationContext in CommonTLSContext.
func ConstructValidationContext(rootCAFilePath string, subjectAltNames []string) *tls.CommonTlsContext_ValidationContext {
	ret := &tls.CommonTlsContext_ValidationContext{
		ValidationContext: &tls.CertificateValidationContext{
			TrustedCa: &core.DataSource{
				Specifier: &core.DataSource_Filename{
					Filename: rootCAFilePath,
				},
			},
		},
	}

	if len(subjectAltNames) > 0 {
		ret.ValidationContext.MatchSubjectAltNames = util.StringToExactMatch(subjectAltNames)
	}

	return ret
}

func appendURIPrefixToTrustDomain(trustDomainAliases []string) []string {
	var res []string
	for _, td := range trustDomainAliases {
		res = append(res, spiffe.URIPrefix+td+"/")
	}
	return res
}

// ApplyToCommonTLSContext completes the commonTlsContext
func ApplyToCommonTLSContext(tlsContext *tls.CommonTlsContext, proxy *model.Proxy,
	subjectAltNames []string, trustDomainAliases []string, validateClient bool) {
	// These are certs being mounted from within the pod. Rather than reading directly in Envoy,
	// which does not support rotation, we will serve them over SDS by reading the files.
	// We should check if these certs have values, if yes we should use them or otherwise fall back to defaults.
	res := model.SdsCertificateConfig{
		CertificatePath:   proxy.Metadata.TLSServerCertChain,
		PrivateKeyPath:    proxy.Metadata.TLSServerKey,
		CaCertificatePath: proxy.Metadata.TLSServerRootCert,
	}

	// TODO: if subjectAltName ends with *, create a prefix match as well.
	// TODO: if user explicitly specifies SANs - should we alter his explicit config by adding all spifee aliases?
	matchSAN := util.StringToExactMatch(subjectAltNames)
	if len(trustDomainAliases) > 0 {
		matchSAN = append(matchSAN, util.StringToPrefixMatch(appendURIPrefixToTrustDomain(trustDomainAliases))...)
	}

	// configure server listeners with SDS.
	if validateClient {
		tlsContext.ValidationContextType = &tls.CommonTlsContext_CombinedValidationContext{
			CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
				DefaultValidationContext:         &tls.CertificateValidationContext{MatchSubjectAltNames: matchSAN},
				ValidationContextSdsSecretConfig: ConstructSdsSecretConfig(model.GetOrDefault(res.GetRootResourceName(), SDSRootResourceName), proxy),
			},
		}
	}
	tlsContext.TlsCertificateSdsSecretConfigs = []*tls.SdsSecretConfig{
		ConstructSdsSecretConfig(model.GetOrDefault(res.GetResourceName(), SDSDefaultResourceName), proxy),
	}
}

// ApplyCustomSDSToClientCommonTLSContext applies the customized sds to CommonTlsContext
// Used for building upstream TLS context for egress gateway's TLS/mTLS origination
func ApplyCustomSDSToClientCommonTLSContext(tlsContext *tls.CommonTlsContext, tlsOpts *networking.ClientTLSSettings) {
	if tlsOpts.Mode == networking.ClientTLSSettings_MUTUAL {
		// create SDS config for gateway to fetch key/cert from agent.
		tlsContext.TlsCertificateSdsSecretConfigs = []*tls.SdsSecretConfig{
			ConstructSdsSecretConfigForCredential(tlsOpts.CredentialName),
		}
	}
	// create SDS config for gateway to fetch certificate validation context
	// at gateway agent.
	defaultValidationContext := &tls.CertificateValidationContext{
		MatchSubjectAltNames: util.StringToExactMatch(tlsOpts.SubjectAltNames),
	}
	tlsContext.ValidationContextType = &tls.CommonTlsContext_CombinedValidationContext{
		CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
			DefaultValidationContext:         defaultValidationContext,
			ValidationContextSdsSecretConfig: ConstructSdsSecretConfigForCredential(tlsOpts.CredentialName + SdsCaSuffix),
		},
	}
}

// ApplyCredentialSDSToServerCommonTLSContext applies the credentialName sds (Gateway/DestinationRule) to CommonTlsContext
// Used for building both gateway/sidecar TLS context
func ApplyCredentialSDSToServerCommonTLSContext(tlsContext *tls.CommonTlsContext, tlsOpts *networking.ServerTLSSettings) {
	// create SDS config for gateway/sidecar to fetch key/cert from agent.
	tlsContext.TlsCertificateSdsSecretConfigs = []*tls.SdsSecretConfig{
		ConstructSdsSecretConfigForCredential(tlsOpts.CredentialName),
	}
	// If tls mode is MUTUAL, create SDS config for gateway/sidecar to fetch certificate validation context
	// at gateway agent. Otherwise, use the static certificate validation context config.
	if tlsOpts.Mode == networking.ServerTLSSettings_MUTUAL {
		defaultValidationContext := &tls.CertificateValidationContext{
			MatchSubjectAltNames:  util.StringToExactMatch(tlsOpts.SubjectAltNames),
			VerifyCertificateSpki: tlsOpts.VerifyCertificateSpki,
			VerifyCertificateHash: tlsOpts.VerifyCertificateHash,
		}
		tlsContext.ValidationContextType = &tls.CommonTlsContext_CombinedValidationContext{
			CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
				DefaultValidationContext:         defaultValidationContext,
				ValidationContextSdsSecretConfig: ConstructSdsSecretConfigForCredential(tlsOpts.CredentialName + SdsCaSuffix),
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
