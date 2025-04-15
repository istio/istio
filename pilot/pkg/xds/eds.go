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

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pilot/pkg/xds/endpoints"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/util/sets"
)

// SvcUpdate is a callback from service discovery when service info changes.
func (s *DiscoveryServer) SvcUpdate(shard model.ShardKey, hostname string, namespace string, event model.Event) {
	// When a service deleted, we should cleanup the endpoint shards and also remove keys from EndpointIndex to
	// prevent memory leaks.
	if event == model.EventDelete {
		inboundServiceDeletes.Increment()
		s.Env.EndpointIndex.DeleteServiceShard(shard, hostname, namespace, false)
	} else {
		inboundServiceUpdates.Increment()
	}
}

// EDSUpdate computes destination address membership across all clusters and networks.
// This is the main method implementing EDS.
// It replaces InstancesByPort in model - instead of iterating over all endpoints it uses
// the hostname-keyed map. And it avoids the conversion from Endpoint to ServiceEntry to envoy
// on each step: instead the conversion happens once, when an endpoint is first discovered.
func (s *DiscoveryServer) EDSUpdate(shard model.ShardKey, serviceName string, namespace string,
	istioEndpoints []*model.IstioEndpoint,
) {
	inboundEDSUpdates.Increment()
	// Update the endpoint shards
	pushType := s.Env.EndpointIndex.UpdateServiceEndpoints(shard, serviceName, namespace, istioEndpoints, true)
	if pushType == model.IncrementalPush || pushType == model.FullPush {
		// Trigger a push
		s.ConfigUpdate(&model.PushRequest{
			Full:           pushType == model.FullPush,
			ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: serviceName, Namespace: namespace}),
			Reason:         model.NewReasonStats(model.EndpointUpdate),
		})
	}
}

// EDSCacheUpdate computes destination address membership across all clusters and networks.
// This is the main method implementing EDS.
// It replaces InstancesByPort in model - instead of iterating over all endpoints it uses
// the hostname-keyed map. And it avoids the conversion from Endpoint to ServiceEntry to envoy
// on each step: instead the conversion happens once, when an endpoint is first discovered.
//
// Note: the difference with `EDSUpdate` is that it only update the cache rather than requesting a push
func (s *DiscoveryServer) EDSCacheUpdate(shard model.ShardKey, serviceName string, namespace string,
	istioEndpoints []*model.IstioEndpoint,
) {
	inboundEDSUpdates.Increment()
	// Update the endpoint shards
	s.Env.EndpointIndex.UpdateServiceEndpoints(shard, serviceName, namespace, istioEndpoints, false)
}

func (s *DiscoveryServer) RemoveShard(shardKey model.ShardKey) {
	s.Env.EndpointIndex.DeleteShard(shardKey)
}

// EdsGenerator implements the new Generate method for EDS, using the in-memory, optimized endpoint
// storage in DiscoveryServer.
type EdsGenerator struct {
	Cache         model.XdsCache
	EndpointIndex *model.EndpointIndex
}

var _ model.XdsDeltaResourceGenerator = &EdsGenerator{}

// Map of all configs that do not impact EDS
var skippedEdsConfigs = sets.New(
	kind.Gateway,
	kind.VirtualService,
	kind.WorkloadGroup,
	kind.AuthorizationPolicy,
	kind.RequestAuthentication,
	kind.Secret,
	kind.Telemetry,
	kind.WasmPlugin,
	kind.ProxyConfig,
	kind.DNSName,
)

func edsNeedsPush(req *model.PushRequest, proxy *model.Proxy) bool {
	if res, ok := xdsNeedsPush(req, proxy); ok {
		return res
	}
	for config := range req.ConfigsUpdated {
		if !skippedEdsConfigs.Contains(config.Kind) {
			return true
		}
	}
	return false
}

func (eds *EdsGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	if !edsNeedsPush(req, proxy) {
		return nil, model.DefaultXdsLogDetails, nil
	}
	resources, logDetails := eds.buildEndpoints(proxy, req, w)
	return resources, logDetails, nil
}

func (eds *EdsGenerator) GenerateDeltas(proxy *model.Proxy, req *model.PushRequest,
	w *model.WatchedResource,
) (model.Resources, model.DeletedResources, model.XdsLogDetails, bool, error) {
	if !edsNeedsPush(req, proxy) {
		return nil, nil, model.DefaultXdsLogDetails, false, nil
	}
	if !shouldUseDeltaEds(req) {
		resources, logDetails := eds.buildEndpoints(proxy, req, w)
		return resources, nil, logDetails, false, nil
	}

	resources, removed, logs := eds.buildDeltaEndpoints(proxy, req, w)
	return resources, removed, logs, true, nil
}

func shouldUseDeltaEds(req *model.PushRequest) bool {
	if !req.Full {
		return false
	}
	return canSendPartialFullPushes(req)
}

