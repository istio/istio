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
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	labelutil "istio.io/istio/pilot/pkg/serviceregistry/util/label"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/env"
	pm "istio.io/istio/pkg/model"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/xds"
)

var (
	log = xds.Log

	// Tracks connections, increment on each new connection.
	connectionNumber = int64(0)
)

// Used only when running in KNative, to handle the load balancing behavior.
var firstRequest = uatomic.NewBool(true)

var knativeEnv = env.Register("K_REVISION", "",
	"KNative revision, set if running in knative").Get()

// DiscoveryStream is a server interface for XDS.
type DiscoveryStream = xds.DiscoveryStream

// DeltaDiscoveryStream is a server interface for Delta XDS.
type DeltaDiscoveryStream = discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesServer

// DiscoveryClient is a client interface for XDS.
type DiscoveryClient = discovery.AggregatedDiscoveryService_StreamAggregatedResourcesClient

// DeltaDiscoveryClient is a client interface for Delta XDS.
type DeltaDiscoveryClient = discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesClient

type Connection struct {
	xds.Connection

	// Original node metadata, to avoid unmarshal/marshal.
	// This is included in internal events.
	node *core.Node

	// proxy is the client to which this connection is established.
	proxy *model.Proxy

	// deltaStream is used for Delta XDS. Only one of deltaStream or stream will be set
	deltaStream DeltaDiscoveryStream

	deltaReqChan chan *discovery.DeltaDiscoveryRequest

	s   *DiscoveryServer
	ids []string
}

func (conn *Connection) XdsConnection() *xds.Connection {
	return &conn.Connection
}

func (conn *Connection) Proxy() *model.Proxy {
	return conn.proxy
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
		Connection: xds.NewConnection(peerAddr, stream),
	}
}

func (conn *Connection) Initialize(node *core.Node) error {
	return conn.s.initConnection(node, conn, conn.ids)
}

func (conn *Connection) Close() {
	conn.s.closeConnection(conn)
}

func (conn *Connection) Watcher() xds.Watcher {
	return conn.proxy
}

func (conn *Connection) Process(req *discovery.DiscoveryRequest) error {
	return conn.s.processRequest(req, conn)
}

func (conn *Connection) Push(ev any) error {
	pushEv := ev.(*Event)
	err := conn.s.pushConnection(conn, pushEv)
	pushEv.done()
	return err
}

// processRequest handles one discovery request. This is currently called from the 'main' thread, which also
// handles 'push' requests and close - the code will eventually call the 'push' code, and it needs more mutex
// protection. Original code avoided the mutexes by doing both 'push' and 'process requests' in same thread.
func (s *DiscoveryServer) processRequest(req *discovery.DiscoveryRequest, con *Connection) error {
	stype := v3.GetShortType(req.TypeUrl)
	log.Debugf("ADS:%s: REQ %s resources:%d nonce:%s version:%s ", stype,
		con.ID(), len(req.ResourceNames), req.ResponseNonce, req.VersionInfo)
	if req.TypeUrl == v3.HealthInfoType {
		s.handleWorkloadHealthcheck(con.proxy, req)
		return nil
	}

	// For now, don't let xDS piggyback debug requests start watchers.
	if strings.HasPrefix(req.TypeUrl, v3.DebugType) {
		return s.pushXds(con,
			&model.WatchedResource{TypeUrl: req.TypeUrl, ResourceNames: sets.New(req.ResourceNames...)},
			&model.PushRequest{Full: true, Push: con.proxy.LastPushContext, Forced: true})
	}

	shouldRespond, delta := xds.ShouldRespond(con.proxy, con.ID(), req)
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
		Start:  con.proxy.LastPushTime,
		Delta:  delta,
		Forced: true,
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
	// initialize with empty config, leading to reconnected Envoys losing
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
	s.globalPushContext().InitContext(s.Env, nil, nil)
	con := newConnection(peerAddr, stream)
	con.ids = ids
	con.s = s
	return xds.Stream(con)
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
	con.SetID(connectionID(proxy.ID))
	con.node = node
	con.proxy = proxy
	if proxy.IsZTunnel() && !features.EnableAmbient {
		return fmt.Errorf("ztunnel requires PILOT_ENABLE_AMBIENT=true")
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
	s.addCon(con.ID(), con)
	// Register that initialization is complete. This triggers to calls that it is safe to access the
	// proxy
	defer con.MarkInitialized()

	// Complete full initialization of the proxy
	if err := s.initializeProxy(con); err != nil {
		s.closeConnection(con)
		return err
	}
	return nil
}

func (s *DiscoveryServer) closeConnection(con *Connection) {
	if con.ID() == "" {
		return
	}
	s.removeCon(con.ID())
	s.WorkloadEntryController.OnDisconnect(con)
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
		return nil, status.New(codes.InvalidArgument, err.Error()).Err()
	}
	proxy, err := model.ParseServiceNodeWithMetadata(node.Id, meta)
	if err != nil {
		return nil, status.New(codes.InvalidArgument, err.Error()).Err()
	}
	// Update the config namespace associated with this proxy
	proxy.ConfigNamespace = model.GetProxyConfigNamespace(proxy)
	proxy.XdsNode = node

	proxy.Metadata.EnableTrailingDot = model.StringBool(computeEnableTrailingDot(node, proxy.Metadata))

	return proxy, nil
}

