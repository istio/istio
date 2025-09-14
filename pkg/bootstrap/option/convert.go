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

package option

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"

	auth "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"google.golang.org/protobuf/types/known/durationpb"
	pstruct "google.golang.org/protobuf/types/known/structpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	networkingAPI "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/model"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/wellknown"
)

// TransportSocket wraps UpstreamTLSContext
type TransportSocket struct {
	Name        string          `json:"name,omitempty"`
	TypedConfig *pstruct.Struct `json:"typed_config,omitempty"`
}

// TCPKeepalive wraps a thin JSON for xDS proto
type TCPKeepalive struct {
	KeepaliveProbes   *wrappers.UInt32Value `json:"keepalive_probes,omitempty"`
	KeepaliveTime     *wrappers.UInt32Value `json:"keepalive_time,omitempty"`
	KeepaliveInterval *wrappers.UInt32Value `json:"keepalive_interval,omitempty"`
}

// UpstreamConnectionOptions wraps a thin JSON for xDS proto
type UpstreamConnectionOptions struct {
	TCPKeepalive *TCPKeepalive `json:"tcp_keepalive,omitempty"`
}

func keepaliveConverter(value *networkingAPI.ConnectionPoolSettings_TCPSettings_TcpKeepalive) convertFunc {
	return func(*instance) (any, error) {
		upstreamConnectionOptions := &UpstreamConnectionOptions{
			TCPKeepalive: &TCPKeepalive{},
		}

		if value.Probes > 0 {
			upstreamConnectionOptions.TCPKeepalive.KeepaliveProbes = &wrappers.UInt32Value{Value: value.Probes}
		}

		if value.Time != nil && value.Time.Seconds > 0 {
			upstreamConnectionOptions.TCPKeepalive.KeepaliveTime = &wrappers.UInt32Value{Value: uint32(value.Time.Seconds)}
		}

		if value.Interval != nil && value.Interval.Seconds > 0 {
			upstreamConnectionOptions.TCPKeepalive.KeepaliveInterval = &wrappers.UInt32Value{Value: uint32(value.Interval.Seconds)}
		}
		return convertToJSON(upstreamConnectionOptions), nil
	}
}

func transportSocketConverter(tls *networkingAPI.ClientTLSSettings, sniName string, metadata *model.BootstrapNodeMetadata, isH2 bool) convertFunc {
	return func(*instance) (any, error) {
		tlsContext := tlsContextConvert(tls, sniName, metadata)
		if tlsContext == nil {
			return "", nil
		}
		if !isH2 {
			tlsContext.CommonTlsContext.AlpnProtocols = nil
		}
		// This double conversion is to encode the typed config and get it out as struct
		// so that convertToJSON properly encodes the structure. Since this is just for
		// bootstrap generation this is better than having our custom structs.
		tlsContextStruct, _ := protomarshal.MessageToStructSlow(protoconv.MessageToAny(tlsContext))
		transportSocket := &TransportSocket{
			Name:        wellknown.TransportSocketTLS,
			TypedConfig: tlsContextStruct,
		}
		return convertToJSON(transportSocket), nil
	}
}

