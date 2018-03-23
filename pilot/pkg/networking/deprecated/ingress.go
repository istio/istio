// Copyright 2017 Istio Authors
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

package deprecated

import (
	"path"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoy_api_v2_auth "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy/v1"
)

// TODO: rename to lds_ingress or ingress_lds

func buildIngressListeners(mesh *meshconfig.MeshConfig, proxyInstances []*model.ServiceInstance, discovery model.ServiceDiscovery,
	config model.IstioConfigStore,
	ingress model.Proxy) []*xdsapi.Listener {

	opts := buildHTTPListenerOpts{
		mesh:             mesh,
		proxy:            ingress,
		proxyInstances:   proxyInstances,
		routeConfig:      nil,
		ip:               v1.WildcardAddress,
		port:             80,
		rds:              "80",
		useRemoteAddress: true,
		direction:        http_conn.EGRESS,
		outboundListener: false,
		store:            config,
	}

	manager := buildHTTPConnectionManager(opts)
	l := newHTTPListener(opts.ip, opts.port, filterHTTPConnectionManager, messageToStruct(manager))

	listeners := []*xdsapi.Listener{l}

	// lack of SNI in Envoy implies that TLS secrets are attached to listeners
	// therefore, we should first check that TLS endpoint is needed before shipping TLS listener
	_, secret := v1.BuildIngressRoutes(mesh, ingress, proxyInstances, discovery, config)
	if secret != "" {
		opts.port = 443
		opts.rds = "443"
		manager := buildHTTPConnectionManager(opts)
		l := newHTTPListener(opts.ip, opts.port, filterHTTPConnectionManager, messageToStruct(manager))

		l.FilterChains = []listener.FilterChain{
			{
				TlsContext: &envoy_api_v2_auth.DownstreamTlsContext{
					CommonTlsContext: &envoy_api_v2_auth.CommonTlsContext{
						AlpnProtocols: v1.ListenersALPNProtocols,
						TlsCertificates: []*envoy_api_v2_auth.TlsCertificate{
							{
								CertificateChain: &core.DataSource{
									Specifier: &core.DataSource_Filename{
										Filename: path.Join(model.IngressCertsPath, model.IngressCertFilename),
									},
								},
								PrivateKey: &core.DataSource{
									Specifier: &core.DataSource_Filename{
										Filename: path.Join(model.IngressCertsPath, model.IngressKeyFilename),
									},
								},
							},
						},
					},
				},
				Filters: []listener.Filter{
					{
						Name:   filterHTTPConnectionManager,
						Config: messageToStruct(manager),
					},
				},
			},
		}

		listeners = append(listeners, l)
	}

	return listeners
}
