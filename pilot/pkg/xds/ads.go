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
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync/atomic"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"istio.io/istio/pilot/pkg/controller/workloadentry"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/spiffe"
	istiolog "istio.io/pkg/log"
)

var (
	adsLog = istiolog.RegisterScope("ads", "ads debugging", 0)

	// sendTimeout is the max time to wait for a ADS send to complete. This helps detect
	// clients in a bad state (not reading). In future it may include checking for ACK
	sendTimeout = features.XdsPushSendTimeout

	// Tracks connections, increment on each new connection.
	connectionNumber = int64(0)
)

// DiscoveryStream is a server interface for XDS.
type DiscoveryStream = discovery.AggregatedDiscoveryService_StreamAggregatedResourcesServer

// DiscoveryClient is a client interface for XDS.
type DiscoveryClient = discovery.AggregatedDiscoveryService_StreamAggregatedResourcesClient

// Connection holds information about connected client.
type Connection struct {
	// PeerAddr is the address of the client, from network layer.
	PeerAddr string

	// Defines associated identities for the connection
	Identities []string

	// Time of connection, for debugging
	Connect time.Time

	// ConID is the connection identifier, used as a key in the connection table.
	// Currently based on the node name and a counter.
	ConID string

	// proxy is the client to which this connection is established.
	proxy *model.Proxy

	// Sending on this channel results in a push.
	pushChannel chan *Event

	// Both ADS and SDS streams implement this interface
	stream DiscoveryStream

	// Original node metadata, to avoid unmarshal/marshal.
	// This is included in internal events.
	node *core.Node

	// stop can be used to end the connection manually via debug endpoints. Only to be used for testing.
	stop chan struct{}

	// blockedPushes is a map of TypeUrl to push request. This is set when we attempt to push to a busy Envoy
	// (last push not ACKed). When we get an ACK from Envoy, if the type is populated here, we will trigger
	// the push.
	blockedPushes map[string]*model.PushRequest
}

// Event represents a config or registry event that results in a push.
type Event struct {
	// pushRequest PushRequest to use for the push.
	pushRequest *model.PushRequest

	// function to call once a push is finished. This must be called or future changes may be blocked.
	done func()
}

