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
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pilot/pkg/xds/endpoints"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/slices"
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

// Map of all configs that impact EDS
// EDS is also affected by MeshConfig changes, but these always trigger a Forced push.
// Sidecar only impacts CDS cluster selection, so it doesn't impact EDS directly, proxy will transparently
// request EDS for new cluster and unsubscribe from old clusters.
var edsAffectingConfigs = sets.New(
	kind.ServiceEntry,
	kind.DestinationRule,
	kind.PeerAuthentication,
	// EnvoyFilter cannot impact EDS directly, but it can modify clusters in-place and this requires we trigger a push
	kind.EnvoyFilter,
)

var deltaAwareEdsConfigs = sets.New(
	kind.ServiceEntry,
	kind.DestinationRule,
	kind.PeerAuthentication,
)

func edsNeedsPush(req *model.PushRequest, proxy *model.Proxy) bool {
	if res, ok := xdsNeedsPush(req, proxy); ok {
		return res
	}
	// CDS needs to be pushed for waypoint proxies on kind.Address changes, so we need to push EDS as well.
	if proxy.Type == model.Waypoint && waypointNeedsPush(req) {
		return true
	}
	for config := range req.ConfigsUpdated {
		if edsAffectingConfigs.Contains(config.Kind) {
			return true
		}
	}
	return false
}

func (eds *EdsGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	if !edsNeedsPush(req, proxy) {
		return nil, model.DefaultXdsLogDetails, nil
	}

	resources, _, logDetails := eds.buildEndpoints(proxy, req, w, false, canSendPartialFullPushes(req))
	return resources, logDetails, nil
}

func (eds *EdsGenerator) GenerateDeltas(proxy *model.Proxy, req *model.PushRequest,
	w *model.WatchedResource,
) (model.Resources, model.DeletedResources, model.XdsLogDetails, bool, error) {
	if !edsNeedsPush(req, proxy) {
		return nil, nil, model.DefaultXdsLogDetails, false, nil
	}

	partialPush := canSendPartialFullPushes(req)
	resources, removed, logs := eds.buildEndpoints(proxy, req, w, partialPush, partialPush)
	return resources, removed, logs, partialPush, nil
}

func canSendPartialFullPushes(req *model.PushRequest) bool {
	if req.Forced {
		return false
	}

	for cfg := range req.ConfigsUpdated {
		if !deltaAwareEdsConfigs.Contains(cfg.Kind) {
			return false
		}
		// same as CDS, if a global PeerAuthentication is updated, all clusters will be rebuilt,
		// so we need to push their endpoints as well.
		if cfg.Kind == kind.PeerAuthentication && cfg.Namespace == req.Push.Mesh.RootNamespace {
			return false
		}
	}

	return true
}

func (eds *EdsGenerator) buildEndpoints(proxy *model.Proxy,
	req *model.PushRequest,
	w *model.WatchedResource,
	delta bool,
	partialPush bool,
) (model.Resources, model.DeletedResources, model.XdsLogDetails) {
	var edsUpdatedServices sets.Set[string]
	var changedDrs sets.Set[types.NamespacedName]
	var changedAuthnNs sets.Set[string]

	if partialPush {
		edsUpdatedServices = sets.New[string]()
		changedDrs = sets.New[types.NamespacedName]()
		changedAuthnNs = sets.New[string]()
		for cfg := range req.ConfigsUpdated {
			switch cfg.Kind {
			case kind.DestinationRule:
				changedDrs.Insert(types.NamespacedName{Name: cfg.Name, Namespace: cfg.Namespace})
			case kind.ServiceEntry:
				edsUpdatedServices.Insert(cfg.Name)
			case kind.PeerAuthentication:
				changedAuthnNs.Insert(cfg.Namespace)
			}
		}
	}
	var resources model.Resources
	var removed model.DeletedResources
	empty := 0
	cached := 0
	regenerated := 0

	for clusterName := range w.ResourceNames {
		affectedService := edsUpdatedServices.Contains(model.ParseSubsetKeyHostname(clusterName))
		if partialPush && changedDrs.IsEmpty() && changedAuthnNs.IsEmpty() &&
			!affectedService {

			// Cluster was not updated and no changes to destination rules or peer authentication policies, skip recomputing.
			continue
		}

		dir, subsetName, hostname, port := model.ParseSubsetKey(clusterName)
		svc := req.Push.ServiceForHostname(proxy, hostname)

		// In delta mode, if a service is not found, it means the cluster is removed
		if delta && svc == nil {
			removed = append(removed, clusterName)
			continue
		}

		var dr *model.ConsolidatedDestRule
		if svc != nil {
			dr = proxy.SidecarScope.DestinationRule(model.TrafficDirectionOutbound, proxy, svc.Hostname)
		}

		// if we can do a partial push, check if the cluster is affected by the changed destination rules or peer authentication policies
		// to avoid recomputing the cluster if it is not affected
		if partialPush && svc != nil && !affectedService {
			if !clusterAffectedByChangedAuthn(svc, changedAuthnNs, req.Push.Mesh.RootNamespace) &&
				!clusterAffectedByChangedDrs(proxy, dr, hostname, changedDrs) {
				continue
			}
		}

		builder := *endpoints.NewCDSEndpointBuilder(proxy, req.Push, clusterName, dir, subsetName, hostname, port, svc, dr)

		// We skip cache if assertions are enabled, so that the cache will assert our eviction logic is correct
		if !features.EnableUnsafeAssertions {
			cachedEndpoint := eds.Cache.Get(&builder)
			if cachedEndpoint != nil {
				resources = append(resources, cachedEndpoint)
				cached++
				continue
			}
		}

		l := builder.BuildClusterLoadAssignment(eds.EndpointIndex)
		if l == nil {
			if delta {
				removed = append(removed, clusterName)
			}
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
	return resources, removed, model.XdsLogDetails{
		Incremental:    len(edsUpdatedServices) != 0,
		AdditionalInfo: fmt.Sprintf("empty:%v cached:%v/%v", empty, cached, cached+regenerated),
	}
}

// clusterAffectedByChangedDrs checks if the service is affected by the changed destination rules
func clusterAffectedByChangedDrs(
	proxy *model.Proxy,
	currentDr *model.ConsolidatedDestRule,
	hostname host.Name,
	changedDrs sets.Set[types.NamespacedName],
) bool {
	if changedDrs.IsEmpty() {
		return false
	}

	if currentDr != nil {
		if slices.ContainsFunc(currentDr.GetFrom(), changedDrs.Contains) {
			return true
		}
	}

	if proxy.PrevSidecarScope != nil {
		if dr := proxy.PrevSidecarScope.DestinationRule(model.TrafficDirectionOutbound, proxy, hostname); dr != nil {
			if slices.ContainsFunc(dr.GetFrom(), changedDrs.Contains) {
				return true
			}
		}
	}

	return false
}

// clusterAffectedByChangedAuthn checks if the service is affected by the changed peer authentication policies
// services can only be affected by peer authentication policies in the same namespace or the root namespace
func clusterAffectedByChangedAuthn(svc *model.Service, changedAuthnNs sets.Set[string], rootNamespace string) bool {
	if changedAuthnNs.IsEmpty() {
		return false
	}

	return changedAuthnNs.Contains(svc.Attributes.Namespace) || changedAuthnNs.Contains(rootNamespace)
}
