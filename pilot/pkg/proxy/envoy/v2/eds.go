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
	"context"
	"errors"
	"reflect"

	"strconv"
	"strings"
	"sync"
	"time"

	"os"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	"github.com/gogo/protobuf/types"
	"github.com/prometheus/client_golang/prometheus"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
)

// EDS returns the list of endpoints (IP:port and in future labels) associated with a real
// service or a subset of a service, selected using labels.
//
// The source of info is a list of service registries.
//
// Primary event is an endpoint creation/deletion. Once the event is fired, EDS needs to
// find the list of services associated with the endpoint.
//
// In case of k8s, Endpoints event is fired when the endpoints are added to service - typically
// after readiness check. At that point we have the 'real' Service. The Endpoint includes a list
// of port numbers and names.
//
// For the subset case, the Pod referenced in the Endpoint must be looked up, and pod checked
// for labels.
//
// In addition, ExternalEndpoint includes IPs and labels directly and can be directly processed.
//
// TODO: for selector-less services (mesh expansion), skip pod processing
// TODO: optimize the code path for ExternalEndpoint, no additional processing needed
// TODO: if a service doesn't have split traffic - we can also skip pod and lable processing
// TODO: efficient label processing. In alpha3, the destination policies are set per service, so
// we may only need to search in a small list.

var (
	edsClusterMutex sync.Mutex
	edsClusters     = map[string]*EdsCluster{}

	// Tracks connections, increment on each new connection.
	connectionNumber = int64(0)

	// edsPartial will push only what changed - Envoy will not delete
	// or modify clusters if an EDS push doesn't contain any data about said cluster.
	// This speeds up the push and reduces memory/CPU use on pilot, and allows scaling
	// to larger number of endpoints.
	// On by default - can be turned off in case of unexpected problems.
	edsPartial = os.Getenv("EDS_PARTIAL") != "0"
)

// EdsCluster tracks eds-related info for monitored clusters. In practice it'll include
// all clusters until we support on-demand cluster loading.
type EdsCluster struct {
	// mutex protects changes to this cluster
	mutex sync.Mutex

	LoadAssignment *xdsapi.ClusterLoadAssignment

	// FirstUse is the time the cluster was first used, for debugging
	FirstUse time.Time

	// EdsClients keeps track of all nodes monitoring the cluster.
	EdsClients map[string]*XdsConnection `json:"-"`

	// NonEmptyTime is the time the cluster first had a non-empty set of endpoints
	NonEmptyTime time.Time

	// The discovery service this cluster is associated with.
	discovery *DiscoveryServer
}

// TODO: add prom metrics !

// Endpoints aggregate a DiscoveryResponse for pushing.
func (s *DiscoveryServer) endpoints(clusterNames []string, outRes []types.Any) *xdsapi.DiscoveryResponse {
	out := &xdsapi.DiscoveryResponse{
		// All resources for EDS ought to be of the type ClusterLoadAssignment
		TypeUrl: EndpointType,

		// Pilot does not really care for versioning. It always supplies what's currently
		// available to it, irrespective of whether Envoy chooses to accept or reject EDS
		// responses. Pilot believes in eventual consistency and that at some point, Envoy
		// will begin seeing results it deems to be good.
		VersionInfo: versionInfo(),
		Nonce:       nonce(),
		Resources:   outRes,
	}

	return out
}

// Return the load assignment. The field can be updated by another routine.
func loadAssignment(c *EdsCluster) *xdsapi.ClusterLoadAssignment {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.LoadAssignment
}

