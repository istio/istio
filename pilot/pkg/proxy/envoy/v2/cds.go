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

// Functions related to clusters, extracted from the lds implementation
// TODO: cleanup and convert to v2

import (
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy/v1"
)

func buildClusters(env model.Environment, node model.Proxy) (v1.Clusters, error) {
	var clusters v1.Clusters
	var proxyInstances []*model.ServiceInstance
	var err error
	switch node.Type {
	case model.Sidecar, model.Router:
		proxyInstances, err = env.GetProxyServiceInstances(node)
		if err != nil {
			return clusters, err
		}
		var services []*model.Service
		services, err = env.Services()
		if err != nil {
			return clusters, err
		}
		_, clusters = buildSidecarListenersClusters(env.Mesh, proxyInstances,
			services, env.ManagementPorts(node.IPAddress), node, env.IstioConfigStore)
	case model.Ingress:
		httpRouteConfigs, _ := v1.BuildIngressRoutes(env.Mesh, node, nil, env.ServiceDiscovery, env.IstioConfigStore)
		clusters = httpRouteConfigs.Clusters().Normalize()
	}

	if err != nil {
		return clusters, err
	}

	// apply custom policies for outbound clusters
	for _, cluster := range clusters {
		v1.ApplyClusterPolicy(cluster, proxyInstances, env.IstioConfigStore, env.Mesh, env.ServiceAccounts, node.Domain)
	}

	// append Mixer service definition if necessary
	if env.Mesh.MixerCheckServer != "" || env.Mesh.MixerReportServer != "" {
		clusters = append(clusters, v1.BuildMixerClusters(env.Mesh, node, env.MixerSAN)...)
		clusters = append(clusters, v1.BuildMixerAuthFilterClusters(env.IstioConfigStore, env.Mesh, proxyInstances)...)
	}

	return clusters, nil
}
