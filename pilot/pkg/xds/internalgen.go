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
	"github.com/golang/protobuf/ptypes/any"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
)

const (
	TypeURLConnections = "istio.io/connections"

	// TODO: TypeURLReady - readiness events for endpoints, agent can propagate

	// TypeDebugSyncronization requests Envoy CSDS for proxy sync status
	TypeDebugSyncronization = "istio.io/debug/syncz"

	// TypeDebugConfigDump requests Envoy configuration for a proxy without creating one
	TypeDebugConfigDump = "istio.io/debug/config_dump"
)

// InternalGen is a Generator for XDS status updates: connect, disconnect, nacks, acks
type InternalGen struct {
	Server *DiscoveryServer

	// TODO: track last N Nacks and connection events, with 'version' based on timestamp.
	// On new connect, use version to send recent events since last update.
}

func NewInternalGen(s *DiscoveryServer) *InternalGen {
	return &InternalGen{
		Server: s,
	}
}

// PushAll will immediately send a response to all connections that
// are watching for the specific type.
// TODO: additional filters can be added, for example namespace.
func (s *DiscoveryServer) PushAll(res *discovery.DiscoveryResponse) {
	// Push config changes, iterating over connected envoys. This cover ADS and EDS(0.7), both share
	// the same connection table
	// Create a temp map to avoid locking the add/remove
	pending := []*Connection{}
	for _, v := range s.Clients() {
		v.proxy.RLock()
		if v.proxy.WatchedResources[res.TypeUrl] != nil {
			pending = append(pending, v)
		}
		v.proxy.RUnlock()
	}

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
				adsLog.Info("Failed to send internal event ", con.ConID, " ", err)
			}
		}()
	}
}

// Generate XDS responses about internal events:
// - connection status
// - NACKs
//
// We can also expose ACKS.
func (sg *InternalGen) Generate(proxy *model.Proxy, push *model.PushContext, w *model.WatchedResource, req *model.PushRequest) model.Resources {
	res := []*any.Any{}

	switch w.TypeUrl {
	case TypeURLConnections:
		for _, v := range sg.Server.Clients() {
			res = append(res, util.MessageToAny(v.node))
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
	return res
}

// isSidecar ad-hoc method to see if connection represents a sidecar
func isProxy(con *Connection) bool {
	return con != nil &&
		con.proxy != nil &&
		con.proxy.Metadata != nil &&
		con.proxy.Metadata.ProxyConfig != nil
}

func (sg *InternalGen) debugSyncz() []*any.Any {
	res := []*any.Any{}

	stypes := []string{
		v3.ListenerType,
		v3.RouteType,
		v3.EndpointType,
		v3.ClusterType,
	}

	for _, con := range sg.Server.Clients() {
		con.proxy.RLock()
		// Skip "nodes" without metdata (they are probably istioctl queries!)
		if isProxy(con) {
			xdsConfigs := []*status.PerXdsConfig{}
			for _, stype := range stypes {
				pxc := &status.PerXdsConfig{}
				if watchedResource, ok := con.proxy.WatchedResources[stype]; ok {
					pxc.Status = debugSyncStatus(watchedResource)
				} else {
					pxc.Status = status.ConfigStatus_NOT_SENT
				}
				switch stype {
				case v3.ListenerType:
					pxc.PerXdsConfig = &status.PerXdsConfig_ListenerConfig{}
				case v3.RouteType:
					pxc.PerXdsConfig = &status.PerXdsConfig_RouteConfig{}
				case v3.EndpointType:
					pxc.PerXdsConfig = &status.PerXdsConfig_EndpointConfig{}
				case v3.ClusterType:
					pxc.PerXdsConfig = &status.PerXdsConfig_ClusterConfig{}
				}
				xdsConfigs = append(xdsConfigs, pxc)
			}
			clientConfig := &status.ClientConfig{
				Node: &core.Node{
					Id: con.proxy.ID,
				},
				XdsConfig: xdsConfigs,
			}
			res = append(res, util.MessageToAny(clientConfig))
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

func (sg *InternalGen) debugConfigDump(proxyID string) ([]*any.Any, error) {
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

	return dump.Configs, nil
}
