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

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/autoregistration"
	"istio.io/istio/pilot/pkg/features"
	istiogrpc "istio.io/istio/pilot/pkg/grpc"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	labelutil "istio.io/istio/pilot/pkg/serviceregistry/util/label"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/env"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/sets"
)

var (
	log = istiolog.RegisterScope("ads", "ads debugging")

	// Tracks connections, increment on each new connection.
	connectionNumber = int64(0)
)

// Used only when running in KNative, to handle the load balancing behavior.
var firstRequest = uatomic.NewBool(true)

var knativeEnv = env.Register("K_REVISION", "",
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
	// peerAddr is the address of the client, from network layer.
	peerAddr string

	// Time of connection, for debugging
	connectedAt time.Time

	// conID is the connection conID, used as a key in the connection table.
	// Currently based on the node name and a counter.
	conID string

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
}

func (conn *Connection) ID() string {
	return conn.conID
}

func (conn *Connection) Proxy() *model.Proxy {
	return conn.proxy
}

func (conn *Connection) ConnectedAt() time.Time {
	return conn.connectedAt
}

func (conn *Connection) Stop() {
	close(conn.stop)
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
		pushChannel: make(chan *Event),
		initialized: make(chan struct{}),
		stop:        make(chan struct{}),
		reqChan:     make(chan *discovery.DiscoveryRequest, 1),
		errorChan:   make(chan error, 1),
		peerAddr:    peerAddr,
		connectedAt: time.Now(),
		stream:      stream,
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
				log.Infof("ADS: %q %s terminated", con.peerAddr, con.conID)
				return
			}
			con.errorChan <- err
			log.Errorf("ADS: %q %s terminated with error: %v", con.peerAddr, con.conID, err)
			totalXDSInternalErrors.Increment()
			return
		}
		// This should be only set for the first request. The node id may not be set - for example malicious clients.
		if firstRequest {
			// probe happens before envoy sends first xDS request
			if req.TypeUrl == v3.HealthInfoType {
				log.Warnf("ADS: %q %s send health check probe before normal xDS request", con.peerAddr, con.conID)
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
			log.Infof("ADS: new connection for node:%s", con.conID)
		}

		select {
		case con.reqChan <- req:
		case <-con.stream.Context().Done():
			log.Infof("ADS: %q %s terminated with stream closed", con.peerAddr, con.conID)
			return
		}
	}
}

// processRequest handles one discovery request. This is currently called from the 'main' thread, which also
// handles 'push' requests and close - the code will eventually call the 'push' code, and it needs more mutex
// protection. Original code avoided the mutexes by doing both 'push' and 'process requests' in same thread.
func (s *DiscoveryServer) processRequest(req *discovery.DiscoveryRequest, con *Connection) error {
	stype := v3.GetShortType(req.TypeUrl)
	log.Debugf("ADS:%s: REQ %s resources:%d nonce:%s version:%s ", stype,
		con.conID, len(req.ResourceNames), req.ResponseNonce, req.VersionInfo)
	if req.TypeUrl == v3.HealthInfoType {
		s.handleWorkloadHealthcheck(con.proxy, req)
		return nil
	}

	// For now, don't let xDS piggyback debug requests start watchers.
	if strings.HasPrefix(req.TypeUrl, v3.DebugType) {
		return s.pushXds(con,
			&model.WatchedResource{TypeUrl: req.TypeUrl, ResourceNames: req.ResourceNames},
			&model.PushRequest{Full: true, Push: con.proxy.LastPushContext})
	}

	if s.StatusReporter != nil {
		s.StatusReporter.RegisterEvent(con.conID, req.TypeUrl, req.ResponseNonce)
	}

	shouldRespond, delta := s.shouldRespond(con, req)
	if !shouldRespond {
		return nil
	}

	request := &model.PushRequest{
		Full:   true,
		Push:   con.proxy.LastPushContext,
		Reason: model.NewReasonStats(model.ProxyRequest),

		// The usage of LastPushTime (rather than time.Now()), is critical here for correctness; This time
		// is used by the XDS cache to determine if a entry is stale. If we use Now() with an old push context,
		// we may end up overriding active cache entries with stale ones.
		Start: con.proxy.LastPushTime,
		Delta: delta,
	}

	// SidecarScope for the proxy may not have been updated based on this pushContext.
	// It can happen when `processRequest` comes after push context has been updated(s.initPushContext),
	// but proxy's SidecarScope has been updated(s.computeProxyState -> SetSidecarScope) due to optimizations that skip sidecar scope
	// computation.
	if con.proxy.SidecarScope != nil && con.proxy.SidecarScope.Version != request.Push.PushVersion {
		s.computeProxyState(con.proxy, request)
	}
	return s.pushXds(con, con.proxy.GetWatchedResource(req.TypeUrl), request)
}

