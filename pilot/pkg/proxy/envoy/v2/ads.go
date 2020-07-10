// Copyright 2017 Istio Authors
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
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	ads "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	istiolog "istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	v3 "istio.io/istio/pilot/pkg/proxy/envoy/v3"
)

var (
	adsLog = istiolog.RegisterScope("ads", "ads debugging", 0)

	// SendTimeout is the max time to wait for a ADS send to complete. This helps detect
	// clients in a bad state (not reading). In future it may include checking for ACK
	SendTimeout = 5 * time.Second
)

// DiscoveryStream is a common interface for EDS and ADS. It also has a
// shorter name.
type DiscoveryStream interface {
	Send(*xdsapi.DiscoveryResponse) error
	Recv() (*xdsapi.DiscoveryRequest, error)
	grpc.ServerStream
}

// XdsConnection is a listener connection type.
type XdsConnection struct {
	// Mutex to protect changes to this XDS connection
	mu sync.RWMutex

	// PeerAddr is the address of the client envoy, from network layer
	PeerAddr string

	// Time of connection, for debugging
	Connect time.Time

	// ConID is the connection identifier, used as a key in the connection table.
	// Currently based on the node name and a counter.
	ConID string

	node *model.Proxy

	// Sending on this channel results in a push. We may also make it a channel of objects so
	// same info can be sent to all clients, without recomputing.
	pushChannel chan *XdsEvent

	LDSListeners []*xdsapi.Listener                    `json:"-"`
	RouteConfigs map[string]*xdsapi.RouteConfiguration `json:"-"`
	CDSClusters  []*xdsapi.Cluster

	// Last nonce sent and ack'd (timestamps) used for debugging
	ClusterNonceSent, ClusterNonceAcked   string
	ListenerNonceSent, ListenerNonceAcked string
	RouteNonceSent, RouteNonceAcked       string
	RouteVersionInfoSent                  string
	EndpointNonceSent, EndpointNonceAcked string

	// current list of clusters monitored by the client
	Clusters []string

	// Both ADS and EDS streams implement this interface
	stream DiscoveryStream

	// Routes is the list of watched Routes.
	Routes []string

	// LDSWatch is set if the remote server is watching Listeners
	LDSWatch bool
	// CDSWatch is set if the remote server is watching Clusters
	CDSWatch bool

	// Envoy may request different versions of configuration (XDS v2 vs v3). While internally Pilot will
	// only generate one version or the other, because the protos are wire compatible we can cast to the
	// requested version. This struct keeps track of the types requested for each resource type.
	// For example, if Envoy requests Clusters v3, we would track that here. Pilot would generate a v2
	// cluster response, but change the TypeUrl in the response to be v3.
	RequestedTypes struct {
		CDS string
		EDS string
		RDS string
		LDS string
	}
}

// XdsEvent represents a config or registry event that results in a push.
type XdsEvent struct {
	// Indicate whether the push is Full Push
	full bool

	configsUpdated map[model.ConfigKey]struct{}

	// Push context to use for the push.
	push *model.PushContext

	// start represents the time a push was started.
	start time.Time

	// function to call once a push is finished. This must be called or future changes may be blocked.
	done func()

	noncePrefix string
}

func newXdsConnection(peerAddr string, stream DiscoveryStream) *XdsConnection {
	return &XdsConnection{
		pushChannel:  make(chan *XdsEvent),
		PeerAddr:     peerAddr,
		Clusters:     []string{},
		Connect:      time.Now(),
		stream:       stream,
		LDSListeners: []*xdsapi.Listener{},
		RouteConfigs: map[string]*xdsapi.RouteConfiguration{},
	}
}

// isExpectedGRPCError checks a gRPC error code and determines whether it is an expected error when
// things are operating normally. This is basically capturing when the client disconnects.
func isExpectedGRPCError(err error) bool {
	if err == io.EOF {
		return true
	}

	s := status.Convert(err)
	if s.Code() == codes.Canceled || s.Code() == codes.DeadlineExceeded {
		return true
	}
	if s.Code() == codes.Unavailable && s.Message() == "client disconnected" {
		return true
	}
	return false
}

