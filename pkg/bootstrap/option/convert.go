// Copyright 2019 Istio Authors
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

	envoyAPI "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoyAPICore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/ptypes/wrappers"

	networkingAPI "istio.io/api/networking/v1alpha3"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/bootstrap/auth"
	"istio.io/istio/pkg/config/constants"
)

func keepaliveConverter(value *networkingAPI.ConnectionPoolSettings_TCPSettings_TcpKeepalive) convertFunc {
	return func(*instance) (interface{}, error) {
		upstreamConnectionOptions := &envoyAPI.UpstreamConnectionOptions{
			TcpKeepalive: &envoyAPICore.TcpKeepalive{},
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

func tlsConverter(tls *networkingAPI.TLSSettings, sniName string, metadata *model.NodeMetadata) convertFunc {
	return func(*instance) (interface{}, error) {
		caCertificates := tls.CaCertificates
		if caCertificates == "" && tls.Mode == networkingAPI.TLSSettings_ISTIO_MUTUAL {
			caCertificates = constants.DefaultCertChain
		}
		var certValidationContext *auth.CertificateValidationContext
		var trustedCa *auth.DataSource
		if len(caCertificates) != 0 {
			trustedCa = &auth.DataSource{
				Filename: model.GetOrDefault(metadata.TLSClientRootCert, caCertificates),
			}
		}
		if trustedCa != nil || len(tls.SubjectAltNames) > 0 {
			certValidationContext = &auth.CertificateValidationContext{
				TrustedCa:            trustedCa,
				VerifySubjectAltName: tls.SubjectAltNames,
			}
		}

		var tlsContext *auth.UpstreamTLSContext
		switch tls.Mode {
		case networkingAPI.TLSSettings_SIMPLE:
			tlsContext = &auth.UpstreamTLSContext{
				CommonTLSContext: &auth.CommonTLSContext{
					ValidationContext: certValidationContext,
				},
				Sni: tls.Sni,
			}
			tlsContext.CommonTLSContext.AlpnProtocols = util.ALPNH2Only
		case networkingAPI.TLSSettings_MUTUAL, networkingAPI.TLSSettings_ISTIO_MUTUAL:
			clientCertificate := tls.ClientCertificate
			if tls.ClientCertificate == "" && tls.Mode == networkingAPI.TLSSettings_ISTIO_MUTUAL {
				clientCertificate = constants.DefaultRootCert
			}
			privateKey := tls.PrivateKey
			if tls.PrivateKey == "" && tls.Mode == networkingAPI.TLSSettings_ISTIO_MUTUAL {
				privateKey = constants.DefaultKey
			}
			if clientCertificate == "" || privateKey == "" {
				// TODO(nmittler): Should this be an error?
				log.Errorf("failed to apply tls setting for %s: client certificate and private key must not be empty", sniName)
				return "", nil
			}

			tlsContext = &auth.UpstreamTLSContext{
				CommonTLSContext: &auth.CommonTLSContext{},
				Sni:              tls.Sni,
			}

			tlsContext.CommonTLSContext.ValidationContext = certValidationContext
			tlsContext.CommonTLSContext.TLSCertificates = []*auth.TLSCertificate{
				{
					CertificateChain: &auth.DataSource{
						Filename: model.GetOrDefault(metadata.TLSClientCertChain, clientCertificate),
					},
					PrivateKey: &auth.DataSource{
						Filename: model.GetOrDefault(metadata.TLSClientKey, privateKey),
					},
				},
			}
			if len(tls.Sni) == 0 && tls.Mode == networkingAPI.TLSSettings_ISTIO_MUTUAL {
				tlsContext.Sni = sniName
			}
			if tls.Mode == networkingAPI.TLSSettings_ISTIO_MUTUAL {
				tlsContext.CommonTLSContext.AlpnProtocols = util.ALPNInMeshH2
			} else {
				tlsContext.CommonTLSContext.AlpnProtocols = util.ALPNH2Only
			}
		default:
			// No TLS.
			return "", nil
		}
		return convertToJSON(tlsContext), nil
	}
}

func nodeMetadataConverter(metadata *model.NodeMetadata, rawMeta map[string]interface{}) convertFunc {
	return func(*instance) (interface{}, error) {
		marshalString, err := marshalMetadata(metadata, rawMeta)
		if err != nil {
			return "", err
		}
		return marshalString, nil
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
func marshalMetadata(metadata *model.NodeMetadata, rawMeta map[string]interface{}) (string, error) {
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