func newConnection(peerAddr string, stream DiscoveryStream) *Connection {
	return &Connection{
		pushChannel:   make(chan *Event),
		stop:          make(chan struct{}),
		PeerAddr:      peerAddr,
		Connect:       time.Now(),
		stream:        stream,
		blockedPushes: map[string]*model.PushRequest{},
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

func (s *DiscoveryServer) receive(con *Connection, reqChannel chan *discovery.DiscoveryRequest, errP *error) {
	defer close(reqChannel) // indicates close of the remote side.
	firstReq := true
	for {
		req, err := con.stream.Recv()
		if err != nil {
			if isExpectedGRPCError(err) {
				adsLog.Infof("ADS: %q %s terminated %v", con.PeerAddr, con.ConID, err)
				return
			}
			*errP = err
			adsLog.Errorf("ADS: %q %s terminated with error: %v", con.PeerAddr, con.ConID, err)
			totalXDSInternalErrors.Increment()
			return
		}
		// This should be only set for the first request. The node id may not be set - for example malicious clients.
		if firstReq {
			firstReq = false
			if req.Node == nil || req.Node.Id == "" {
				*errP = errors.New("missing node ID")
				return
			}
			// TODO: We should validate that the namespace in the cert matches the claimed namespace in metadata.
			if err := s.initConnection(req.Node, con); err != nil {
				*errP = err
				return
			}
			adsLog.Infof("ADS: new connection for node:%s", con.ConID)
			defer func() {
				s.removeCon(con.ConID)
				s.WorkloadEntryController.QueueUnregisterWorkload(con.proxy)
			}()
		}

		select {
		case reqChannel <- req:
		case <-con.stream.Context().Done():
			adsLog.Infof("ADS: %q %s terminated with stream closed", con.PeerAddr, con.ConID)
			return
		}
	}
}

// processRequest is handling one request. This is currently called from the 'main' thread, which also
// handles 'push' requests and close - the code will eventually call the 'push' code, and it needs more mutex
// protection. Original code avoided the mutexes by doing both 'push' and 'process requests' in same thread.
func (s *DiscoveryServer) processRequest(req *discovery.DiscoveryRequest, con *Connection) error {
	if !s.preProcessRequest(con.proxy, req) {
		return nil
	}

	if s.StatusReporter != nil {
		s.StatusReporter.RegisterEvent(con.ConID, req.TypeUrl, req.ResponseNonce)
	}
	shouldRespond := s.shouldRespond(con, req)

	// Check if we have a blocked push. If this was an ACK, we will send it. Either way we remove the blocked push
	// as we will send a push.
	con.proxy.Lock()
	request, haveBlockedPush := con.blockedPushes[req.TypeUrl]
	delete(con.blockedPushes, req.TypeUrl)
	con.proxy.Unlock()

	if shouldRespond {
		// This is a request, trigger a full push for this type
		// Override the blocked push (if it exists), as this full push is guaranteed to be a superset
		// of what we would have pushed from the blocked push.
		request = &model.PushRequest{Full: true}
	} else if !haveBlockedPush {
		// This is an ACK, no delayed push
		// Return immediately, no action needed
		return nil
	} else {
		// we have a blocked push which we will use
		adsLog.Debugf("%s: DEQUEUE for node:%s", v3.GetShortType(req.TypeUrl), con.proxy.ID)
	}

	push := s.globalPushContext()

	return s.pushXds(con, push, versionInfo(), con.Watched(req.TypeUrl), request)
}

// StreamAggregatedResources implements the ADS interface.
func (s *DiscoveryServer) StreamAggregatedResources(stream discovery.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	// Check if server is ready to accept clients and process new requests.
	// Currently ready means caches have been synced and hence can build
	// clusters correctly. Without this check, InitContext() call below would
	// initialize with empty config, leading to reconnected Envoys loosing
	// configuration. This is an additional safety check inaddition to adding
	// cachesSynced logic to readiness probe to handle cases where kube-proxy
	// ip tables update latencies.
	// See https://github.com/istio/istio/issues/25495.
	if !s.IsServerReady() {
		return errors.New("server is not ready to serve discovery information")
	}

	ctx := stream.Context()
	peerAddr := "0.0.0.0"
	if peerInfo, ok := peer.FromContext(ctx); ok {
		peerAddr = peerInfo.Addr.String()
	}

	ids, err := s.authenticate(ctx)
	if err != nil {
		return err
	}
	if ids != nil {
		adsLog.Debugf("Authenticated XDS: %v with identity %v", peerAddr, ids)
	} else {
		adsLog.Debug("Unauthenticated XDS: ", peerAddr)
	}

	// InitContext returns immediately if the context was already initialized.
	if err = s.globalPushContext().InitContext(s.Env, nil, nil); err != nil {
		// Error accessing the data - log and close, maybe a different pilot replica
		// has more luck
		adsLog.Warnf("Error reading config %v", err)
		return err
	}
	con := newConnection(peerAddr, stream)
	con.Identities = ids

	// Do not call: defer close(con.pushChannel). The push channel will be garbage collected
	// when the connection is no longer used. Closing the channel can cause subtle race conditions
	// with push. According to the spec: "It's only necessary to close a channel when it is important
	// to tell the receiving goroutines that all data have been sent."

	// Reading from a stream is a blocking operation. Each connection needs to read
	// discovery requests and wait for push commands on config change, so we add a
	// go routine. If go grpc adds gochannel support for streams this will not be needed.
	// This also detects close.
	var receiveError error
	reqChannel := make(chan *discovery.DiscoveryRequest, 1)
	go s.receive(con, reqChannel, &receiveError)

	for {
		// Block until either a request is received or a push is triggered.
		// We need 2 go routines because 'read' blocks in Recv().
		//
		// To avoid 2 routines, we tried to have Recv() in StreamAggregateResource - and the push
		// on different short-lived go routines started when the push is happening. This would cut in 1/2
		// the number of long-running go routines, since push is throttled. The main problem is with
		// closing - the current gRPC library didn't allow closing the stream.
		select {
		case req, ok := <-reqChannel:
			if !ok {
				// Remote side closed connection or error processing the request.
				return receiveError
			}
			// processRequest is calling pushXXX, accessing common structs with pushConnection.
			// Adding sync is the second issue to be resolved if we want to save 1/2 of the threads.
			err := s.processRequest(req, con)
			if err != nil {
				return err
			}

		case pushEv := <-con.pushChannel:
			// TODO: possible race condition: if a config change happens while the envoy
			// was getting the initial config, between LDS and RDS, the push will miss the
			// monitored 'routes'. Same for CDS/EDS interval. It is very tricky to handle
			// due to the protocol - but the periodic push recovers from it.
			err := s.pushConnection(con, pushEv)
			pushEv.done()
			if err != nil {
				return err
			}
		case <-con.stop:
			return nil
		}
	}
}

// shouldRespond determines whether this request needs to be responded back. It applies the ack/nack rules as per xds protocol
// using WatchedResource for previous state and discovery request for the current state.
func (s *DiscoveryServer) shouldRespond(con *Connection, request *discovery.DiscoveryRequest) bool {
	stype := v3.GetShortType(request.TypeUrl)

	// If there is an error in request that means previous response is erroneous.
	// We do not have to respond in that case. In this case request's version info
	// will be different from the version sent. But it is fragile to rely on that.
	if request.ErrorDetail != nil {
		errCode := codes.Code(request.ErrorDetail.Code)
		adsLog.Warnf("ADS:%s: ACK ERROR %s %s:%s", stype, con.ConID, errCode.String(), request.ErrorDetail.GetMessage())
		incrementXDSRejects(request.TypeUrl, con.proxy.ID, errCode.String())
		con.proxy.Lock()
		con.proxy.WatchedResources[request.TypeUrl].NonceNacked = request.ResponseNonce
		con.proxy.Unlock()
		return false
	}

	if shouldUnsubscribe(request) {
		adsLog.Debugf("ADS:%s: UNSUBSCRIBE %s %s %s", stype, con.ConID, request.VersionInfo, request.ResponseNonce)
		con.proxy.Lock()
		delete(con.proxy.WatchedResources, request.TypeUrl)
		con.proxy.Unlock()
		return false
	}

	// This is first request - initialize typeUrl watches.
	if request.ResponseNonce == "" {
		adsLog.Debugf("ADS:%s: INIT %s %s %s", stype, con.ConID, request.VersionInfo, request.ResponseNonce)
		con.proxy.Lock()
		con.proxy.WatchedResources[request.TypeUrl] = &model.WatchedResource{TypeUrl: request.TypeUrl, ResourceNames: request.ResourceNames, LastRequest: request}
		con.proxy.Unlock()
		return true
	}

	con.proxy.RLock()
	previousInfo := con.proxy.WatchedResources[request.TypeUrl]
	con.proxy.RUnlock()

	// This is a case of Envoy reconnecting Istiod i.e. Istiod does not have
	// information about this typeUrl, but Envoy sends response nonce - either
	// because Istiod is restarted or Envoy disconnects and reconnects.
	// We should always respond with the current resource names.
	if previousInfo == nil {
		adsLog.Debugf("ADS:%s: RECONNECT %s %s %s", stype, con.ConID, request.VersionInfo, request.ResponseNonce)
		con.proxy.Lock()
		con.proxy.WatchedResources[request.TypeUrl] = &model.WatchedResource{TypeUrl: request.TypeUrl, ResourceNames: request.ResourceNames, LastRequest: request}
		con.proxy.Unlock()
		return true
	}

	// If there is mismatch in the nonce, that is a case of expired/stale nonce.
	// A nonce becomes stale following a newer nonce being sent to Envoy.
	if request.ResponseNonce != previousInfo.NonceSent {
		adsLog.Debugf("ADS:%s: REQ %s Expired nonce received %s, sent %s", stype,
			con.ConID, request.ResponseNonce, previousInfo.NonceSent)
		xdsExpiredNonce.With(typeTag.Value(v3.GetMetricType(request.TypeUrl))).Increment()
		con.proxy.Lock()
		con.proxy.WatchedResources[request.TypeUrl].NonceNacked = ""
		con.proxy.WatchedResources[request.TypeUrl].LastRequest = request
		con.proxy.Unlock()
		return false
	}

	// If it comes here, that means nonce match. This an ACK. We should record
	// the ack details and respond if there is a change in resource names.
	con.proxy.Lock()
	previousResources := con.proxy.WatchedResources[request.TypeUrl].ResourceNames
	con.proxy.WatchedResources[request.TypeUrl].VersionAcked = request.VersionInfo
	con.proxy.WatchedResources[request.TypeUrl].NonceAcked = request.ResponseNonce
	con.proxy.WatchedResources[request.TypeUrl].NonceNacked = ""
	con.proxy.WatchedResources[request.TypeUrl].ResourceNames = request.ResourceNames
	con.proxy.WatchedResources[request.TypeUrl].LastRequest = request
	con.proxy.Unlock()

	// Envoy can send two DiscoveryRequests with same version and nonce
	// when it detects a new resource. We should respond if they change.
	if listEqualUnordered(previousResources, request.ResourceNames) {
		adsLog.Debugf("ADS:%s: ACK %s %s %s", stype, con.ConID, request.VersionInfo, request.ResponseNonce)
		return false
	}
	adsLog.Debugf("ADS:%s: RESOURCE CHANGE previous resources: %v, new resources: %v %s %s %s", stype,
		previousResources, request.ResourceNames, con.ConID, request.VersionInfo, request.ResponseNonce)

	return true
}

// shouldUnsubscribe checks if we should unsubscribe. This is done when Envoy is
// no longer watching. For example, we remove all RDS references, we will
// unsubscribe from RDS. NOTE: This may happen as part of the initial request. If
// there are no routes needed, Envoy will send an empty request, which this
// properly handles by not adding it to the watched resource list.
func shouldUnsubscribe(request *discovery.DiscoveryRequest) bool {
	return len(request.ResourceNames) == 0 && !isWildcardTypeURL(request.TypeUrl)
}

// isWildcardTypeURL checks whether a given type is a wildcard type
// https://www.envoyproxy.io/docs/envoy/latest/api-docs/xds_protocol#how-the-client-specifies-what-resources-to-return
// If the list of resource names becomes empty, that means that the client is no
// longer interested in any resources of the specified type. For Listener and
// Cluster resource types, there is also a “wildcard” mode, which is triggered
// when the initial request on the stream for that resource type contains no
// resource names.
func isWildcardTypeURL(typeURL string) bool {
	switch typeURL {
	case v3.SecretType, v3.EndpointType, v3.RouteType:
		// By XDS spec, these are not wildcard
		return false
	case v3.ClusterType, v3.ListenerType:
		// By XDS spec, these are wildcard
		return true
	default:
		// All of our internal types use wildcard semantics
		return true
	}
}

// listEqualUnordered checks that two lists contain all the same elements
func listEqualUnordered(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	first := make(map[string]struct{}, len(a))
	for _, c := range a {
		first[c] = struct{}{}
	}
	for _, c := range b {
		_, f := first[c]
		if !f {
			return false
		}
	}
	return true
}

// update the node associated with the connection, after receiving a a packet from envoy, also adds the connection
// to the tracking map.
func (s *DiscoveryServer) initConnection(node *core.Node, con *Connection) error {
	proxy, err := s.initProxy(node, con)
	if err != nil {
		return err
	}

	proxy.WatchedResources = map[string]*model.WatchedResource{}
	// Based on node metadata and version, we can associate a different generator.
	if proxy.Metadata.Generator != "" {
		proxy.XdsResourceGenerator = s.Generators[proxy.Metadata.Generator]
	}

	// First request so initialize connection id and start tracking it.
	con.proxy = proxy
	con.ConID = connectionID(node.Id)
	con.node = node

	if features.EnableXDSIdentityCheck && con.Identities != nil {
		// TODO: allow locking down, rejecting unauthenticated requests.
		id, err := checkConnectionIdentity(con)
		if err != nil {
			adsLog.Warnf("Unauthorized XDS: %v with identity %v: %v", con.PeerAddr, con.Identities, err)
			return fmt.Errorf("authorization failed: %v", err)
		}
		con.proxy.VerifiedIdentity = id
	}

	s.addCon(con.ConID, con)

	return nil
}

func checkConnectionIdentity(con *Connection) (*spiffe.Identity, error) {
	for _, rawID := range con.Identities {
		spiffeID, err := spiffe.ParseIdentity(rawID)
		if err != nil {
			continue
		}
		if con.proxy.ConfigNamespace != "" && spiffeID.Namespace != con.proxy.ConfigNamespace {
			continue
		}
		if con.proxy.Metadata.ServiceAccount != "" && spiffeID.ServiceAccount != con.proxy.Metadata.ServiceAccount {
			continue
		}
		return &spiffeID, nil
	}
	return nil, fmt.Errorf("no identities (%v) matched %v/%v", con.Identities, con.proxy.ConfigNamespace, con.proxy.Metadata.ServiceAccount)
}

func connectionID(node string) string {
	id := atomic.AddInt64(&connectionNumber, 1)
	return node + "-" + strconv.FormatInt(id, 10)
}

// initProxy initializes the Proxy from node.
func (s *DiscoveryServer) initProxy(node *core.Node, con *Connection) (*model.Proxy, error) {
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

	// this should be done before we look for service instances, but after we load metadata
	// TODO fix check in kubecontroller treat echo VMs like there isn't a pod
	if err := s.WorkloadEntryController.RegisterWorkload(proxy, con.Connect); err != nil {
		return nil, err
	}
	s.setProxyState(proxy, s.globalPushContext())

	// Get the locality from the proxy's service instances.
	// We expect all instances to have the same IP and therefore the same locality.
	// So its enough to look at the first instance.
	if len(proxy.ServiceInstances) > 0 {
		proxy.Locality = util.ConvertLocality(proxy.ServiceInstances[0].Endpoint.Locality.Label)
	}

	// If there is no locality in the registry then use the one sent as part of the discovery request.
	// This is not preferable as only the connected Pilot is aware of this proxies location, but it
	// can still help provide some client-side Envoy context when load balancing based on location.
	if util.IsLocalityEmpty(proxy.Locality) {
		proxy.Locality = &core.Locality{
			Region:  node.Locality.GetRegion(),
			Zone:    node.Locality.GetZone(),
			SubZone: node.Locality.GetSubZone(),
		}
	}

	// Discover supported IP Versions of proxy so that appropriate config can be delivered.
	proxy.DiscoverIPVersions()

	return proxy, nil
}

func (s *DiscoveryServer) updateProxy(proxy *model.Proxy, push *model.PushContext) {
	s.setProxyState(proxy, push)
	if util.IsLocalityEmpty(proxy.Locality) {
		// Get the locality from the proxy's service instances.
		// We expect all instances to have the same locality. So its enough to look at the first instance
		if len(proxy.ServiceInstances) > 0 {
			proxy.Locality = util.ConvertLocality(proxy.ServiceInstances[0].Endpoint.Locality.Label)
		}
	}
}

func (s *DiscoveryServer) setProxyState(proxy *model.Proxy, push *model.PushContext) {
	proxy.SetWorkloadLabels(s.Env)
	proxy.SetServiceInstances(push.ServiceDiscovery)

	// Precompute the sidecar scope and merged gateways associated with this proxy.
	// Saves compute cycles in networking code. Though this might be redundant sometimes, we still
	// have to compute this because as part of a config change, a new Sidecar could become
	// applicable to this proxy
	proxy.SetSidecarScope(push)
	proxy.SetGatewaysForProxy(push)
}

// pre-process request. returns whether or not to continue.
func (s *DiscoveryServer) preProcessRequest(proxy *model.Proxy, req *discovery.DiscoveryRequest) bool {
	if req.TypeUrl == v3.HealthInfoType {
		if features.WorkloadEntryHealthChecks {
			event := workloadentry.HealthEvent{}
			if req.ErrorDetail == nil {
				event.Healthy = true
			} else {
				event.Healthy = false
				event.Message = req.ErrorDetail.Message
			}
			s.WorkloadEntryController.UpdateWorkloadEntryHealth(proxy, event)
		}
		return false
	}
	return true
}

// DeltaAggregatedResources is not implemented.
// Instead, Generators may send only updates/add, with Delete indicated by an empty spec.
// This works if both ends follow this model. For example EDS and the API generator follow this
// pattern.
//
// The delta protocol changes the request, adding unsubscribe/subscribe instead of sending full
// list of resources. On the response it adds 'removed resources' and sends changes for everything.
// TODO: we could implement this method if needed, the change is not very big.
func (s *DiscoveryServer) DeltaAggregatedResources(stream discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesServer) error {
	return status.Errorf(codes.Unimplemented, "not implemented")
}

// Compute and send the new configuration for a connection. This is blocking and may be slow
// for large configs. The method will hold a lock on con.pushMutex.
func (s *DiscoveryServer) pushConnection(con *Connection, pushEv *Event) error {
	pushRequest := pushEv.pushRequest

	if pushRequest.Full {
		// Update Proxy with current information.
		s.updateProxy(con.proxy, pushRequest.Push)
	}

	if !ProxyNeedsPush(con.proxy, pushEv) {
		adsLog.Debugf("Skipping push to %v, no updates required", con.ConID)
		if pushRequest.Full {
			// Only report for full versions, incremental pushes do not have a new version
			reportAllEvents(s.StatusReporter, con.ConID, pushRequest.Push.Version, nil)
		}
		return nil
	}

	currentVersion := versionInfo()

	// Send pushes to all generators
	// Each Generator is responsible for determining if the push event requires a push
	for _, w := range getWatchedResources(con.proxy.WatchedResources) {
		if !features.EnableFlowControl {
			// Always send the push if flow control disabled
			if err := s.pushXds(con, pushRequest.Push, currentVersion, w, pushRequest); err != nil {
				return err
			}
			continue
		}
		// If flow control is enabled, we will only push if we got an ACK for the previous response
		synced, timeout := con.Synced(w.TypeUrl)
		if !synced && timeout {
			// We are not synced, but we have been stuck for too long. We will trigger the push anyways to
			// avoid any scenario where this may deadlock.
			// This can possibly be removed in the future if we find this never causes issues
			totalDelayedPushes.With(typeTag.Value(v3.GetMetricType(w.TypeUrl))).Increment()
			adsLog.Warnf("%s: QUEUE TIMEOUT for node:%s", v3.GetShortType(w.TypeUrl), con.proxy.ID)
		}
		if synced || timeout {
			// Send the push now
			if err := s.pushXds(con, pushRequest.Push, currentVersion, w, pushRequest); err != nil {
				return err
			}
		} else {
			// The type is not yet synced. Instead of pushing now, which may overload Envoy,
			// we will wait until the last push is ACKed and trigger the push. See
			// https://github.com/istio/istio/issues/25685 for details on the performance
			// impact of sending pushes before Envoy ACKs.
			totalDelayedPushes.With(typeTag.Value(v3.GetMetricType(w.TypeUrl))).Increment()
			adsLog.Debugf("%s: QUEUE for node:%s", v3.GetShortType(w.TypeUrl), con.proxy.ID)
			con.proxy.Lock()
			con.blockedPushes[w.TypeUrl] = con.blockedPushes[w.TypeUrl].Merge(pushEv.pushRequest)
			con.proxy.Unlock()
		}
	}
	if pushRequest.Full {
		// Report all events for unwatched resources. Watched resources will be reported in pushXds or on ack.
		reportAllEvents(s.StatusReporter, con.ConID, pushRequest.Push.Version, con.proxy.WatchedResources)
	}

	proxiesConvergeDelay.Record(time.Since(pushRequest.Start).Seconds())
	return nil
}

// PushOrder defines the order that updates will be pushed in. Any types not listed here will be pushed in random
// order after the types listed here
var PushOrder = []string{v3.ClusterType, v3.EndpointType, v3.ListenerType, v3.RouteType, v3.SecretType}
var KnownPushOrder = map[string]struct{}{
	v3.ClusterType:  {},
	v3.EndpointType: {},
	v3.ListenerType: {},
	v3.RouteType:    {},
	v3.SecretType:   {},
}

func getWatchedResources(resources map[string]*model.WatchedResource) []*model.WatchedResource {
	wr := make([]*model.WatchedResource, 0, len(resources))
	// first add all known types, in order
	for _, tp := range PushOrder {
		if w, f := resources[tp]; f {
			wr = append(wr, w)
		}
	}
	// Then add any undeclared types
	for tp, w := range resources {
		if _, f := KnownPushOrder[tp]; !f {
			wr = append(wr, w)
		}
	}
	return wr
}

func reportAllEvents(s DistributionStatusCache, id, version string, ignored map[string]*model.WatchedResource) {
	if s == nil {
		return
	}
	// this version of the config will never be distributed to this envoy because it is not a relevant diff.
	// inform distribution status reporter that this connection has been updated, because it effectively has
	for distributionType := range AllEventTypes {
		if _, f := ignored[distributionType]; f {
			// Skip this type
			continue
		}
		s.RegisterEvent(id, distributionType, version)
	}
}

func (s *DiscoveryServer) adsClientCount() int {
	s.adsClientsMutex.RLock()
	defer s.adsClientsMutex.RUnlock()
	return len(s.adsClients)
}

func (s *DiscoveryServer) ProxyUpdate(clusterID, ip string) {
	var connection *Connection

	for _, v := range s.Clients() {
		if v.proxy.Metadata.ClusterID == clusterID && v.proxy.IPAddresses[0] == ip {
			connection = v
			break
		}
	}

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
	// If we don't know what updated, cannot safely cache. Clear the whole cache
	if len(req.ConfigsUpdated) == 0 {
		s.Cache.ClearAll()
	} else {
		// Otherwise, just clear the updated configs
		s.Cache.Clear(req.ConfigsUpdated)
	}
	if !req.Full {
		adsLog.Infof("XDS: Incremental Pushing:%s ConnectedEndpoints:%d",
			version, s.adsClientCount())
	} else {
		totalService := len(req.Push.Services(nil))
		adsLog.Infof("XDS: Pushing:%s Services:%d ConnectedEndpoints:%d",
			version, totalService, s.adsClientCount())
		monServices.Record(float64(totalService))

		// Make sure the ConfigsUpdated map exists
		if req.ConfigsUpdated == nil {
			req.ConfigsUpdated = make(map[model.ConfigKey]struct{})
		}
	}

	s.startPush(req)
}

// Send a signal to all connections, with a push event.
func (s *DiscoveryServer) startPush(req *model.PushRequest) {
	// Push config changes, iterating over connected envoys. This cover ADS and EDS(0.7), both share
	// the same connection table

	if adsLog.DebugEnabled() {
		currentlyPending := s.pushQueue.Pending()
		if currentlyPending != 0 {
			adsLog.Infof("Starting new push while %v were still pending", currentlyPending)
		}
	}
	req.Start = time.Now()
	for _, p := range s.Clients() {
		s.pushQueue.Enqueue(p, req)
	}
}

func (s *DiscoveryServer) addCon(conID string, con *Connection) {
	s.adsClientsMutex.Lock()
	defer s.adsClientsMutex.Unlock()
	s.adsClients[conID] = con
	recordXDSClients(con.proxy.Metadata.IstioVersion, 1)
}

func (s *DiscoveryServer) removeCon(conID string) {
	s.adsClientsMutex.Lock()
	defer s.adsClientsMutex.Unlock()

	if con, exist := s.adsClients[conID]; !exist {
		adsLog.Errorf("ADS: Removing connection for non-existing node:%v.", conID)
		totalXDSInternalErrors.Increment()
	} else {
		delete(s.adsClients, conID)
		recordXDSClients(con.proxy.Metadata.IstioVersion, -1)
	}

	if s.StatusReporter != nil {
		s.StatusReporter.RegisterDisconnect(conID, AllEventTypesList)
	}
}

// Send with timeout
func (conn *Connection) send(res *discovery.DiscoveryResponse) error {
	errChan := make(chan error, 1)

	// sendTimeout may be modified via environment
	t := time.NewTimer(sendTimeout)
	go func() {
		start := time.Now()
		defer func() { recordSendTime(time.Since(start)) }()
		errChan <- conn.stream.Send(res)
		close(errChan)
	}()

	select {
	case <-t.C:
		adsLog.Infof("Timeout writing %s", conn.ConID)
		xdsResponseWriteTimeouts.Increment()
		return status.Errorf(codes.DeadlineExceeded, "timeout sending")
	case err := <-errChan:
		if err == nil {
			sz := 0
			for _, rc := range res.Resources {
				sz += len(rc.Value)
			}
			conn.proxy.Lock()
			if res.Nonce != "" {
				if conn.proxy.WatchedResources[res.TypeUrl] == nil {
					conn.proxy.WatchedResources[res.TypeUrl] = &model.WatchedResource{TypeUrl: res.TypeUrl}
				}
				conn.proxy.WatchedResources[res.TypeUrl].NonceSent = res.Nonce
				conn.proxy.WatchedResources[res.TypeUrl].VersionSent = res.VersionInfo
				conn.proxy.WatchedResources[res.TypeUrl].LastSent = time.Now()
				conn.proxy.WatchedResources[res.TypeUrl].LastSize = sz
			}
			conn.proxy.Unlock()
		}
		// To ensure the channel is empty after a call to Stop, check the
		// return value and drain the channel (from Stop docs).
		if !t.Stop() {
			<-t.C
		}
		return err
	}
}

// nolint
// Synced checks if the type has been synced, meaning the most recent push was ACKed
func (conn *Connection) Synced(typeUrl string) (bool, bool) {
	conn.proxy.RLock()
	defer conn.proxy.RUnlock()
	acked := conn.proxy.WatchedResources[typeUrl].NonceAcked
	sent := conn.proxy.WatchedResources[typeUrl].NonceSent
	nacked := conn.proxy.WatchedResources[typeUrl].NonceNacked != ""
	sendTime := conn.proxy.WatchedResources[typeUrl].LastSent
	return nacked || acked == sent, time.Since(sendTime) > features.FlowControlTimeout
}

// nolint
func (conn *Connection) NonceAcked(typeUrl string) string {
	conn.proxy.RLock()
	defer conn.proxy.RUnlock()
	if conn.proxy.WatchedResources != nil && conn.proxy.WatchedResources[typeUrl] != nil {
		return conn.proxy.WatchedResources[typeUrl].NonceAcked
	}
	return ""
}

// nolint
func (conn *Connection) NonceSent(typeUrl string) string {
	conn.proxy.RLock()
	defer conn.proxy.RUnlock()
	if conn.proxy.WatchedResources != nil && conn.proxy.WatchedResources[typeUrl] != nil {
		return conn.proxy.WatchedResources[typeUrl].NonceSent
	}
	return ""
}

func (conn *Connection) Clusters() []string {
	conn.proxy.RLock()
	defer conn.proxy.RUnlock()
	if conn.proxy.WatchedResources != nil && conn.proxy.WatchedResources[v3.EndpointType] != nil {
		return conn.proxy.WatchedResources[v3.EndpointType].ResourceNames
	}
	return []string{}
}

func (conn *Connection) Routes() []string {
	conn.proxy.RLock()
	defer conn.proxy.RUnlock()
	if conn.proxy.WatchedResources != nil && conn.proxy.WatchedResources[v3.RouteType] != nil {
		return conn.proxy.WatchedResources[v3.RouteType].ResourceNames
	}
	return []string{}
}

// nolint
func (conn *Connection) Watching(typeUrl string) bool {
	conn.proxy.RLock()
	defer conn.proxy.RUnlock()
	if conn.proxy.WatchedResources != nil && conn.proxy.WatchedResources[typeUrl] != nil {
		return true
	}
	return false
}

// nolint
func (conn *Connection) Watched(typeUrl string) *model.WatchedResource {
	conn.proxy.RLock()
	defer conn.proxy.RUnlock()
	if conn.proxy.WatchedResources != nil && conn.proxy.WatchedResources[typeUrl] != nil {
		return conn.proxy.WatchedResources[typeUrl]
	}
	return nil
}

func (conn *Connection) Stop() {
	conn.stop <- struct{}{}
}
