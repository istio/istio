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
	"sync"
	"time"

	"istio.io/istio/pkg/spiffe"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoy_config_grpc_credential "github.com/envoyproxy/go-control-plane/envoy/config/grpc_credential/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/any"

	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/constants"
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

	// FileBasedMetadataPlugName is File Based Metadata credentials plugin name.
	FileBasedMetadataPlugName = "envoy.grpc_credentials.file_based_metadata"

	// K8sSAJwtTokenHeaderKey is the request header key for k8s jwt token.
	// Binary header name must has suffix "-bin", according to https://github.com/grpc/grpc/blob/master/doc/PROTOCOL-HTTP2.md.
	K8sSAJwtTokenHeaderKey = "istio_sds_credentials_header-bin"

	// GatewaySdsUdsPath is the UDS path for ingress gateway to get credentials via SDS.
	GatewaySdsUdsPath = "unix:./var/run/ingress_gateway/sds"

	// SdsCaSuffix is the suffix of the sds resource name for root CA.
	SdsCaSuffix = "-cacert"

	// EnvoyJwtFilterName is the name of the Envoy JWT filter. This should be the same as the name defined
	// in https://github.com/envoyproxy/envoy/blob/v1.9.1/source/extensions/filters/http/well_known_names.h#L48
	EnvoyJwtFilterName = "envoy.filters.http.jwt_authn"

	// AuthnFilterName is the name for the Istio AuthN filter. This should be the same
	// as the name defined in
	// https://github.com/istio/proxy/blob/master/src/envoy/http/authn/http_filter_factory.cc#L30
	AuthnFilterName = "istio_authn"
)

func useV3Sds(requestedType string) bool {
	// For v3 clusters/listeners, send v3 secrets
	return requestedType == v3.ClusterType || requestedType == v3.ListenerType || requestedType == ""
}

// ConstructSdsSecretConfigWithCustomUds constructs SDS secret configuration for ingress gateway.
func ConstructSdsSecretConfigWithCustomUds(name, sdsUdsPath, requestedType string) *tls.SdsSecretConfig {
	if name == "" || sdsUdsPath == "" {
		return nil
	}

	gRPCConfig := &core.GrpcService_GoogleGrpc{
		TargetUri:  sdsUdsPath,
		StatPrefix: SDSStatPrefix,
	}

	resourceVersion := core.ApiVersion_AUTO

	if useV3Sds(requestedType) {
		resourceVersion = core.ApiVersion_V3
	}
	cfg := &tls.SdsSecretConfig{
		Name: name,
		SdsConfig: &core.ConfigSource{
			ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
				ApiConfigSource: &core.ApiConfigSource{
					ApiType:             core.ApiConfigSource_GRPC,
					TransportApiVersion: resourceVersion,
					GrpcServices: []*core.GrpcService{
						{
							TargetSpecifier: &core.GrpcService_GoogleGrpc_{
								GoogleGrpc: gRPCConfig,
							},
						},
					},
				},
			},
			ResourceApiVersion:  resourceVersion,
			InitialFetchTimeout: features.InitialFetchTimeout,
		},
	}

	return cfg
}

