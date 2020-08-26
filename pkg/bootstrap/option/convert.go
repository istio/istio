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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	auth "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/envoyproxy/go-control-plane/pkg/conversion"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/gogo/protobuf/types"
	pstruct "github.com/golang/protobuf/ptypes/struct"
	"github.com/golang/protobuf/ptypes/wrappers"

	networkingAPI "istio.io/api/networking/v1alpha3"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	authn_model "istio.io/istio/pilot/pkg/security/model"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
)

//TransportSocket wraps UpstreamTLSContext
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
		tlsContextStruct, _ := conversion.MessageToStruct(util.MessageToAny(tlsContext))
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

	// We always set v3, since we know this is a new proxy that supports v3
	requestedType := v3.ClusterType

	switch tls.Mode {
	case networkingAPI.ClientTLSSettings_SIMPLE:
		res := model.SdsCertificateConfig{
			CaCertificatePath: model.GetOrDefault(metadata.TLSClientRootCert, tls.CaCertificates),
		}

		tlsContext.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_CombinedValidationContext{
			CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
				DefaultValidationContext:         &auth.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch(tls.SubjectAltNames)},
				ValidationContextSdsSecretConfig: authn_model.ConstructSdsSecretConfig(res.GetRootResourceName(), requestedType),
			},
		}
		tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNH2Only
	case networkingAPI.ClientTLSSettings_MUTUAL:
		res := model.SdsCertificateConfig{
			CertificatePath:   model.GetOrDefault(metadata.TLSClientCertChain, tls.ClientCertificate),
			PrivateKeyPath:    model.GetOrDefault(metadata.TLSClientKey, tls.PrivateKey),
			CaCertificatePath: model.GetOrDefault(metadata.TLSClientRootCert, tls.CaCertificates),
		}
		if len(res.GetResourceName()) > 0 {
			tlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs = append(tlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs,
				authn_model.ConstructSdsSecretConfig(res.GetResourceName(), requestedType))
		}

		tlsContext.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_CombinedValidationContext{
			CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
				DefaultValidationContext:         &auth.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch(tls.SubjectAltNames)},
				ValidationContextSdsSecretConfig: authn_model.ConstructSdsSecretConfig(res.GetRootResourceName(), requestedType),
			},
		}
		tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNH2Only
	case networkingAPI.ClientTLSSettings_ISTIO_MUTUAL:
		tlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs = append(tlsContext.CommonTlsContext.TlsCertificateSdsSecretConfigs,
			authn_model.ConstructSdsSecretConfig(authn_model.SDSDefaultResourceName, requestedType))

		tlsContext.CommonTlsContext.ValidationContextType = &auth.CommonTlsContext_CombinedValidationContext{
			CombinedValidationContext: &auth.CommonTlsContext_CombinedCertificateValidationContext{
				DefaultValidationContext:         &auth.CertificateValidationContext{MatchSubjectAltNames: util.StringToExactMatch(tls.SubjectAltNames)},
				ValidationContextSdsSecretConfig: authn_model.ConstructSdsSecretConfig(authn_model.SDSRootResourceName, requestedType),
			},
		}
		tlsContext.CommonTlsContext.AlpnProtocols = util.ALPNInMeshH2
	default:
		// No TLS.
		return nil
	}
	if len(tls.Sni) == 0 && tls.Mode == networkingAPI.ClientTLSSettings_ISTIO_MUTUAL {
		tlsContext.Sni = sniName
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

		return fmt.Sprintf("{\"address\": \"%s\", \"port_value\": %s}", host, port), nil
	}
}

func durationConverter(value *types.Duration) convertFunc {
	return func(*instance) (interface{}, error) {
		return value.String(), nil
	}
}

func podIPConverter(value net.IP) convertFunc {
	return func(*instance) (interface{}, error) {
		return base64.StdEncoding.EncodeToString(value), nil
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
