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

package v2

import (
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy/v1"
)

func buildExternalServiceTCPListeners(mesh *meshconfig.MeshConfig, config model.IstioConfigStore) ([]*xdsapi.Listener, v1.Clusters) {
	listeners := make([]*xdsapi.Listener, 0)
	clusters := make(v1.Clusters, 0)

	for _, externalServiceConfig := range config.ExternalServices() {
		externalService := externalServiceConfig.Spec.(*networking.ExternalService)
		for _, port := range externalService.Ports {
			modelPort := v1.BuildExternalServicePort(port)
			switch modelPort.Protocol {
			case model.ProtocolTCP, model.ProtocolMongo, model.ProtocolRedis, model.ProtocolHTTPS:
				routes := make([]*v1.TCPRoute, 0)

				for _, host := range externalService.Hosts {
					cluster := v1.BuildExternalServiceCluster(mesh, host, port.Name, modelPort, nil,
						externalService.Discovery, externalService.Endpoints)
					route := v1.BuildTCPRoute(cluster, []string{host})

					clusters = append(clusters, cluster)
					routes = append(routes, route)
				}

				// TODO: handle port conflicts
				routeConfig := &v1.TCPRouteConfig{Routes: routes}
				listeners = append(listeners,
					buildTCPListener(routeConfig, v1.WildcardAddress, port.Number, modelPort.Protocol))
			default:
				// handled elsewhere
			}
		}
	}

	return listeners, clusters
}
