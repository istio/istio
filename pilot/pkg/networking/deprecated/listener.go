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
	"fmt"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	"github.com/gogo/protobuf/types"

	"strings"

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
		listeners, _ := buildSidecarListenersClusters(env.Mesh, proxyInstances,
			services, env.ManagementPorts(node.IPAddress), node, env.IstioConfigStore)
		return listeners, nil
	case model.Ingress:
		services, err := env.Services()
		if err != nil {
			return nil, err
		}
		var svc *model.Service
		for _, s := range services {
			if strings.HasPrefix(s.Hostname, istioIngress) {
				svc = s
				break
			}
		}
		insts := make([]*model.ServiceInstance, 0, 1)
		if svc != nil {
			insts = append(insts, &model.ServiceInstance{Service: svc})
		}
		return buildIngressListeners(env.Mesh, insts, env.ServiceDiscovery, env.IstioConfigStore, node), nil
	}
	return nil, nil
}

func newHTTPListener(ip string, port int, name string, config *types.Struct) *xdsapi.Listener {
	return &xdsapi.Listener{
		Address: buildAddress(ip, uint32(port)),
		Name:    fmt.Sprintf("http_%s_%d", ip, port),
		FilterChains: []listener.FilterChain{
			{
				Filters: []listener.Filter{
					{
						Name:   filterHTTPConnectionManager,
						Config: config,
					},
				},
			},
		},
	}

}
