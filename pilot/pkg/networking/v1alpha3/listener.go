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
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/kylelemons/godebug/pretty"

	google_protobuf "github.com/gogo/protobuf/types"
	_ "github.com/golang/glog" // nolint

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

// LdsConnection is a listener connection type.
type LdsConnection struct {
	// PeerAddr is the address of the client envoy, from network layer
	PeerAddr string

	// Time of connection, for debugging
	Connect time.Time

	// Sending on this channel results in  push. We may also make it a channel of objects so
	// same info can be sent to all clients, without recomputing.
	PushChannel chan struct{}

	// TODO: migrate other fields as needed from model.Proxy and replace it

	//HttpConnectionManagers map[string]*http_conn.HttpConnectionManager

	HTTPListeners map[string]*xdsapi.Listener

	// TODO: TcpListeners (may combine mongo/etc)
}

// LdsDiscoveryResponse returns a list of listeners for the given environment and source node.
func (lc *LdsConnection) LdsDiscoveryResponse(env model.Environment, node model.Proxy) (*xdsapi.DiscoveryResponse, error) {
	ls, err := lc.BuildListeners(env, node)
	if err != nil {
		return nil, err
	}
	log.Infof("LDS: %s %s %s: \n%s", node.ID, node.IPAddress, node.Type, pretty.Sprint(ls))
	resp := &xdsapi.DiscoveryResponse{TypeUrl: ldsType}
	for _, ll := range ls {
		lr, _ := google_protobuf.MarshalAny(ll)
		resp.Resources = append(resp.Resources, *lr)
	}

	return resp, nil
}

// BuildListeners produces a list of listeners and referenced clusters for all proxies
func (lc *LdsConnection) BuildListeners(env model.Environment, node model.Proxy) ([]*xdsapi.Listener, error) {
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
	case model.Router:
		// TODO: add listeners for other protocols too
		return buildGatewayHTTPListeners(env.Mesh, env.IstioConfigStore, node)
	case model.Ingress:
		// TODO : Need v1alpha3 equivalent of buildIngressGateway
	}
	return nil, nil
}