func receiveThread(con *XdsConnection, reqChannel chan *xdsapi.DiscoveryRequest, errP *error) {
	defer close(reqChannel) // indicates close of the remote side.
	for {
		req, err := con.stream.Recv()
		if err != nil {
			if isExpectedGRPCError(err) {
				con.mu.RLock()
				adsLog.Infof("ADS: %q %s terminated %v", con.PeerAddr, con.ConID, err)
				con.mu.RUnlock()
				return
			}
			*errP = err
			adsLog.Errorf("ADS: %q %s terminated with error: %v", con.PeerAddr, con.ConID, err)
			totalXDSInternalErrors.Increment()
			return
		}
		select {
		case reqChannel <- req:
		case <-con.stream.Context().Done():
			adsLog.Infof("ADS: %q %s terminated with stream closed", con.PeerAddr, con.ConID)
			return
		}
	}
}

// StreamAggregatedResources implements the ADS interface.
func (s *DiscoveryServer) StreamAggregatedResources(stream ads.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	peerInfo, ok := peer.FromContext(stream.Context())
	peerAddr := "0.0.0.0"
	if ok {
		peerAddr = peerInfo.Addr.String()
	}

	// first call - lazy loading, in tests. This should not happen if readiness
	// check works, since it assumes ClearCache is called (and as such PushContext
	// is initialized)
	// InitContext returns immediately if the context was already initialized.
	err := s.globalPushContext().InitContext(s.Env, nil, nil)
	if err != nil {
		// Error accessing the data - log and close, maybe a different pilot replica
		// has more luck
		adsLog.Warnf("Error reading config %v", err)
		return err
	}
	con := newXdsConnection(peerAddr, stream)

	// Do not call: defer close(con.pushChannel) !
	// the push channel will be garbage collected when the connection is no longer used.
	// Closing the channel can cause subtle race conditions with push. According to the spec:
	// "It's only necessary to close a channel when it is important to tell the receiving goroutines that all data
	// have been sent."

	// Reading from a stream is a blocking operation. Each connection needs to read
	// discovery requests and wait for push commands on config change, so we add a
	// go routine. If go grpc adds gochannel support for streams this will not be needed.
	// This also detects close.
	var receiveError error
	reqChannel := make(chan *xdsapi.DiscoveryRequest, 1)
	go receiveThread(con, reqChannel, &receiveError)

	for {
		// Block until either a request is received or a push is triggered.
		select {
		case discReq, ok := <-reqChannel:
			if !ok {
				// Remote side closed connection.
				return receiveError
			}
			// This should be only set for the first request. The node id may not be set - for example malicious clients.
			if con.node == nil {
				if err := s.initConnection(discReq.Node, con); err != nil {
					return err
				}
				defer s.removeCon(con.ConID)
			}
			if s.StatusReporter != nil {
				s.StatusReporter.RegisterEvent(con.ConID, discReq.TypeUrl, discReq.ResponseNonce)
			}

			// Based on node metadata a different generator was selected, use it instead of the default
			// behavior.
			if con.node.XdsResourceGenerator != nil && discReq.TypeUrl != EndpointType {
				// Endpoints are special - will use the optimized code path.
				err = s.handleCustomGenerator(con, discReq)
				if err != nil {
					return err
				}
				continue
			}

			switch discReq.TypeUrl {
			case ClusterType, v3.ClusterType:
				if err := s.handleTypeURL(discReq.TypeUrl, &con.RequestedTypes.CDS); err != nil {
					return err
				}
				if err := s.handleCds(con, discReq); err != nil {
					return err
				}
			case ListenerType, v3.ListenerType:
				if err := s.handleTypeURL(discReq.TypeUrl, &con.RequestedTypes.LDS); err != nil {
					return err
				}
				if err := s.handleLds(con, discReq); err != nil {
					return err
				}
			case RouteType, v3.RouteType:
				if err := s.handleTypeURL(discReq.TypeUrl, &con.RequestedTypes.RDS); err != nil {
					return err
				}
				if err := s.handleRds(con, discReq); err != nil {
					return err
				}
			case EndpointType, v3.EndpointType:
				if err := s.handleTypeURL(discReq.TypeUrl, &con.RequestedTypes.EDS); err != nil {
					return err
				}
				if err := s.handleEds(con, discReq); err != nil {
					return err
				}
			default:
				adsLog.Warnf("ADS: Unknown watched resources %s", discReq.String())
			}

		case pushEv := <-con.pushChannel:
			// It is called when config changes.
			// This is not optimized yet - we should detect what changed based on event and only
			// push resources that need to be pushed.

			// TODO: possible race condition: if a config change happens while the envoy
			// was getting the initial config, between LDS and RDS, the push will miss the
			// monitored 'routes'. Same for CDS/EDS interval.
			// It is very tricky to handle due to the protocol - but the periodic push recovers
			// from it.

			err := s.pushConnection(con, pushEv)
			pushEv.done()
			if err != nil {
				return nil
			}
		}
	}
}