// computeEnableTrailingDot determines whether trailing dots should be enabled for this client.
// Computed once at connection time and cached in proxy.Metadata.EnableTrailingDot.
//
// Precedence: client metadata (TRAILING_DOT) > user agent detection > system default
func computeEnableTrailingDot(node *core.Node, metadata *model.NodeMetadata) bool {
	if node == nil || metadata == nil {
		// Default to system-wide setting
		return features.EnableAbsoluteFqdnVhostDomain
	}

	// Check for explicit client metadata override
	if trailingDotRaw, exists := metadata.Raw["TRAILING_DOT"]; exists {
		switch v := trailingDotRaw.(type) {
		case string:
			if parsed, err := strconv.ParseBool(v); err == nil {
				return parsed
			}
			log.Warnf("Invalid TRAILING_DOT value %q for node %s, falling back to auto-detection", v, node.GetId())
		case bool:
			return v
		default:
			log.Warnf("TRAILING_DOT must be string or bool, got %T for node %s", trailingDotRaw, node.GetId())
		}
	}

	// Auto-detect gRPC Java clients
	if node.GetUserAgentName() == "gRPC Java" {
		return false
	}

	return features.EnableAbsoluteFqdnVhostDomain
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
		localityLabel := pm.GetLocalityLabel(proxy.Labels)
		if localityLabel != "" {
			return util.ConvertLocality(localityLabel)
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
	proxy.Lock()
	defer proxy.Unlock()
	var shouldResetGateway, shouldResetSidecarScope bool
	// 1. If request == nil(initiation phase) or request.ConfigsUpdated == nil(global push), set proxy serviceTargets.
	// 2. otherwise only set when svc update, this is for the case that a service may select the proxy
	if request == nil || request.Forced ||
		proxy.ShouldUpdateServiceTargets(request.ConfigsUpdated) {
		proxy.SetServiceTargets(s.Env.ServiceDiscovery)
		// proxy.SetGatewaysForProxy depends on the serviceTargets,
		// so when we reset serviceTargets, should reset gateway as well.
		shouldResetGateway = true
	}

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
	push := proxy.LastPushContext
	if request == nil {
		shouldResetSidecarScope = true
	} else {
		push = request.Push
		if request.Forced {
			shouldResetSidecarScope = true
		}
		for conf := range request.ConfigsUpdated {
			switch conf.Kind {
			case kind.ServiceEntry, kind.DestinationRule, kind.VirtualService, kind.Sidecar:
				shouldResetSidecarScope = true
			case kind.Gateway:
				shouldResetGateway = true
			case kind.Ingress:
				shouldResetSidecarScope = true
				shouldResetGateway = true
			}
			if shouldResetSidecarScope && shouldResetGateway {
				break
			}
		}
	}
	// compute the sidecarscope for both proxy type whenever it changes.
	if shouldResetSidecarScope {
		proxy.SetSidecarScope(push)
	}
	// only compute gateways for "router" type proxy.
	if shouldResetGateway && proxy.Type == model.Router {
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

	pushRequest, needsPush := s.ProxyNeedsPush(con.proxy, pushRequest)
	if !needsPush {
		log.Debugf("Skipping push to %v, no updates required", con.ID())
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
	proxiesConvergeDelay.Record(time.Since(pushRequest.Start).Seconds())
	return nil
}

// PushOrder defines the order that updates will be pushed in. Any types not listed here will be pushed in random
// order after the types listed here
var PushOrder = []string{
	v3.ClusterType,
	v3.EndpointType,
	v3.ListenerType,
	v3.RouteType,
	v3.SecretType,
	v3.AddressType,
	v3.WorkloadType,
	v3.WorkloadAuthorizationType,
}

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
		Forced: true,
	})
}

// AdsPushAll will send updates to all nodes, with a full push.
// Mainly used in Debug interface.
func AdsPushAll(s *DiscoveryServer) {
	s.AdsPushAll(&model.PushRequest{
		Full:   true,
		Push:   s.globalPushContext(),
		Reason: model.NewReasonStats(model.DebugTrigger),
		Forced: true,
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
		xds.TotalXDSInternalErrors.Increment()
	} else {
		delete(s.adsClients, conID)
		recordXDSClients(con.proxy.Metadata.IstioVersion, -1)
	}
}

func (conn *Connection) Clusters() []string {
	return conn.proxy.Clusters()
}

// watchedResourcesByOrder returns the ordered list of
// watched resources for the proxy, ordered in accordance with known push order.
func (conn *Connection) watchedResourcesByOrder() []*model.WatchedResource {
	allWatched := conn.proxy.ShallowCloneWatchedResources()
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