func serviceEntry2Endpoint(UID string, family model.AddressFamily, address string, port uint32) *endpoint.LbEndpoint {
	var addr core.Address
	switch family {
	case model.AddressFamilyTCP:
		addr = core.Address{
			Address: &core.Address_SocketAddress{
				SocketAddress: &core.SocketAddress{
					Address: address,
					PortSpecifier: &core.SocketAddress_PortValue{
						PortValue: port,
					},
				},
			},
		}
	case model.AddressFamilyUnix:
		addr = core.Address{Address: &core.Address_Pipe{Pipe: &core.Pipe{Path: address}}}
	}

	ep := &endpoint.LbEndpoint{
		Endpoint: &endpoint.Endpoint{
			Address: &addr,
		},
	}

	// Istio telemetry depends on the metadata value being set for endpoints in the mesh.
	// Do not remove: mixerfilter depends on this logic.
	if UID != "" {
		ep.Metadata = &core.Metadata{
			FilterMetadata: map[string]*types.Struct{
				"istio": {
					Fields: map[string]*types.Value{
						"uid": {Kind: &types.Value_StringValue{StringValue: UID}},
					},
				},
			},
		}
	}

	//log.Infoa("EDS: endpoint ", ipAddr, ep.String())
	return ep
}

func newEndpoint(e *model.NetworkEndpoint) (*endpoint.LbEndpoint, error) {
	err := model.ValidateNetworkEndpointAddress(e)
	if err != nil {
		return nil, err
	}
	addr := util.GetNetworkEndpointAddress(e)
	ep := &endpoint.LbEndpoint{
		Endpoint: &endpoint.Endpoint{
			Address: &addr,
		},
	}

	// Istio telemetry depends on the metadata value being set for endpoints in the mesh.
	// Do not remove: mixerfilter depends on this logic.
	if e.UID != "" {
		ep.Metadata = &core.Metadata{
			FilterMetadata: map[string]*types.Struct{
				"istio": {
					Fields: map[string]*types.Value{
						"uid": {Kind: &types.Value_StringValue{StringValue: e.UID}},
					},
				},
			},
		}
	}

	//log.Infoa("EDS: endpoint ", ipAddr, ep.String())
	return ep, nil
}

// updateClusterInc computes an envoy cluster assignment from the service shards.
func (s *DiscoveryServer) updateClusterInc(push *model.PushContext, clusterName string,
	edsCluster *EdsCluster) error {

	var hostname model.Hostname

	var port int
	var subsetName string
	_, subsetName, hostname, port = model.ParseSubsetKey(clusterName)
	labels := push.SubsetToLabels(subsetName, hostname)

	portMap, f := push.ServicePort2Name[string(hostname)]
	if !f {
		return s.updateCluster(push, clusterName, edsCluster)
	}
	portName, f := portMap[uint32(port)]
	if !f {
		return s.updateCluster(push, clusterName, edsCluster)
	}

	// The service was never updated - do the full update
	se, f := s.EndpointShardsByService[string(hostname)]
	if !f {
		return s.updateCluster(push, clusterName, edsCluster)
	}

	cnt := 0
	localityEpMap := make(map[string]*endpoint.LocalityLbEndpoints)

	// The shards are updated independently, now need to filter and merge
	// for this cluster
	for _, es := range se.Shards {
		for _, el := range es.Entries {
			if portName != el.ServicePortName {
				continue
			}
			// Port labels
			if !labels.HasSubsetOf(model.Labels(el.Labels)) {
				continue
			}
			cnt++

			// TODO: Need to accommodate region, zone and subzone. Older Pilot datamodel only has zone = availability zone.
			// Once we do that, the key must be a | separated tupple.
			locality := (el.Labels)[model.AZLabel] // may be ""
			locLbEps, found := localityEpMap[locality]
			if !found {
				locLbEps = &endpoint.LocalityLbEndpoints{
					Locality: &core.Locality{
						Zone: locality,
					},
				}
				localityEpMap[locality] = locLbEps
			}
			if el.EnvoyEndpoint == nil {
				el.EnvoyEndpoint = serviceEntry2Endpoint(el.UID, el.Family, el.Address, el.EndpointPort)
			}
			locLbEps.LbEndpoints = append(locLbEps.LbEndpoints, *el.EnvoyEndpoint)
		}
	}
	locEps := make([]endpoint.LocalityLbEndpoints, 0, len(localityEpMap))
	for _, locLbEps := range localityEpMap {
		locEps = append(locEps, *locLbEps)
	}

	if cnt == 0 {
		push.Add(model.ProxyStatusClusterNoInstances, clusterName, nil, "")
		//adsLog.Infof("EDS: no instances %s (host=%s ports=%v labels=%v)", clusterName, hostname, p, labels)
	}
	edsInstances.With(prometheus.Labels{"cluster": clusterName}).Set(float64(cnt))

	// There is a chance multiple goroutines will update the cluster at the same time.
	// This could be prevented by a lock - but because the update may be slow, it may be
	// better to accept the extra computations.
	// We still lock the access to the LoadAssignments.
	edsCluster.mutex.Lock()
	defer edsCluster.mutex.Unlock()

	edsCluster.LoadAssignment = &xdsapi.ClusterLoadAssignment{
		ClusterName: clusterName,
		Endpoints:   locEps,
	}
	if len(locEps) > 0 && edsCluster.NonEmptyTime.IsZero() {
		edsCluster.NonEmptyTime = time.Now()
	}
	return nil
}

