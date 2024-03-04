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

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
)

const (
	TypeDebugPrefix = v3.DebugType + "/"

	// TypeDebugSyncronization requests Envoy CSDS for proxy sync status
	TypeDebugSyncronization = v3.DebugType + "/syncz"

	// TypeDebugConfigDump requests Envoy configuration for a proxy without creating one
	TypeDebugConfigDump = v3.DebugType + "/config_dump"

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
	return sg.handleInternalRequest(proxy, w, req)
}

// Generate delta XDS responses about internal events:
// - connection status
// - NACKs
// We can also expose ACKS.
func (sg *StatusGen) GenerateDeltas(
	proxy *model.Proxy,
	req *model.PushRequest,
	w *model.WatchedResource,
) (model.Resources, model.DeletedResources, model.XdsLogDetails, bool, error) {
	res, detail, err := sg.handleInternalRequest(proxy, w, req)
	return res, nil, detail, true, err
}

func (sg *StatusGen) handleInternalRequest(_ *model.Proxy, w *model.WatchedResource, _ *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	res := model.Resources{}

	switch w.TypeUrl {
	case TypeDebugSyncronization:
		res = sg.debugSyncz()
	case TypeDebugConfigDump:
		if len(w.ResourceNames) == 0 || len(w.ResourceNames) > 1 {
			// Malformed request from client
			log.Infof("%s with %d ResourceNames", TypeDebugConfigDump, len(w.ResourceNames))
			break
		}
		var err error
		dumpRes, err := sg.debugConfigDump(w.ResourceNames[0])
		if err != nil {
			log.Infof("%s failed: %v", TypeDebugConfigDump, err)
			break
		}
		res = dumpRes
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

func isZtunnel(con *Connection) bool {
	return con != nil &&
		con.proxy != nil &&
		con.proxy.Metadata != nil &&
		con.proxy.Type == model.Ztunnel
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
		// Skip "nodes" without metadata (they are probably istioctl queries!)
		if isProxy(con) || isZtunnel(con) {
			xdsConfigs := make([]*status.ClientConfig_GenericXdsConfig, 0)
			for _, stype := range stypes {
				pxc := &status.ClientConfig_GenericXdsConfig{}
				if watchedResource, ok := con.proxy.WatchedResources[stype]; ok {
					pxc.ConfigStatus = debugSyncStatus(watchedResource)
				} else if isZtunnel(con) {
					pxc.ConfigStatus = status.ConfigStatus_UNKNOWN
				} else {
					pxc.ConfigStatus = status.ConfigStatus_NOT_SENT
				}

				pxc.TypeUrl = stype

				xdsConfigs = append(xdsConfigs, pxc)
			}
			clientConfig := &status.ClientConfig{
				Node: &core.Node{
					Id: con.proxy.ID,
					Metadata: model.NodeMetadata{
						ClusterID:    con.proxy.Metadata.ClusterID,
						Namespace:    con.proxy.Metadata.Namespace,
						IstioVersion: con.proxy.Metadata.IstioVersion,
					}.ToStruct(),
				},
				GenericXdsConfigs: xdsConfigs,
			}
			res = append(res, &discovery.Resource{
				Name:     clientConfig.Node.Id,
				Resource: protoconv.MessageToAny(clientConfig),
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

	dump, err := sg.Server.connectionConfigDump(conn, false)
	if err != nil {
		return nil, err
	}

	return model.AnyToUnnamedResources(dump.Configs), nil
}