// handleTypeURL records the type url received in an XDS response. If this conflicts with a previously sent type,
// an error is returned. For example, if a v2 cluster request was sent initially, then a v3 response was received, we will throw an error.
// This is to ensure that when we do pushes, we are sending a consistent type, rather than flipping between v2 and v3.
// A proper XDS client will not send mixed versions.
func (s *DiscoveryServer) handleTypeURL(typeURL string, requestedType *string) error {
	if *requestedType == "" {
		*requestedType = typeURL
	} else if *requestedType != typeURL {
		return fmt.Errorf("invalid type %v, expected %v", typeURL, *requestedType)
	}
	return nil
}

func (s *DiscoveryServer) handleLds(con *XdsConnection, discReq *xdsapi.DiscoveryRequest) error {
	if con.LDSWatch {
		// Already received a cluster watch request, this is an ACK
		if discReq.ErrorDetail != nil {
			errCode := codes.Code(discReq.ErrorDetail.Code)
			adsLog.Warnf("ADS:LDS: ACK ERROR %s %s:%s", con.ConID, errCode.String(), discReq.ErrorDetail.GetMessage())
			incrementXDSRejects(ldsReject, con.node.ID, errCode.String())
		} else if discReq.ResponseNonce != "" {
			con.ListenerNonceAcked = discReq.ResponseNonce
		}
		adsLog.Debugf("ADS:LDS: ACK %s %s %s", con.ConID, discReq.VersionInfo, discReq.ResponseNonce)
		return nil
	}
	adsLog.Debugf("ADS:LDS: REQ %s", con.ConID)
	con.LDSWatch = true
	err := s.pushLds(con, s.globalPushContext(), versionInfo())
	if err != nil {
		return err
	}
	return nil
}

func (s *DiscoveryServer) handleCds(con *XdsConnection, discReq *xdsapi.DiscoveryRequest) error {
	if con.CDSWatch {
		// Already received a cluster watch request, this is an ACK
		if discReq.ErrorDetail != nil {
			errCode := codes.Code(discReq.ErrorDetail.Code)
			adsLog.Warnf("ADS:CDS: ACK ERROR %s %s:%s", con.ConID, errCode.String(), discReq.ErrorDetail.GetMessage())
			incrementXDSRejects(cdsReject, con.node.ID, errCode.String())
		} else if discReq.ResponseNonce != "" {
			con.ClusterNonceAcked = discReq.ResponseNonce
		}
		adsLog.Debugf("ADS:CDS: ACK %s %s %s", con.ConID, discReq.VersionInfo, discReq.ResponseNonce)
		return nil
	}
	// CDS REQ is the first request an envoy makes. This shows up
	// immediately after connect. It is followed by EDS REQ as
	// soon as the CDS push is returned.
	adsLog.Infof("ADS:CDS: REQ %v version:%s", con.ConID, discReq.VersionInfo)
	con.CDSWatch = true
	err := s.pushCds(con, s.globalPushContext(), versionInfo())
	if err != nil {
		return err
	}
	return nil
}

