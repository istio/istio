// Copyright 2018 Istio Authors
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

package v2

import (
	"fmt"
	"math"
	"strings"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/google/go-cmp/cmp"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/loadbalancer"
	"istio.io/istio/pilot/pkg/networking/util"
)

// EndpointGroups slices endpoints within a shard into small groups. It makes pushing efficient by
// pushing only a small subset of endpoints within the shard. The related xDS resource is called EGDS.
type EndpointGroups struct {
	// The fixed name prefix of each group names within this group set. Generately, group set and service
	// has 1:1 mapping relationship
	NamePrefix string

	// The designed size of each endpoint group. The actual size may be slightly different from this.
	GroupSize uint32

	// The number of groups constructed.
	GroupCount uint32

	// A reference copy of all endpoints within groups. This is used to support old method.
	IstioEndpoints []*model.IstioEndpoint

	// A map stores the endpoint groups. The key is the name of each group.
	IstioEndpointGroups map[string][]*model.IstioEndpoint
}

func (g *EndpointGroups) getEndpoints(groupName string) []*model.IstioEndpoint {
	if g == nil {
		return nil
	}

	// no group name specified, return all group endpoints
	if groupName == "" {
		return g.IstioEndpoints
	}

	_, _, _, groupID := ExtractEndpointGroupKeys(groupName)

	if eps, f := g.IstioEndpointGroups[groupID]; f {
		return eps
	}

	return nil
}

// ExtractEndpointGroupKeys extracts the keys within the group name string
// the key can be the form of "[hostname]-[namespace]-[clusterID]-[groupID]"
func ExtractEndpointGroupKeys(groupName string) (string, string, string, string) {
	if groupName == "" {
		return "", "", "", ""
	}

	keys := strings.Split(groupName, "|")
	if len(keys) != 4 {
		return "", "", "", ""
	}

	return keys[0], keys[1], keys[2], keys[3]
}

func endpointGroupDiscoveryResponse(groups []*xdsapi.EndpointGroup, version string, noncePrefix string) *xdsapi.DiscoveryResponse {
	out := &xdsapi.DiscoveryResponse{
		TypeUrl:     EndpointGroupType,
		VersionInfo: version,
		Nonce:       nonce(noncePrefix),
	}

	for _, group := range groups {
		resource := util.MessageToAny(group)
		out.Resources = append(out.Resources, resource)
	}

	return out
}

func (s *DiscoveryServer) generateEndpoints(clusterName string, groupName string, proxy *model.Proxy, push *model.PushContext) *xdsapi.ClusterLoadAssignment {
	l := s.loadAssignmentsForClusterIsolated(proxy, push, clusterName, groupName)
	if l == nil {
		return nil
	}

	// If networks are set (by default they aren't) apply the Split Horizon
	// EDS filter on the endpoints
	if push.Networks != nil && len(push.Networks.Networks) > 0 {
		endpoints := EndpointsByNetworkFilter(push, proxy.Metadata.Network, l.Endpoints)
		filteredCLA := &xdsapi.ClusterLoadAssignment{
			ClusterName: l.ClusterName,
			Endpoints:   endpoints,
			Policy:      l.Policy,
		}
		l = filteredCLA
	}

	// If locality aware routing is enabled, prioritize endpoints or set their lb weight.
	if push.Mesh.LocalityLbSetting != nil {
		// Make a shallow copy of the cla as we are mutating the endpoints with priorities/weights relative to the calling proxy
		clonedCLA := util.CloneClusterLoadAssignment(l)
		l = &clonedCLA

		// Failover should only be enabled when there is an outlier detection, otherwise Envoy
		// will never detect the hosts are unhealthy and redirect traffic.
		enableFailover, loadBalancerSettings := getOutlierDetectionAndLoadBalancerSettings(push, proxy, l.ClusterName)
		var localityLbSettings = push.Mesh.LocalityLbSetting
		if loadBalancerSettings != nil && loadBalancerSettings.LocalityLbSetting != nil {
			localityLbSettings = loadBalancerSettings.LocalityLbSetting
		}
		loadbalancer.ApplyLocalityLBSetting(proxy.Locality, l, localityLbSettings, enableFailover)
	}

	return l
}

