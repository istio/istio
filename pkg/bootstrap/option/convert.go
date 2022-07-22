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

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	auth "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/envoyproxy/go-control-plane/pkg/conversion"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"google.golang.org/protobuf/types/known/durationpb"
	pstruct "google.golang.org/protobuf/types/known/structpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	meshAPI "istio.io/api/mesh/v1alpha1"
	networkingAPI "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/security"
	"istio.io/pkg/log"
)

// TransportSocket wraps UpstreamTLSContext
type TransportSocket struct {
	Name        string          `json:"name,omitempty"`
	TypedConfig *pstruct.Struct `json:"typed_config,omitempty"`
}

func keepaliveConverter(value *networkingAPI.ConnectionPoolSettings_TCPSettings_TcpKeepalive) convertFunc {
	return func(*instance) (interface{}, error) {
		upstreamConnectionOptions := &cluster.UpstreamConnectionOptions{
			TcpKeepalive: &core.TcpKeepalive{},
		}

		if value.Probes > 0 {
			upstreamConnectionOptions.TcpKeepalive.KeepaliveProbes = &wrappers.UInt32Value{Value: value.Probes}
		}

		if value.Time != nil && value.Time.Seconds > 0 {
			upstreamConnectionOptions.TcpKeepalive.KeepaliveTime = &wrappers.UInt32Value{Value: uint32(value.Time.Seconds)}
		}

		if value.Interval != nil && value.Interval.Seconds > 0 {
			upstreamConnectionOptions.TcpKeepalive.KeepaliveInterval = &wrappers.UInt32Value{Value: uint32(value.Interval.Seconds)}
		}
		return convertToJSON(upstreamConnectionOptions), nil
	}
}

func transportSocketConverter(tls *networkingAPI.ClientTLSSettings, sniName string, metadata *model.BootstrapNodeMetadata, isH2 bool) convertFunc {
	return func(*instance) (interface{}, error) {
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
		tlsContextStruct, _ := conversion.MessageToStruct(protoconv.MessageToAny(tlsContext))
		transportSocket := &TransportSocket{
			Name:        wellknown.TransportSocketTls,
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
				DefaultValidationContext:         &auth.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch(tls.SubjectAltNames)},
				ValidationContextSdsSecretConfig: authn_model.ConstructSdsSecretConfig(res.GetRootResourceName()),
			},
		}
		tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNH2Only
		tlsContext.Sni = tls.Sni
	case networkingAPI.ClientTLSSettings_MUTUAL:
		res := security.SdsCertificateConfig{
			CertificatePath:   model.GetOrDefault(metadata.TLSClientCertChain, tls.ClientCertificate),
			PrivateKeyPath:    model.GetOrDefault(metadata.TLSClientKey, tls.PrivateKey),
			CaCertificatePath: model.GetOrDefault(metadata.TLSClientRootCert, tls.CaCertificates),
		}
		if len(res.GetResourceName()) > 0 {
			tlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs = append(tlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs,
				authn_model.ConstructSdsSecretConfig(res.GetResourceName()))
		}

		tlsContext.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_CombinedValidationContext{
			CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
				DefaultValidationContext:         &auth.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch(tls.SubjectAltNames)},
				ValidationContextSdsSecretConfig: authn_model.ConstructSdsSecretConfig(res.GetRootResourceName()),
			},
		}
		tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNH2Only
		tlsContext.Sni = tls.Sni
	case networkingAPI.ClientTLSSettings_ISTIO_MUTUAL:
		tlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs = append(tlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs,
			authn_model.ConstructSdsSecretConfig(authn_model.SDSDefaultResourceName))

		tlsContext.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_CombinedValidationContext{
			CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
				DefaultValidationContext:         &auth.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch(tls.SubjectAltNames)},
				ValidationContextSdsSecretConfig: authn_model.ConstructSdsSecretConfig(authn_model.SDSRootResourceName),
			},
		}
		tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNInMeshH2
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

func nodeMetadataConverter(metadata *model.BootstrapNodeMetadata, rawMeta map[string]interface{}) convertFunc {
	return func(*instance) (interface{}, error) {
		marshalString, err := marshalMetadata(metadata, rawMeta)
		if err != nil {
			return "", err
		}
		return marshalString, nil
	}
}

func sanConverter(sans []string) convertFunc {
	return func(*instance) (interface{}, error) {
		matchers := []string{}
		for _, s := range sans {
			matchers = append(matchers, fmt.Sprintf(`{"exact":"%s"}`, s))
		}
		return "[" + strings.Join(matchers, ",") + "]", nil
	}
}

func addressConverter(addr string) convertFunc {
	return func(o *instance) (interface{}, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("unable to parse %s address %q: %v", o.name, addr, err)
		}
		if host == "$(HOST_IP)" {
			// Replace host with HOST_IP env var if it is "$(HOST_IP)".
			// This is to support some tracer setting (Datadog, Zipkin), where "$(HOST_IP)"" is used for address.
			// Tracer address used to be specified within proxy container params, and thus could be interpreted with pod HOST_IP env var.
			// Now tracer config is passed in with mesh config volumn at gateway, k8s env var interpretation does not work.
			// This is to achieve the same interpretation as k8s.
			hostIPEnv := os.Getenv("HOST_IP")
			if hostIPEnv != "" {
				host = hostIPEnv
			}
		}

		return fmt.Sprintf("{\"address\": \"%s\", \"port_value\": %s}", host, port), nil
	}
}

func jsonConverter(d interface{}) convertFunc {
	return func(o *instance) (interface{}, error) {
		b, err := json.Marshal(d)
		return string(b), err
	}
}

func durationConverter(value *durationpb.Duration) convertFunc {
	return func(*instance) (interface{}, error) {
		return value.AsDuration().String(), nil
	}
}

// openCensusAgentContextConverter returns a converter that returns the list of
// distributed trace contexts to propagate with envoy.
func openCensusAgentContextConverter(contexts []meshAPI.Tracing_OpenCensusAgent_TraceContext) convertFunc {
	allContexts := `["TRACE_CONTEXT","GRPC_TRACE_BIN","CLOUD_TRACE_CONTEXT","B3"]`
	return func(*instance) (interface{}, error) {
		if len(contexts) == 0 {
			return allContexts, nil
		}

		var envoyContexts []string
		for _, c := range contexts {
			switch c {
			// Ignore UNSPECIFIED
			case meshAPI.Tracing_OpenCensusAgent_W3C_TRACE_CONTEXT:
				envoyContexts = append(envoyContexts, "TRACE_CONTEXT")
			case meshAPI.Tracing_OpenCensusAgent_GRPC_BIN:
				envoyContexts = append(envoyContexts, "GRPC_TRACE_BIN")
			case meshAPI.Tracing_OpenCensusAgent_CLOUD_TRACE_CONTEXT:
				envoyContexts = append(envoyContexts, "CLOUD_TRACE_CONTEXT")
			case meshAPI.Tracing_OpenCensusAgent_B3:
				envoyContexts = append(envoyContexts, "B3")
			}
		}
		return convertToJSON(envoyContexts), nil
	}
}

func convertToJSON(v interface{}) string {
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
func marshalMetadata(metadata *model.BootstrapNodeMetadata, rawMeta map[string]interface{}) (string, error) {
	b, err := json.Marshal(metadata)
	if err != nil {
		return "", err
	}
	var output map[string]interface{}
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