// StreamAggregatedResources implements the ADS interface.
func (s *DiscoveryServer) StreamAggregatedResources(stream DiscoveryStream) error {
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
		// Go select{} statements are not ordered; the same channel can be chosen many times.
		// For requests, these are higher priority (client may be blocked on startup until these are done)
		// and often very cheap to handle (simple ACK), so we check it first.
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
		case <-con.stop:
			return nil
		default:
		}
		// If there wasn't already a request, poll for requests and pushes. Note: if we have a huge
		// amount of incoming requests, we may still send some pushes, as we do not `continue` above;
		// however, requests will be handled ~2x as much as pushes. This ensures a wave of requests
		// cannot completely starve pushes. However, this scenario is unlikely.
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

var emptyResourceDelta = model.ResourceDelta{}

// shouldRespond determines whether this request needs to be responded back. It applies the ack/nack rules as per xds protocol
// using WatchedResource for previous state and discovery request for the current state.
func (s *DiscoveryServer) shouldRespond(con *Connection, request *discovery.DiscoveryRequest) (bool, model.ResourceDelta) {
	stype := v3.GetShortType(request.TypeUrl)

	// If there is an error in request that means previous response is erroneous.
	// We do not have to respond in that case. In this case request's version info
	// will be different from the version sent. But it is fragile to rely on that.
	if request.ErrorDetail != nil {
		errCode := codes.Code(request.ErrorDetail.Code)
		log.Warnf("ADS:%s: ACK ERROR %s %s:%s", stype, con.conID, errCode.String(), request.ErrorDetail.GetMessage())
		incrementXDSRejects(request.TypeUrl, con.proxy.ID, errCode.String())
		return false, emptyResourceDelta
	}

	if shouldUnsubscribe(request) {
		log.Debugf("ADS:%s: UNSUBSCRIBE %s %s %s", stype, con.conID, request.VersionInfo, request.ResponseNonce)
		con.proxy.DeleteWatchedResource(request.TypeUrl)
		return false, emptyResourceDelta
	}

	previousInfo := con.proxy.GetWatchedResource(request.TypeUrl)
	// This can happen in two cases:
	// 1. When Envoy starts for the first time, it sends an initial Discovery request to Istiod.
	// 2. When Envoy reconnects to a new Istiod that does not have information about this typeUrl
	// i.e. non empty response nonce.
	// We should always respond with the current resource names.
	if request.ResponseNonce == "" || previousInfo == nil {
		con.proxy.Lock()
		defer con.proxy.Unlock()

		log.Debugf("ADS:%s: INIT/RECONNECT %s %s %s", stype, con.conID, request.VersionInfo, request.ResponseNonce)
		con.proxy.WatchedResources[request.TypeUrl] = &model.WatchedResource{TypeUrl: request.TypeUrl, ResourceNames: request.ResourceNames}
		// For all EDS requests that we have already responded with in the same stream let us
		// force the response. It is important to respond to those requests for Envoy to finish
		// warming of those resources(Clusters).
		// This can happen with the following sequence
		// 1. Envoy disconnects and reconnects to Istiod.
		// 2. Envoy sends EDS request and we respond with it.
		// 3. Envoy sends CDS request and we respond with clusters.
		// 4. Envoy detects a change in cluster state and tries to warm those clusters and send EDS request for them.
		// 5. We should respond to the EDS request with Endpoints to let Envoy finish cluster warming.
		// Refer to https://github.com/envoyproxy/envoy/issues/13009 for more details.
		for _, dependent := range warmingDependencies(request.TypeUrl) {
			if dwr, exists := con.proxy.WatchedResources[dependent]; exists {
				dwr.AlwaysRespond = true
			}
		}
		return true, emptyResourceDelta
	}

	// If there is mismatch in the nonce, that is a case of expired/stale nonce.
	// A nonce becomes stale following a newer nonce being sent to Envoy.
	// previousInfo.NonceSent can be empty if we previously had shouldRespond=true but didn't send any resources.
	if request.ResponseNonce != previousInfo.NonceSent {
		if features.EnableUnsafeAssertions && previousInfo.NonceSent == "" {
			// Assert we do not end up in an invalid state
			log.Fatalf("ADS:%s: REQ %s Expired nonce received %s, but we never sent any nonce", stype,
				con.conID, request.ResponseNonce)
		}
		log.Debugf("ADS:%s: REQ %s Expired nonce received %s, sent %s", stype,
			con.conID, request.ResponseNonce, previousInfo.NonceSent)
		xdsExpiredNonce.With(typeTag.Value(v3.GetMetricType(request.TypeUrl))).Increment()
		return false, emptyResourceDelta
	}

	// If it comes here, that means nonce match.
	var previousResources []string
	var alwaysRespond bool
	con.proxy.UpdateWatchedResource(request.TypeUrl, func(wr *model.WatchedResource) *model.WatchedResource {
		previousResources = wr.ResourceNames
		wr.NonceAcked = request.ResponseNonce
		wr.ResourceNames = request.ResourceNames
		alwaysRespond = wr.AlwaysRespond
		wr.AlwaysRespond = false
		return wr
	})

	// Envoy can send two DiscoveryRequests with same version and nonce.
	// when it detects a new resource. We should respond if they change.
	prev := sets.New(previousResources...)
	cur := sets.New(request.ResourceNames...)
	removed := prev.Difference(cur)
	added := cur.Difference(prev)

	// We should always respond "alwaysRespond" marked requests to let Envoy finish warming
	// even though Nonce match and it looks like an ACK.
	if alwaysRespond {
		log.Infof("ADS:%s: FORCE RESPONSE %s for warming.", stype, con.conID)
		return true, emptyResourceDelta
	}

	if len(removed) == 0 && len(added) == 0 {
		log.Debugf("ADS:%s: ACK %s %s %s", stype, con.conID, request.VersionInfo, request.ResponseNonce)
		return false, emptyResourceDelta
	}
	log.Debugf("ADS:%s: RESOURCE CHANGE added %v removed %v %s %s %s", stype,
		added, removed, con.conID, request.VersionInfo, request.ResponseNonce)

	// For non wildcard resource, if no new resources are subscribed, it means we do not need to push.
	if !isWildcardTypeURL(request.TypeUrl) && len(added) == 0 {
		return false, emptyResourceDelta
	}

	return true, model.ResourceDelta{
		Subscribed: added,
		// we do not need to set unsubscribed for StoW
	}
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

// warmingDependencies returns the dependent typeURLs that need to be responded with
// for warming of this typeURL.
func warmingDependencies(typeURL string) []string {
	switch typeURL {
	case v3.ClusterType:
		return []string{v3.EndpointType}
	default:
		return nil
	}
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
	con.conID = connectionID(proxy.ID)
	con.node = node
	con.proxy = proxy
	if proxy.IsZTunnel() && !features.EnableAmbientControllers {
		return fmt.Errorf("ztunnel requires PILOT_ENABLE_AMBIENT_CONTROLLERS=true")
	}

	// Authorize xds clients
	if err := s.authorize(con, identities); err != nil {
		return err
	}

	// Register the connection. this allows pushes to be triggered for the proxy. Note: the timing of
	// this and initializeProxy important. While registering for pushes *after* initialization is complete seems like
	// a better choice, it introduces a race condition; If we complete initialization of a new push
	// context between initializeProxy and addCon, we would not get any pushes triggered for the new
	// push context, leading the proxy to have a stale state until the next full push.
	s.addCon(con.conID, con)
	// Register that initialization is complete. This triggers to calls that it is safe to access the
	// proxy
	defer close(con.initialized)

	// Complete full initialization of the proxy
	if err := s.initializeProxy(con); err != nil {
		s.closeConnection(con)
		return err
	}
	return nil
}

func (s *DiscoveryServer) closeConnection(con *Connection) {
	if con.conID == "" {
		return
	}
	s.removeCon(con.conID)
	if s.StatusReporter != nil {
		s.StatusReporter.RegisterDisconnect(con.conID, AllTrackingEventTypes)
	}
	s.WorkloadEntryController.OnDisconnect(con)
}

func connectionID(node string) string {
	id := atomic.AddInt64(&connectionNumber, 1)
	return node + "-" + strconv.FormatInt(id, 10)
}

// Only used for test
func ResetConnectionNumberForTest() {
	atomic.StoreInt64(&connectionNumber, 0)
}

// initProxyMetadata initializes just the basic metadata of a proxy. This is decoupled from
// initProxyState such that we can perform authorization before attempting expensive computations to
// fully initialize the proxy.
func (s *DiscoveryServer) initProxyMetadata(node *core.Node) (*model.Proxy, error) {
	meta, err := model.ParseMetadata(node.Metadata)
	if err != nil {
		return nil, status.New(codes.InvalidArgument, err.Error()).Err()
	}
	proxy, err := model.ParseServiceNodeWithMetadata(node.Id, meta)
	if err != nil {
		return nil, status.New(codes.InvalidArgument, err.Error()).Err()
	}
	// Update the config namespace associated with this proxy
	proxy.ConfigNamespace = model.GetProxyConfigNamespace(proxy)
	proxy.XdsNode = node
	return proxy, nil
}

// setTopologyLabels sets locality, cluster, network label
// must be called after `SetWorkloadLabels` and `SetServiceTargets`.
func setTopologyLabels(proxy *model.Proxy) {
	// This is a bit un-intuitive, but pull the locality from Labels first. The service registries have the best access to
	// locality information, as they can read from various sources (Node on Kubernetes, for example). They will take this
	// information and add it to the labels. So while the proxy may not originally have these labels,
	// it will by the time we get here (as a result of calling this after SetWorkloadLabels).
	proxy.Locality = localityFromProxyLabels(proxy)
	if proxy.Locality == nil {
		// If there is no locality in the registry then use the one sent as part of the discovery request.
		// This is not preferable as only the connected Pilot is aware of this proxies location, but it
		// can still help provide some client-side Envoy context when load balancing based on location.
		proxy.Locality = &core.Locality{
			Region:  proxy.XdsNode.Locality.GetRegion(),
			Zone:    proxy.XdsNode.Locality.GetZone(),
			SubZone: proxy.XdsNode.Locality.GetSubZone(),
		}
	}
	// add topology labels to proxy labels
	proxy.Labels = labelutil.AugmentLabels(
		proxy.Labels,
		proxy.Metadata.ClusterID,
		util.LocalityToString(proxy.Locality),
		proxy.GetNodeName(),
		proxy.Metadata.Network,
	)
}

func localityFromProxyLabels(proxy *model.Proxy) *core.Locality {
	region, f1 := proxy.Labels[labelutil.LabelTopologyRegion]
	zone, f2 := proxy.Labels[labelutil.LabelTopologyZone]
	subzone, f3 := proxy.Labels[label.TopologySubzone.Name]
	if !f1 && !f2 && !f3 {
		// If no labels set, we didn't find the locality from the service registry. We do support a (mostly undocumented/internal)
		// label to override the locality, so respect that here as well.
		ls, f := proxy.Labels[model.LocalityLabel]
		if f {
			return util.ConvertLocality(ls)
		}
		return nil
	}
	return &core.Locality{
		Region:  region,
		Zone:    zone,
		SubZone: subzone,
	}
}

// initializeProxy completes the initialization of a proxy. It is expected to be called only after
// initProxyMetadata.
func (s *DiscoveryServer) initializeProxy(con *Connection) error {
	proxy := con.proxy
	// this should be done before we look for service instances, but after we load metadata
	// TODO fix check in kubecontroller treat echo VMs like there isn't a pod
	if err := s.WorkloadEntryController.OnConnect(con); err != nil {
		return err
	}
	s.computeProxyState(proxy, nil)
	// Discover supported IP Versions of proxy so that appropriate config can be delivered.
	proxy.DiscoverIPMode()

	proxy.WatchedResources = map[string]*model.WatchedResource{}
	// Based on node metadata and version, we can associate a different generator.
	if proxy.Metadata.Generator != "" {
		proxy.XdsResourceGenerator = s.Generators[proxy.Metadata.Generator]
	}

	return nil
}

func (s *DiscoveryServer) computeProxyState(proxy *model.Proxy, request *model.PushRequest) {
	proxy.SetServiceTargets(s.Env.ServiceDiscovery)
	// only recompute workload labels when
	// 1. stream established and proxy first time initialization
	// 2. proxy update
	recomputeLabels := request == nil || request.IsProxyUpdate()
	if recomputeLabels {
		proxy.SetWorkloadLabels(s.Env)
		setTopologyLabels(proxy)
	}
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
			case kind.ServiceEntry, kind.DestinationRule, kind.VirtualService, kind.Sidecar, kind.HTTPRoute, kind.TCPRoute, kind.TLSRoute, kind.GRPCRoute:
				sidecar = true
			case kind.Gateway, kind.KubernetesGateway, kind.GatewayClass, kind.ReferenceGrant:
				gateway = true
			case kind.Ingress:
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

// handleWorkloadHealthcheck processes HealthInformation type Url.
func (s *DiscoveryServer) handleWorkloadHealthcheck(proxy *model.Proxy, req *discovery.DiscoveryRequest) {
	if features.WorkloadEntryHealthChecks {
		event := autoregistration.HealthEvent{}
		event.Healthy = req.ErrorDetail == nil
		if !event.Healthy {
			event.Message = req.ErrorDetail.Message
		}
		s.WorkloadEntryController.QueueWorkloadEntryHealth(proxy, event)
	}
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
		s.computeProxyState(con.proxy, pushRequest)
	}

	if !s.ProxyNeedsPush(con.proxy, pushRequest) {
		log.Debugf("Skipping push to %v, no updates required", con.conID)
		if pushRequest.Full {
			// Only report for full versions, incremental pushes do not have a new version.
			reportAllEventsForProxyNoPush(con, s.StatusReporter, pushRequest.Push.LedgerVersion)
		}
		return nil
	}

	// Send pushes to all generators
	// Each Generator is responsible for determining if the push event requires a push
	wrl := con.watchedResourcesByOrder()
	for _, w := range wrl {
		if err := s.pushXds(con, w, pushRequest); err != nil {
			return err
		}
	}
	if pushRequest.Full {
		// Report all events for unwatched resources. Watched resources will be reported in pushXds or on ack.
		reportEventsForUnWatched(con, s.StatusReporter, pushRequest.Push.LedgerVersion)
	}

	proxiesConvergeDelay.Record(time.Since(pushRequest.Start).Seconds())
	return nil
}

// PushOrder defines the order that updates will be pushed in. Any types not listed here will be pushed in random
// order after the types listed here
var PushOrder = []string{v3.ClusterType, v3.EndpointType, v3.ListenerType, v3.RouteType, v3.SecretType}

// KnownOrderedTypeUrls has typeUrls for which we know the order of push.
var KnownOrderedTypeUrls = sets.New(PushOrder...)

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
		Reason: model.NewReasonStats(model.ProxyUpdate),
	})
}