// updateServiceShards will list the endpoints and create the shards.
// This is used to reconcile and to support non-k8s registries (until they migrate).
// Note that aggreaged list is expensive (for large numbers) - we want to replace
// it with a model where DiscoveryServer keeps track of all endpoint registries
// directly, and calls them one by one.
func (s *DiscoveryServer) updateServiceShards(push *model.PushContext) error {

	// TODO: if ServiceDiscovery is aggregate, and all members support direct, use
	// the direct interface.
	var regs []aggregate.Registry
	if agg, ok := s.Env.ServiceDiscovery.(*aggregate.Controller); ok {
		regs = agg.GetRegistries()
	} else {
		regs = []aggregate.Registry{
			aggregate.Registry{
				ServiceDiscovery: s.Env.ServiceDiscovery,
			},
		}
	}

	svc2acc := map[string]map[string]bool{}

	for _, reg := range regs {
		// Each registry acts as a shard - we don't want to combine them because some
		// may individually update their endpoints incrementally
		for _, svc := range push.Services {
			entries := []*model.IstioEndpoint{}
			hn := string(svc.Hostname)
			for _, port := range svc.Ports {
				if port.Protocol == model.ProtocolUDP {
					continue
				}

				// This loses track of grouping (shards)
				epi, err := reg.InstancesByPort(svc.Hostname, port.Port, model.LabelsCollection{})
				if err != nil {
					return err
				}

				for _, ep := range epi {
					//shard := ep.AvailabilityZone
					l := map[string]string(ep.Labels)

					entries = append(entries, &model.IstioEndpoint{
						Family:          ep.Endpoint.Family,
						Address:         ep.Endpoint.Address,
						EndpointPort:    uint32(ep.Endpoint.Port),
						ServicePortName: port.Name,
						Labels:          l,
						UID:             ep.Endpoint.UID,
						ServiceAccount:  ep.ServiceAccount,
					})
					if ep.ServiceAccount != "" {
						acc, f := svc2acc[hn]
						if !f {
							acc = map[string]bool{}
							svc2acc[hn] = acc
						}
						acc[ep.ServiceAccount] = true
					}
				}
			}

			s.edsUpdate(reg.ClusterID, hn, entries, true)
		}
	}

	s.mutex.Lock()
	for k, v := range svc2acc {
		ep, _ := s.EndpointShardsByService[k]
		ep.ServiceAccounts = v
	}
	s.mutex.Unlock()

	return nil
}

