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

package v1alpha3

import (
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"

	_ "github.com/golang/glog" // nolint

	"istio.io/istio/pilot/pkg/model"
)

// BuildListeners produces a list of listeners and referenced clusters for all proxies
func BuildListeners(env model.Environment, node model.Proxy) ([]*xdsapi.Listener, error) {
	switch node.Type {
	case model.Sidecar:
		proxyInstances, err := env.GetProxyServiceInstances(node)
		if err != nil {
			return nil, err
		}
		services, err := env.Services()
		if err != nil {
			return nil, err
		}
		listeners := buildSidecarListeners(env.Mesh, proxyInstances,
			services, env.ManagementPorts(node.IPAddress), node, env.IstioConfigStore)
		return listeners, nil
	case model.Router:
		// TODO: add listeners for other protocols too
		return buildGatewayHTTPListeners(env.Mesh, env.IstioConfigStore, node)
	case model.Ingress:
		// TODO : Need v1alpha3 equivalent of buildIngressGateway
	}
	return nil, nil
}
