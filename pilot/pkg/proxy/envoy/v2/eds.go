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
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/gogo/protobuf/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
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
	// It doesn't appear the logging framework has support for per-component logging, yet
	// we need to be able to debug EDS2.
	edsDebug = len(os.Getenv("PILOT_DEBUG_EDS")) == 0

	edsClusterMutex sync.Mutex
	edsClusters     = map[string]*EdsCluster{}
	version         = 1

	// Tracks connections, increment on each new connection.
	connectionNumber = int64(0)
)

// EdsCluster tracks eds-related info for monitored clusters. In practice it'll include
// all clusters until we support on-demand cluster loading.
type EdsCluster struct {
	// mutex protects changes to this cluster
	mutex sync.Mutex

	// EdsClients keeps track of all nodes monitoring the cluster.
	EdsClients map[string]*EdsConnection

	LoadAssignment *xdsapi.ClusterLoadAssignment

	// FirstUse is the time the cluster was first used, for debugging
	FirstUse time.Time

	// NonEmptyTime is the time the cluster first had a non-empty set of endpoints
	NonEmptyTime time.Time

	// The discovery service this cluster is associated with.
	discovery *DiscoveryServer
}

// TODO: add prom metrics !

// EdsConnection represents a streaming grpc connection from an envoy server.
// This is primarily intended for supporting push, but also for debug and statusz.
type EdsConnection struct {
	PeerAddr string

	// current list of clusters monitored by the client
	Clusters []string

	// Time of connection, for debugging
	Connect time.Time

	// Sending on this channel results in  push. We may also make it a channel of objects so
	// same info can be sent to all clients, without recomputing.
	pushChannel chan bool
}

// Endpoints aggregate a DiscoveryResponse for pushing.
func (s *DiscoveryServer) endpoints(clusterNames []string) *xdsapi.DiscoveryResponse {
	// Not using incCounters/observeResources: grpc has an interceptor for prometheus.
	version := strconv.Itoa(version)
	clAssignment := &xdsapi.ClusterLoadAssignment{}
	clAssignmentRes, _ := types.MarshalAny(clAssignment)
	out := &xdsapi.DiscoveryResponse{
		// All resources for EDS ought to be of the type ClusterLoadAssignment
		TypeUrl: clAssignmentRes.GetTypeUrl(),

		// Pilot does not really care for versioning. It always supplies what's currently
		// available to it, irrespective of whether Envoy chooses to accept or reject EDS
		// responses. Pilot believes in eventual consistency and that at some point, Envoy
		// will begin seeing results it deems to be good.
		VersionInfo: version,
		Nonce:       version + "-" + time.Now().String(),
	}

	out.Resources = make([]types.Any, 0, len(clusterNames))
	for _, clusterName := range clusterNames {
		clAssignmentRes := s.clusterEndpoints(clusterName)
		if clAssignmentRes != nil {
			out.Resources = append(out.Resources, *clAssignmentRes)
		}
	}

	return out
}

// Get the ClusterLoadAssignment for a cluster.
func (s *DiscoveryServer) clusterEndpoints(clusterName string) *types.Any {
	c := s.getOrAddEdsCluster(clusterName)
	if c.LoadAssignment == nil { // fresh cluster
		updateCluster(clusterName, c)
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()
	// Previously computed load assignments. They are re-computed on cache invalidation or
	// event, but don't have to be recomputed once for each sidecar.
	clAssignmentRes, _ := types.MarshalAny(c.LoadAssignment)
	return clAssignmentRes

}

func newEndpoint(address string, port uint32) (*endpoint.LbEndpoint, error) {
	ipAddr := net.ParseIP(address)
	if ipAddr == nil {
		return nil, errors.New("Invalid IP address " + address)
	}
	ep := &endpoint.LbEndpoint{
		Endpoint: &endpoint.Endpoint{
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					&core.SocketAddress{
						Address:    address,
						Ipv4Compat: ipAddr.To4() != nil,
						Protocol:   core.TCP,
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: port,
						},
					},
				},
			},
		},
	}

	//log.Infoa("EDS: endpoint ", ipAddr, ep.String())
	return ep, nil
}