// Preconfigured SDS configs to avoid excessive memory allocations
var (
	// set the fetch timeout to 0 here in defaultV3SDSConfig and rootV3SDSConfig
	// because workload certs are guaranteed exist.
	defaultV3SDSConfig = &tls.SdsSecretConfig{
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
	rootV3SDSConfig = &tls.SdsSecretConfig{
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
)

// ConstructSdsSecretConfig constructs SDS Secret Configuration for workload proxy.
func ConstructSdsSecretConfig(name, requestedType string) *tls.SdsSecretConfig {
	if name == "" {
		return nil
	}

	useV3 := useV3Sds(requestedType)

	if name == SDSDefaultResourceName && useV3 {
		return defaultV3SDSConfig
	}
	if name == SDSRootResourceName && useV3 {
		return rootV3SDSConfig
	}

	resourceVersion := core.ApiVersion_AUTO
	// For v3 clusters/listeners, send v3 secrets
	if useV3 {
		resourceVersion = core.ApiVersion_V3
	}
	cfg := &tls.SdsSecretConfig{
		Name: name,
		SdsConfig: &core.ConfigSource{
			ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
				ApiConfigSource: &core.ApiConfigSource{
					ApiType:             core.ApiConfigSource_GRPC,
					TransportApiVersion: resourceVersion,
					GrpcServices: []*core.GrpcService{
						{
							TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
								EnvoyGrpc: &core.GrpcService_EnvoyGrpc{ClusterName: SDSClusterName},
							},
						},
					},
				},
			},
			ResourceApiVersion:  resourceVersion,
			InitialFetchTimeout: features.InitialFetchTimeout,
		},
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

// ApplyToCommonTLSContext completes the commonTlsContext for `ISTIO_MUTUAL` TLS mode
func ApplyToCommonTLSContext(tlsContext *tls.CommonTlsContext, metadata *model.NodeMetadata,
	sdsPath string, subjectAltNames []string, resourceType string, trustDomainAliases []string) {
	// configure TLS with SDS
	if metadata.SdsEnabled && sdsPath != "" {
		// These are certs being mounted from within the pod. Rather than reading directly in Envoy,
		// which does not support rotation, we will serve them over SDS by reading the files.
		// We should check if these certs have values, if yes we should use them or otherwise fall back to defaults.
		res := model.SdsCertificateConfig{
			CertificatePath:   metadata.TLSServerCertChain,
			PrivateKeyPath:    metadata.TLSServerKey,
			CaCertificatePath: metadata.TLSServerRootCert,
		}

		// TODO: if subjectAltName ends with *, create a prefix match as well.
		// TODO: if user explicitly specifies SANs - should we alter his explicit config by adding all spifee aliases?
		matchSAN := util.StringToExactMatch(subjectAltNames)
		if len(trustDomainAliases) > 0 {
			matchSAN = append(matchSAN, util.StringToPrefixMatch(appendURIPrefixToTrustDomain(trustDomainAliases))...)
		}

		// configure server listeners with SDS.
		tlsContext.ValidationContextType = &tls.CommonTlsContext_CombinedValidationContext{
			CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
				DefaultValidationContext:         &tls.CertificateValidationContext{MatchSubjectAltNames: matchSAN},
				ValidationContextSdsSecretConfig: ConstructSdsSecretConfig(model.GetOrDefault(res.GetRootResourceName(), SDSRootResourceName), resourceType),
			},
		}
		tlsContext.TlsCertificateSdsSecretConfigs = []*tls.SdsSecretConfig{
			ConstructSdsSecretConfig(model.GetOrDefault(res.GetResourceName(), SDSDefaultResourceName), resourceType),
		}
	} else {
		// TODO(ramaraochavali): Clean this codepath later as we default to SDS.
		// SDS disabled, fall back on using mounted certificates
		base := metadata.CertBaseDir + constants.AuthCertsPath
		tlsServerRootCert := model.GetOrDefault(metadata.TLSServerRootCert, base+constants.RootCertFilename)

		tlsContext.ValidationContextType = ConstructValidationContext(tlsServerRootCert, subjectAltNames)

		tlsServerCertChain := model.GetOrDefault(metadata.TLSServerCertChain, base+constants.CertChainFilename)
		tlsServerKey := model.GetOrDefault(metadata.TLSServerKey, base+constants.KeyFilename)

		tlsContext.TlsCertificates = []*tls.TlsCertificate{
			{
				CertificateChain: &core.DataSource{
					Specifier: &core.DataSource_Filename{
						Filename: tlsServerCertChain,
					},
				},
				PrivateKey: &core.DataSource{
					Specifier: &core.DataSource_Filename{
						Filename: tlsServerKey,
					},
				},
			},
		}
	}
}

// ApplyCustomSDSToClientCommonTLSContext applies the customized sds to CommonTlsContext
// Used for building upstream TLS context for egress gateway's TLS/mTLS origination
func ApplyCustomSDSToClientCommonTLSContext(tlsContext *tls.CommonTlsContext, tlsOpts *networking.ClientTLSSettings, sdsUdsPath, requestedType string) {
	if tlsOpts.Mode == networking.ClientTLSSettings_MUTUAL {
		// create SDS config for gateway to fetch key/cert from agent.
		tlsContext.TlsCertificateSdsSecretConfigs = []*tls.SdsSecretConfig{
			ConstructSdsSecretConfigWithCustomUds(tlsOpts.CredentialName, sdsUdsPath, requestedType),
		}
	}
	// create SDS config for gateway to fetch certificate validation context
	// at gateway agent.
	defaultValidationContext := &tls.CertificateValidationContext{
		MatchSubjectAltNames: util.StringToExactMatch(tlsOpts.SubjectAltNames),
	}
	tlsContext.ValidationContextType = &tls.CommonTlsContext_CombinedValidationContext{
		CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
			DefaultValidationContext: defaultValidationContext,
			ValidationContextSdsSecretConfig: ConstructSdsSecretConfigWithCustomUds(
				tlsOpts.CredentialName+SdsCaSuffix, sdsUdsPath, requestedType),
		},
	}
}

