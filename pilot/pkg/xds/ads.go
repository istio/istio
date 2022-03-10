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
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	uatomic "go.uber.org/atomic"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"istio.io/istio/pilot/pkg/controller/workloadentry"
	"istio.io/istio/pilot/pkg/features"
	istiogrpc "istio.io/istio/pilot/pkg/grpc"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	labelutil "istio.io/istio/pilot/pkg/serviceregistry/util/label"
	"istio.io/istio/pilot/pkg/util/sets"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/pkg/env"
	istiolog "istio.io/pkg/log"
)

var (
	log = istiolog.RegisterScope("ads", "ads debugging", 0)

	// Tracks connections, increment on each new connection.
	connectionNumber = int64(0)
)

// Used only when running in KNative, to handle the load balancing behavior.
var firstRequest = uatomic.NewBool(true)

var knativeEnv = env.RegisterStringVar("K_REVISION", "",
	"KNative revision, set if running in knative").Get()

// DiscoveryStream is a server interface for XDS.
type DiscoveryStream = discovery.AggregatedDiscoveryService_StreamAggregatedResourcesServer

// DeltaDiscoveryStream is a server interface for Delta XDS.
type DeltaDiscoveryStream = discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesServer

// DiscoveryClient is a client interface for XDS.
type DiscoveryClient = discovery.AggregatedDiscoveryService_StreamAggregatedResourcesClient

// DeltaDiscoveryClient is a client interface for Delta XDS.
type DeltaDiscoveryClient = discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesClient

// Connection holds information about connected client.
type Connection struct {
	// PeerAddr is the address of the client, from network layer.
	PeerAddr string

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
	// deltaStream is used for Delta XDS. Only one of deltaStream or stream will be set
	deltaStream DeltaDiscoveryStream

	// Original node metadata, to avoid unmarshal/marshal.
	// This is included in internal events.
	node *core.Node

	// initialized channel will be closed when proxy is initialized. Pushes, or anything accessing
	// the proxy, should not be started until this channel is closed.
	initialized chan struct{}

	// stop can be used to end the connection manually via debug endpoints. Only to be used for testing.
	stop chan struct{}

	// reqChan is used to receive discovery requests for this connection.
	reqChan      chan *discovery.DiscoveryRequest
	deltaReqChan chan *discovery.DeltaDiscoveryRequest

	// errorChan is used to process error during discovery request processing.
	errorChan chan error

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
		initialized:   make(chan struct{}),
		stop:          make(chan struct{}),
		reqChan:       make(chan *discovery.DiscoveryRequest, 1),
		errorChan:     make(chan error, 1),
		PeerAddr:      peerAddr,
		Connect:       time.Now(),
		stream:        stream,
		blockedPushes: map[string]*model.PushRequest{},
	}
}

func (s *DiscoveryServer) receive(con *Connection, identities []string) {
	defer func() {
		close(con.errorChan)
		close(con.reqChan)
		// Close the initialized channel, if its not already closed, to prevent blocking the stream.
		select {
		case <-con.initialized:
		default:
			close(con.initialized)
		}
	}()

	firstRequest := true
	for {
		req, err := con.stream.Recv()
		if err != nil {
			if istiogrpc.IsExpectedGRPCError(err) {
				log.Infof("ADS: %q %s terminated %v", con.PeerAddr, con.ConID, err)
				return
			}
			con.errorChan <- err
			log.Errorf("ADS: %q %s terminated with error: %v", con.PeerAddr, con.ConID, err)
			totalXDSInternalErrors.Increment()
			return
		}
		// This should be only set for the first request. The node id may not be set - for example malicious clients.
		if firstRequest {
			// probe happens before envoy sends first xDS request
			if req.TypeUrl == v3.HealthInfoType {
				log.Warnf("ADS: %q %s send health check probe before normal xDS request", con.PeerAddr, con.ConID)
				continue
			}
			firstRequest = false
			if req.Node == nil || req.Node.Id == "" {
				con.errorChan <- status.New(codes.InvalidArgument, "missing node information").Err()
				return
			}
			if err := s.initConnection(req.Node, con, identities); err != nil {
				con.errorChan <- err
				return
			}
			defer s.closeConnection(con)
			log.Infof("ADS: new connection for node:%s", con.ConID)
		}

		select {
		case con.reqChan <- req:
		case <-con.stream.Context().Done():
			log.Infof("ADS: %q %s terminated with stream closed", con.PeerAddr, con.ConID)
			return
		}
	}
}

