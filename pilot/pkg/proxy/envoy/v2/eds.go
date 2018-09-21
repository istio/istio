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
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	"github.com/gogo/protobuf/types"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc/peer"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
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
	edsClusterMutex sync.RWMutex
	edsClusters     = map[string]*EdsCluster{}

	// Tracks connections, increment on each new connection.
	connectionNumber = int64(0)
)

// EdsCluster tracks eds-related info for monitored clusters. In practice it'll include
// all clusters until we support on-demand cluster loading.
type EdsCluster struct {
	// mutex protects changes to this cluster
	mutex sync.Mutex

	// A map keyed by network ID tracking the ClusterLoadAssignment for the
	// network and it holds both local endpoints and weighted remote endpoints
	// to other networks.
	ClusterLoadAssignments map[string]*xdsapi.ClusterLoadAssignment

	// Pointer to the endpoint of the gateway of each network (if exists).
	// This endpoint is weighted with a value equal to the number of local
	// endpoints for cluster.
	GatewaysEndpoints map[string]*endpoint.LocalityLbEndpoints

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
func loadAssignment(c *EdsCluster, node *model.Proxy) *xdsapi.ClusterLoadAssignment {
	if node == nil {
		return c.ClusterLoadAssignments[""]
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.filterEndpointsByMetadata(node.Metadata)
}

func (c *EdsCluster) filterEndpointsByMetadata(nodeMeta map[string]string) *xdsapi.ClusterLoadAssignment {
	if network, exists := nodeMeta["ISTIO_NETWORK"]; exists {
		if cla, ok := c.ClusterLoadAssignments[network]; ok {
			return cla
		}
	}
	return c.ClusterLoadAssignments[""]
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

// updateCluster is called from the event (or global cache invalidation) to update
// the endpoints for the cluster.
func (s *DiscoveryServer) updateCluster(push *model.PushContext, clusterName string, edsCluster *EdsCluster) error {
	// TODO: should we lock this as well ? Once we move to event-based it may not matter.
	locEpsByNetwork := map[string][]endpoint.LocalityLbEndpoints{"": []endpoint.LocalityLbEndpoints{}}
	var gwByNetwork map[string]*endpoint.LocalityLbEndpoints
	direction, subsetName, hostname, port := model.ParseSubsetKey(clusterName)
	if direction == model.TrafficDirectionInbound ||
		direction == model.TrafficDirectionOutbound {
		labels := push.SubsetToLabels(subsetName, hostname)
		instances, err := edsCluster.discovery.env.ServiceDiscovery.InstancesByPort(hostname, port, labels)
		if err != nil {
			adsLog.Errorf("endpoints for service cluster %q returned error %v", clusterName, err)
			return err
		}
		if len(instances) == 0 {
			push.Add(model.ProxyStatusClusterNoInstances, clusterName, nil, "")
			adsLog.Debugf("EDS: cluster %q (host=%s ports=%v labels=%v) has no instances", clusterName, hostname, port, labels)
		}
		edsInstances.With(prometheus.Labels{"cluster": clusterName}).Set(float64(len(instances)))

		if len(instances) != 0 {
			locEpsByNetwork = localityLbEndpointsFromInstances(instances)
			gwByNetwork = networkGateways(instances, locEpsByNetwork)
		}
	}

	// There is a chance multiple goroutines will update the cluster at the same time.
	// This could be prevented by a lock - but because the update may be slow, it may be
	// better to accept the extra computations.
	// We still lock the access to the LoadAssignments.
	edsCluster.mutex.Lock()
	defer edsCluster.mutex.Unlock()
	for n, locEps := range locEpsByNetwork {
		edsCluster.ClusterLoadAssignments[n] = &xdsapi.ClusterLoadAssignment{
			ClusterName: clusterName,
			Endpoints:   locEps,
		}
		if len(locEps) > 0 && edsCluster.NonEmptyTime.IsZero() {
			edsCluster.NonEmptyTime = time.Now()
		}
	}
	edsCluster.GatewaysEndpoints = gwByNetwork

	return nil
}

// networkGateway returns an endpoint to the gateway of a remote Istio network.
// The weight of the endpoint will equal to the number of endpoints in the
// remote network. If the remote network has no cluster endpoints (weight 0)
// nil will be returned.
func networkGateways(instances []*model.ServiceInstance, endpoints map[string][]endpoint.LocalityLbEndpoints) map[string]*endpoint.LocalityLbEndpoints {
	gateways := make(map[string]*endpoint.LocalityLbEndpoints)
	for _, instance := range instances {
		network := instance.Labels["ISTIO_NETWORK"]
		if gateways[network] != nil || len(endpoints[network]) == 0 {
			continue
		}

		weight := 0
		for _, ep := range endpoints[network] {
			weight += len(ep.LbEndpoints)
		}
		if weight == 0 {
			continue
		}

		var addr core.Address
		addrIP := instance.Labels["ISTIO_NETWORK_GATEWAY_IP"]
		addrPort, err := strconv.ParseUint(instance.Labels["ISTIO_NETWORK_GATEWAY_PORT"], 10, 32)
		if addrIP != "" && err == nil {
			addr = util.BuildAddress(addrIP, uint32(addrPort))
		} else {
			//TODO: For K8S, the gateway IP can be extracted from the Service
			// object for LoadBalancer services.
			addr = util.BuildAddress("1.1.1.1", 80)
		}

		gateways[network] = &endpoint.LocalityLbEndpoints{
			LbEndpoints: []endpoint.LbEndpoint{
				endpoint.LbEndpoint{
					Endpoint: &endpoint.Endpoint{
						Address: &addr,
					},
				},
			},
			LoadBalancingWeight: &types.UInt32Value{
				Value: uint32(weight),
			},
		}
	}

	return gateways
}

// LocalityLbEndpointsFromInstances returns a list of Envoy v2 LocalityLbEndpoints.
// Envoy v2 Endpoints are constructed from Pilot's older data structure involving
// model.ServiceInstance objects. Envoy expects the endpoints grouped by zone, so
// a map is created - in new data structures this should be part of the model.
func localityLbEndpointsFromInstances(instances []*model.ServiceInstance) map[string][]endpoint.LocalityLbEndpoints {
	localityEpMapByNetwork := make(map[string]map[string]*endpoint.LocalityLbEndpoints)
	resultsMap := make(map[string][]endpoint.LocalityLbEndpoints)
	for _, instance := range instances {
		lbEp, err := newEndpoint(&instance.Endpoint)
		if err != nil {
			adsLog.Errorf("EDS: unexpected pilot model endpoint v1 to v2 conversion: %v", err)
			continue
		}
		// TODO: Need to accommodate region, zone and subzone. Older Pilot datamodel only has zone = availability zone.
		// Once we do that, the key must be a | separated tupple.
		locality := instance.GetAZ()
		network := instance.Labels["ISTIO_NETWORK"]
		_, found := localityEpMapByNetwork[network]
		if !found {
			localityEpMapByNetwork[network] = make(map[string]*endpoint.LocalityLbEndpoints)
		}
		locLbEps, found := localityEpMapByNetwork[network][locality]
		if !found {
			locLbEps = &endpoint.LocalityLbEndpoints{
				Locality: &core.Locality{
					Zone: locality,
				},
			}
			localityEpMapByNetwork[network][locality] = locLbEps
		}
		locLbEps.LbEndpoints = append(locLbEps.LbEndpoints, *lbEp)
	}
	for n, localityEpMap := range localityEpMapByNetwork {
		out := make([]endpoint.LocalityLbEndpoints, 0, len(localityEpMap))
		for _, locLbEps := range localityEpMap {
			out = append(out, *locLbEps)
		}
		resultsMap[n] = out
	}
	return resultsMap
}

// Function will go through other networks and add a remote endpoint pointing
// to the gateway of each one of them. The remote endpoints (one per each network)
// will be added to the ClusterLoadAssignment of the specified network.
func (c *EdsCluster) updateRemoteEndpoints(node *model.Proxy) {
	if node == nil {
		return
	}
	network := node.Metadata["ISTIO_NETWORK"]
	cla, found := c.ClusterLoadAssignments[network]
	if !found {
		return
	}
	for n := range c.ClusterLoadAssignments {
		if n == network {
			continue
		}

		// Append an endpoint to the remote network gateway (if it doesn't
		// already exist) and add it to the list of local endpoints of this
		// network
		if rgw := c.GatewaysEndpoints[n]; rgw != nil {
			exists := false
			for _, ep := range cla.Endpoints {
				if ep.Equal(rgw) {
					exists = true
					break
				}
			}
			if !exists {
				cla.Endpoints = append(cla.Endpoints, *rgw)
			}
		}
	}
}

func connectionID(node string) string {
	id := atomic.AddInt64(&connectionNumber, 1)
	return node + "-" + strconv.FormatInt(id, 10)
}

// StreamEndpoints implements xdsapi.EndpointDiscoveryServiceServer.StreamEndpoints().
func (s *DiscoveryServer) StreamEndpoints(stream xdsapi.EndpointDiscoveryService_StreamEndpointsServer) error {
	peerInfo, ok := peer.FromContext(stream.Context())
	peerAddr := "Unknown peer address"
	if ok {
		peerAddr = peerInfo.Addr.String()
	}
	var discReq *xdsapi.DiscoveryRequest
	var receiveError error
	reqChannel := make(chan *xdsapi.DiscoveryRequest, 1)

	initialRequestReceived := false

	con := newXdsConnection(peerAddr, stream)
	defer close(con.doneChannel)

	// node is the key used in the cluster map. It includes the pod name and an unique identifier,
	// since multiple envoys may connect from the same pod.
	go receiveThread(con, reqChannel, &receiveError)

	for {
		// Block until either a request is received or a push is triggered.
		select {
		case discReq, ok = <-reqChannel:
			if !ok {
				return receiveError
			}

			// Should not change. A node monitors multiple clusters
			if con.ConID == "" && discReq.Node != nil {
				con.ConID = connectionID(discReq.Node.Id)
			}

			clusters2 := discReq.GetResourceNames()
			if initialRequestReceived {
				if len(clusters2) > len(con.Clusters) {
					// This doesn't happen with current envoy - but should happen in future, there is no reason
					// to keep so many open grpc streams (one per cluster, for each envoy)
					adsLog.Infof("EDS: Multiple clusters monitoring %v -> %v %s", con.Clusters, clusters2, discReq.String())
					initialRequestReceived = false // treat this as an initial request (updates monitoring state)
				}
			}

			// Given that Pilot holds an eventually consistent data model, Pilot ignores any acknowledgements
			// from Envoy, whether they indicate ack success or ack failure of Pilot's previous responses.
			if initialRequestReceived {
				// TODO: once the deps are updated, log the ErrorCode if set (missing in current version)
				if discReq.ErrorDetail != nil {
					adsLog.Warnf("EDS: ACK ERROR %v %s %v", peerAddr, con.ConID, discReq.String())
				}
				adsLog.Debugf("EDS: ACK %s %s %s", con.ConID, discReq.VersionInfo, con.Clusters)
				if len(con.Clusters) > 0 {
					continue
				}
			}
			adsLog.Infof("EDS: REQ %s %v %v raw: %s ", con.ConID, con.Clusters, peerAddr, discReq.String())
			con.Clusters = discReq.GetResourceNames()
			initialRequestReceived = true

			// In 0.7 EDS only listens for 1 cluster for each stream. In 0.8 EDS is no longer
			// used.
			for _, c := range con.Clusters {
				s.addEdsCon(c, con.ConID, con)
			}

			// Keep track of active EDS client. In 0.7 EDS push happened by pushing for all
			// tracked clusters. In 0.8+ push happens by iterating active connections, in ADS.
			if !con.added {
				con.added = true
				s.addCon(con.ConID, con)
				defer s.removeCon(con.ConID, con)
			}

		case <-con.pushChannel:
		}

		if len(con.Clusters) > 0 {
			err := s.pushEds(s.env.PushContext, con)
			if err != nil {
				adsLog.Errorf("Closing EDS connection, failure to push %v", err)
				return err
			}
		}

	}
}

func (s *DiscoveryServer) pushEds(push *model.PushContext, con *XdsConnection) error {
	resAny := []types.Any{}

	emptyClusters := 0
	endpoints := 0
	empty := []string{}

	for _, clusterName := range con.Clusters {
		c := s.getEdsCluster(clusterName)
		if c == nil {
			adsLog.Errorf("cluster %s was nil skipping it.", clusterName)
			continue
		}

		l := loadAssignment(c, con.modelNode)
		if l == nil { // fresh cluster
			if err := s.updateCluster(push, clusterName, c); err != nil {
				adsLog.Errorf("error returned from updateCluster for cluster name %s, skipping it.", clusterName)
				continue
			}
			l = loadAssignment(c, con.modelNode)
		}
		c.updateRemoteEndpoints(con.modelNode)
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

	adsLog.Debugf("EDS: PUSH for %s clusters %d endpoints %d empty %d",
		con.ConID, len(con.Clusters), endpoints, emptyClusters)
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
	edsClusterMutex.RLock()
	defer edsClusterMutex.RUnlock()
	return edsClusters[clusterName]
}

func (s *DiscoveryServer) getOrAddEdsCluster(clusterName string) *EdsCluster {
	edsClusterMutex.Lock()
	defer edsClusterMutex.Unlock()

	c := edsClusters[clusterName]
	if c == nil {
		c = &EdsCluster{
			discovery:              s,
			ClusterLoadAssignments: map[string]*xdsapi.ClusterLoadAssignment{},
			GatewaysEndpoints:      map[string]*endpoint.LocalityLbEndpoints{},
			EdsClients:             map[string]*XdsConnection{},
			FirstUse:               time.Now(),
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