// updateCluster is called from the event (or global cache invalidation) to update
// the endpoints for the cluster.
func (s *DiscoveryServer) updateCluster(push *model.PushContext, clusterName string,
	edsCluster *EdsCluster) error {
	// TODO: should we lock this as well ? Once we move to event-based it may not matter.
	var hostname model.Hostname
	//var ports model.PortList
	var labels model.LabelsCollection
	var instances []*model.ServiceInstance
	var err error
	if strings.Index(clusterName, "outbound") == 0 ||
		strings.Index(clusterName, "inbound") == 0 { //new style cluster names
		var p int
		var subsetName string
		_, subsetName, hostname, p = model.ParseSubsetKey(clusterName)

		labels = push.SubsetToLabels(subsetName, hostname)

		// TODO: k8s adapter should use EdsUpdate. This would return non-k8s stuff, needs to
		// be merged with k8s. This returns ServiceEntries.
		instances, err = edsCluster.discovery.Env.ServiceDiscovery.InstancesByPort(hostname, p, labels)
		if len(instances) == 0 {
			push.Add(model.ProxyStatusClusterNoInstances, clusterName, nil, "")
			//adsLog.Infof("EDS: no instances %s (host=%s ports=%v labels=%v)", clusterName, hostname, p, labels)
		}
		edsInstances.With(prometheus.Labels{"cluster": clusterName}).Set(float64(len(instances)))
	}
	if err != nil {
		adsLog.Warnf("endpoints for service cluster %q returned error %q", clusterName, err)
		return err
	}
	locEps := localityLbEndpointsFromInstances(instances)

	// There is a chance multiple goroutines will update the cluster at the same time.
	// This could be prevented by a lock - but because the update may be slow, it may be
	// better to accept the extra computations.
	// We still lock the access to the LoadAssignments.
	edsCluster.mutex.Lock()
	defer edsCluster.mutex.Unlock()
	edsCluster.LoadAssignment = &xdsapi.ClusterLoadAssignment{
		ClusterName: clusterName,
		Endpoints:   locEps,
	}
	if len(locEps) > 0 && edsCluster.NonEmptyTime.IsZero() {
		edsCluster.NonEmptyTime = time.Now()
	}
	return nil
}

// SvcUpdate is a callback from service discovery to update the port mapping.
func (s *DiscoveryServer) SvcUpdate(cluster, hostname string, ports map[string]uint32, rports map[uint32]string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if cluster == "" {
		s.Env.PushContext.ServiceName2Ports[hostname] = ports
		s.Env.PushContext.ServicePort2Name[hostname] = rports
	}
	// TODO: for updates from other clusters, warn if they don't match primary.
}

// Update clusters for an incremental EDS push, and initiate the push.
// Only clusters that changed are updated/pushed.
func (s *DiscoveryServer) edsIncremental(version string, push *model.PushContext, edsUpdates map[string]*model.ServiceShards) {
	adsLog.Infof("XDS:EDSInc Pushing %s Services: %v, "+
		"VirtualServices: %d, ConnectedEndpoints: %d", version, edsUpdates,
		len(push.VirtualServiceConfigs), adsClientCount())
	t0 := time.Now()

	// First update all cluster load assignments. This is computed for each cluster once per config change
	// instead of once per endpoint.
	edsClusterMutex.Lock()
	// Create a temp map to avoid locking the add/remove
	cMap := make(map[string]*EdsCluster, len(edsClusters))
	for k, v := range edsClusters {
		_, _, hostname, _ := model.ParseSubsetKey(k)
		if edsUpdates[string(hostname)] == nil {
			// Cluster was not updated, skip recomputing.
			continue
		}
		cMap[k] = v
	}
	edsClusterMutex.Unlock()

	// UpdateCluster updates the cluster with a mutex, this code is safe ( but computing
	// the update may be duplicated if multiple goroutines compute at the same time).
	// In general this code is called from the 'event' callback that is throttled.
	for clusterName, edsCluster := range cMap {
		if err := s.updateClusterInc(push, clusterName, edsCluster); err != nil {
			adsLog.Errorf("updateCluster failed with clusterName %s", clusterName)
		}
	}
	adsLog.Infof("Cluster init time %v %s", time.Since(t0), version)

	s.startPush(version, push, false, edsUpdates)
}

// WorkloadUpdate is called when workload labels/annotations are updated.
func (s *DiscoveryServer) WorkloadUpdate(id string, labels map[string]string, annotations map[string]string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if labels == nil {
		// No push needed - the Endpoints object will also be triggered.
		delete(s.WorkloadsByID, id)
		return
	}
	w, f := s.WorkloadsByID[id]
	if !f {
		// First time this workload has been seen. Likely never connected, no need to
		// push
		s.WorkloadsByID[id] = &Workload{
			Labels:      labels,
			Annotations: annotations,
		}
		return
	}
	if reflect.DeepEqual(w.Labels, labels) {
		// No label change.
		return
	}

	w.Labels = labels
	// Label changes require recomputing the config.
	// TODO: we can do a push for the affected workload only, but we need to confirm
	// no other workload can be affected. Safer option is to fallback to full push.

	adsLog.Infof("Label change, full push %s ", id)
	s.ConfigUpdater.ConfigUpdate(true)
}

