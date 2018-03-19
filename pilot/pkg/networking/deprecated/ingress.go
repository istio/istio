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
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy/v1"
)

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

	listeners := []*xdsapi.Listener{buildHTTPListener(opts)}

	// lack of SNI in Envoy implies that TLS secrets are attached to listeners
	// therefore, we should first check that TLS endpoint is needed before shipping TLS listener
	_, secret := v1.BuildIngressRoutes(mesh, ingress, proxyInstances, discovery, config)
	if secret != "" {
		opts.port = 443
		opts.rds = "443"
		listener := buildHTTPListener(opts)
		// TODO(mostrowski)
		/*listener.SSLContext = &SSLContext{
			CertChainFile:  path.Join(model.IngressCertsPath, model.IngressCertFilename),
			PrivateKeyFile: path.Join(model.IngressCertsPath, model.IngressKeyFilename),
			ALPNProtocols:  strings.Join(ListenersALPNProtocols, ","),
		} */
		listeners = append(listeners, listener)
	}

	return listeners
}