// pushEgds is pushing updated EGDS resources for a single connection. This method will only be called when the EGDS feature was enabled.
func (s *DiscoveryServer) pushEgds(push *model.PushContext, con *XdsConnection, version string,
	edsUpdatedServices map[string]struct{}, egdsUpdatedGroups map[string]struct{}) error {
	pushStart := time.Now()
	groups := make([]*xdsapi.EndpointGroup, len(edsUpdatedServices))

	// All clusters that this endpoint is watching. For 1.0 - it's typically all clusters in the mesh.
	// For 1.1+Sidecar - it's the small set of explicitly imported clusters, using the isolated DestinationRules
	for _, clusterName := range con.Clusters {
		_, _, hostname, _ := model.ParseSubsetKey(clusterName)
		if edsUpdatedServices != nil {
			if _, ok := edsUpdatedServices[string(hostname)]; !ok {
				// Cluster was not updated, skip recomputing. This happens when we get an incremental update for a
				// specific Hostname. On connect or for full push edsUpdatedServices will be empty.
				continue
			}
		}

		push.Mutex.Lock()
		svc := con.node.SidecarScope.ServiceForHostname(hostname, push.ServiceByHostnameAndNamespace)
		push.Mutex.Unlock()
		if svc == nil {
			// Should not happen, EGDS data not exists
			adsLog.Warnf("EGDS: missing data for cluster %s, node id %s", clusterName, con.ConID)
			continue
		}

		for groupName := range s.getServiceGroupNames(clusterName, svc.Attributes.Namespace) {
			if egdsUpdatedGroups != nil {
				if _, f := egdsUpdatedGroups[groupName]; !f {
					// This happens in a incremental EGDS push
					continue
				}
			}

			l := s.generateEndpoints(clusterName, groupName, con.node, push)
			g := &xdsapi.EndpointGroup{
				Name:      groupName,
				Endpoints: l.Endpoints,
			}

			groups = append(groups, g)
		}
	}

	response := endpointGroupDiscoveryResponse(groups, version, push.Version)
	err := con.send(response)
	egdsPushTime.Record(time.Since(pushStart).Seconds())
	if err != nil {
		adsLog.Warnf("EGDS: Send failure %s: %v", con.ConID, err)
		recordSendError(egdsSendErrPushes, err)
		return err
	}
	egdsPushes.Increment()

	return nil
}

func (s *DiscoveryServer) generateEgdsForCluster(clusterName string, proxy *model.Proxy, push *model.PushContext) *xdsapi.ClusterLoadAssignment {
	// This code is similar with the update code.
	_, _, hostname, _ := model.ParseSubsetKey(clusterName)

	push.Mutex.Lock()
	svc := proxy.SidecarScope.ServiceForHostname(hostname, push.ServiceByHostnameAndNamespace)
	push.Mutex.Unlock()
	if svc == nil {
		// Shouldn't happen here - unable to genereate EGDS data
		adsLog.Warnf("EGDS: missing data for cluster %s", clusterName)
		return nil
	}

	// Service resolution type might have changed and Cluster may be still in the EDS cluster list of "XdsConnection.Clusters".
	// This can happen if a ServiceEntry's resolution is changed from STATIC to DNS which changes the Envoy cluster type from
	// EDS to STRICT_DNS. When pushEds is called before Envoy sends the updated cluster list via Endpoint request which in turn
	// will update "XdsConnection.Clusters", we might accidentally send EDS updates for STRICT_DNS cluster. This check guards
	// against such behavior and returns nil. When the updated cluster warms up in Envoy, it would update with new endpoints
	// automatically.
	// Gateways use EDS for Passthrough cluster. So we should allow Passthrough here.
	if svc.Resolution == model.DNSLB {
		adsLog.Infof("XdsConnection has %s in its eds clusters but its resolution now is updated to %v, skipping it.", clusterName, svc.Resolution)
		return nil
	}

	// The service was never updated - do the full update
	s.mutex.RLock()
	se, f := s.EndpointShardsByService[string(hostname)][svc.Attributes.Namespace]
	s.mutex.RUnlock()
	if !f {
		// Shouldn't happen here - unable to genereate EGDS data
		adsLog.Warnf("EGDS: missing data for cluster %s", clusterName)
		return nil
	}

	epGroups := make([]*xdsapi.Egds, 0)
	for _, shard := range se.Shards {
		for name := range shard.IstioEndpointGroups {
			egds := &xdsapi.Egds{
				ConfigSource: &core.ConfigSource{
					ConfigSourceSpecifier: &core.ConfigSource_Ads{
						Ads: &core.AggregatedConfigSource{},
					},
				},
				EndpointGroupName: name,
			}

			epGroups = append(epGroups, egds)
		}
	}

	return &xdsapi.ClusterLoadAssignment{
		ClusterName:    clusterName,
		EndpointGroups: epGroups,
	}
}