func (s *DiscoveryServer) handleEds(con *XdsConnection, discReq *xdsapi.DiscoveryRequest) error {
	if discReq.ErrorDetail != nil {
		errCode := codes.Code(discReq.ErrorDetail.Code)
		adsLog.Warnf("ADS:EDS: ACK ERROR %s %s:%s", con.ConID, errCode.String(), discReq.ErrorDetail.GetMessage())
		incrementXDSRejects(edsReject, con.node.ID, errCode.String())
		return nil
	}
	clusters := discReq.GetResourceNames()
	if clusters == nil && discReq.ResponseNonce != "" {
		// There is no requirement that ACK includes clusters. The test doesn't.
		con.mu.Lock()
		con.EndpointNonceAcked = discReq.ResponseNonce
		con.mu.Unlock()
		return nil
	}

	// clusters and con.Clusters are all empty, this is not an ack and will do nothing.
	if len(clusters) == 0 && len(con.Clusters) == 0 {
		return nil
	}

	// Already got a list of endpoints to watch and it is the same as the request, this is an ack
	if listEqualUnordered(con.Clusters, clusters) {
		adsLog.Debugf("ADS:EDS: ACK %s %s %s", con.ConID, discReq.VersionInfo, discReq.ResponseNonce)
		if discReq.ResponseNonce != "" {
			con.mu.Lock()
			con.EndpointNonceAcked = discReq.ResponseNonce
			con.mu.Unlock()
		}
		return nil
	}

	con.Clusters = clusters
	adsLog.Debugf("ADS:EDS: REQ %s clusters:%d", con.ConID, len(con.Clusters))
	err := s.pushEds(s.globalPushContext(), con, versionInfo(), nil)
	if err != nil {
		return err
	}
	return nil
}

func (s *DiscoveryServer) handleRds(con *XdsConnection, discReq *xdsapi.DiscoveryRequest) error {
	if discReq.ErrorDetail != nil {
		errCode := codes.Code(discReq.ErrorDetail.Code)
		adsLog.Warnf("ADS:RDS: ACK ERROR %s %s:%s", con.ConID, errCode.String(), discReq.ErrorDetail.GetMessage())
		incrementXDSRejects(rdsReject, con.node.ID, errCode.String())
		return nil
	}
	routes := discReq.GetResourceNames()
	if discReq.ResponseNonce != "" {
		con.mu.RLock()
		routeNonceSent := con.RouteNonceSent
		routeVersionInfoSent := con.RouteVersionInfoSent
		con.mu.RUnlock()
		if routeNonceSent != "" && routeNonceSent != discReq.ResponseNonce {
			adsLog.Debugf("ADS:RDS: Expired nonce received %s, sent %s, received %s",
				con.ConID, routeNonceSent, discReq.ResponseNonce)
			rdsExpiredNonce.Increment()
			return nil
		}
		if discReq.VersionInfo == routeVersionInfoSent {
			if listEqualUnordered(con.Routes, routes) {
				adsLog.Debugf("ADS:RDS: ACK %s %s %s", con.ConID, discReq.VersionInfo, discReq.ResponseNonce)
				con.mu.Lock()
				con.RouteNonceAcked = discReq.ResponseNonce
				con.mu.Unlock()
				return nil
			}
		} else if len(routes) == 0 {
			// XDS protocol indicates an empty request means to send all route information
			// In practice we can just skip this request, as this seems to happen when
			// we don't have any routes to send.
			return nil
		}
	}
	con.Routes = routes
	adsLog.Debugf("ADS:RDS: REQ %s routes:%d", con.ConID, len(con.Routes))
	err := s.pushRoute(con, s.globalPushContext(), versionInfo())
	if err != nil {
		return err
	}
	return nil
}

// listEqualUnordered checks that two lists contain all the same elements
func listEqualUnordered(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	clusterSet := make(map[string]struct{}, len(a))
	for _, c := range a {
		clusterSet[c] = struct{}{}
	}
	for _, c := range b {
		_, f := clusterSet[c]
		if !f {
			return false
		}
	}
	return true
}

// update the node associated with the connection, after receiving a a packet from envoy, also adds the connection
// to the tracking map.
func (s *DiscoveryServer) initConnection(node *core.Node, con *XdsConnection) error {
	proxy, err := s.initProxy(node)
	if err != nil {
		return err
	}

	// Based on node metadata and version, we can associate a different generator.
	// TODO: use a map of generators, so it's easily customizable and to avoid deps
	proxy.Active = map[string]*model.WatchedResource{}
	if proxy.Metadata.Generator != "" {
		proxy.XdsResourceGenerator = s.Generators[proxy.Metadata.Generator]
	}

	// First request so initialize connection id and start tracking it.
	con.mu.Lock()
	con.node = proxy
	con.ConID = connectionID(node.Id)
	s.addCon(con.ConID, con)
	con.mu.Unlock()

	return nil
}

