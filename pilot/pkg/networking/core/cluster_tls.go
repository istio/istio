// Copyright Istio Authors. All Rights Reserved.
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

package core

import (
	"fmt"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	internalupstream "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/internal_upstream/v3"
	tlsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	http "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	metadata "github.com/envoyproxy/go-control-plane/envoy/type/metadata/v3"
	"google.golang.org/protobuf/types/known/structpb"

	"istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	sec_model "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/pkg/util/protoconv"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/wellknown"
)

var istioMtlsTransportSocketMatch = &structpb.Struct{
	Fields: map[string]*structpb.Value{
		model.TLSModeLabelShortname: {Kind: &structpb.Value_StringValue{StringValue: model.IstioMutualTLSModeLabel}},
	},
}

var internalUpstreamSocket = &core.TransportSocket{
	Name: "envoy.transport_sockets.internal_upstream",
	ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: protoconv.MessageToAny(&internalupstream.InternalUpstreamTransport{
		PassthroughMetadata: []*internalupstream.InternalUpstreamTransport_MetadataValueSource{
			{
				Kind: &metadata.MetadataKind{Kind: &metadata.MetadataKind_Host_{}},
				Name: util.OriginalDstMetadataKey,
			},
			{
				Kind: &metadata.MetadataKind{Kind: &metadata.MetadataKind_Cluster_{
					Cluster: &metadata.MetadataKind_Cluster{},
				}},
				Name: "istio",
			},
			{
				Kind: &metadata.MetadataKind{Kind: &metadata.MetadataKind_Host_{
					Host: &metadata.MetadataKind_Host{},
				}},
				Name: "istio",
			},
		},
		TransportSocket: xdsfilters.RawBufferTransportSocket,
	})},
}

var hboneTransportSocket = &cluster.Cluster_TransportSocketMatch{
	Name: "hbone",
	Match: &structpb.Struct{
		Fields: map[string]*structpb.Value{
			model.TunnelLabelShortName: {Kind: &structpb.Value_StringValue{StringValue: model.TunnelHTTP}},
		},
	},
	TransportSocket: internalUpstreamSocket,
}

var hboneOrPlaintextSocket = []*cluster.Cluster_TransportSocketMatch{
	hboneTransportSocket,
	defaultTransportSocketMatch(),
}

// applyUpstreamTLSSettings applies upstream tls context to the cluster
func (cb *ClusterBuilder) applyUpstreamTLSSettings(
	opts *buildClusterOpts,
	tls *networking.ClientTLSSettings,
	mtlsCtxType mtlsContextType,
) {
	c := opts.mutable
	tlsContext, err := cb.buildUpstreamClusterTLSContext(opts, tls)
	if err != nil {
		log.Errorf("failed to build Upstream TLSContext: %s", err.Error())
		return
	}

	if tlsContext != nil {
		c.cluster.TransportSocket = &core.TransportSocket{
			Name:       wellknown.TransportSocketTLS,
			ConfigType: &core.TransportSocket_TypedConfig{TypedConfig: protoconv.MessageToAny(tlsContext)},
		}
	}
	istioAutodetectedMtls := tls != nil && tls.Mode == networking.ClientTLSSettings_ISTIO_MUTUAL &&
		mtlsCtxType == autoDetected
	if cb.hbone {
		cb.applyHBONETransportSocketMatches(c.cluster, tls, istioAutodetectedMtls)
	} else if c.cluster.GetType() != cluster.Cluster_ORIGINAL_DST {
		// For headless service, discovery type will be `Cluster_ORIGINAL_DST`
		// Apply auto mtls to clusters excluding these kind of headless services.
		if istioAutodetectedMtls {
			// convert to transport socket matcher if the mode was auto detected
			transportSocket := c.cluster.TransportSocket
			c.cluster.TransportSocket = nil
			c.cluster.TransportSocketMatches = []*cluster.Cluster_TransportSocketMatch{
				{
					Name:            "tlsMode-" + model.IstioMutualTLSModeLabel,
					Match:           istioMtlsTransportSocketMatch,
					TransportSocket: transportSocket,
				},
				defaultTransportSocketMatch(),
			}
		}
	}
}

