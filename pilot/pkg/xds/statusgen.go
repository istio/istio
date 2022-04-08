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

package xds

import (
	"fmt"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	status "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"
	"google.golang.org/protobuf/proto"
	any "google.golang.org/protobuf/types/known/anypb"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
)

const (
	// TypeURLConnect generate connect event.
	TypeURLConnect = "istio.io/connect"

	// TypeURLDisconnect generate disconnect event.
	TypeURLDisconnect = "istio.io/disconnect"

	// TypeURLNACK will receive messages of type DiscoveryRequest, containing
	// the 'NACK' from envoy on rejected configs. Only ID is set in metadata.
	// This includes all the info that envoy (client) provides.
	TypeURLNACK = "istio.io/nack"

	// TypeDebugSyncronization requests Envoy CSDS for proxy sync status
	TypeDebugSyncronization = "istio.io/debug/syncz"

	// TypeDebugConfigDump requests Envoy configuration for a proxy without creating one
	TypeDebugConfigDump = "istio.io/debug/config_dump"

	// TODO: TypeURLReady - readiness events for endpoints, agent can propagate
)

// StatusGen is a Generator for XDS status: connections, syncz, configdump
type StatusGen struct {
	Server *DiscoveryServer

	// TODO: track last N Nacks and connection events, with 'version' based on timestamp.
	// On new connect, use version to send recent events since last update.
}

func NewStatusGen(s *DiscoveryServer) *StatusGen {
	return &StatusGen{
		Server: s,
	}
}

// Generate XDS responses about internal events:
// - connection status
// - NACKs
// We can also expose ACKS.
func (sg *StatusGen) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	res := model.Resources{}

	switch w.TypeUrl {
	case TypeURLConnect:
		for _, v := range sg.Server.Clients() {
			res = append(res, &discovery.Resource{
				Name:     v.node.Id,
				Resource: util.MessageToAny(v.node),
			})
		}
	case TypeDebugSyncronization:
		res = sg.debugSyncz()
	case TypeDebugConfigDump:
		if len(w.ResourceNames) == 0 || len(w.ResourceNames) > 1 {
			// Malformed request from client
			log.Infof("%s with %d ResourceNames", TypeDebugConfigDump, len(w.ResourceNames))
			break
		}
		var err error
		res, err = sg.debugConfigDump(w.ResourceNames[0])
		if err != nil {
			log.Infof("%s failed: %v", TypeDebugConfigDump, err)
			break
		}
	}
	return res, model.DefaultXdsLogDetails, nil
}

// isSidecar ad-hoc method to see if connection represents a sidecar
func isProxy(con *Connection) bool {
	return con != nil &&
		con.proxy != nil &&
		con.proxy.Metadata != nil &&
		con.proxy.Metadata.ProxyConfig != nil
}

func (sg *StatusGen) debugSyncz() model.Resources {
	res := model.Resources{}

	stypes := []string{
		v3.ListenerType,
		v3.RouteType,
		v3.EndpointType,
		v3.ClusterType,
		v3.ExtensionConfigurationType,
	}

	for _, con := range sg.Server.Clients() {
		con.proxy.RLock()
		// Skip "nodes" without metdata (they are probably istioctl queries!)
		if isProxy(con) {
			xdsConfigs := make([]*status.ClientConfig_GenericXdsConfig, 0)
			for _, stype := range stypes {
				pxc := &status.ClientConfig_GenericXdsConfig{}
				if watchedResource, ok := con.proxy.WatchedResources[stype]; ok {
					pxc.ConfigStatus = debugSyncStatus(watchedResource)
				} else {
					pxc.ConfigStatus = status.ConfigStatus_NOT_SENT
				}

				pxc.TypeUrl = stype

				xdsConfigs = append(xdsConfigs, pxc)
			}
			clientConfig := &status.ClientConfig{
				Node: &core.Node{
					Id:       con.proxy.ID,
					Metadata: con.proxy.Metadata.ToStruct(),
				},
				GenericXdsConfigs: xdsConfigs,
			}
			res = append(res, &discovery.Resource{
				Name:     clientConfig.Node.Id,
				Resource: util.MessageToAny(clientConfig),
			})
		}
		con.proxy.RUnlock()
	}

	return res
}

func debugSyncStatus(wr *model.WatchedResource) status.ConfigStatus {
	if wr.NonceSent == "" {
		return status.ConfigStatus_NOT_SENT
	}
	if wr.NonceAcked == wr.NonceSent {
		return status.ConfigStatus_SYNCED
	}
	return status.ConfigStatus_STALE
}

func (sg *StatusGen) debugConfigDump(proxyID string) (model.Resources, error) {
	conn := sg.Server.getProxyConnection(proxyID)
	if conn == nil {
		// This is "like" a 404.  The error is the client's.  However, this endpoint
		// only tracks a single "shard" of connections.  The client may try another instance.
		return nil, fmt.Errorf("config dump could not find connection for proxyID %q", proxyID)
	}

	dump, err := sg.Server.configDump(conn)
	if err != nil {
		return nil, err
	}

	return model.AnyToUnnamedResources(dump.Configs), nil
}

func (sg *StatusGen) OnConnect(con *Connection) {
	sg.pushStatusEvent(TypeURLConnect, []proto.Message{con.node})
}

func (sg *StatusGen) OnDisconnect(con *Connection) {
	sg.pushStatusEvent(TypeURLDisconnect, []proto.Message{con.node})
}

func (sg *StatusGen) OnNack(node *model.Proxy, dr *discovery.DiscoveryRequest) {
	// Make sure we include the ID - the DR may not include metadata
	if dr.Node == nil {
		dr.Node = &core.Node{}
	}
	dr.Node.Id = node.ID
	sg.pushStatusEvent(TypeURLNACK, []proto.Message{dr})
}

// pushStatusEvent is similar with DiscoveryServer.pushStatusEvent() - but called directly,
// since status discovery is not driven by config change events.
// We also want connection events to be dispatched as soon as possible,
// they may be consumed by other instances of Istiod to update internal state.
func (sg *StatusGen) pushStatusEvent(typeURL string, data []proto.Message) {
	clients := sg.Server.ClientsOf(typeURL)
	if len(clients) == 0 {
		return
	}

	resources := make([]*any.Any, 0, len(data))
	for _, v := range data {
		resources = append(resources, util.MessageToAny(v))
	}
	dr := &discovery.DiscoveryResponse{
		TypeUrl:   typeURL,
		Resources: resources,
	}

	sg.Server.SendResponse(clients, dr)
}