// AdsPushAll will send updates to all nodes, with a full push.
// Mainly used in Debug interface.
func AdsPushAll(s *DiscoveryServer) {
	s.AdsPushAll(&model.PushRequest{
		Full:   true,
		Push:   s.globalPushContext(),
		Reason: model.NewReasonStats(model.DebugTrigger),
	})
}

// AdsPushAll will send updates to all nodes, for a full config or incremental EDS.
func (s *DiscoveryServer) AdsPushAll(req *model.PushRequest) {
	if !req.Full {
		log.Infof("XDS: Incremental Pushing ConnectedEndpoints:%d Version:%s",
			s.adsClientCount(), req.Push.PushVersion)
	} else {
		totalService := len(req.Push.GetAllServices())
		log.Infof("XDS: Pushing Services:%d ConnectedEndpoints:%d Version:%s",
			totalService, s.adsClientCount(), req.Push.PushVersion)
		monServices.Record(float64(totalService))

		// Make sure the ConfigsUpdated map exists
		if req.ConfigsUpdated == nil {
			req.ConfigsUpdated = make(sets.Set[model.ConfigKey])
		}
	}

	s.StartPush(req)
}

// Send a signal to all connections, with a push event.
func (s *DiscoveryServer) StartPush(req *model.PushRequest) {
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

func (conn *Connection) send(res *discovery.DiscoveryResponse) error {
	sendResponse := func() error {
		start := time.Now()
		defer func() { recordSendTime(time.Since(start)) }()
		return conn.stream.Send(res)
	}
	err := sendResponse()
	if err == nil {
		if res.Nonce != "" && !strings.HasPrefix(res.TypeUrl, v3.DebugType) {
			conn.proxy.UpdateWatchedResource(res.TypeUrl, func(wr *model.WatchedResource) *model.WatchedResource {
				if wr == nil {
					wr = &model.WatchedResource{TypeUrl: res.TypeUrl}
				}
				wr.NonceSent = res.Nonce
				return wr
			})
		}
	} else if status.Convert(err).Code() == codes.DeadlineExceeded {
		log.Infof("Timeout writing %s: %v", conn.conID, v3.GetShortType(res.TypeUrl))
		xdsResponseWriteTimeouts.Increment()
	}
	return err
}

// nolint
func (conn *Connection) NonceAcked(typeUrl string) string {
	wr := conn.proxy.GetWatchedResource(typeUrl)
	if wr != nil {
		return wr.NonceAcked
	}
	return ""
}

// nolint
func (conn *Connection) NonceSent(typeUrl string) string {
	wr := conn.proxy.GetWatchedResource(typeUrl)
	if wr != nil {
		return wr.NonceSent
	}
	return ""
}

func (conn *Connection) Clusters() []string {
	wr := conn.proxy.GetWatchedResource(v3.EndpointType)
	if wr != nil {
		return wr.ResourceNames
	}
	return []string{}
}

// watchedResourcesByOrder returns the ordered list of
// watched resources for the proxy, ordered in accordance with known push order.
func (conn *Connection) watchedResourcesByOrder() []*model.WatchedResource {
	allWatched := conn.proxy.CloneWatchedResources()
	ordered := make([]*model.WatchedResource, 0, len(allWatched))
	// first add all known types, in order
	for _, tp := range PushOrder {
		if allWatched[tp] != nil {
			ordered = append(ordered, allWatched[tp])
		}
	}
	// Then add any undeclared types
	for tp, res := range allWatched {
		if !KnownOrderedTypeUrls.Contains(tp) {
			ordered = append(ordered, res)
		}
	}
	return ordered
}

// reportAllEventsForProxyNoPush reports all tracking events for a proxy without need to push xds.
func reportAllEventsForProxyNoPush(con *Connection, statusReporter DistributionStatusCache, nonce string) {
	if statusReporter == nil {
		return
	}
	for distributionType := range AllTrackingEventTypes {
		statusReporter.RegisterEvent(con.conID, distributionType, nonce)
	}
}

// reportEventsForUnWatched is to report events for unwatched types after push.
// e.g. there is no rds if no route configured for gateway.
// nolint
func reportEventsForUnWatched(con *Connection, statusReporter DistributionStatusCache, nonce string) {
	if statusReporter == nil {
		return
	}

	// if typeUrl is not empty, report all events that are not being watched
	unWatched := sets.NewWithLength[EventType](len(AllTrackingEventTypes))
	watchedTypes := con.proxy.GetWatchedResourceTypes()
	for tyeUrl := range AllTrackingEventTypes {
		if _, exists := watchedTypes[tyeUrl]; !exists {
			unWatched.Insert(tyeUrl)
		}
	}
	for tyeUrl := range unWatched {
		statusReporter.RegisterEvent(con.conID, tyeUrl, nonce)
	}
}