func (cb *ClusterBuilder) buildUpstreamClusterTLSContext(opts *buildClusterOpts, tls *networking.ClientTLSSettings) (*tlsv3.UpstreamTlsContext, error) {
	if tls == nil {
		return nil, nil
	}
	// Hack to avoid egress sds cluster config generation for sidecar when
	// CredentialName is set in DestinationRule without a workloadSelector.
	// We do not want to support CredentialName setting in non workloadSelector based DestinationRules, because
	// that would result in the CredentialName being supplied to all the sidecars which the DestinationRule is scoped to,
	// resulting in delayed startup of sidecars who do not have access to the credentials.
	if tls.CredentialName != "" && cb.sidecarProxy() && !opts.isDrWithSelector {
		if tls.Mode == networking.ClientTLSSettings_SIMPLE || tls.Mode == networking.ClientTLSSettings_MUTUAL {
			return nil, nil
		}
	}

	c := opts.mutable
	var tlsContext *tlsv3.UpstreamTlsContext

	switch tls.Mode {
	case networking.ClientTLSSettings_DISABLE:
		tlsContext = nil
	case networking.ClientTLSSettings_ISTIO_MUTUAL:
		tlsContext = &tlsv3.UpstreamTlsContext{
			CommonTlsContext: defaultUpstreamCommonTLSContext(),
			Sni:              tls.Sni,
		}

		tlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs = append(tlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs,
			sec_model.ConstructSdsSecretConfig(sec_model.SDSDefaultResourceName))

		tlsContext.CommonTlsContext.ValidationContextType = &tlsv3.CommonTlsContext_CombinedValidationContext{
			CombinedValidationContext: &tlsv3.CommonTlsContext_CombinedCertificateValidationContext{
				DefaultValidationContext:         &tlsv3.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch(tls.SubjectAltNames)},
				ValidationContextSdsSecretConfig: sec_model.ConstructSdsSecretConfig(sec_model.SDSRootResourceName),
			},
		}
		// Set default SNI of cluster name for istio_mutual if sni is not set.
		if len(tlsContext.Sni) == 0 {
			tlsContext.Sni = c.cluster.Name
		}
		// `istio-peer-exchange` alpn is only used when using mtls communication between peers.
		// We add `istio-peer-exchange` to the list of alpn strings.
		// The code has repeated snippets because We want to use predefined alpn strings for efficiency.
		if cb.isHttp2Cluster(c) {
			// This is HTTP/2 in-mesh cluster, advertise it with ALPN.
			if features.MetadataExchange && !features.DisableMxALPN {
				tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNInMeshH2WithMxc
			} else {
				tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNInMeshH2
			}
		} else {
			// This is in-mesh cluster, advertise it with ALPN.
			if features.MetadataExchange && !features.DisableMxALPN {
				tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNInMeshWithMxc
			} else {
				tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNInMesh
			}
		}
	case networking.ClientTLSSettings_SIMPLE:
		tlsContext = &tlsv3.UpstreamTlsContext{
			CommonTlsContext: defaultUpstreamCommonTLSContext(),
			Sni:              tls.Sni,
		}

		cb.setAutoSniAndAutoSanValidation(c, tls)

		// Use subject alt names specified in service entry if TLS settings does not have subject alt names.
		if opts.serviceRegistry == provider.External && len(tls.SubjectAltNames) == 0 {
			tls = tls.DeepCopy()
			tls.SubjectAltNames = opts.serviceAccounts
		}
		if tls.CredentialName != "" {
			// If  credential name is specified at Destination Rule config and originating node is egress gateway, create
			// SDS config for egress gateway to fetch key/cert at gateway agent.
			sec_model.ApplyCustomSDSToClientCommonTLSContext(tlsContext.CommonTlsContext, tls, cb.credentialSocketExist)
		} else {
			// If CredentialName is not set fallback to files specified in DR.
			res := security.SdsCertificateConfig{
				CaCertificatePath: tls.CaCertificates,
			}
			// If tls.CaCertificate or CaCertificate in Metadata isn't configured, or tls.InsecureSkipVerify is true,
			// don't set up SdsSecretConfig
			if !res.IsRootCertificate() || tls.GetInsecureSkipVerify().GetValue() {
				tlsContext.CommonTlsContext.ValidationContextType = &tlsv3.CommonTlsContext_ValidationContext{}
			} else {
				defaultValidationContext := &tlsv3.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch(tls.SubjectAltNames)}
				if tls.GetCaCrl() != "" {
					defaultValidationContext.Crl = &core.DataSource{
						Specifier: &core.DataSource_Filename{
							Filename: tls.GetCaCrl(),
						},
					}
				}
				tlsContext.CommonTlsContext.ValidationContextType = &tlsv3.CommonTlsContext_CombinedValidationContext{
					CombinedValidationContext: &tlsv3.CommonTlsContext_CombinedCertificateValidationContext{
						DefaultValidationContext:         defaultValidationContext,
						ValidationContextSdsSecretConfig: sec_model.ConstructSdsSecretConfig(res.GetRootResourceName()),
					},
				}

			}
		}

		applyTLSDefaults(tlsContext, opts.mesh.GetTlsDefaults())

		if cb.isHttp2Cluster(c) {
			// This is HTTP/2 cluster, advertise it with ALPN.
			tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNH2Only
		}

	case networking.ClientTLSSettings_MUTUAL:
		tlsContext = &tlsv3.UpstreamTlsContext{
			CommonTlsContext: defaultUpstreamCommonTLSContext(),
			Sni:              tls.Sni,
		}

		cb.setAutoSniAndAutoSanValidation(c, tls)

		// Use subject alt names specified in service entry if TLS settings does not have subject alt names.
		if opts.serviceRegistry == provider.External && len(tls.SubjectAltNames) == 0 {
			tls = tls.DeepCopy()
			tls.SubjectAltNames = opts.serviceAccounts
		}
		if tls.CredentialName != "" {
			// If credential name is specified at Destination Rule config and originating node is egress gateway, create
			// SDS config for egress gateway to fetch key/cert at gateway agent.
			sec_model.ApplyCustomSDSToClientCommonTLSContext(tlsContext.CommonTlsContext, tls, cb.credentialSocketExist)
		} else {
			// If CredentialName is not set fallback to file based approach
			if tls.ClientCertificate == "" || tls.PrivateKey == "" {
				err := fmt.Errorf("failed to apply tls setting for %s: client certificate and private key must not be empty",
					c.cluster.Name)
				return nil, err
			}
			// These are certs being mounted from within the pod and specified in Destination Rules.
			// Rather than reading directly in Envoy, which does not support rotation, we will
			// serve them over SDS by reading the files.
			res := security.SdsCertificateConfig{
				CertificatePath:   tls.ClientCertificate,
				PrivateKeyPath:    tls.PrivateKey,
				CaCertificatePath: tls.CaCertificates,
			}
			tlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs = append(tlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs,
				sec_model.ConstructSdsSecretConfig(res.GetResourceName()))

			// If tls.CaCertificate or CaCertificate in Metadata isn't configured, or tls.InsecureSkipVerify is true,
			// don't set up SdsSecretConfig
			if !res.IsRootCertificate() || tls.GetInsecureSkipVerify().GetValue() {
				tlsContext.CommonTlsContext.ValidationContextType = &tlsv3.CommonTlsContext_ValidationContext{}
			} else {
				defaultValidationContext := &tlsv3.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch(tls.SubjectAltNames)}
				if tls.GetCaCrl() != "" {
					defaultValidationContext.Crl = &core.DataSource{
						Specifier: &core.DataSource_Filename{
							Filename: tls.GetCaCrl(),
						},
					}
				}
				tlsContext.CommonTlsContext.ValidationContextType = &tlsv3.CommonTlsContext_CombinedValidationContext{
					CombinedValidationContext: &tlsv3.CommonTlsContext_CombinedCertificateValidationContext{
						DefaultValidationContext:         defaultValidationContext,
						ValidationContextSdsSecretConfig: sec_model.ConstructSdsSecretConfig(res.GetRootResourceName()),
					},
				}
			}
		}

		applyTLSDefaults(tlsContext, opts.mesh.GetTlsDefaults())

		if cb.isHttp2Cluster(c) {
			// This is HTTP/2 cluster, advertise it with ALPN.
			tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNH2Only
		}
	}

	// Compliance for Envoy TLS upstreams.
	if tlsContext != nil {
		sec_model.EnforceCompliance(tlsContext.CommonTlsContext)
	}
	return tlsContext, nil
}

