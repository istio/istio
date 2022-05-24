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

package grpcgen

import (
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/host"
	istiolog "istio.io/pkg/log"
)

// Support generation of 'ApiListener' LDS responses, used for native support of gRPC.
// The same response can also be used by other apps using XDS directly.

// GRPC proposal:
// https://github.com/grpc/proposal/blob/master/A27-xds-global-load-balancing.md
//
// Note that this implementation is tested against gRPC, but it is generic - any other framework can
// use this XDS mode to get load balancing info from Istio, including MC/VM/etc.

// The corresponding RDS response is also generated - currently gRPC has special differences
// and can't understand normal Istio RDS - in particular expects "" instead of "/" as
// default prefix, and is expects just the route for one host.
// handleAck will detect if the message is an ACK or NACK, and update/log/count
// using the generic structures. "Classical" CDS/LDS/RDS/EDS use separate logic -
// this is used for the API-based LDS and generic messages.

var log = istiolog.RegisterScope("grpcgen", "xDS Generator for Proxyless gRPC", 0)

type GrpcConfigGenerator struct{}

func clusterKey(hostname string, port int) string {
	return subsetClusterKey("", hostname, port)
}

func subsetClusterKey(subset, hostname string, port int) string {
	return model.BuildSubsetKey(model.TrafficDirectionOutbound, subset, host.Name(hostname), port)
}

func (g *GrpcConfigGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	switch w.TypeUrl {
	case v3.ListenerType:
		return g.BuildListeners(proxy, req.Push, w.ResourceNames), model.DefaultXdsLogDetails, nil
	case v3.ClusterType:
		return g.BuildClusters(proxy, req.Push, w.ResourceNames), model.DefaultXdsLogDetails, nil
	case v3.RouteType:
		return g.BuildHTTPRoutes(proxy, req.Push, w.ResourceNames), model.DefaultXdsLogDetails, nil
	}

	return nil, model.DefaultXdsLogDetails, nil
}

// buildCommonTLSContext creates a TLS context that assumes 'default' name, and credentials/tls/certprovider/pemfile
// (see grpc/xds/internal/client/xds.go securityConfigFromCluster).
func buildCommonTLSContext(sans []string) *tls.CommonTlsContext {
	var sanMatch []*matcher.StringMatcher
	if len(sans) > 0 {
		sanMatch = util.StringToExactMatch(sans)
	}
	return &tls.CommonTlsContext{
		TlsCertificateCertificateProviderInstance: &tls.CommonTlsContext_CertificateProviderInstance{
			InstanceName:    "default",
			CertificateName: "default",
		},
		ValidationContextType: &tls.CommonTlsContext_CombinedValidationContext{
			CombinedValidationContext: &tls.CommonTlsContext_CombinedCertificateValidationContext{
				ValidationContextCertificateProviderInstance: &tls.CommonTlsContext_CertificateProviderInstance{
					InstanceName:    "default",
					CertificateName: "ROOTCA",
				},
				DefaultValidationContext: &tls.CertificateValidationContext{
					MatchSubjectAltNames: sanMatch,
				},
			},
		},
	}
}
