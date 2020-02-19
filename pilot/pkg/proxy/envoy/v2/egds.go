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
	"sync"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"

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

	// The mutex to avoid concurrent modification in endpoint groups, especially the IstioEndpointGroups map
	mutex sync.Mutex

	// Version changes when the group enter reshard state
	VersionInfo int
}

func (g *EndpointGroups) getEndpoints(groupName string) []*model.IstioEndpoint {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	if g == nil {
		return nil
	}

	// no group name specified, return all group endpoints
	if groupName == "" {
		return g.IstioEndpoints
	}

	if eps, f := g.IstioEndpointGroups[groupName]; f {
		return eps
	}

	return nil
}

// ExtractClusterGroupKeys the keys from proxy requested resource names. It can be
// the format "clusterName@groupName"
func ExtractClusterGroupKeys(clusterGroupName string) (string, string) {
	if clusterGroupName == "" {
		return "", ""
	}

	keys := strings.Split(clusterGroupName, "@")
	if len(keys) != 2 {
		return "", ""
	}

	return keys[0], keys[1]
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
	// Failover should only be enabled when there is an outlier detection, otherwise Envoy
	// will never detect the hosts are unhealthy and redirect traffic.
	enableFailover, lb := getOutlierDetectionAndLoadBalancerSettings(push, proxy, clusterName)
	lbSetting := loadbalancer.GetLocalityLbSetting(push.Mesh.GetLocalityLbSetting(), lb.GetLocalityLbSetting())
	if lbSetting != nil {
		// Make a shallow copy of the cla as we are mutating the endpoints with priorities/weights relative to the calling proxy
		clonedCLA := util.CloneClusterLoadAssignment(l)
		l = &clonedCLA
		loadbalancer.ApplyLocalityLBSetting(proxy.Locality, l, lbSetting, enableFailover)
	}
	return l
}