// initProxy initializes the Proxy from node.
func (s *DiscoveryServer) initProxy(node *core.Node) (*model.Proxy, error) {
	meta, err := model.ParseMetadata(node.Metadata)
	if err != nil {
		return nil, err
	}
	proxy, err := model.ParseServiceNodeWithMetadata(node.Id, meta)
	if err != nil {
		return nil, err
	}
	// Update the config namespace associated with this proxy
	proxy.ConfigNamespace = model.GetProxyConfigNamespace(proxy)

	if err = s.setProxyState(proxy, s.globalPushContext()); err != nil {
		return nil, err
	}

	// Get the locality from the proxy's service instances.
	// We expect all instances to have the same IP and therefore the same locality. So its enough to look at the first instance
	if len(proxy.ServiceInstances) > 0 {
		proxy.Locality = util.ConvertLocality(proxy.ServiceInstances[0].Endpoint.Locality.Label)
	}

	// If there is no locality in the registry then use the one sent as part of the discovery request.
	// This is not preferable as only the connected Pilot is aware of this proxies location, but it
	// can still help provide some client-side Envoy context when load balancing based on location.
	if util.IsLocalityEmpty(proxy.Locality) {
		proxy.Locality = node.Locality
	}

	// Discover supported IP Versions of proxy so that appropriate config can be delivered.
	proxy.DiscoverIPVersions()

	return proxy, nil
}

func (s *DiscoveryServer) updateProxy(proxy *model.Proxy, push *model.PushContext) error {
	if err := s.setProxyState(proxy, push); err != nil {
		return err
	}
	if util.IsLocalityEmpty(proxy.Locality) {
		// Get the locality from the proxy's service instances.
		// We expect all instances to have the same locality. So its enough to look at the first instance
		if len(proxy.ServiceInstances) > 0 {
			proxy.Locality = util.ConvertLocality(proxy.ServiceInstances[0].Endpoint.Locality.Label)
		}
	}

	return nil
}

func (s *DiscoveryServer) setProxyState(proxy *model.Proxy, push *model.PushContext) error {
	if err := proxy.SetWorkloadLabels(s.Env); err != nil {
		return err
	}

	if err := proxy.SetServiceInstances(push.ServiceDiscovery); err != nil {
		return err
	}

	// Precompute the sidecar scope and merged gateways associated with this proxy.
	// Saves compute cycles in networking code. Though this might be redundant sometimes, we still
	// have to compute this because as part of a config change, a new Sidecar could become
	// applicable to this proxy
	proxy.SetSidecarScope(push)
	proxy.SetGatewaysForProxy(push)
	return nil
}

// DeltaAggregatedResources is not implemented.
// Instead, Generators may send only updates/add, with Delete indicated by an empty spec.
// This works if both ends follow this model. For example EDS and the API generator follow this
// pattern.
//
// The delta protocol changes the request, adding unsubscribe/subscribe instead of sending full
// list of resources. On the response it adds 'removed resources' and sends changes for everything.
// TODO: we could implement this method if needed, the change is not very big.
func (s *DiscoveryServer) DeltaAggregatedResources(stream ads.AggregatedDiscoveryService_DeltaAggregatedResourcesServer) error {
	return status.Errorf(codes.Unimplemented, "not implemented")
}