func (g *EndpointGroups) accept(newEps []*model.IstioEndpoint) map[string]struct{} {
	prevEps := g.IstioEndpoints
	g.IstioEndpoints = newEps

	// Means the EGDS feature has been disabled globally
	if g.GroupSize <= 0 {
		return nil
	}

	currentMax := int(g.GroupSize * g.GroupCount)
	if len(newEps) > currentMax*2 || len(newEps) < currentMax/2 {
		return g.reshard()
	}

	// Calculate the diff in memory

	added := make([]*model.IstioEndpoint, 0, len(newEps))

MainAddedLoop:
	for _, nep := range newEps {
		for _, pep := range prevEps {
			if cmp.Equal(nep, pep) == true {
				continue MainAddedLoop
			}
		}

		added = append(added, nep)
	}

	removed := make([]*model.IstioEndpoint, 0, len(prevEps))

MainRemovedLoop:
	for _, pep := range prevEps {
		for _, nep := range newEps {
			if cmp.Equal(pep, nep) == true {
				continue MainRemovedLoop
			}
		}

		// The endpoint was not found in new list. Mark it removed.
		removed = append(removed, pep)
	}

	names := g.updateEndpointGroups(added, removed)

	return names
}

// updateEndpointGroups accepts changed groups with mapped endpoints. It does the update
// by replacing existing groups entirely.
func (g *EndpointGroups) updateEndpointGroups(updated []*model.IstioEndpoint, removed []*model.IstioEndpoint) map[string]struct{} {
	updatedGroupKeys := make(map[string]struct{})

	// Merge keys of changed groups.
	for _, ep := range updated {
		updatedGroupKeys[g.makeGroupKey(ep)] = struct{}{}
	}

	for _, ep := range removed {
		updatedGroupKeys[g.makeGroupKey(ep)] = struct{}{}
	}

	// Now reconstruct the group of changed.
	updatedGroups := make(map[string][]*model.IstioEndpoint, g.GroupCount)
	for _, ep := range g.IstioEndpoints {
		key := g.makeGroupKey(ep)

		if _, f := updatedGroupKeys[key]; !f {
			_, f := updatedGroups[key]
			if !f {
				updatedGroups[key] = make([]*model.IstioEndpoint, 0, g.GroupSize)
			}

			updatedGroups[key] = append(updatedGroups[key], ep)
		}
	}

	// Changed EGDS resource names
	names := make(map[string]struct{})

	for key, eps := range updatedGroups {
		g.IstioEndpointGroups[key] = eps
		names[key] = struct{}{}
	}

	return names
}

func (g *EndpointGroups) makeGroupKey(ep *model.IstioEndpoint) string {
	index := ep.HashUint32() % g.GroupCount
	key := fmt.Sprintf("%s|%d", g.NamePrefix, index)

	return key
}

func (g *EndpointGroups) reshard() map[string]struct{} {
	eps := g.IstioEndpoints

	// Reset the group map first
	g.IstioEndpointGroups = make(map[string][]*model.IstioEndpoint)

	// Total number of slices
	g.GroupCount = uint32(math.Ceil(float64(len(eps)) / float64(g.GroupSize)))

	// Generate all groups first. Since groups won't change until next reshard event
	for ix := uint32(0); ix < g.GroupCount; ix++ {
		key := fmt.Sprintf("%s|%d", g.NamePrefix, ix)
		if _, f := g.IstioEndpointGroups[key]; !f {
			g.IstioEndpointGroups[key] = make([]*model.IstioEndpoint, 0, g.GroupSize)
		}
	}

	for _, ep := range eps {
		key := g.makeGroupKey(ep)

		if group, f := g.IstioEndpointGroups[key]; !f {
			// Should never happen
			panic(fmt.Sprintf("unexpect group key: %s", key))
		} else {
			group = append(group, ep)
		}
	}

	return nil
}
