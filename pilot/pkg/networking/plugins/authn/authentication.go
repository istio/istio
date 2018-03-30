// Copyright 2018 Istio Authors
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

package authn

import (
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"

	authn "istio.io/api/authentication/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
)

const (
	// jwtFilterName is the name for the Jwt filter. This should be the same
	// as the name defined in
	// https://github.com/istio/proxy/blob/master/src/envoy/http/jwt_auth/http_filter_factory.cc#L50
	jwtFilterName = "jwt-auth"
)

// AuthenticationPlugin implements Istio mTLS auth
type AuthenticationPlugin struct{}

// NewPluginInstance returns an instance of the authn plugin
func NewPluginInstance() *AuthenticationPlugin {
	return &AuthenticationPlugin{}
}

// BuildJwtFilter returns a Jwt filter for all Jwt specs in the policy.
func BuildJwtFilter(policy *authn.Policy) *http_conn.HttpFilter {
	filterConfigProto := model.ConvertPolicyToJwtConfig(policy)
	if filterConfigProto == nil {
		return nil
	}
	return &http_conn.HttpFilter{
		Name:   jwtFilterName,
		Config: util.MessageToStruct(filterConfigProto),
	}
}

// OnOutboundListener is called whenever a new outbound listener is added to the LDS output for a given service
// Can be used to add additional filters on the outbound path
func (*AuthenticationPlugin) OnOutboundListener(env model.Environment, node model.Proxy, service *model.Service,
	servicePort *model.Port, listener *xdsapi.Listener) {
}

// OnInboundListener is called whenever a new listener is added to the LDS output for a given service
// Can be used to add additional filters (e.g., mixer filter) or add more stuff to the HTTP connection manager
// on the inbound path
func (*AuthenticationPlugin) OnInboundListener(env model.Environment, node model.Proxy, service *model.Service,
	servicePort *model.Port, listener *xdsapi.Listener) {
}

// OnInboundCluster is called whenever a new cluster is added to the CDS output
// Not used typically
func (*AuthenticationPlugin) OnInboundCluster(env model.Environment, node model.Proxy, service *model.Service,
	servicePort *model.Port, cluster *xdsapi.Cluster) {
}

// OnOutboundRoute is called whenever a new set of virtual hosts (a set of virtual hosts with routes) is added to
// RDS in the outbound path. Can be used to add route specific metadata or additional headers to forward
func (*AuthenticationPlugin) OnOutboundRoute(env model.Environment, node model.Proxy,
	route *xdsapi.RouteConfiguration) {
}

// OnInboundRoute is called whenever a new set of virtual hosts are added to the inbound path.
// Can be used to enable route specific stuff like Lua filters or other metadata.
func (*AuthenticationPlugin) OnInboundRoute(env model.Environment, node model.Proxy, service *model.Service,
	servicePort *model.Port, route *xdsapi.RouteConfiguration) {
}

// OnOutboundCluster is called whenever a new cluster is added to the CDS output
// Typically used by AuthN plugin to add mTLS settings
func (*AuthenticationPlugin) OnOutboundCluster(env model.Environment, node model.Proxy, service *model.Service,
	servicePort *model.Port, cluster *xdsapi.Cluster) {
	mesh := env.Mesh
	config := env.IstioConfigStore

	// Original DST cluster are used to route to services outside the mesh
	// where Istio auth does not apply.
	if cluster.Type == xdsapi.Cluster_ORIGINAL_DST {
		return
	}

	required, _ := model.RequireTLS(model.GetConsolidateAuthenticationPolicy(mesh, config, service.Hostname, servicePort))
	if isDestinationExcludedForMTLS(service.Hostname, mesh.MtlsExcludedServices) && !required {
		return
	}

	// apply auth policies
	serviceAccounts := env.ServiceAccounts.GetIstioServiceAccounts(service.Hostname, []string{servicePort.Name})

	cluster.TlsContext = &auth.UpstreamTlsContext{
		CommonTlsContext: &auth.CommonTlsContext{
			TlsCertificates: []*auth.TlsCertificate{
				{
					CertificateChain: &core.DataSource{
						Specifier: &core.DataSource_Filename{
							Filename: model.AuthCertsPath + model.CertChainFilename,
						},
					},
					PrivateKey: &core.DataSource{
						Specifier: &core.DataSource_Filename{
							Filename: model.AuthCertsPath + model.KeyFilename,
						},
					},
				},
			},
			ValidationContext: &auth.CertificateValidationContext{
				TrustedCa: &core.DataSource{
					Specifier: &core.DataSource_Filename{
						Filename: model.AuthCertsPath + model.RootCertFilename,
					},
				},
				VerifySubjectAltName: serviceAccounts,
			},
		},
	}
}

func isDestinationExcludedForMTLS(destService string, mtlsExcludedServices []string) bool {
	for _, serviceName := range mtlsExcludedServices {
		if destService == serviceName {
			return true
		}
	}
	return false
}