// updateCluster is called from the event (or global cache invalidation) to update
// the endpoints for the cluster.
func updateCluster(clusterName string, edsCluster *EdsCluster) {
	// TODO: should we lock this as well ? Once we move to event-based it may not matter.
	hostname, ports, labels := model.ParseServiceKey(clusterName)
	instances, err := edsCluster.discovery.mesh.Instances(hostname, ports.GetNames(), labels)
	if err != nil {
		log.Warnf("endpoints for service cluster %q returned error %q", clusterName, err)
		return
	}
	locEps := localityLbEndpointsFromInstances(instances)
	if len(instances) == 0 && edsDebug {
		log.Infoa("EDS: no instances ", clusterName, hostname, ports, labels)
	}
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

}

// LocalityLbEndpointsFromInstances returns a list of Envoy v2 LocalityLbEndpoints.
// Envoy v2 Endpoints are constructed from Pilot's older data structure involving
// model.ServiceInstance objects. Envoy expects the endpoints grouped by zone, so
// a map is created - in new data structures this should be part of the model.
func localityLbEndpointsFromInstances(instances []*model.ServiceInstance) []endpoint.LocalityLbEndpoints {
	localityEpMap := make(map[string]*endpoint.LocalityLbEndpoints)
	for _, instance := range instances {
		lbEp, err := newEndpoint(instance.Endpoint.Address, (uint32)(instance.Endpoint.Port))
		if err != nil {
			log.Errorf("EDS: unexpected pilot model endpoint v1 to v2 conversion: %v", err)
			continue
		}
		// TODO: Need to accommodate region, zone and subzone. Older Pilot datamodel only has zone = availability zone.
		// Once we do that, the key must be a | separated tupple.
		locality := instance.AvailabilityZone
		locLbEps, found := localityEpMap[locality]
		if !found {
			locLbEps = &endpoint.LocalityLbEndpoints{
				Locality: &core.Locality{
					Zone: instance.AvailabilityZone,
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
	con := &EdsConnection{
		pushChannel: make(chan bool, 1),
		PeerAddr:    peerAddr,
		Clusters:    []string{},
		Connect:     time.Now(),
	}
	// node is the key used in the cluster map. It includes the pod name and an unique identifier,
	// since multiple envoys may connect from the same pod.
	var node string
	go func() {
		defer close(reqChannel)
		for {
			req, err := stream.Recv()
			if err != nil {
				log.Errorf("EDS close for client %s %q terminated with errors %v",
					node, peerAddr, err)
				for _, c := range con.Clusters {
					s.removeEdsCon(c, node, con)
				}
				if status.Code(err) == codes.Canceled || err == io.EOF {
					return
				}
				receiveError = err
				return
			}
			reqChannel <- req
		}
	}()
	for {
		// Block until either a request is received or the ticker ticks
		select {
		case discReq, ok = <-reqChannel:
			if !ok {
				return receiveError
			}
			/* Example:
			 node:<id:"ingress~~istio-ingress-6796c456f4-7zqtm.istio-system~istio-system.svc.cluster.local"
				cluster:"istio-ingress"
				build_version:"0/1.6.0-dev//RELEASE" >
				resource_names:"echosrv.istio-system.svc.cluster.local|http-echo"
				type_url:"type.googleapis.com/envoy.api.v2.ClusterLoadAssignment"
			*/

			clusters2 := discReq.GetResourceNames()
			// Should not change. A node monitors multiple clusters
			if node == "" && discReq.Node != nil {
				node = connectionID(discReq.Node.Id)

			}

			if node == "" {
				log.Infoa("Missing node ", discReq.String())
				return errors.New("Missing node " + discReq.String())
			}

			// This doesn't seem to happen - add a log just in case. Typical requests from envoy
			// have a single cluster, and
			if len(con.Clusters) > 0 && len(clusters2) > 0 && con.Clusters[0] != clusters2[0] {
				log.Infof("EDS: Clusters override %v -> %v %s", con.Clusters, clusters2, discReq.String())
			}
			// Doesn't seem to happen - logging in case something changes in envoy
			if len(clusters2) > 1 {
				log.Infof("EDS: Batch clusters %v %s", clusters2, discReq.String())
			}
			// Given that Pilot holds an eventually consistent data model, Pilot ignores any acknowledgements
			// from Envoy, whether they indicate ack success or ack failure of Pilot's previous responses.
			if discReq.ResponseNonce != "" {
				// TODO: once the deps are updated, log the ErrorCode if set (missing in current version)
				if edsDebug {
					log.Infof("EDS: ACK %s %s %s", node, discReq.VersionInfo, con.Clusters)
				}
				continue
			}
			if len(con.Clusters) > 0 {
				// Should not happen
				log.Infof("EDS REQ repeated %s %v/%v %v raw: %s ",
					node, clusters2, con.Clusters, peerAddr, discReq.String())
			}
			// Initial request
			if edsDebug {
				log.Infof("EDS REQ %s %v %v raw: %s ",
					node, clusters2, peerAddr, discReq.String())
			}

			if len(clusters2) > 0 {
				con.Clusters = clusters2
			}

			for _, c := range con.Clusters {
				s.addEdsCon(c, node, con)
			}

		case <-con.pushChannel:
			if len(con.Clusters) == 0 {
				continue
			}
		}

		if len(con.Clusters) > 0 {
			response := s.endpoints(con.Clusters)
			err := stream.Send(response)
			if err != nil {
				return err
			}

			if edsDebug {
				log.Infof("EDS RES for %s %q clusters %v, Response: \n%s\n",
					node, peerAddr, con.Clusters, response.String())
			}
		} else {
			if edsDebug {
				log.Infof("EDS empty clusters %v \n", peerAddr)
			}
		}
	}
}

// EdsPushAll implements old style invalidation, generated when any rule or endpoint changes.
// Primary code path is from v1 discoveryService.clearCache(), which is added as a handler
// to the model ConfigStorageCache and Controller.
func EdsPushAll() {
	if edsDebug {
		log.Infoa("EDS cache reset")
	}
	edsClusterMutex.Lock()
	// Create a temp map to avoid locking the add/remove
	tmpMap := map[string]*EdsCluster{}
	for k, v := range edsClusters {
		tmpMap[k] = v
	}
	version++
	edsClusterMutex.Unlock()

	for clusterName, edsCluster := range tmpMap {
		updateCluster(clusterName, edsCluster)
		edsCluster.mutex.Lock()
		for _, edsCon := range edsCluster.EdsClients {
			edsCon.pushChannel <- true
		}
		edsCluster.mutex.Unlock()
	}
}

// Edsz implements a status and debug interface for EDS.
// It is mapped to /debug/edsz on the monitor port (9093).
func Edsz(w http.ResponseWriter, req *http.Request) {
	if req.Form.Get("debug") != "" {
		edsDebug = req.Form.Get("debug") == "1"
		return
	}
	if req.Form.Get("push") != "" {
		EdsPushAll()
	}
	data, err := json.Marshal(edsClusters)
	if err != nil {
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	_, _ = w.Write(data)
}

// addEdsCon will track the eds connection, for push and debug
func (s *DiscoveryServer) addEdsCon(clusterName string, node string, connection *EdsConnection) {

	c := s.getOrAddEdsCluster(clusterName)
	c.mutex.Lock()
	existing := c.EdsClients[node]
	c.mutex.Unlock()

	// May replace an existing connection
	if existing != nil {
		existing.pushChannel <- false // force closing it
	}
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
			EdsClients: map[string]*EdsConnection{},
			FirstUse:   time.Now(),
		}
		edsClusters[clusterName] = c
	}
	return c
}

// removeEdsCon is called when a gRPC stream is closed, for each cluster that was watched by the
// stream. As of 0.7 envoy watches a single cluster per gprc stream.
func (s *DiscoveryServer) removeEdsCon(clusterName string, node string, connection *EdsConnection) {
	c := s.getEdsCluster(clusterName)
	if c == nil {
		log.Warnf("EDS: missing cluster %s", clusterName)
		return
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	oldcon := c.EdsClients[node]
	if oldcon != connection {
		if edsDebug {
			log.Infof("EDS: Envoy restart %s %v, cleanup old connection %v", node, connection.PeerAddr, oldcon.PeerAddr)
		}
		return
	}
	delete(c.EdsClients, node)
	if len(c.EdsClients) == 0 {
		log.Infof("EDS: remove unused cluster node=%s cluster=%s all=%v", node, clusterName, edsClusters)
		edsClusterMutex.Lock()
		defer edsClusterMutex.Unlock()
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
