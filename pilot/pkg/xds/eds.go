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

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
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
	if pushType != model.NoPush {
		configKind := kind.Endpoints
		if pushType == model.FullPush {
			configKind = kind.ServiceEntry
		}
		// Trigger a push
		s.ConfigUpdate(&model.PushRequest{
			ConfigsUpdated: sets.New(model.ConfigKey{Kind: configKind, Name: serviceName, Namespace: namespace}),
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

func (s *DiscoveryServer) PruneShard(shardKey model.ShardKey, keep map[string]sets.String) {
	s.Env.EndpointIndex.PruneShard(shardKey, keep)
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
	kind.TrafficExtension,
	kind.ProxyConfig,
	kind.DNSName,
	kind.Sidecar,
	// we can skip Address here to avoid pushing Sidecars and Gateways on Address changes,
	// it's already checked in waypointNeedsPush
	kind.Address,
)

var deltaAwareEdsConfigs = sets.New(
	kind.Endpoints,
	kind.ServiceEntry,
	kind.DestinationRule,
	kind.PeerAuthentication,
)

func edsNeedsPush(req *model.PushRequest, proxy *model.Proxy) bool {
	if res, ok := xdsNeedsPush(req, proxy); ok {
		return res
	}
	// CDS needs to be pushed for waypoint proxies on kind.Address changes, so we need to push EDS as well.
	if proxy.Type == model.Waypoint && waypointNeedsPush(req, proxy) {
		return true
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

	resources, logDetails := eds.buildEndpoints(proxy, req, w, canSendPartialFullPushes(req))
	return resources, logDetails, nil
}

func (eds *EdsGenerator) GenerateDeltas(proxy *model.Proxy, req *model.PushRequest,
	w *model.WatchedResource,
) (model.Resources, model.DeletedResources, model.XdsLogDetails, bool, error) {
	if !edsNeedsPush(req, proxy) {
		return nil, nil, model.DefaultXdsLogDetails, false, nil
	}

	partialPush := canSendPartialFullPushes(req)
	resources, logs := eds.buildEndpoints(proxy, req, w, partialPush)
	return resources, nil, logs, partialPush, nil
}

func canSendPartialFullPushes(req *model.PushRequest) bool {
	if req.Forced {
		return false
	}

	for cfg := range req.ConfigsUpdated {
		// Skipped kinds do not affect EDS and may be coalesced with an update that does.
		// TODO(ilrudie): Address can affect waypoint endpoint tunnel metadata. We cannot determine
		// which services are affected, so Address updates must conservatively trigger full EDS.
		if skippedEdsConfigs.Contains(cfg.Kind) && cfg.Kind != kind.Address {
			continue
		}
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
	partialPush bool,
) (model.Resources, model.XdsLogDetails) {
	var edsUpdatedServices sets.Set[string]
	var explicitlyUpdatedServices sets.Set[string]
	var changedAuthnNs sets.Set[string]

	if partialPush {
		edsUpdatedServices = sets.New[string]()
		explicitlyUpdatedServices = sets.New[string]()
		changedAuthnNs = sets.New[string]()
		for cfg := range req.ConfigsUpdated {
			switch cfg.Kind {
			case kind.DestinationRule:
				for _, svc := range servicesAffectedByDestinationRule(proxy, req.Push, cfg) {
					edsUpdatedServices.Insert(svc.Hostname.String())
				}
			case kind.ServiceEntry, kind.Endpoints:
				edsUpdatedServices.Insert(cfg.Name)
				explicitlyUpdatedServices.Insert(cfg.Name)
			case kind.PeerAuthentication:
				changedAuthnNs.Insert(cfg.Namespace)
			}
		}
	}
	var resources model.Resources
	empty := 0
	cached := 0
	regenerated := 0

	for clusterName := range w.ResourceNames {
		affected := affectedService(proxy, edsUpdatedServices, clusterName)
		isSelfDiscoveryCluster := clusterName == util.SelfDiscoveryCluster
		if isSelfDiscoveryCluster {
			// DestinationRules do not affect the self-discovery local cluster.
			affected = affectedService(proxy, explicitlyUpdatedServices, clusterName)
		}
		if partialPush && changedAuthnNs.IsEmpty() && !affected {

			// No relevant service or peer authentication changes affect this cluster, so skip recomputing it.
			continue
		}

		dir, subsetName, hostname, port := parseClusterName(clusterName, proxy)
		svc := req.Push.ServiceForHostname(proxy, hostname)

		if svc == nil && isSelfDiscoveryCluster {
			// The self-discovery local_cluster represents the proxy's own service, which may be outside
			// the proxy's egress scope. Fall back to the global service index, scoped to the local
			// service's namespace.
			svc = req.Push.ServiceIndex.HostnameAndNamespace[hostname][proxy.LocalService.Namespace]
		}

		var dr *model.ConsolidatedDestRule
		if svc != nil && !isSelfDiscoveryCluster {
			// disable DR lookup for self discovery cluster, we don't need to apply subsetting or traffic policies.
			dr = proxy.SidecarScope.DestinationRule(model.TrafficDirectionOutbound, proxy, svc.Hostname)
		}

		// If no service or destination rule update affects this cluster, check peer authentication before recomputing.
		if partialPush && svc != nil && !affected {
			// The local cluster is unaffected by these policy changes.
			if isSelfDiscoveryCluster {
				continue
			}

			if !clusterAffectedByChangedAuthn(svc, changedAuthnNs, req.Push.Mesh.RootNamespace) {
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
	return resources, model.XdsLogDetails{
		Incremental:    len(edsUpdatedServices) != 0,
		AdditionalInfo: fmt.Sprintf("empty:%v cached:%v/%v", empty, cached, cached+regenerated),
	}
}

func parseClusterName(clusterName string, proxy *model.Proxy) (model.TrafficDirection, string, host.Name, int) {
	if clusterName == util.SelfDiscoveryCluster {
		if proxy.LocalService.Name == "" {
			return model.TrafficDirectionOutbound, "", "", 0
		}

		return model.TrafficDirectionOutbound, "", host.Name(proxy.LocalService.Name), proxy.LocalService.Port
	}

	return model.ParseSubsetKey(clusterName)
}

func affectedService(proxy *model.Proxy, edsUpdatedServices sets.Set[string], clusterName string) bool {
	if clusterName == util.SelfDiscoveryCluster {
		// Detect a local-service transition (add, delete, or replace). Both fields are
		// empty for proxies that never had a local service, so steady-state pushes for
		// those proxies fall through to "no service, nothing to push".
		if proxy.LocalService != proxy.PrevLocalService {
			return true
		}
		if proxy.LocalService.Name == "" {
			return false
		}
		return edsUpdatedServices.Contains(proxy.LocalService.Name)
	}
	return edsUpdatedServices.Contains(model.ParseSubsetKeyHostname(clusterName))
}

// servicesAffectedByDestinationRule returns services matching the current or previous host of a changed rule.
func servicesAffectedByDestinationRule(
	proxy *model.Proxy,
	push *model.PushContext,
	updatedDr model.ConfigKey,
) []*model.Service {
	if proxy.SidecarScope == nil {
		return nil
	}

	var services []*model.Service
	cfg := proxy.SidecarScope.DestinationRuleByName(updatedDr.Name, updatedDr.Namespace)
	if cfg == nil {
		// The rule was deleted. Resolve its old host against the current service scope.
		prevCfg := proxy.PrevSidecarScope.DestinationRuleByName(updatedDr.Name, updatedDr.Namespace)
		if prevCfg == nil {
			return nil
		}
		prevDr := prevCfg.Spec.(*networking.DestinationRule)
		services = append(services, proxy.SidecarScope.ServicesForHostname(host.Name(prevDr.Host))...)
	} else {
		dr := cfg.Spec.(*networking.DestinationRule)
		services = append(services, proxy.SidecarScope.ServicesForHostname(host.Name(dr.Host))...)
		// If the host changed, include services matched by the rule before the update.
		prevCfg := proxy.PrevSidecarScope.DestinationRuleByName(updatedDr.Name, updatedDr.Namespace)
		if prevCfg != nil {
			prevDr := prevCfg.Spec.(*networking.DestinationRule)
			if dr.Host != prevDr.Host {
				services = append(services, proxy.SidecarScope.ServicesForHostname(host.Name(prevDr.Host))...)
			}
		}
	}

	if features.FilterGatewayClusterConfig && proxy.Type == model.Router {
		services = slices.FilterInPlace(services, func(s *model.Service) bool {
			return push.ServiceAttachedToGateway(string(s.Hostname), s.Attributes.Namespace, proxy)
		})
	}

	return services
}

// clusterAffectedByChangedAuthn checks if the service is affected by the changed peer authentication policies
// services can only be affected by peer authentication policies in the same namespace or the root namespace
func clusterAffectedByChangedAuthn(svc *model.Service, changedAuthnNs sets.Set[string], rootNamespace string) bool {
	if changedAuthnNs.IsEmpty() {
		return false
	}

	return changedAuthnNs.Contains(svc.Attributes.Namespace) || changedAuthnNs.Contains(rootNamespace)
}