// ApplyCustomSDSToServerCommonTLSContext applies the customized sds to CommonTlsContext
// Used for building both gateway/sidecar TLS context
func ApplyCustomSDSToServerCommonTLSContext(tlsContext *tls.CommonTlsContext, tlsOpts *networking.ServerTLSSettings, sdsUdsPath, requestedType string) {
	// create SDS config for gateway/sidecar to fetch key/cert from agent.
	tlsContext.TlsCertificateSdsSecretConfigs = []*tls.SdsSecretConfig{
		ConstructSdsSecretConfigWithCustomUds(tlsOpts.CredentialName, sdsUdsPath, requestedType),
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
				DefaultValidationContext: defaultValidationContext,
				ValidationContextSdsSecretConfig: ConstructSdsSecretConfigWithCustomUds(
					tlsOpts.CredentialName+SdsCaSuffix, sdsUdsPath, requestedType),
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

// ConstructgRPCCallCredentials is used to construct SDS config which is only available from 1.1
func ConstructgRPCCallCredentials(tokenFileName, headerKey string) []*core.GrpcService_GoogleGrpc_CallCredentials {
	// If k8s sa jwt token file exists, envoy only handles plugin credentials.
	config := &envoy_config_grpc_credential.FileBasedMetadataConfig{
		SecretData: &core.DataSource{
			Specifier: &core.DataSource_Filename{
				Filename: tokenFileName,
			},
		},
		HeaderKey: headerKey,
	}

	any := findOrMarshalFileBasedMetadataConfig(tokenFileName, headerKey, config)

	return []*core.GrpcService_GoogleGrpc_CallCredentials{
		{
			CredentialSpecifier: &core.GrpcService_GoogleGrpc_CallCredentials_FromPlugin{
				FromPlugin: &core.GrpcService_GoogleGrpc_CallCredentials_MetadataCredentialsFromPlugin{
					Name: FileBasedMetadataPlugName,
					ConfigType: &core.GrpcService_GoogleGrpc_CallCredentials_MetadataCredentialsFromPlugin_TypedConfig{
						TypedConfig: any},
				},
			},
		},
	}
}

type fbMetadataAnyKey struct {
	tokenFileName string
	headerKey     string
}

var fileBasedMetadataConfigAnyMap sync.Map

// findOrMarshalFileBasedMetadataConfig searches google.protobuf.Any in fileBasedMetadataConfigAnyMap
// by tokenFileName and headerKey, and returns google.protobuf.Any proto if found. If not found,
// it takes the fbMetadata and marshals it into google.protobuf.Any, and stores this new
// google.protobuf.Any into fileBasedMetadataConfigAnyMap.
// FileBasedMetadataConfig only supports non-deterministic marshaling. As each SDS config contains
// marshaled FileBasedMetadataConfig, the SDS config would differ if marshaling FileBasedMetadataConfig
// returns different result. Once SDS config differs, Envoy will create multiple SDS clients to fetch
// same SDS resource. To solve this problem, we use findOrMarshalFileBasedMetadataConfig so that
// FileBasedMetadataConfig is marshaled once, and is reused in all SDS configs.
func findOrMarshalFileBasedMetadataConfig(tokenFileName, headerKey string, fbMetadata *envoy_config_grpc_credential.FileBasedMetadataConfig) *any.Any {
	key := fbMetadataAnyKey{
		tokenFileName: tokenFileName,
		headerKey:     headerKey,
	}
	if v, found := fileBasedMetadataConfigAnyMap.Load(key); found {
		return v.(*any.Any)
	}
	any, _ := ptypes.MarshalAny(fbMetadata)
	fileBasedMetadataConfigAnyMap.Store(key, any)
	return any
}