// pushEgds is pushing updated EGDS resources for a single connection. This method will only be called when the EGDS feature was enabled.
func (s *DiscoveryServer) pushEgds(push *model.PushContext, con *XdsConnection, version string,
	updatedClusterGroups map[string]struct{}) error {
	pushStart := time.Now()
	groups := make([]*xdsapi.EndpointGroup, 0, len(updatedClusterGroups))
	epCount := 0

	// A clusterGroup is combination of Cluster and Endpoint Groups. Because Endpoint Group represent static
	// data within a service and the data returned to Envoy should be processed based on different Clusters.
	// The concept of cluster group is used to represent such key of data.
	for _, clusterGroup := range con.ClusterGroups {
		clusterName, groupName := ExtractClusterGroupKeys(clusterGroup)
		if !isNeedUpdate(updatedClusterGroups, clusterName, groupName) {
			// ClusterGroup was not updated, skip recomputing. This happens in an incremental EGDS push.
			continue
		}

		l := s.generateEndpoints(clusterName, groupName, con.node, push)
		if l == nil {
			continue
		}

		g := &xdsapi.EndpointGroup{
			Name:      MakeClusterGroupKey(clusterName, groupName),
			Endpoints: l.Endpoints,
		}

		groups = append(groups, g)

		// Count the total endpoints sent within this EGDS response.
		for _, lep := range l.Endpoints {
			epCount += len(lep.LbEndpoints)
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

	adsLog.Infof("EGDS: PUSH for node:%s, groups:%d, endpoints: %d", con.node.ID, len(groups), epCount)
	return nil
}

func isNeedUpdate(updatedClusterGroups map[string]struct{}, clusterName string, groupName string) bool {
	if updatedClusterGroups == nil {
		return true
	}

	if _, f := updatedClusterGroups[MakeClusterGroupKey(clusterName, groupName)]; f {
		return true
	}

	// If there was group changes from DiscoveryServer.edsUpdate, there's only changed group names present
	// since there is no cluster name available there. This means all cluster related to this group should
	// get updated.
	if _, f := updatedClusterGroups[groupName]; f {
		return true
	}

	return false
}

func (s *DiscoveryServer) generateEgdsForCluster(clusterName string, proxy *model.Proxy, push *model.PushContext) *xdsapi.ClusterLoadAssignment {
	// This code is similar with the update code.
	_, _, hostname, _ := model.ParseSubsetKey(clusterName)

	push.Mutex.Lock()
	svc := proxy.SidecarScope.ServiceForHostname(hostname, push.ServiceByHostnameAndNamespace)
	push.Mutex.Unlock()
	if svc == nil {
		// Can't find group data - unable to genereate EGDS data
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

	// The service was never updated - do the full update.
	s.mutex.RLock()
	se, f := s.EndpointShardsByService[string(hostname)][svc.Attributes.Namespace]
	s.mutex.RUnlock()
	if !f {
		// Shouldn't happen here - unable to genereate EGDS data.
		adsLog.Warnf("EGDS: missing shards for cluster %s, namespace %s", clusterName, svc.Attributes.Namespace)
		return nil
	}

	epGroups := make([]*xdsapi.Egds, 0)
	for _, shard := range se.Shards {
		for groupName := range shard.IstioEndpointGroups {
			egds := &xdsapi.Egds{
				ConfigSource: &core.ConfigSource{
					ConfigSourceSpecifier: &core.ConfigSource_Ads{
						Ads: &core.AggregatedConfigSource{},
					},
				},
				EndpointGroupName: MakeClusterGroupKey(clusterName, groupName),
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
	g.mutex.Lock()
	defer g.mutex.Unlock()

	prevEps := g.IstioEndpoints
	g.IstioEndpoints = newEps

	// Means the EGDS feature has been disabled globally.
	if g.GroupSize <= 0 {
		return nil
	}

	currentMax := int(g.GroupSize * g.GroupCount)
	// If "g.GroupCount == 1", this means this is not startup and of course the group can't reduce any more.
	if (len(newEps) > currentMax*2 || len(newEps) < currentMax/2 || len(prevEps) <= 0) && (g.GroupCount != 1) {
		adsLog.Debugf("Reshard triggered, curMax: %d, newEndpoints count: %d, current group count: %d",
			currentMax, len(newEps), g.GroupCount)
		return g.reshard()
	}

	names := make(map[string]struct{})
	updatedCount := 0
	emtpyCount := 0

	newGroups := make(map[string][]*model.IstioEndpoint)

MainLoop:
	for _, ep := range newEps {
		key := g.makeGroupKey(ep)
		if _, f := newGroups[key]; !f {
			newGroups[key] = make([]*model.IstioEndpoint, 0)
		}

		newGroups[key] = append(newGroups[key], ep)

		if eps, f := g.IstioEndpointGroups[key]; !f {
			// Should not happen
			panic(fmt.Sprintf("encounter unknown group id before reshard event, id: %s", key))
		} else {
			for _, pep := range eps {
				if ep.Equals(pep) {
					continue MainLoop
				}
			}

			names[key] = struct{}{}
		}
	}

	updatedCount = len(names)
	newGroupCount := len(newGroups)

	for key := range g.IstioEndpointGroups {
		if _, f := newGroups[key]; !f {
			newGroups[key] = make([]*model.IstioEndpoint, 0)

			if len(g.IstioEndpointGroups[key]) > 0 {
				// Old group name did present on new groups. This means groups contains no endpoints any more.
				// But the previous group had. Mark it changed and we still need to construct an empty group.
				names[key] = struct{}{}
			}
		}
	}

	emtpyCount = len(newGroups) - newGroupCount

	adsLog.Debugf("EGDS updatedCount: %d, emptyCount: %d, newGroups: %d", updatedCount, emtpyCount, newGroupCount)

	g.IstioEndpointGroups = newGroups

	return names
}

func (g *EndpointGroups) makeGroupKey(ep *model.IstioEndpoint) string {
	index := ep.HashUint32(g.GroupSize) % g.GroupCount
	key := fmt.Sprintf("%s|%d-%d", g.NamePrefix, index, g.VersionInfo)

	return key
}

// MakeClusterGroupKey generates the clusterGroup key returned to proxy.
func MakeClusterGroupKey(clusterName string, groupName string) string {
	return fmt.Sprintf("%s@%s", clusterName, groupName)
}

func (g *EndpointGroups) reshard() map[string]struct{} {
	eps := g.IstioEndpoints

	// Reset the group map first.
	g.IstioEndpointGroups = make(map[string][]*model.IstioEndpoint)

	// Increase the version
	g.VersionInfo++

	// Total number of group slices.
	g.GroupCount = uint32(math.Ceil(float64(len(eps)) / float64(g.GroupSize)))

	// Generate all groups first. Since groups won't change until next reshard event.
	for ix := uint32(0); ix < g.GroupCount; ix++ {
		key := fmt.Sprintf("%s|%d-%d", g.NamePrefix, ix, g.VersionInfo)
		g.IstioEndpointGroups[key] = make([]*model.IstioEndpoint, 0, g.GroupSize)
	}

	for _, ep := range eps {
		key := g.makeGroupKey(ep)

		if eps, f := g.IstioEndpointGroups[key]; !f {
			// Should never happen.
			panic(fmt.Sprintf("unexpect group key: %s", key))
		} else {
			g.IstioEndpointGroups[key] = append(eps, ep)
		}
	}

	// Reshard event must return nil changed groups to indicate a EDS re-push event
	// in order to update the group names in Proxy memory
	return nil
}