// processRequest handles one discovery request. This is currently called from the 'main' thread, which also
// handles 'push' requests and close - the code will eventually call the 'push' code, and it needs more mutex
// protection. Original code avoided the mutexes by doing both 'push' and 'process requests' in same thread.
func (s *DiscoveryServer) processRequest(req *discovery.DiscoveryRequest, con *Connection) error {
	if !s.shouldProcessRequest(con.proxy, req) {
		return nil
	}

	// For now, don't let xDS piggyback debug requests start watchers.
	if strings.HasPrefix(req.TypeUrl, v3.DebugType) {
		return s.pushXds(con, con.proxy.LastPushContext, &model.WatchedResource{
			TypeUrl: req.TypeUrl, ResourceNames: req.ResourceNames,
		}, &model.PushRequest{Full: true})
	}
	if s.StatusReporter != nil {
		s.StatusReporter.RegisterEvent(con.ConID, req.TypeUrl, req.ResponseNonce)
	}
	shouldRespond := s.shouldRespond(con, req)

	var request *model.PushRequest
	push := con.proxy.LastPushContext
	if shouldRespond {
		// This is a request, trigger a full push for this type. Override the blocked push (if it exists),
		// as this full push is guaranteed to be a superset of what we would have pushed from the blocked push.
		request = &model.PushRequest{Full: true, Push: push}
	} else {
		// Check if we have a blocked push. If this was an ACK, we will send it.
		// Either way we remove the blocked push as we will send a push.
		haveBlockedPush := false
		con.proxy.Lock()
		request, haveBlockedPush = con.blockedPushes[req.TypeUrl]
		delete(con.blockedPushes, req.TypeUrl)
		con.proxy.Unlock()
		if haveBlockedPush {
			// we have a blocked push which we will use
			log.Debugf("%s: DEQUEUE for node:%s", v3.GetShortType(req.TypeUrl), con.proxy.ID)
		} else {
			// This is an ACK, no delayed push
			// Return immediately, no action needed
			return nil
		}
	}

	request.Reason = append(request.Reason, model.ProxyRequest)
	request.Start = con.proxy.LastPushTime
	// SidecarScope for the proxy may not have been updated based on this pushContext.
	// It can happen when `processRequest` comes after push context has been updated(s.initPushContext),
	// but before proxy's SidecarScope has been updated(s.updateProxy).
	if con.proxy.SidecarScope != nil && con.proxy.SidecarScope.Version != push.PushVersion {
		s.computeProxyState(con.proxy, request)
	}
	return s.pushXds(con, push, con.Watched(req.TypeUrl), request)
}

// StreamAggregatedResources implements the ADS interface.
func (s *DiscoveryServer) StreamAggregatedResources(stream discovery.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	return s.Stream(stream)
}