// canSendPartialFullPushes checks if a request contains *only* endpoints updates except `skippedEdsConfigs`.
// This allows us to perform more efficient pushes where we only update the endpoints that did change.
func canSendPartialFullPushes(req *model.PushRequest) bool {
	// If we don't know what configs are updated, just send a full push
	if req.Forced {
		return false
	}
	for cfg := range req.ConfigsUpdated {
		if skippedEdsConfigs.Contains(cfg.Kind) {
			// the updated config does not impact EDS, skip it
			// this happens when push requests are merged due to debounce
			continue
		}
		if cfg.Kind != kind.ServiceEntry {
			return false
		}
	}
	return true
}

func (eds *EdsGenerator) buildEndpoints(proxy *model.Proxy,
	req *model.PushRequest,
	w *model.WatchedResource,
) (model.Resources, model.XdsLogDetails) {
	var edsUpdatedServices map[string]struct{}
	// canSendPartialFullPushes determines if we can send a partial push (ie a subset of known CLAs).
	// This is safe when only Services has changed, as this implies that only the CLAs for the
	// associated Service changed. Note when a multi-network Service changes it triggers a push with
	// ConfigsUpdated=ALL, so in this case we would not enable a partial push.
	// Despite this code existing on the SotW code path, sending these partial pushes is still allowed;
	// see https://www.envoyproxy.io/docs/envoy/latest/api-docs/xds_protocol#grouping-resources-into-responses
	if !req.Full || canSendPartialFullPushes(req) {
		edsUpdatedServices = model.ConfigNamesOfKind(req.ConfigsUpdated, kind.ServiceEntry)
	}
	var resources model.Resources
	empty := 0
	cached := 0
	regenerated := 0
	for clusterName := range w.ResourceNames {
		if edsUpdatedServices != nil {
			if _, ok := edsUpdatedServices[model.ParseSubsetKeyHostname(clusterName)]; !ok {
				// Cluster was not updated, skip recomputing. This happens when we get an incremental update for a
				// specific Hostname. On connect or for full push edsUpdatedServices will be empty.
				continue
			}
		}
		builder := endpoints.NewEndpointBuilder(clusterName, proxy, req.Push)

		// We skip cache if assertions are enabled, so that the cache will assert our eviction logic is correct
		if !features.EnableUnsafeAssertions {
			cachedEndpoint := eds.Cache.Get(&builder)
			if cachedEndpoint != nil {
				resources = append(resources, cachedEndpoint)
				cached++
				continue
			}
		}

		// generate eds from beginning
		{
			l := builder.BuildClusterLoadAssignment(eds.EndpointIndex)
			if l == nil {
				continue
			}
			regenerated++

			if len(l.Endpoints) == 0 {
				empty++
			}
			resource := &discovery.Resource{
				Name:     l.ClusterName,
				Resource: protoconv.MessageToAny(l),
			}
			resources = append(resources, resource)
			eds.Cache.Add(&builder, req, resource)
		}
	}
	return resources, model.XdsLogDetails{
		Incremental:    len(edsUpdatedServices) != 0,
		AdditionalInfo: fmt.Sprintf("empty:%v cached:%v/%v", empty, cached, cached+regenerated),
	}
}

// TODO(@hzxuzhonghu): merge with buildEndpoints
func (eds *EdsGenerator) buildDeltaEndpoints(proxy *model.Proxy,
	req *model.PushRequest,
	w *model.WatchedResource,
) (model.Resources, []string, model.XdsLogDetails) {
	edsUpdatedServices := model.ConfigNamesOfKind(req.ConfigsUpdated, kind.ServiceEntry)
	var resources model.Resources
	var removed []string
	empty := 0
	cached := 0
	regenerated := 0

	for clusterName := range w.ResourceNames {
		// filter out eds that are not updated for clusters
		if _, ok := edsUpdatedServices[model.ParseSubsetKeyHostname(clusterName)]; !ok {
			continue
		}

		builder := endpoints.NewEndpointBuilder(clusterName, proxy, req.Push)
		// if a service is not found, it means the cluster is removed
		if !builder.ServiceFound() {
			removed = append(removed, clusterName)
			continue
		}

		// We skip cache if assertions are enabled, so that the cache will assert our eviction logic is correct
		if !features.EnableUnsafeAssertions {
			cachedEndpoint := eds.Cache.Get(&builder)
			if cachedEndpoint != nil {
				resources = append(resources, cachedEndpoint)
				cached++
				continue
			}
		}
		// generate new eds cache
		{
			l := builder.BuildClusterLoadAssignment(eds.EndpointIndex)
			if l == nil {
				removed = append(removed, clusterName)
				continue
			}
			regenerated++
			if len(l.Endpoints) == 0 {
				empty++
			}
			resource := &discovery.Resource{
				Name:     l.ClusterName,
				Resource: protoconv.MessageToAny(l),
			}
			resources = append(resources, resource)
			eds.Cache.Add(&builder, req, resource)
		}
	}
	return resources, removed, model.XdsLogDetails{
		Incremental:    len(edsUpdatedServices) != 0,
		AdditionalInfo: fmt.Sprintf("empty:%v cached:%v/%v", empty, cached, cached+regenerated),
	}
}