// EDSUpdate computes destination address membership across all clusters and networks.
// This is the main method implementing EDS.
// It replaces InstancesByPort in model - instead of iterating over all endpoints it uses
// the hostname-keyed map. And it avoids the conversion from Endpoint to ServiceEntry to envoy
// on each step: instead the conversion happens once, when an endpoint is first discovered.
func (s *DiscoveryServer) EDSUpdate(shard, serviceName string,
	entries []*model.IstioEndpoint) error {
	return s.edsUpdate(shard, serviceName, entries, false)
}

// BeforePush is a callback invoked just before the push. It currently swaps and returns the
// EdsUpdates map - additional preparation will be added as we move to full incremental.
// This is needed to keep things isolated and use the right mutex.
// Once proxy/envoy/discovery is merged into v2 discovery this can become non-public.
func (s *DiscoveryServer) BeforePush() map[string]*model.ServiceShards {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	edsUpdates := s.edsUpdates
	// Reset - any new updates will be tracked by the new map
	s.edsUpdates = map[string]*model.ServiceShards{}

	return edsUpdates
}

func (s *DiscoveryServer) edsUpdate(shard, serviceName string,
	entries []*model.IstioEndpoint, internal bool) error {
	// edsShardUpdate replaces a subset (shard) of endpoints, as result of an incremental
	// update. The endpoint updates may be grouped by K8S clusters, other service registries
	// or by deployment. Multiple updates are debounced, to avoid too frequent pushes.
	// After debounce, the services are merged and pushed.
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Update the data structures for the service.
	// 1. Find the 'per service' data
	ep, f := s.EndpointShardsByService[serviceName]
	if !f {
		// This endpoint is for a service that was not previously loaded.
		// Return an error to force a full sync, which will also cause the
		// EndpointsShardsByService to be initialized with all services.
		ep = &model.ServiceShards{
			Shards:          map[string]*model.EndpointShard{},
			ServiceAccounts: map[string]bool{},
		}
		s.EndpointShardsByService[serviceName] = ep
		if !internal {
			adsLog.Infof("Full push, new service %s", serviceName)
			s.ConfigUpdater.ConfigUpdate(true)
		}
	}

	// 2. Update data for the specific cluster. Each cluster gets independent
	// updates containing the full list of endpoints for the service in that cluster.
	ce := &model.EndpointShard{
		Shard:   shard,
		Entries: []*model.IstioEndpoint{},
	}

	for _, e := range entries {
		ce.Entries = append(ce.Entries, e)
		if e.ServiceAccount != "" {
			_, f = ep.ServiceAccounts[e.ServiceAccount]
			if !f && !internal {
				// The entry has a service account that was not previously associated.
				// Requires a CDS push and full sync.
				adsLog.Infof("Endpoint updating service account %s %s", e.ServiceAccount, serviceName)
				s.ConfigUpdater.ConfigUpdate(true)
			}
		}
	}
	ep.Shards[shard] = ce
	s.edsUpdates[serviceName] = ep

	return nil
}

//// update the 'byPort' structure by merging info from all clusters.
//func (s *DiscoveryServer) updateByPort(ep *ServiceShards)  {
//	ep.ByPort = map[uint32][]*IstioEndpoint{}
//
//	// 3. Based on the updated list, merge the cluster data ( including previously
//	// received data from the other clusters )
//	for _, ce := range ep.ByCluster {
//		for _, ee := range ce.Entries {
//			// Each ee represents a Pod - with IP address, labels and ports.
//			//for svcPortName, epPort := range ee.Ports {
//			byPort, f := ep.ByPort[ee.servicePort]
//			if !f {
//				byPort = []*IstioEndpoint{}
//				ep.ByPort[ee.servicePort] = byPort
//			}
//			byPort = append(byPort, ee)
//		}
//	}
//}