// Compute and send the new configuration for a connection. This is blocking and may be slow
// for large configs. The method will hold a lock on con.pushMutex.
func (s *DiscoveryServer) pushConnection(con *XdsConnection, pushEv *XdsEvent) error {
	// TODO: update the service deps based on NetworkScope
	if !pushEv.full {
		edsUpdatedServices := model.ConfigNamesOfKind(pushEv.configsUpdated, model.ServiceEntryKind)

		if !ProxyNeedsPush(con.node, pushEv) {
			adsLog.Debugf("Skipping EDS push to %v, no updates required", con.ConID)
			return nil
		}
		// Push only EDS. This is indexed already - push immediately
		// (may need a throttle)
		if len(con.Clusters) > 0 && len(edsUpdatedServices) > 0 {
			if err := s.pushEds(pushEv.push, con, versionInfo(), edsUpdatedServices); err != nil {
				return err
			}
		}
		return nil
	}

	// Update Proxy with current information.
	if err := s.updateProxy(con.node, pushEv.push); err != nil {
		return nil
	}

	// This depends on SidecarScope updates, so it should be called after SetSidecarScope.
	if !ProxyNeedsPush(con.node, pushEv) {
		if con.node.XdsResourceGenerator != nil {
			// to verify if logic works on generator
			adsLog.Infof("Skipping generator push to %v, no updates required", con.ConID)
		} else {
			adsLog.Debugf("Skipping push to %v, no updates required", con.ConID)
		}

		if s.StatusReporter != nil {
			// this version of the config will never be distributed to this envoy because it is not a relevant diff.
			// inform distribution status reporter that this connection has been updated, because it effectively has
			for _, typeURL := range []string{ClusterType, ListenerType, RouteType, EndpointType} {
				s.StatusReporter.RegisterEvent(con.ConID, typeURL, pushEv.noncePrefix)
			}
		}
		return nil
	}

	adsLog.Infof("Pushing %v", con.ConID)

	// check version, suppress if changed.
	currentVersion := versionInfo()

	// When using Generator, the generic WatchedResource is used instead of the individual
	// 'LDSWatch', etc.
	// Each Generator is responsible for determining if the push event requires a push -
	// returning nil if the push is not needed.
	if con.node.XdsResourceGenerator != nil {
		for _, w := range con.node.Active {
			err := s.pushGeneratorV2(con, pushEv.push, currentVersion, w)
			if err != nil {
				return err
			}
		}
	}

	pushTypes := PushTypeFor(con.node, pushEv)

	if con.CDSWatch && pushTypes[CDS] {
		err := s.pushCds(con, pushEv.push, currentVersion)
		if err != nil {
			return err
		}
	} else if s.StatusReporter != nil {
		s.StatusReporter.RegisterEvent(con.ConID, ClusterType, pushEv.noncePrefix)
	}

	if len(con.Clusters) > 0 && pushTypes[EDS] {
		err := s.pushEds(pushEv.push, con, currentVersion, nil)
		if err != nil {
			return err
		}
	} else if s.StatusReporter != nil {
		s.StatusReporter.RegisterEvent(con.ConID, EndpointType, pushEv.noncePrefix)
	}
	if con.LDSWatch && pushTypes[LDS] {
		err := s.pushLds(con, pushEv.push, currentVersion)
		if err != nil {
			return err
		}
	} else if s.StatusReporter != nil {
		s.StatusReporter.RegisterEvent(con.ConID, ListenerType, pushEv.noncePrefix)
	}
	if len(con.Routes) > 0 && pushTypes[RDS] {
		err := s.pushRoute(con, pushEv.push, currentVersion)
		if err != nil {
			return err
		}
	} else if s.StatusReporter != nil {
		s.StatusReporter.RegisterEvent(con.ConID, RouteType, pushEv.noncePrefix)
	}
	proxiesConvergeDelay.Record(time.Since(pushEv.start).Seconds())
	return nil
}

func (s *DiscoveryServer) adsClientCount() int {
	s.adsClientsMutex.RLock()
	defer s.adsClientsMutex.RUnlock()
	return len(s.adsClients)
}

func (s *DiscoveryServer) ProxyUpdate(clusterID, ip string) {
	var connection *XdsConnection

	s.adsClientsMutex.RLock()
	for _, v := range s.adsClients {
		if v.node.ClusterID == clusterID && v.node.IPAddresses[0] == ip {
			connection = v
			break
		}

	}
	s.adsClientsMutex.RUnlock()

	// It is possible that the envoy has not connected to this pilot, maybe connected to another pilot
	if connection == nil {
		return
	}
	if adsLog.DebugEnabled() {
		currentlyPending := s.pushQueue.Pending()
		if currentlyPending != 0 {
			adsLog.Debugf("Starting new push while %v were still pending", currentlyPending)
		}
	}

	s.pushQueue.Enqueue(connection, &model.PushRequest{
		Full:   true,
		Push:   s.globalPushContext(),
		Start:  time.Now(),
		Reason: []model.TriggerReason{model.ProxyUpdate},
	})
}

