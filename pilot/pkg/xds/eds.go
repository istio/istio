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

	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	anypb "google.golang.org/protobuf/types/known/anypb"

	networkingapi "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	networking "istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/loadbalancer"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/util/protoconv"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
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
	pushType := s.Env.EndpointIndex.UpdateServiceEndpoints(shard, serviceName, namespace, istioEndpoints, s.Cache.Clear)
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
	s.Env.EndpointIndex.UpdateServiceEndpoints(shard, serviceName, namespace, istioEndpoints, s.Cache.Clear)
}

func (s *DiscoveryServer) RemoveShard(shardKey model.ShardKey) {
	s.Env.EndpointIndex.DeleteShard(shardKey)
}

// localityEndpointsForCluster returns the endpoints for a cluster
func (s *DiscoveryServer) localityEndpointsForCluster(b EndpointBuilder) ([]*LocalityEndpoints, error) {
	if b.service == nil {
		// Shouldn't happen here
		log.Debugf("can not find the service for cluster %s", b.clusterName)
		return nil, nil
	}

	// Service resolution type might have changed and Cluster may be still in the EDS cluster list of "Connection.Clusters".
	// This can happen if a ServiceEntry's resolution is changed from STATIC to DNS which changes the Envoy cluster type from
	// EDS to STRICT_DNS or LOGICAL_DNS. When pushEds is called before Envoy sends the updated cluster list via Endpoint request which in turn
	// will update "Connection.Clusters", we might accidentally send EDS updates for STRICT_DNS cluster. This check guards
	// against such behavior and returns nil. When the updated cluster warms up in Envoy, it would update with new endpoints
	// automatically.
	// Gateways use EDS for Passthrough cluster. So we should allow Passthrough here.
	if b.service.Resolution == model.DNSLB || b.service.Resolution == model.DNSRoundRobinLB {
		log.Infof("cluster %s in eds cluster, but its resolution now is updated to %v, skipping it.", b.clusterName, b.service.Resolution)
		return nil, fmt.Errorf("cluster %s in eds cluster", b.clusterName)
	}

	svcPort, f := b.service.Ports.GetByPort(b.port)
	if !f {
		// Shouldn't happen here
		log.Debugf("can not find the service port %d for cluster %s", b.port, b.clusterName)
		return nil, nil
	}

	epShards, f := s.Env.EndpointIndex.ShardsForService(string(b.hostname), b.service.Attributes.Namespace)
	if !f {
		// Shouldn't happen here
		log.Debugf("can not find the endpointShards for cluster %s", b.clusterName)
		return nil, nil
	}

	return b.buildLocalityLbEndpointsFromShards(epShards, svcPort), nil
}

func (s *DiscoveryServer) generateEndpoints(b EndpointBuilder) *endpoint.ClusterLoadAssignment {
	localityLbEndpoints, err := s.localityEndpointsForCluster(b)
	if err != nil {
		return buildEmptyClusterLoadAssignment(b.clusterName)
	}

	// Apply the Split Horizon EDS filter, if applicable.
	localityLbEndpoints = b.EndpointsByNetworkFilter(localityLbEndpoints)

	if model.IsDNSSrvSubsetKey(b.clusterName) {
		// For the SNI-DNAT clusters, we are using AUTO_PASSTHROUGH gateway. AUTO_PASSTHROUGH is intended
		// to passthrough mTLS requests. However, at the gateway we do not actually have any way to tell if the
		// request is a valid mTLS request or not, since its passthrough TLS.
		// To ensure we allow traffic only to mTLS endpoints, we filter out non-mTLS endpoints for these cluster types.
		localityLbEndpoints = b.EndpointsWithMTLSFilter(localityLbEndpoints)
	}
	l := b.createClusterLoadAssignment(localityLbEndpoints)

	// If locality aware routing is enabled, prioritize endpoints or set their lb weight.
	// Failover should only be enabled when there is an outlier detection, otherwise Envoy
	// will never detect the hosts are unhealthy and redirect traffic.
	enableFailover, lb := getOutlierDetectionAndLoadBalancerSettings(b.DestinationRule(), b.port, b.subsetName)
	lbSetting := loadbalancer.GetLocalityLbSetting(b.push.Mesh.GetLocalityLbSetting(), lb.GetLocalityLbSetting())
	if lbSetting != nil {
		// Make a shallow copy of the cla as we are mutating the endpoints with priorities/weights relative to the calling proxy
		l = util.CloneClusterLoadAssignment(l)
		wrappedLocalityLbEndpoints := make([]*loadbalancer.WrappedLocalityLbEndpoints, len(localityLbEndpoints))
		for i := range localityLbEndpoints {
			wrappedLocalityLbEndpoints[i] = &loadbalancer.WrappedLocalityLbEndpoints{
				IstioEndpoints:      localityLbEndpoints[i].istioEndpoints,
				LocalityLbEndpoints: l.Endpoints[i],
			}
		}
		loadbalancer.ApplyLocalityLBSetting(l, wrappedLocalityLbEndpoints, b.locality, b.proxy.Labels, lbSetting, enableFailover)
	}
	return l
}

// EdsGenerator implements the new Generate method for EDS, using the in-memory, optimized endpoint
// storage in DiscoveryServer.
type EdsGenerator struct {
	Server *DiscoveryServer
}

var _ model.XdsDeltaResourceGenerator = &EdsGenerator{}