func (s *DiscoveryServer) Stream(stream DiscoveryStream) error {
	if knativeEnv != "" && firstRequest.Load() {
		// How scaling works in knative is the first request is the "loading" request. During
		// loading request, concurrency=1. Once that request is done, concurrency is enabled.
		// However, the XDS stream is long lived, so the first request would block all others. As a
		// result, we should exit the first request immediately; clients will retry.
		firstRequest.Store(false)
		return status.Error(codes.Unavailable, "server warmup not complete; try again")
	}
	// Check if server is ready to accept clients and process new requests.
	// Currently ready means caches have been synced and hence can build
	// clusters correctly. Without this check, InitContext() call below would
	// initialize with empty config, leading to reconnected Envoys loosing
	// configuration. This is an additional safety check inaddition to adding
	// cachesSynced logic to readiness probe to handle cases where kube-proxy
	// ip tables update latencies.
	// See https://github.com/istio/istio/issues/25495.
	if !s.IsServerReady() {
		return status.Error(codes.Unavailable, "server is not ready to serve discovery information")
	}

	ctx := stream.Context()
	peerAddr := "0.0.0.0"
	if peerInfo, ok := peer.FromContext(ctx); ok {
		peerAddr = peerInfo.Addr.String()
	}

	if err := s.WaitForRequestLimit(stream.Context()); err != nil {
		log.Warnf("ADS: %q exceeded rate limit: %v", peerAddr, err)
		return status.Errorf(codes.ResourceExhausted, "request rate limit exceeded: %v", err)
	}

	ids, err := s.authenticate(ctx)
	if err != nil {
		return status.Error(codes.Unauthenticated, err.Error())
	}
	if ids != nil {
		log.Debugf("Authenticated XDS: %v with identity %v", peerAddr, ids)
	} else {
		log.Debugf("Unauthenticated XDS: %s", peerAddr)
	}

	// InitContext returns immediately if the context was already initialized.
	if err = s.globalPushContext().InitContext(s.Env, nil, nil); err != nil {
		// Error accessing the data - log and close, maybe a different pilot replica
		// has more luck
		log.Warnf("Error reading config %v", err)
		return status.Error(codes.Unavailable, "error reading config")
	}
	con := newConnection(peerAddr, stream)

	// Do not call: defer close(con.pushChannel). The push channel will be garbage collected
	// when the connection is no longer used. Closing the channel can cause subtle race conditions
	// with push. According to the spec: "It's only necessary to close a channel when it is important
	// to tell the receiving goroutines that all data have been sent."

	// Block until either a request is received or a push is triggered.
	// We need 2 go routines because 'read' blocks in Recv().
	go s.receive(con, ids)

	// Wait for the proxy to be fully initialized before we start serving traffic. Because
	// initialization doesn't have dependencies that will block, there is no need to add any timeout
	// here. Prior to this explicit wait, we were implicitly waiting by receive() not sending to
	// reqChannel and the connection not being enqueued for pushes to pushChannel until the
	// initialization is complete.
	<-con.initialized

	for {
		select {
		case req, ok := <-con.reqChan:
			if ok {
				if err := s.processRequest(req, con); err != nil {
					return err
				}
			} else {
				// Remote side closed connection or error processing the request.
				return <-con.errorChan
			}
		case pushEv := <-con.pushChannel:
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
		log.Warnf("ADS:%s: ACK ERROR %s %s:%s", stype, con.ConID, errCode.String(), request.ErrorDetail.GetMessage())
		incrementXDSRejects(request.TypeUrl, con.proxy.ID, errCode.String())
		if s.StatusGen != nil {
			s.StatusGen.OnNack(con.proxy, request)
		}
		con.proxy.Lock()
		if w, f := con.proxy.WatchedResources[request.TypeUrl]; f {
			w.NonceNacked = request.ResponseNonce
		}
		con.proxy.Unlock()
		return false
	}

	if shouldUnsubscribe(request) {
		log.Debugf("ADS:%s: UNSUBSCRIBE %s %s %s", stype, con.ConID, request.VersionInfo, request.ResponseNonce)
		con.proxy.Lock()
		delete(con.proxy.WatchedResources, request.TypeUrl)
		con.proxy.Unlock()
		return false
	}

	con.proxy.RLock()
	previousInfo := con.proxy.WatchedResources[request.TypeUrl]
	con.proxy.RUnlock()

	// This can happen in two cases:
	// 1. Envoy initially send request to Istiod
	// 2. Envoy reconnect to Istiod i.e. Istiod does not have
	// information about this typeUrl, but Envoy sends response nonce - either
	// because Istiod is restarted or Envoy disconnects and reconnects.
	// We should always respond with the current resource names.
	if request.ResponseNonce == "" || previousInfo == nil {
		log.Debugf("ADS:%s: INIT/RECONNECT %s %s %s", stype, con.ConID, request.VersionInfo, request.ResponseNonce)
		con.proxy.Lock()
		con.proxy.WatchedResources[request.TypeUrl] = &model.WatchedResource{TypeUrl: request.TypeUrl, ResourceNames: request.ResourceNames}
		con.proxy.Unlock()
		return true
	}

	// If there is mismatch in the nonce, that is a case of expired/stale nonce.
	// A nonce becomes stale following a newer nonce being sent to Envoy.
	if request.ResponseNonce != previousInfo.NonceSent {
		log.Debugf("ADS:%s: REQ %s Expired nonce received %s, sent %s", stype,
			con.ConID, request.ResponseNonce, previousInfo.NonceSent)
		xdsExpiredNonce.With(typeTag.Value(v3.GetMetricType(request.TypeUrl))).Increment()
		con.proxy.Lock()
		con.proxy.WatchedResources[request.TypeUrl].NonceNacked = ""
		con.proxy.Unlock()
		return false
	}

	// If it comes here, that means nonce match. This an ACK. We should record
	// the ack details and respond if there is a change in resource names.
	con.proxy.Lock()
	previousResources := con.proxy.WatchedResources[request.TypeUrl].ResourceNames
	con.proxy.WatchedResources[request.TypeUrl].NonceAcked = request.ResponseNonce
	con.proxy.WatchedResources[request.TypeUrl].NonceNacked = ""
	con.proxy.WatchedResources[request.TypeUrl].ResourceNames = request.ResourceNames
	con.proxy.Unlock()

	// Envoy can send two DiscoveryRequests with same version and nonce
	// when it detects a new resource. We should respond if they change.
	if listEqualUnordered(previousResources, request.ResourceNames) {
		log.Debugf("ADS:%s: ACK %s %s %s", stype, con.ConID, request.VersionInfo, request.ResponseNonce)
		return false
	}
	log.Debugf("ADS:%s: RESOURCE CHANGE previous resources: %v, new resources: %v %s %s %s", stype,
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
	case v3.SecretType, v3.EndpointType, v3.RouteType, v3.ExtensionConfigurationType:
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

// update the node associated with the connection, after receiving a packet from envoy, also adds the connection
// to the tracking map.
func (s *DiscoveryServer) initConnection(node *core.Node, con *Connection, identities []string) error {
	// Setup the initial proxy metadata
	proxy, err := s.initProxyMetadata(node)
	if err != nil {
		return err
	}
	// Check if proxy cluster has an alias configured, if yes use that as cluster ID for this proxy.
	if alias, exists := s.ClusterAliases[proxy.Metadata.ClusterID]; exists {
		proxy.Metadata.ClusterID = alias
	}
	// To ensure push context is monotonically increasing, setup LastPushContext before we addCon. This
	// way only new push contexts will be registered for this proxy.
	proxy.LastPushContext = s.globalPushContext()
	// First request so initialize connection id and start tracking it.
	con.ConID = connectionID(proxy.ID)
	con.node = node
	con.proxy = proxy

	// Authorize xds clients
	if err := s.authorize(con, identities); err != nil {
		return err
	}

	// Register the connection. this allows pushes to be triggered for the proxy. Note: the timing of
	// this and initializeProxy important. While registering for pushes *after* initialization is complete seems like
	// a better choice, it introduces a race condition; If we complete initialization of a new push
	// context between initializeProxy and addCon, we would not get any pushes triggered for the new
	// push context, leading the proxy to have a stale state until the next full push.
	s.addCon(con.ConID, con)
	// Register that initialization is complete. This triggers to calls that it is safe to access the
	// proxy
	defer close(con.initialized)

	// Complete full initialization of the proxy
	if err := s.initializeProxy(node, con); err != nil {
		s.closeConnection(con)
		return err
	}

	if s.StatusGen != nil {
		s.StatusGen.OnConnect(con)
	}
	return nil
}

func (s *DiscoveryServer) closeConnection(con *Connection) {
	if con.ConID == "" {
		return
	}
	s.removeCon(con.ConID)
	if s.StatusGen != nil {
		s.StatusGen.OnDisconnect(con)
	}
	if s.StatusReporter != nil {
		s.StatusReporter.RegisterDisconnect(con.ConID, AllEventTypesList)
	}
	s.WorkloadEntryController.QueueUnregisterWorkload(con.proxy, con.Connect)
}

func connectionID(node string) string {
	id := atomic.AddInt64(&connectionNumber, 1)
	return node + "-" + strconv.FormatInt(id, 10)
}

// initProxyMetadata initializes just the basic metadata of a proxy. This is decoupled from
// initProxyState such that we can perform authorization before attempting expensive computations to
// fully initialize the proxy.
func (s *DiscoveryServer) initProxyMetadata(node *core.Node) (*model.Proxy, error) {
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
	proxy.XdsNode = node
	return proxy, nil
}

// initializeProxy completes the initialization of a proxy. It is expected to be called only after
// initProxyMetadata.
func (s *DiscoveryServer) initializeProxy(node *core.Node, con *Connection) error {
	proxy := con.proxy
	// this should be done before we look for service instances, but after we load metadata
	// TODO fix check in kubecontroller treat echo VMs like there isn't a pod
	if err := s.WorkloadEntryController.RegisterWorkload(proxy, con.Connect); err != nil {
		return err
	}
	s.computeProxyState(proxy, nil)

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

	locality := util.LocalityToString(proxy.Locality)
	// add topology labels to proxy metadata labels
	proxy.Metadata.Labels = labelutil.AugmentLabels(proxy.Metadata.Labels, proxy.Metadata.ClusterID, locality, proxy.Metadata.Network)
	// Discover supported IP Versions of proxy so that appropriate config can be delivered.
	proxy.DiscoverIPVersions()

	proxy.WatchedResources = map[string]*model.WatchedResource{}
	// Based on node metadata and version, we can associate a different generator.
	if proxy.Metadata.Generator != "" {
		proxy.XdsResourceGenerator = s.Generators[proxy.Metadata.Generator]
	}

	return nil
}

func (s *DiscoveryServer) updateProxy(proxy *model.Proxy, request *model.PushRequest) {
	s.computeProxyState(proxy, request)
	if util.IsLocalityEmpty(proxy.Locality) {
		// Get the locality from the proxy's service instances.
		// We expect all instances to have the same locality.
		// So its enough to look at the first instance.
		if len(proxy.ServiceInstances) > 0 {
			proxy.Locality = util.ConvertLocality(proxy.ServiceInstances[0].Endpoint.Locality.Label)
			locality := proxy.ServiceInstances[0].Endpoint.Locality.Label
			// add topology labels to proxy metadata labels
			proxy.Metadata.Labels = labelutil.AugmentLabels(proxy.Metadata.Labels, proxy.Metadata.ClusterID, locality, proxy.Metadata.Network)
		}
	}
}

func (s *DiscoveryServer) computeProxyState(proxy *model.Proxy, request *model.PushRequest) {
	proxy.SetWorkloadLabels(s.Env)
	proxy.SetServiceInstances(s.Env.ServiceDiscovery)
	// Precompute the sidecar scope and merged gateways associated with this proxy.
	// Saves compute cycles in networking code. Though this might be redundant sometimes, we still
	// have to compute this because as part of a config change, a new Sidecar could become
	// applicable to this proxy
	var sidecar, gateway bool
	push := proxy.LastPushContext
	if request == nil {
		sidecar = true
		gateway = true
	} else {
		push = request.Push
		if len(request.ConfigsUpdated) == 0 {
			sidecar = true
			gateway = true
		}
		for conf := range request.ConfigsUpdated {
			switch conf.Kind {
			case gvk.ServiceEntry, gvk.DestinationRule, gvk.VirtualService, gvk.Sidecar, gvk.HTTPRoute, gvk.TCPRoute:
				sidecar = true
			case gvk.Gateway, gvk.KubernetesGateway, gvk.GatewayClass:
				gateway = true
			case gvk.Ingress:
				sidecar = true
				gateway = true
			}
			if sidecar && gateway {
				break
			}
		}
	}
	// compute the sidecarscope for both proxy type whenever it changes.
	if sidecar {
		proxy.SetSidecarScope(push)
	}
	// only compute gateways for "router" type proxy.
	if gateway && proxy.Type == model.Router {
		proxy.SetGatewaysForProxy(push)
	}
	proxy.LastPushContext = push
	if request != nil {
		proxy.LastPushTime = request.Start
	}
}

// shouldProcessRequest returns whether or not to continue with the request.
func (s *DiscoveryServer) shouldProcessRequest(proxy *model.Proxy, req *discovery.DiscoveryRequest) bool {
	if req.TypeUrl != v3.HealthInfoType {
		return true
	}
	if features.WorkloadEntryHealthChecks {
		event := workloadentry.HealthEvent{}
		event.Healthy = req.ErrorDetail == nil
		if !event.Healthy {
			event.Message = req.ErrorDetail.Message
		}
		s.WorkloadEntryController.QueueWorkloadEntryHealth(proxy, event)
	}
	return false
}

// DeltaAggregatedResources is not implemented.
// Instead, Generators may send only updates/add, with Delete indicated by an empty spec.
// This works if both ends follow this model. For example EDS and the API generator follow this
// pattern.
//
// The delta protocol changes the request, adding unsubscribe/subscribe instead of sending full
// list of resources. On the response it adds 'removed resources' and sends changes for everything.
func (s *DiscoveryServer) DeltaAggregatedResources(stream discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesServer) error {
	return s.StreamDeltas(stream)
}

// Compute and send the new configuration for a connection.
func (s *DiscoveryServer) pushConnection(con *Connection, pushEv *Event) error {
	pushRequest := pushEv.pushRequest

	if pushRequest.Full {
		// Update Proxy with current information.
		s.updateProxy(con.proxy, pushRequest)
	}

	if !s.ProxyNeedsPush(con.proxy, pushRequest) {
		log.Debugf("Skipping push to %v, no updates required", con.ConID)
		if pushRequest.Full {
			// Only report for full versions, incremental pushes do not have a new version.
			reportAllEvents(s.StatusReporter, con.ConID, pushRequest.Push.LedgerVersion, nil)
		}
		return nil
	}

	// Send pushes to all generators
	// Each Generator is responsible for determining if the push event requires a push
	wrl, ignoreEvents := con.pushDetails()
	for _, w := range wrl {
		if !features.EnableFlowControl {
			// Always send the push if flow control disabled
			if err := s.pushXds(con, pushRequest.Push, w, pushRequest); err != nil {
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
			log.Warnf("%s: QUEUE TIMEOUT for node:%s", v3.GetShortType(w.TypeUrl), con.proxy.ID)
		}
		if synced || timeout {
			// Send the push now
			if err := s.pushXds(con, pushRequest.Push, w, pushRequest); err != nil {
				return err
			}
		} else {
			// The type is not yet synced. Instead of pushing now, which may overload Envoy,
			// we will wait until the last push is ACKed and trigger the push. See
			// https://github.com/istio/istio/issues/25685 for details on the performance
			// impact of sending pushes before Envoy ACKs.
			totalDelayedPushes.With(typeTag.Value(v3.GetMetricType(w.TypeUrl))).Increment()
			log.Debugf("%s: QUEUE for node:%s", v3.GetShortType(w.TypeUrl), con.proxy.ID)
			con.proxy.Lock()
			con.blockedPushes[w.TypeUrl] = con.blockedPushes[w.TypeUrl].CopyMerge(pushEv.pushRequest)
			con.proxy.Unlock()
		}
	}
	if pushRequest.Full {
		// Report all events for unwatched resources. Watched resources will be reported in pushXds or on ack.
		reportAllEvents(s.StatusReporter, con.ConID, pushRequest.Push.LedgerVersion, ignoreEvents)
	}

	proxiesConvergeDelay.Record(time.Since(pushRequest.Start).Seconds())
	return nil
}

// PushOrder defines the order that updates will be pushed in. Any types not listed here will be pushed in random
// order after the types listed here
var PushOrder = []string{v3.ClusterType, v3.EndpointType, v3.ListenerType, v3.RouteType, v3.SecretType}

// KnownOrderedTypeUrls has typeUrls for which we know the order of push.
var KnownOrderedTypeUrls = map[string]struct{}{
	v3.ClusterType:  {},
	v3.EndpointType: {},
	v3.ListenerType: {},
	v3.RouteType:    {},
	v3.SecretType:   {},
}

func reportAllEvents(s DistributionStatusCache, id, version string, ignored sets.Set) {
	if s == nil {
		return
	}
	// this version of the config will never be distributed to this envoy because it is not a relevant diff.
	// inform distribution status reporter that this connection has been updated, because it effectively has
	for distributionType := range AllEventTypes {
		if ignored.Contains(distributionType) {
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

func (s *DiscoveryServer) ProxyUpdate(clusterID cluster.ID, ip string) {
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
	if log.DebugEnabled() {
		currentlyPending := s.pushQueue.Pending()
		if currentlyPending != 0 {
			log.Debugf("Starting new push while %v were still pending", currentlyPending)
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
		log.Infof("XDS: Incremental Pushing:%s ConnectedEndpoints:%d Version:%s",
			version, s.adsClientCount(), req.Push.PushVersion)
	} else {
		totalService := len(req.Push.GetAllServices())
		log.Infof("XDS: Pushing:%s Services:%d ConnectedEndpoints:%d Version:%s",
			version, totalService, s.adsClientCount(), req.Push.PushVersion)
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
	// Push config changes, iterating over connected envoys.
	if log.DebugEnabled() {
		currentlyPending := s.pushQueue.Pending()
		if currentlyPending != 0 {
			log.Debugf("Starting new push while %v were still pending", currentlyPending)
		}
	}
	req.Start = time.Now()
	for _, p := range s.AllClients() {
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
		log.Errorf("ADS: Removing connection for non-existing node:%v.", conID)
		totalXDSInternalErrors.Increment()
	} else {
		delete(s.adsClients, conID)
		recordXDSClients(con.proxy.Metadata.IstioVersion, -1)
	}
}

// Send with timeout if configured.
func (conn *Connection) send(res *discovery.DiscoveryResponse) error {
	sendHandler := func() error {
		start := time.Now()
		defer func() { recordSendTime(time.Since(start)) }()
		return conn.stream.Send(res)
	}
	err := istiogrpc.Send(conn.stream.Context(), sendHandler)
	if err == nil {
		sz := 0
		for _, rc := range res.Resources {
			sz += len(rc.Value)
		}
		if res.Nonce != "" && !strings.HasPrefix(res.TypeUrl, v3.DebugType) {
			conn.proxy.Lock()
			if conn.proxy.WatchedResources[res.TypeUrl] == nil {
				conn.proxy.WatchedResources[res.TypeUrl] = &model.WatchedResource{TypeUrl: res.TypeUrl}
			}
			conn.proxy.WatchedResources[res.TypeUrl].NonceSent = res.Nonce
			conn.proxy.WatchedResources[res.TypeUrl].VersionSent = res.VersionInfo
			conn.proxy.WatchedResources[res.TypeUrl].LastSent = time.Now()
			conn.proxy.Unlock()
		}
	} else if status.Convert(err).Code() == codes.DeadlineExceeded {
		log.Infof("Timeout writing %s", conn.ConID)
		xdsResponseWriteTimeouts.Increment()
	}
	return err
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

// pushDetails returns the details needed for current push. It returns ordered list of
// watched resources for the proxy, ordered in accordance with known push order.
// It also returns the lis of typeUrls.
// nolint
func (conn *Connection) pushDetails() ([]*model.WatchedResource, sets.Set) {
	conn.proxy.RLock()
	defer conn.proxy.RUnlock()
	typeUrls := sets.NewSet()
	for k := range conn.proxy.WatchedResources {
		typeUrls.Insert(k)
	}
	return orderWatchedResources(conn.proxy.WatchedResources), typeUrls
}

func orderWatchedResources(resources map[string]*model.WatchedResource) []*model.WatchedResource {
	wr := make([]*model.WatchedResource, 0, len(resources))
	// first add all known types, in order
	for _, tp := range PushOrder {
		if w, f := resources[tp]; f {
			wr = append(wr, w)
		}
	}
	// Then add any undeclared types
	for tp, w := range resources {
		if _, f := KnownOrderedTypeUrls[tp]; !f {
			wr = append(wr, w)
		}
	}
	return wr
}

func (conn *Connection) Stop() {
	close(conn.stop)
}