// applyTLSDefaults applies tls default settings from mesh config to UpstreamTlsContext.
func applyTLSDefaults(tlsContext *tlsv3.UpstreamTlsContext, tlsDefaults *v1alpha1.MeshConfig_TLSConfig) {
	if tlsDefaults == nil {
		return
	}
	if len(tlsDefaults.EcdhCurves) > 0 {
		tlsContext.CommonTlsContext.TlsParams.EcdhCurves = tlsDefaults.EcdhCurves
	}
	if len(tlsDefaults.CipherSuites) > 0 {
		tlsContext.CommonTlsContext.TlsParams.CipherSuites = tlsDefaults.CipherSuites
	}
}

// Set auto_sni if EnableAutoSni feature flag is enabled and if sni field is not explicitly set in DR.
// Set auto_san_validation if VerifyCertAtClient feature flag is enabled and if there is no explicit SubjectAltNames specified  in DR.
func (cb *ClusterBuilder) setAutoSniAndAutoSanValidation(mc *clusterWrapper, tls *networking.ClientTLSSettings) {
	if mc == nil || !features.EnableAutoSni {
		return
	}

	setAutoSni := false
	setAutoSanValidation := false
	if len(tls.Sni) == 0 {
		setAutoSni = true
	}
	if features.VerifyCertAtClient && setAutoSni && len(tls.SubjectAltNames) == 0 && !tls.GetInsecureSkipVerify().GetValue() {
		setAutoSanValidation = true
	}

	if setAutoSni || setAutoSanValidation {
		if mc.httpProtocolOptions == nil {
			mc.httpProtocolOptions = &http.HttpProtocolOptions{}
		}
		if mc.httpProtocolOptions.UpstreamHttpProtocolOptions == nil {
			mc.httpProtocolOptions.UpstreamHttpProtocolOptions = &core.UpstreamHttpProtocolOptions{}
		}
		if setAutoSni {
			mc.httpProtocolOptions.UpstreamHttpProtocolOptions.AutoSni = true
		}
		if setAutoSanValidation {
			mc.httpProtocolOptions.UpstreamHttpProtocolOptions.AutoSanValidation = true
		}
	}
}

