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

package xds

import (
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/any"
	structpb "github.com/golang/protobuf/ptypes/struct"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
)

const (
	TypeURLConnections = "istio.io/connections"
	TypeURLDisconnect  = "istio.io/disconnect"

	// TODO: TypeURLReady - readiness events for endpoints, agent can propagate

	// TypeURLNACK will receive messages of type DiscoveryRequest, containing
	// the 'NACK' from envoy on rejected configs. Only ID is set in metadata.
	// This includes all the info that envoy (client) provides.
	TypeURLNACK = "istio.io/nack"
)

// InternalGen is a Generator for XDS status updates: connect, disconnect, nacks, acks
type InternalGen struct {
	Server *DiscoveryServer

	// TODO: track last N Nacks and connection events, with 'version' based on timestamp.
	// On new connect, use version to send recent events since last update.
}

func (sg *InternalGen) OnConnect(con *Connection) {
	if con.xdsNode.Metadata != nil && con.xdsNode.Metadata.Fields != nil {
		con.xdsNode.Metadata.Fields["istiod"] = &structpb.Value{
			Kind: &structpb.Value_StringValue{
				StringValue: "TODO", // TODO: fill in the Istiod address - may include network, cluster, IP
			},
		}
		con.xdsNode.Metadata.Fields["con"] = &structpb.Value{
			Kind: &structpb.Value_StringValue{
				StringValue: con.ConID,
			},
		}
	}
	sg.startPush(TypeURLConnections, []proto.Message{con.xdsNode})
}

func (sg *InternalGen) OnDisconnect(con *Connection) {
	sg.startPush(TypeURLDisconnect, []proto.Message{con.xdsNode})

	if con.xdsNode.Metadata != nil && con.xdsNode.Metadata.Fields != nil {
		con.xdsNode.Metadata.Fields["istiod"] = &structpb.Value{
			Kind: &structpb.Value_StringValue{
				StringValue: "", // TODO: using empty string to indicate this node has no istiod connection. We'll iterate.
			},
		}
	}

	// Note that it is quite possible for a 'connect' on a different istiod to happen before a disconnect.
}

func (sg *InternalGen) OnNack(node *model.Proxy, dr *discovery.DiscoveryRequest) {
	// Make sure we include the ID - the DR may not include metadata
	dr.Node.Id = node.ID
	sg.startPush(TypeURLNACK, []proto.Message{dr})
}

// PushAll will immediately send a response to all connections that
// are watching for the specific type.
// TODO: additional filters can be added, for example namespace.
func (s *DiscoveryServer) PushAll(res *discovery.DiscoveryResponse) {
	// Push config changes, iterating over connected envoys. This cover ADS and EDS(0.7), both share
	// the same connection table
	s.adsClientsMutex.RLock()
	// Create a temp map to avoid locking the add/remove
	pending := []*Connection{}
	for _, v := range sg.Server.adsClients {
		if v.node.Active[typeURL] != nil {
			pending = append(pending, v)
		}
	}
	s.adsClientsMutex.RUnlock()

	// only marshal resources if there are connected clients
	if len(pending) == 0 {
		return
	}

	for _, p := range pending {
		// p.send() waits for an ACK - which is reasonable for normal push,
		// but in this case we want to sync fast and not bother with stuck connections.
		// This is expecting a relatively small number of watchers - each other istiod
		// plus few admin tools or bridges to real message brokers. The normal
		// push expects 1000s of envoy connections.
		con := p
		go func() {
			err := con.stream.Send(res)
			if err != nil {
				adsLog.Infoa("Failed to send internal event ", con.ConID, " ", err)
			}
		}()
	}
}

// startPush is similar with DiscoveryServer.startPush() - but called directly,
// since status discovery is not driven by config change events.
// We also want connection events to be dispatched as soon as possible,
// they may be consumed by other instances of Istiod to update internal state.
func (sg *InternalGen) startPush(typeURL string, data []proto.Message) {

	resources := make([]*any.Any, 0, len(data))
  for _, v := range data {
  	resources = append(resources, util.MessageToAny(v))
  }
	dr := &discovery.DiscoveryResponse{
		TypeUrl:   typeURL,
		Resources: resources,
	}

	sg.Server.PushAll(dr)
}

// Generate XDS responses about internal events:
// - connection status
// - NACKs
//
// We can also expose ACKS.
func (sg *InternalGen) Generate(proxy *model.Proxy, push *model.PushContext, w *model.WatchedResource, updates model.XdsUpdates) model.Resources {
	res := []*any.Any{}
	switch w.TypeUrl {
	case TypeURLConnections:
		sg.Server.adsClientsMutex.RLock()
		// Create a temp map to avoid locking the add/remove
		for _, v := range sg.Server.adsClients {
			res = append(res, util.MessageToAny(v.xdsNode))
		}
		sg.Server.adsClientsMutex.RUnlock()
	}
	return res
}