// AdsPushAll will send updates to all nodes, for a full config or incremental EDS.
func AdsPushAll(s *DiscoveryServer) {
	s.AdsPushAll(versionInfo(), &model.PushRequest{
		Full:   true,
		Push:   s.globalPushContext(),
		Reason: []model.TriggerReason{model.DebugTrigger},
	})
}

// AdsPushAll implements old style invalidation, generated when any rule or endpoint changes.
// Primary code path is from v1 discoveryService.clearCache(), which is added as a handler
// to the model ConfigStorageCache and Controller.
func (s *DiscoveryServer) AdsPushAll(version string, req *model.PushRequest) {
	if !req.Full {
		s.edsIncremental(version, req)
		return
	}

	adsLog.Infof("XDS: Pushing:%s Services:%d ConnectedEndpoints:%d",
		version, len(req.Push.Services(nil)), s.adsClientCount())
	monServices.Record(float64(len(req.Push.Services(nil))))

	// Make sure the ConfigsUpdated map exists
	if req.ConfigsUpdated == nil {
		req.ConfigsUpdated = make(map[model.ConfigKey]struct{})
	}

	s.startPush(req)
}

// Send a signal to all connections, with a push event.
func (s *DiscoveryServer) startPush(req *model.PushRequest) {

	// Push config changes, iterating over connected envoys. This cover ADS and EDS(0.7), both share
	// the same connection table
	s.adsClientsMutex.RLock()
	// Create a temp map to avoid locking the add/remove
	pending := []*XdsConnection{}
	for _, v := range s.adsClients {
		pending = append(pending, v)
	}
	s.adsClientsMutex.RUnlock()

	if adsLog.DebugEnabled() {
		currentlyPending := s.pushQueue.Pending()
		if currentlyPending != 0 {
			adsLog.Infof("Starting new push while %v were still pending", currentlyPending)
		}
	}
	req.Start = time.Now()
	for _, p := range pending {
		s.pushQueue.Enqueue(p, req)
	}
}

func (s *DiscoveryServer) addCon(conID string, con *XdsConnection) {
	s.adsClientsMutex.Lock()
	defer s.adsClientsMutex.Unlock()
	s.adsClients[conID] = con
	recordXDSClients(con.node.Metadata.IstioVersion, 1)
}

func (s *DiscoveryServer) removeCon(conID string) {
	s.adsClientsMutex.Lock()
	defer s.adsClientsMutex.Unlock()

	if con, exist := s.adsClients[conID]; !exist {
		adsLog.Errorf("ADS: Removing connection for non-existing node:%v.", conID)
		totalXDSInternalErrors.Increment()
	} else {
		delete(s.adsClients, conID)
		recordXDSClients(con.node.Metadata.IstioVersion, -1)
	}
	if s.StatusReporter != nil {
		go s.StatusReporter.RegisterDisconnect(conID, []string{ClusterType, ListenerType, RouteType, EndpointType})
	}
}

// Send with timeout
func (conn *XdsConnection) send(res *xdsapi.DiscoveryResponse) error {
	done := make(chan error, 1)
	// hardcoded for now - not sure if we need a setting
	t := time.NewTimer(SendTimeout)
	go func() {
		err := conn.stream.Send(res)
		conn.mu.Lock()
		if res.Nonce != "" {
			switch res.TypeUrl {
			case ClusterType, v3.ClusterType:
				conn.ClusterNonceSent = res.Nonce
			case ListenerType, v3.ListenerType:
				conn.ListenerNonceSent = res.Nonce
			case RouteType, v3.RouteType:
				conn.RouteNonceSent = res.Nonce
			case EndpointType, v3.EndpointType:
				conn.EndpointNonceSent = res.Nonce
			default:
				adsLog.Warnf("sent unknown XDS type: %v", res.TypeUrl)
			}
		}
		if res.TypeUrl == RouteType || res.TypeUrl == v3.RouteType {
			conn.RouteVersionInfoSent = res.VersionInfo
		}
		conn.mu.Unlock()
		done <- err
	}()
	select {
	case <-t.C:
		// TODO: wait for ACK
		adsLog.Infof("Timeout writing %s", conn.ConID)
		xdsResponseWriteTimeouts.Increment()
		return errors.New("timeout sending")
	case err := <-done:
		t.Stop()
		return err
	}
}