// LocalityLbEndpointsFromInstances returns a list of Envoy v2 LocalityLbEndpoints.
// Envoy v2 Endpoints are constructed from Pilot's older data structure involving
// model.ServiceInstance objects. Envoy expects the endpoints grouped by zone, so
// a map is created - in new data structures this should be part of the model.
func localityLbEndpointsFromInstances(instances []*model.ServiceInstance) []endpoint.LocalityLbEndpoints {
	localityEpMap := make(map[string]*endpoint.LocalityLbEndpoints)
	for _, instance := range instances {
		lbEp, err := newEndpoint(&instance.Endpoint)
		if err != nil {
			adsLog.Errorf("EDS: unexpected pilot model endpoint v1 to v2 conversion: %v", err)
			continue
		}
		// TODO: Need to accommodate region, zone and subzone. Older Pilot datamodel only has zone = availability zone.
		// Once we do that, the key must be a | separated tupple.
		locality := instance.GetAZ()
		locLbEps, found := localityEpMap[locality]
		if !found {
			locLbEps = &endpoint.LocalityLbEndpoints{
				Locality: &core.Locality{
					Zone: locality,
				},
			}
			localityEpMap[locality] = locLbEps
		}
		locLbEps.LbEndpoints = append(locLbEps.LbEndpoints, *lbEp)
	}
	out := make([]endpoint.LocalityLbEndpoints, 0, len(localityEpMap))
	for _, locLbEps := range localityEpMap {
		out = append(out, *locLbEps)
	}
	return out
}

func connectionID(node string) string {
	edsClusterMutex.Lock()
	connectionNumber++
	c := connectionNumber
	edsClusterMutex.Unlock()
	return node + "-" + strconv.Itoa(int(c))
}

// pushEds is pushing EDS updates for a single connection. Called the first time
// a client connects, for incremental updates and for full periodic updates.
func (s *DiscoveryServer) pushEds(push *model.PushContext, con *XdsConnection,
	full bool, edsUpdatedServices map[string]*model.ServiceShards) error {
	resAny := []types.Any{}

	emptyClusters := 0
	endpoints := 0
	empty := []string{}

	updated := []string{}

	for _, clusterName := range con.Clusters {
		if edsPartial {
			_, _, hostname, _ := model.ParseSubsetKey(clusterName)
			if edsUpdatedServices != nil && edsUpdatedServices[string(hostname)] == nil {
				// Cluster was not updated, skip recomputing.
				continue
			}
			// for debug
			if edsUpdatedServices != nil {
				updated = append(updated, clusterName)
			}
		}

		c := s.getEdsCluster(clusterName)
		if c == nil {
			adsLog.Errorf("cluster %s was nil skipping it.", clusterName)
			continue
		}

		l := loadAssignment(c)
		if l == nil { // fresh cluster
			if err := s.updateCluster(push, clusterName, c); err != nil {
				adsLog.Errorf("error returned from updateCluster for cluster name %s, skipping it.", clusterName)
				continue
			}
			l = loadAssignment(c)
		}
		endpoints += len(l.Endpoints)
		if len(l.Endpoints) == 0 {
			emptyClusters++
			empty = append(empty, clusterName)
		}

		// Previously computed load assignments. They are re-computed on cache invalidation or
		// event, but don't have to be recomputed once for each sidecar.
		clAssignmentRes, _ := types.MarshalAny(l)
		resAny = append(resAny, *clAssignmentRes)
	}

	response := s.endpoints(con.Clusters, resAny)
	err := con.send(response)
	if err != nil {
		adsLog.Warnf("EDS: Send failure, closing grpc %v", err)
		pushes.With(prometheus.Labels{"type": "eds_senderr"}).Add(1)
		return err
	}
	pushes.With(prometheus.Labels{"type": "eds"}).Add(1)

	if full {
		// TODO: switch back to debug
		adsLog.Infof("EDS: PUSH for %s clusters %d endpoints %d empty %d",
			con.ConID, len(con.Clusters), endpoints, emptyClusters)
	} else {
		adsLog.Infof("EDS: INC PUSH for %s clusters %d endpoints %d empty %d %v",
			con.ConID, len(con.Clusters), endpoints, emptyClusters, updated)
	}
	return nil
}