// TODO(ramaraochavali): Unify this code with cluster upstream TLS settings logic.
func tlsContextConvert(tls *networkingAPI.ClientTLSSettings, sniName string, metadata *model.BootstrapNodeMetadata) *auth.UpstreamTlsContext {
	tlsContext := &auth.UpstreamTlsContext{
		CommonTlsContext: &auth.CommonTlsContext{},
	}

	switch tls.Mode {
	case networkingAPI.ClientTLSSettings_SIMPLE:
		res := security.SdsCertificateConfig{
			CaCertificatePath: model.GetOrDefault(metadata.TLSClientRootCert, tls.CaCertificates),
		}

		tlsContext.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_CombinedValidationContext{
			CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
				DefaultValidationContext:         &auth.CertificateValidationContext{MatchSubjectAltNames: model.StringToExactMatch(tls.SubjectAltNames)},
				ValidationContextSdsSecretConfig: model.ConstructSdsSecretConfig(res.GetRootResourceName()),
			},
		}
		tlsContext.CommonTlsContext.AlpnProtocols = model.ALPNH2Only
		tlsContext.Sni = tls.Sni
	case networkingAPI.ClientTLSSettings_MUTUAL:
		res := security.SdsCertificateConfig{
			CertificatePath:   model.GetOrDefault(metadata.TLSClientCertChain, tls.ClientCertificate),
			PrivateKeyPath:    model.GetOrDefault(metadata.TLSClientKey, tls.PrivateKey),
			CaCertificatePath: model.GetOrDefault(metadata.TLSClientRootCert, tls.CaCertificates),
		}
		if len(res.GetResourceName()) > 0 {
			tlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs = append(tlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs,
				model.ConstructSdsSecretConfig(res.GetResourceName()))
		}

		tlsContext.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_CombinedValidationContext{
			CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
				DefaultValidationContext:         &auth.CertificateValidationContext{MatchSubjectAltNames: model.StringToExactMatch(tls.SubjectAltNames)},
				ValidationContextSdsSecretConfig: model.ConstructSdsSecretConfig(res.GetRootResourceName()),
			},
		}
		tlsContext.CommonTlsContext.AlpnProtocols = model.ALPNH2Only
		tlsContext.Sni = tls.Sni
	case networkingAPI.ClientTLSSettings_ISTIO_MUTUAL:
		tlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs = append(tlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs,
			model.ConstructSdsSecretConfig(model.SDSDefaultResourceName))

		tlsContext.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_CombinedValidationContext{
			CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
				DefaultValidationContext:         &auth.CertificateValidationContext{MatchSubjectAltNames: model.StringToExactMatch(tls.SubjectAltNames)},
				ValidationContextSdsSecretConfig: model.ConstructSdsSecretConfig(model.SDSRootResourceName),
			},
		}
		tlsContext.CommonTlsContext.AlpnProtocols = model.ALPNInMeshH2
		tlsContext.Sni = tls.Sni
		// For ISTIO_MUTUAL if custom SNI is not provided, use the default SNI name.
		if len(tls.Sni) == 0 {
			tlsContext.Sni = sniName
		}
	default:
		// No TLS.
		return nil
	}
	return tlsContext
}

func nodeMetadataConverter(metadata *model.BootstrapNodeMetadata, rawMeta map[string]any) convertFunc {
	return func(*instance) (any, error) {
		marshalString, err := marshalMetadata(metadata, rawMeta)
		if err != nil {
			return "", err
		}
		return marshalString, nil
	}
}

func sanConverter(sans []string) convertFunc {
	return func(*instance) (any, error) {
		matchers := []string{}
		for _, s := range sans {
			matchers = append(matchers, fmt.Sprintf(`{"exact":"%s"}`, s))
		}
		return "[" + strings.Join(matchers, ",") + "]", nil
	}
}

func addressConverter(addr string) convertFunc {
	return func(o *instance) (any, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("unable to parse %s address %q: %v", o.name, addr, err)
		}
		if host == "$(HOST_IP)" {
			// Replace host with HOST_IP env var if it is "$(HOST_IP)".
			// This is to support some tracer setting (Datadog, Zipkin), where "$(HOST_IP)"" is used for address.
			// Tracer address used to be specified within proxy container params, and thus could be interpreted with pod HOST_IP env var.
			// Now tracer config is passed in with mesh config volume at gateway, k8s env var interpretation does not work.
			// This is to achieve the same interpretation as k8s.
			hostIPEnv := os.Getenv("HOST_IP")
			if hostIPEnv != "" {
				host = hostIPEnv
			}
		}

		return fmt.Sprintf("{\"address\": \"%s\", \"port_value\": %s}", host, port), nil
	}
}

func jsonConverter(d any) convertFunc {
	return func(o *instance) (any, error) {
		b, err := json.Marshal(d)
		return string(b), err
	}
}

func durationConverter(value *durationpb.Duration) convertFunc {
	return func(*instance) (any, error) {
		return value.AsDuration().String(), nil
	}
}

func convertToJSON(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		log.Error(err.Error())
		return ""
	}
	return string(b)
}

// marshalMetadata combines type metadata and untyped metadata and marshals to json
// This allows passing arbitrary metadata to Envoy, while still supported typed metadata for known types
func marshalMetadata(metadata *model.BootstrapNodeMetadata, rawMeta map[string]any) (string, error) {
	b, err := json.Marshal(metadata)
	if err != nil {
		return "", err
	}
	var output map[string]any
	if err := json.Unmarshal(b, &output); err != nil {
		return "", err
	}
	// Add all untyped metadata
	for k, v := range rawMeta {
		// Do not override fields, as we may have made modifications to the type metadata
		// This means we will only add "unknown" fields here
		if _, f := output[k]; !f {
			output[k] = v
		}
	}
	res, err := json.Marshal(output)
	if err != nil {
		return "", err
	}
	return string(res), nil
}