func (cb *ClusterBuilder) applyHBONETransportSocketMatches(c *cluster.Cluster, tls *networking.ClientTLSSettings,
	istioAutoDetectedMtls bool,
) {
	if tls == nil {
		c.TransportSocketMatches = hboneOrPlaintextSocket
		return
	}
	// For headless service, discovery type will be `Cluster_ORIGINAL_DST`
	// Apply auto mtls to clusters excluding these kind of headless services.
	if c.GetType() != cluster.Cluster_ORIGINAL_DST {
		// convert to transport socket matcher if the mode was auto detected
		if istioAutoDetectedMtls {
			transportSocket := c.TransportSocket
			c.TransportSocket = nil
			c.TransportSocketMatches = []*cluster.Cluster_TransportSocketMatch{
				hboneTransportSocket,
				{
					Name:            "tlsMode-" + model.IstioMutualTLSModeLabel,
					Match:           istioMtlsTransportSocketMatch,
					TransportSocket: transportSocket,
				},
				defaultTransportSocketMatch(),
			}
		} else {
			if c.TransportSocket == nil {
				c.TransportSocketMatches = hboneOrPlaintextSocket
			} else {
				ts := c.TransportSocket
				c.TransportSocket = nil

				c.TransportSocketMatches = []*cluster.Cluster_TransportSocketMatch{
					hboneTransportSocket,
					{
						Name:            "tlsMode-" + model.IstioMutualTLSModeLabel,
						TransportSocket: ts,
					},
				}
			}
		}
	}
}

func defaultUpstreamCommonTLSContext() *tlsv3.CommonTlsContext {
	return &tlsv3.CommonTlsContext{
		TlsParams: &tlsv3.TlsParameters{
			// if not specified, envoy use TLSv1_2 as default for client.
			TlsMaximumProtocolVersion: tlsv3.TlsParameters_TLSv1_3,
			TlsMinimumProtocolVersion: tlsv3.TlsParameters_TLSv1_2,
		},
	}
}