// addEdsCon will track the eds connection with clusters, for optimized event-based push and debug
func (s *DiscoveryServer) addEdsCon(clusterName string, node string, connection *XdsConnection) {

	c := s.getOrAddEdsCluster(clusterName)
	// TODO: left the code here so we can skip sending the already-sent clusters.
	// See comments in ads - envoy keeps adding one cluster to the list (this seems new
	// previous version sent all the clusters from CDS in bulk).

	//c.mutex.Lock()
	//existing := c.EdsClients[node]
	//c.mutex.Unlock()
	//
	//// May replace an existing connection: this happens when Envoy adds more clusters
	//// one by one, creating new grpc requests each time it adds one more cluster.
	//if existing != nil {
	//	log.Warnf("Replacing existing connection %s %s old: %s", clusterName, node, existing.ConID)
	//}
	c.mutex.Lock()
	c.EdsClients[node] = connection
	c.mutex.Unlock()
}

// getEdsCluster returns a cluster.
func (s *DiscoveryServer) getEdsCluster(clusterName string) *EdsCluster {
	// separate method only to have proper lock.
	edsClusterMutex.Lock()
	defer edsClusterMutex.Unlock()
	return edsClusters[clusterName]
}

func (s *DiscoveryServer) getOrAddEdsCluster(clusterName string) *EdsCluster {
	edsClusterMutex.Lock()
	defer edsClusterMutex.Unlock()

	c := edsClusters[clusterName]
	if c == nil {
		c = &EdsCluster{discovery: s,
			EdsClients: map[string]*XdsConnection{},
			FirstUse:   time.Now(),
		}
		edsClusters[clusterName] = c
	}
	return c
}

// removeEdsCon is called when a gRPC stream is closed, for each cluster that was watched by the
// stream. As of 0.7 envoy watches a single cluster per gprc stream.
func (s *DiscoveryServer) removeEdsCon(clusterName string, node string, connection *XdsConnection) {
	c := s.getEdsCluster(clusterName)
	if c == nil {
		adsLog.Warnf("EDS: missing cluster %s", clusterName)
		return
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	oldcon := c.EdsClients[node]
	if oldcon == nil {
		adsLog.Warnf("EDS: Envoy restart %s %v, cleanup old connection missing %v", node, connection.PeerAddr, c.EdsClients)
		return
	}
	if oldcon != connection {
		adsLog.Infof("EDS: Envoy restart %s %v, cleanup old connection %v", node, connection.PeerAddr, oldcon.PeerAddr)
		return
	}
	delete(c.EdsClients, node)
	if len(c.EdsClients) == 0 {
		edsClusterMutex.Lock()
		defer edsClusterMutex.Unlock()
		// This happens when a previously used cluster is no longer watched by any
		// sidecar. It should not happen very often - normally all clusters are sent
		// in CDS requests to all sidecars. It may happen if all connections are closed.
		adsLog.Infof("EDS: remove unwatched cluster node=%s cluster=%s", node, clusterName)
		delete(edsClusters, clusterName)
	}
}

// FetchEndpoints implements xdsapi.EndpointDiscoveryServiceServer.FetchEndpoints().
func (s *DiscoveryServer) FetchEndpoints(ctx context.Context, req *xdsapi.DiscoveryRequest) (*xdsapi.DiscoveryResponse, error) {
	return nil, errors.New("not implemented")
}

// StreamLoadStats implements xdsapi.EndpointDiscoveryServiceServer.StreamLoadStats().
func (s *DiscoveryServer) StreamLoadStats(xdsapi.EndpointDiscoveryService_StreamEndpointsServer) error {
	return errors.New("unsupported streaming method")
}
