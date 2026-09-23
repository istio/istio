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
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/util/sets"
)

// NdsGenerator generates config for Nds i.e. Name Discovery Service. Istio agents
// send NDS requests to istiod and istiod responds with a list of services and their
// associated IPs (including service entries).
// The agent then updates its internal DNS based on this data. If DNS capture is enabled
// in the pod the agent will capture all DNS requests and attempt to resolve locally before
// forwarding to upstream dns servers.
type NdsGenerator struct {
	ConfigGenerator core.ConfigGenerator
}

var _ model.XdsResourceGenerator = &NdsGenerator{}

var _ model.XdsDeltaResourceGenerator = &NdsGenerator{}

var minimumDeltaNDSVersion = &model.IstioVersion{Major: 1, Minor: 32, Patch: 0}

// Map of all configs that do not impact NDS
var skippedNdsConfigs = func() sets.Set[kind.Kind] {
	s := sets.New(
		kind.Gateway,
		kind.VirtualService,
		kind.DestinationRule,
		kind.Secret,
		kind.Telemetry,
		kind.EnvoyFilter,
		kind.WorkloadEntry,
		kind.WorkloadGroup,
		kind.AuthorizationPolicy,
		kind.RequestAuthentication,
		kind.PeerAuthentication,
		kind.WasmPlugin,
		kind.TrafficExtension,
		kind.ProxyConfig,
		kind.MeshConfig,
		kind.Endpoints,
	)
	if features.ScopedAddressPushes {
		// The DNS name table is derived from services; ambient Address changes that matter
		// to it arrive as ServiceEntry updates.
		s.Insert(kind.Address)
	}
	return s
}()

func ndsNeedsPush(req *model.PushRequest, proxy *model.Proxy) bool {
	if res, ok := xdsNeedsPush(req, proxy); ok {
		return res
	}
	for config := range req.ConfigsUpdated {
		if _, f := skippedNdsConfigs[config.Kind]; !f {
			return true
		}
	}
	return false
}

func (n NdsGenerator) Generate(proxy *model.Proxy, _ *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	if !ndsNeedsPush(req, proxy) {
		return nil, model.DefaultXdsLogDetails, nil
	}
	nt := n.ConfigGenerator.BuildNameTable(proxy, req.Push)
	if nt == nil {
		return nil, model.DefaultXdsLogDetails, nil
	}
	resources := model.Resources{&discovery.Resource{Resource: protoconv.MessageToAny(nt)}}
	return resources, model.DefaultXdsLogDetails, nil
}

// GenerateDeltas uses generic Delta xDS NACK handling: record the rejection without resetting the stream or forcing a snapshot.
func (n NdsGenerator) GenerateDeltas(proxy *model.Proxy, req *model.PushRequest,
	watched *model.WatchedResource,
) (model.Resources, model.DeletedResources, model.XdsLogDetails, bool, error) {
	// DELTA_NDS expresses intent; known Istio versions before 1.32 cannot consume named resources.
	if !supportsDeltaNDS(proxy) {
		resources, details, err := n.Generate(proxy, watched, req)
		return resources, nil, details, false, err
	}
	req, needsPush := filterNdsPush(req, proxy)
	if !needsPush {
		return nil, nil, model.DefaultXdsLogDetails, false, nil
	}
	resources, removed, details, usedDelta := n.ConfigGenerator.BuildDeltaNameTable(proxy, req, watched)
	return resources, removed, details, usedDelta, nil
}

func supportsDeltaNDS(proxy *model.Proxy) bool {
	// Non-empty custom versions follow Istio's optimistic version handling; a missing version falls back to legacy NDS.
	return proxy.Metadata != nil && bool(proxy.Metadata.DeltaNDS) && proxy.Metadata.IstioVersion != "" &&
		proxy.IstioVersion != nil && proxy.VersionGreaterOrEqual(minimumDeltaNDSVersion)
}

// filterNdsPush retains the exact update keys needed to calculate additions and removals.
func filterNdsPush(req *model.PushRequest, proxy *model.Proxy) (*model.PushRequest, bool) {
	if res, ok := xdsNeedsPush(req, proxy); ok {
		return req, res
	}
	relevantUpdates := make(sets.Set[model.ConfigKey])
	for config := range req.ConfigsUpdated {
		if _, skipped := skippedNdsConfigs[config.Kind]; !skipped {
			relevantUpdates.Insert(config)
		}
	}
	if len(relevantUpdates) == len(req.ConfigsUpdated) {
		return req, len(relevantUpdates) > 0
	}
	filteredReq := *req
	filteredReq.ConfigsUpdated = relevantUpdates
	return &filteredReq, len(relevantUpdates) > 0
}