// defaultTransportSocketMatch applies to endpoints that have no security.istio.io/tlsMode label
// or those whose label value does not match "istio"
func defaultTransportSocketMatch() *cluster.Cluster_TransportSocketMatch {
	return &cluster.Cluster_TransportSocketMatch{
		Name:            "tlsMode-disabled",
		Match:           &structpb.Struct{},
		TransportSocket: xdsfilters.RawBufferTransportSocket,
	}
}

// buildUpstreamTLSSettings fills key cert fields for all TLSSettings when the mode is `ISTIO_MUTUAL`.
// If the (input) TLS setting is nil (i.e not set), *and* the service mTLS mode is STRICT, it also
// creates and populates the config as if they are set as ISTIO_MUTUAL.
func (cb *ClusterBuilder) buildUpstreamTLSSettings(
	tls *networking.ClientTLSSettings,
	serviceAccounts []string,
	sni string,
	autoMTLSEnabled bool,
	meshExternal bool,
	serviceMTLSMode model.MutualTLSMode,
) (*networking.ClientTLSSettings, mtlsContextType) {
	if tls != nil {
		if tls.Mode == networking.ClientTLSSettings_DISABLE || tls.Mode == networking.ClientTLSSettings_SIMPLE {
			return tls, userSupplied
		}
		// For backward compatibility, use metadata certs if provided.
		if cb.hasMetadataCerts() {
			// When building Mutual TLS settings, we should always use user supplied SubjectAltNames and SNI
			// in destination rule. The Service Accounts and auto computed SNI should only be used for
			// ISTIO_MUTUAL.
			return cb.buildMutualTLS(tls.SubjectAltNames, tls.Sni), userSupplied
		}
		if tls.Mode != networking.ClientTLSSettings_ISTIO_MUTUAL {
			return tls, userSupplied
		}
		// Update TLS settings for ISTIO_MUTUAL. Use client provided SNI if set. Otherwise,
		// overwrite with the auto generated SNI. User specified SNIs in the istio mtls settings
		// are useful when routing via gateways. Use Service Accounts if Subject Alt names
		// are not specified in TLS settings.
		sniToUse := tls.Sni
		if len(sniToUse) == 0 {
			sniToUse = sni
		}
		subjectAltNamesToUse := tls.SubjectAltNames
		if subjectAltNamesToUse == nil {
			subjectAltNamesToUse = serviceAccounts
		}
		return cb.buildIstioMutualTLS(subjectAltNamesToUse, sniToUse), userSupplied
	}

	if meshExternal || !autoMTLSEnabled || serviceMTLSMode == model.MTLSUnknown || serviceMTLSMode == model.MTLSDisable {
		return nil, userSupplied
	}

	// For backward compatibility, use metadata certs if provided.
	if cb.hasMetadataCerts() {
		return cb.buildMutualTLS(serviceAccounts, sni), autoDetected
	}

	// Build settings for auto MTLS.
	return cb.buildIstioMutualTLS(serviceAccounts, sni), autoDetected
}

func (cb *ClusterBuilder) hasMetadataCerts() bool {
	return cb.metadataCerts != nil
}

// buildMutualTLS returns a `TLSSettings` for MUTUAL mode with proxy metadata certificates.
func (cb *ClusterBuilder) buildMutualTLS(serviceAccounts []string, sni string) *networking.ClientTLSSettings {
	return &networking.ClientTLSSettings{
		Mode:              networking.ClientTLSSettings_MUTUAL,
		CaCertificates:    cb.metadataCerts.tlsClientRootCert,
		ClientCertificate: cb.metadataCerts.tlsClientCertChain,
		PrivateKey:        cb.metadataCerts.tlsClientKey,
		SubjectAltNames:   serviceAccounts,
		Sni:               sni,
	}
}

// buildIstioMutualTLS returns a `TLSSettings` for ISTIO_MUTUAL mode.
func (cb *ClusterBuilder) buildIstioMutualTLS(san []string, sni string) *networking.ClientTLSSettings {
	return &networking.ClientTLSSettings{
		Mode:            networking.ClientTLSSettings_ISTIO_MUTUAL,
		SubjectAltNames: san,
		Sni:             sni,
	}
}