// Map of all configs that do not impact EDS
var skippedEdsConfigs = map[kind.Kind]struct{}{
	kind.Gateway:               {},
	kind.VirtualService:        {},
	kind.WorkloadGroup:         {},
	kind.AuthorizationPolicy:   {},
	kind.RequestAuthentication: {},
	kind.Secret:                {},
	kind.Telemetry:             {},
	kind.WasmPlugin:            {},
	kind.ProxyConfig:           {},
}

func edsNeedsPush(updates model.XdsUpdates) bool {
	// If none set, we will always push
	if len(updates) == 0 {
		return true
	}
	for config := range updates {
		if _, f := skippedEdsConfigs[config.Kind]; !f {
			return true
		}
	}
	return false
}

func (eds *EdsGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	if !edsNeedsPush(req.ConfigsUpdated) {
		return nil, model.DefaultXdsLogDetails, nil
	}
	resources, logDetails := eds.buildEndpoints(proxy, req, w)
	return resources, logDetails, nil
}

func getOutlierDetectionAndLoadBalancerSettings(
	destinationRule *networkingapi.DestinationRule,
	portNumber int,
	subsetName string,
) (bool, *networkingapi.LoadBalancerSettings) {
	if destinationRule == nil {
		return false, nil
	}
	outlierDetectionEnabled := false
	var lbSettings *networkingapi.LoadBalancerSettings

	port := &model.Port{Port: portNumber}
	policy := networking.MergeTrafficPolicy(nil, destinationRule.TrafficPolicy, port)

	for _, subset := range destinationRule.Subsets {
		if subset.Name == subsetName {
			policy = networking.MergeTrafficPolicy(policy, subset.TrafficPolicy, port)
			break
		}
	}

	if policy != nil {
		lbSettings = policy.LoadBalancer
		if policy.OutlierDetection != nil {
			outlierDetectionEnabled = true
		}
	}

	return outlierDetectionEnabled, lbSettings
}

func endpointDiscoveryResponse(loadAssignments []*anypb.Any, version, noncePrefix string) *discovery.DiscoveryResponse {
	out := &discovery.DiscoveryResponse{
		TypeUrl: v3.EndpointType,
		// Pilot does not really care for versioning. It always supplies what's currently
		// available to it, irrespective of whether Envoy chooses to accept or reject EDS
		// responses. Pilot believes in eventual consistency and that at some point, Envoy
		// will begin seeing results it deems to be good.
		VersionInfo: version,
		Nonce:       nonce(noncePrefix),
		Resources:   loadAssignments,
	}

	return out
}

// cluster with no endpoints
func buildEmptyClusterLoadAssignment(clusterName string) *endpoint.ClusterLoadAssignment {
	return &endpoint.ClusterLoadAssignment{
		ClusterName: clusterName,
	}
}

func (eds *EdsGenerator) GenerateDeltas(proxy *model.Proxy, req *model.PushRequest,
	w *model.WatchedResource,
) (model.Resources, model.DeletedResources, model.XdsLogDetails, bool, error) {
	if !edsNeedsPush(req.ConfigsUpdated) {
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
	if len(req.ConfigsUpdated) == 0 {
		return false
	}
	for cfg := range req.ConfigsUpdated {
		if _, f := skippedEdsConfigs[cfg.Kind]; f {
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
	for _, clusterName := range w.ResourceNames {
		if edsUpdatedServices != nil {
			_, _, hostname, _ := model.ParseSubsetKey(clusterName)
			if _, ok := edsUpdatedServices[string(hostname)]; !ok {
				// Cluster was not updated, skip recomputing. This happens when we get an incremental update for a
				// specific Hostname. On connect or for full push edsUpdatedServices will be empty.
				continue
			}
		}
		builder := NewEndpointBuilder(clusterName, proxy, req.Push)

		// We skip cache if assertions are enabled, so that the cache will assert our eviction logic is correct
		if !features.EnableUnsafeAssertions {
			cachedEndpoint := eds.Server.Cache.Get(&builder)
			if cachedEndpoint != nil {
				resources = append(resources, cachedEndpoint)
				cached++
				continue
			}
		}

		// generate eds from beginning
		{
			l := eds.Server.generateEndpoints(builder)
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
			eds.Server.Cache.Add(&builder, req, resource)
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

	for _, clusterName := range w.ResourceNames {
		// filter out eds that are not updated for clusters
		_, _, hostname, _ := model.ParseSubsetKey(clusterName)
		if _, ok := edsUpdatedServices[string(hostname)]; !ok {
			continue
		}

		builder := NewEndpointBuilder(clusterName, proxy, req.Push)
		// if a service is not found, it means the cluster is removed
		if builder.service == nil {
			removed = append(removed, clusterName)
			continue
		}

		// We skip cache if assertions are enabled, so that the cache will assert our eviction logic is correct
		if !features.EnableUnsafeAssertions {
			cachedEndpoint := eds.Server.Cache.Get(&builder)
			if cachedEndpoint != nil {
				resources = append(resources, cachedEndpoint)
				cached++
				continue
			}
		}
		// generate new eds cache
		{
			l := eds.Server.generateEndpoints(builder)
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
			eds.Server.Cache.Add(&builder, req, resource)
		}
	}
	return resources, removed, model.XdsLogDetails{
		Incremental:    len(edsUpdatedServices) != 0,
		AdditionalInfo: fmt.Sprintf("empty:%v cached:%v/%v", empty, cached, cached+regenerated),
	}
}
