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
	"strconv"
	"strings"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"istio.io/istio/pilot/pkg/features"
	istiogrpc "istio.io/istio/pilot/pkg/grpc"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
	_ "istio.io/istio/pkg/util/protomarshal" // Ensure we get the more efficient vtproto gRPC encoder
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/xds"
)

var deltaLog = istiolog.RegisterScope("delta", "delta xds debugging")

func (s *DiscoveryServer) StreamDeltas(stream DeltaDiscoveryStream) error {
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
		return errors.New("server is not ready to serve discovery information")
	}

	ctx := stream.Context()
	peerAddr := "0.0.0.0"
	if peerInfo, ok := peer.FromContext(ctx); ok {
		peerAddr = peerInfo.Addr.String()
	}

	if err := s.WaitForRequestLimit(stream.Context()); err != nil {
		deltaLog.Warnf("ADS: %q exceeded rate limit: %v", peerAddr, err)
		return status.Errorf(codes.ResourceExhausted, "request rate limit exceeded: %v", err)
	}

	ids, err := s.authenticate(ctx)
	if err != nil {
		return status.Error(codes.Unauthenticated, err.Error())
	}
	if ids != nil {
		deltaLog.Debugf("Authenticated XDS: %v with identity %v", peerAddr, ids)
	} else {
		deltaLog.Debugf("Unauthenticated XDS: %v", peerAddr)
	}

	// InitContext returns immediately if the context was already initialized.
	s.globalPushContext().InitContext(s.Env, nil, nil)
	con := newDeltaConnection(peerAddr, stream)

	// Do not call: defer close(con.pushChannel). The push channel will be garbage collected
	// when the connection is no longer used. Closing the channel can cause subtle race conditions
	// with push. According to the spec: "It's only necessary to close a channel when it is important
	// to tell the receiving goroutines that all data have been sent."

	// Block until either a request is received or a push is triggered.
	// We need 2 go routines because 'read' blocks in Recv().
	go s.receiveDelta(con, ids)

	// Wait for the proxy to be fully initialized before we start serving traffic. Because
	// initialization doesn't have dependencies that will block, there is no need to add any timeout
	// here. Prior to this explicit wait, we were implicitly waiting by receive() not sending to
	// reqChannel and the connection not being enqueued for pushes to pushChannel until the
	// initialization is complete.
	<-con.InitializedCh()

	for {
		// Go select{} statements are not ordered; the same channel can be chosen many times.
		// For requests, these are higher priority (client may be blocked on startup until these are done)
		// and often very cheap to handle (simple ACK), so we check it first.
		select {
		case req, ok := <-con.deltaReqChan:
			if ok {
				if err := s.processDeltaRequest(req, con); err != nil {
					return err
				}
			} else {
				// Remote side closed connection or error processing the request.
				return <-con.ErrorCh()
			}
		case <-con.StopCh():
			return nil
		default:
		}
		// If there wasn't already a request, poll for requests and pushes. Note: if we have a huge
		// amount of incoming requests, we may still send some pushes, as we do not `continue` above;
		// however, requests will be handled ~2x as much as pushes. This ensures a wave of requests
		// cannot completely starve pushes. However, this scenario is unlikely.
		select {
		case req, ok := <-con.deltaReqChan:
			if ok {
				if err := s.processDeltaRequest(req, con); err != nil {
					return err
				}
			} else {
				// Remote side closed connection or error processing the request.
				return <-con.ErrorCh()
			}
		case ev := <-con.PushCh():
			pushEv := ev.(*Event)
			err := s.pushConnectionDelta(con, pushEv)
			pushEv.done()
			if err != nil {
				return err
			}
		case <-con.StopCh():
			return nil
		}
	}
}

// Compute and send the new configuration for a connection.
func (s *DiscoveryServer) pushConnectionDelta(con *Connection, pushEv *Event) error {
	pushRequest := pushEv.pushRequest

	if pushRequest.Full {
		// Update Proxy with current information.
		s.computeProxyState(con.proxy, pushRequest)
	}

	pushRequest, needsPush := s.ProxyNeedsPush(con.proxy, pushRequest)
	if !needsPush {
		deltaLog.Debugf("Skipping push to %v, no updates required", con.ID())
		return nil
	}

	// Send pushes to all generators
	// Each Generator is responsible for determining if the push event requires a push
	wrl := con.watchedResourcesByOrder()
	for _, w := range wrl {
		if err := s.pushDeltaXds(con, w, pushRequest); err != nil {
			return err
		}
	}

	proxiesConvergeDelay.Record(time.Since(pushRequest.Start).Seconds())
	return nil
}

func (s *DiscoveryServer) receiveDelta(con *Connection, identities []string) {
	defer func() {
		close(con.deltaReqChan)
		close(con.ErrorCh())
		// Close the initialized channel, if its not already closed, to prevent blocking the stream
		select {
		case <-con.InitializedCh():
		default:
			close(con.InitializedCh())
		}
	}()
	firstRequest := true
	for {
		req, err := con.deltaStream.Recv()
		if err != nil {
			if istiogrpc.GRPCErrorType(err) != istiogrpc.UnexpectedError {
				deltaLog.Infof("ADS: %q %s terminated", con.Peer(), con.ID())
				return
			}
			con.ErrorCh() <- err
			deltaLog.Errorf("ADS: %q %s terminated with error: %v", con.Peer(), con.ID(), err)
			xds.TotalXDSInternalErrors.Increment()
			return
		}
		// This should be only set for the first request. The node id may not be set - for example malicious clients.
		if firstRequest {
			// probe happens before envoy sends first xDS request
			if req.TypeUrl == v3.HealthInfoType {
				log.Warnf("ADS: %q %s send health check probe before normal xDS request", con.Peer(), con.ID())
				continue
			}
			firstRequest = false
			if req.Node == nil || req.Node.Id == "" {
				con.ErrorCh() <- status.New(codes.InvalidArgument, "missing node information").Err()
				return
			}
			if err := s.initConnection(req.Node, con, identities); err != nil {
				con.ErrorCh() <- err
				return
			}
			defer s.closeConnection(con)
			deltaLog.Infof("ADS: new delta connection for node:%s", con.ID())
		}

		select {
		case con.deltaReqChan <- req:
		case <-con.deltaStream.Context().Done():
			deltaLog.Infof("ADS: %q %s terminated with stream closed", con.Peer(), con.ID())
			return
		}
	}
}

func (conn *Connection) sendDelta(res *discovery.DeltaDiscoveryResponse, newResourceNames sets.String) error {
	sendResonse := func() error {
		start := time.Now()
		defer func() { xds.RecordSendTime(time.Since(start)) }()
		return conn.deltaStream.Send(res)
	}
	err := sendResonse()
	if err == nil {
		if !strings.HasPrefix(res.TypeUrl, v3.DebugType) {
			conn.proxy.UpdateWatchedResource(res.TypeUrl, func(wr *model.WatchedResource) *model.WatchedResource {
				if wr == nil {
					wr = &model.WatchedResource{TypeUrl: res.TypeUrl}
				}
				// some resources dynamically update ResourceNames. Most don't though
				if newResourceNames != nil {
					wr.ResourceNames = newResourceNames
				}
				wr.NonceSent = res.Nonce
				wr.LastSendTime = time.Now()
				if features.EnableUnsafeDeltaTest {
					wr.LastResources = applyDelta(wr.LastResources, res)
				}
				return wr
			})
		}
	} else if status.Convert(err).Code() == codes.DeadlineExceeded {
		deltaLog.Infof("Timeout writing %s: %v", conn.ID(), v3.GetShortType(res.TypeUrl))
		xds.ResponseWriteTimeouts.Increment()
	}
	return err
}

// processDeltaRequest is handling one request. This is currently called from the 'main' thread, which also
// handles 'push' requests and close - the code will eventually call the 'push' code, and it needs more mutex
// protection. Original code avoided the mutexes by doing both 'push' and 'process requests' in same thread.
func (s *DiscoveryServer) processDeltaRequest(req *discovery.DeltaDiscoveryRequest, con *Connection) error {
	stype := v3.GetShortType(req.TypeUrl)
	deltaLog.Debugf("ADS:%s: REQ %s resources sub:%d unsub:%d nonce:%s", stype,
		con.ID(), len(req.ResourceNamesSubscribe), len(req.ResourceNamesUnsubscribe), req.ResponseNonce)

	if req.TypeUrl == v3.HealthInfoType {
		s.handleWorkloadHealthcheck(con.proxy, deltaToSotwRequest(req))
		return nil
	}
	if strings.HasPrefix(req.TypeUrl, v3.DebugType) {
		return s.pushDeltaXds(con,
			&model.WatchedResource{TypeUrl: req.TypeUrl, ResourceNames: sets.New(req.ResourceNamesSubscribe...)},
			&model.PushRequest{Full: true, Push: con.proxy.LastPushContext, Forced: true})
	}

	shouldRespond := shouldRespondDelta(con, req)
	if !shouldRespond {
		return nil
	}

	subs, _, _ := deltaWatchedResources(nil, req)
	request := &model.PushRequest{
		Full:   true,
		Push:   con.proxy.LastPushContext,
		Reason: model.NewReasonStats(model.ProxyRequest),

		// The usage of LastPushTime (rather than time.Now()), is critical here for correctness; This time
		// is used by the XDS cache to determine if a entry is stale. If we use Now() with an old push context,
		// we may end up overriding active cache entries with stale ones.
		Start: con.proxy.LastPushTime,
		Delta: model.ResourceDelta{
			// Record sub/unsub, but drop synthetic wildcard info
			Subscribed:   subs,
			Unsubscribed: sets.New(req.ResourceNamesUnsubscribe...).Delete("*"),
		},
		Forced: true,
	}
	// SidecarScope for the proxy may has not been updated based on this pushContext.
	// It can happen when `processRequest` comes after push context has been updated(s.initPushContext),
	// but before proxy's SidecarScope has been updated(s.updateProxy).
	if con.proxy.SidecarScope != nil && con.proxy.SidecarScope.Version != request.Push.PushVersion {
		s.computeProxyState(con.proxy, request)
	}

	err := s.pushDeltaXds(con, con.proxy.GetWatchedResource(req.TypeUrl), request)
	if err != nil {
		return err
	}
	// Anytime we get a CDS request on reconnect, we should always push EDS as well.
	// It is always the server's responsibility to send EDS after CDS, regardless if
	// Envoy asks for it or not (See https://github.com/envoyproxy/envoy/issues/33607 for more details).
	// Without this logic, there are cases where the clusters we send could stay warming forever,
	// expecting an EDS response. Note that in SotW, Envoy sends an EDS request after the delayed
	// CDS request; however, this is not guaranteed in delta, and has been observed to cause issues
	// with EDS and SDS.
	// This can happen with the following sequence
	// 1. Envoy disconnects and reconnects to Istiod.
	// 2. Envoy sends EDS request and we respond with it.
	// 3. Envoy sends CDS request and we respond with clusters.
	// 4. Envoy detects a change in cluster state and tries to warm those clusters but never sends
	//    an EDS request for them.
	// 5. Therefore, any initial CDS request should always trigger an EDS response
	// 	  to let Envoy finish cluster warming.
	// Refer to https://github.com/envoyproxy/envoy/issues/13009 for some more details on this type of issues.
	if req.TypeUrl != v3.ClusterType {
		return nil
	}
	return s.forceEDSPush(con)
}

func (s *DiscoveryServer) forceEDSPush(con *Connection) error {
	if dwr := con.proxy.GetWatchedResource(v3.EndpointType); dwr != nil {
		request := &model.PushRequest{
			Full:   true,
			Push:   con.proxy.LastPushContext,
			Reason: model.NewReasonStats(model.DependentResource),
			Start:  con.proxy.LastPushTime,
			Forced: true,
		}
		deltaLog.Infof("ADS:%s: FORCE %s PUSH for warming.", v3.GetShortType(v3.EndpointType), con.ID())
		return s.pushDeltaXds(con, dwr, request)
	}
	return nil
}

// shouldRespondDelta determines whether this request needs to be responded back. It applies the ack/nack rules as per xds protocol
// using WatchedResource for previous state and discovery request for the current state.
func shouldRespondDelta(con *Connection, request *discovery.DeltaDiscoveryRequest) bool {
	stype := v3.GetShortType(request.TypeUrl)

	// If there is an error in request that means previous response is erroneous.
	// We do not have to respond in that case. In this case request's version info
	// will be different from the version sent. But it is fragile to rely on that.
	if request.ErrorDetail != nil {
		errCode := codes.Code(request.ErrorDetail.Code)
		deltaLog.Warnf("ADS:%s: ACK ERROR %s %s:%s", stype, con.ID(), errCode.String(), request.ErrorDetail.GetMessage())
		xds.IncrementXDSRejects(request.TypeUrl, con.proxy.ID, errCode.String())
		con.proxy.UpdateWatchedResource(request.TypeUrl, func(wr *model.WatchedResource) *model.WatchedResource {
			wr.LastError = request.ErrorDetail.GetMessage()
			return wr
		})
		return false
	}

	deltaLog.Debugf("ADS:%s REQUEST %v: sub:%v unsub:%v initial:%v", stype, con.ID(),
		request.ResourceNamesSubscribe, request.ResourceNamesUnsubscribe, request.InitialResourceVersions)
	previousInfo := con.proxy.GetWatchedResource(request.TypeUrl)

	// This can happen in two cases:
	// 1. Envoy initially send request to Istiod
	// 2. Envoy reconnect to Istiod i.e. Istiod does not have
	// information about this typeUrl, but Envoy sends response nonce - either
	// because Istiod is restarted or Envoy disconnects and reconnects.
	// We should always respond with the current resource names.
	if previousInfo == nil {
		con.proxy.Lock()
		defer con.proxy.Unlock()

		if len(request.InitialResourceVersions) > 0 {
			deltaLog.Debugf("ADS:%s: RECONNECT %s %s resources:%v", stype, con.ID(), request.ResponseNonce, len(request.InitialResourceVersions))
		} else {
			deltaLog.Debugf("ADS:%s: INIT %s %s", stype, con.ID(), request.ResponseNonce)
		}

		res, wildcard, _ := deltaWatchedResources(nil, request)
		skip := request.TypeUrl == v3.AddressType && wildcard
		if skip {
			// Due to the high resource count in WDS at scale, we do not store ResourceName.
			// See the workload generator for more information on why we don't use this.
			res = nil
		}
		con.proxy.WatchedResources[request.TypeUrl] = &model.WatchedResource{
			TypeUrl:       request.TypeUrl,
			ResourceNames: res,
			Wildcard:      wildcard,
		}
		return true
	}

	// If there is mismatch in the nonce, that is a case of expired/stale nonce.
	// A nonce becomes stale following a newer nonce being sent to Envoy.
	if request.ResponseNonce != "" && request.ResponseNonce != previousInfo.NonceSent {
		deltaLog.Debugf("ADS:%s: REQ %s Expired nonce received %s, sent %s", stype,
			con.ID(), request.ResponseNonce, previousInfo.NonceSent)
		xds.ExpiredNonce.With(typeTag.Value(v3.GetMetricType(request.TypeUrl))).Increment()
		return false
	}

	// Spontaneous DeltaDiscoveryRequests from the client.
	// This can be done to dynamically add or remove elements from the tracked resource_names set.
	// In this case response_nonce is empty.
	spontaneousReq := request.ResponseNonce == ""

	var alwaysRespond bool
	var subChanged bool

	// Update resource names, and record ACK if required.
	con.proxy.UpdateWatchedResource(request.TypeUrl, func(wr *model.WatchedResource) *model.WatchedResource {
		wr.ResourceNames, _, subChanged = deltaWatchedResources(wr.ResourceNames, request)
		if !spontaneousReq {
			// Clear last error, we got an ACK.
			// Otherwise, this is just a change in resource subscription, so leave the last ACK info in place.
			wr.LastError = ""
			wr.NonceAcked = request.ResponseNonce
		}
		alwaysRespond = wr.AlwaysRespond
		wr.AlwaysRespond = false
		return wr
	})

	// It is invalid in the below two cases:
	// 1. no subscribed resources change from spontaneous delta request.
	// 2. subscribed resources changes from ACK.
	if spontaneousReq && !subChanged || !spontaneousReq && subChanged {
		deltaLog.Errorf("ADS:%s: Subscribed resources check mismatch: %v vs %v", stype, spontaneousReq, subChanged)
		if features.EnableUnsafeAssertions {
			panic(fmt.Sprintf("ADS:%s: Subscribed resources check mismatch: %v vs %v", stype, spontaneousReq, subChanged))
		}
	}

	// Envoy can send two DiscoveryRequests with same version and nonce
	// when it detects a new resource. We should respond if they change.
	if !subChanged {
		// We should always respond "alwaysRespond" marked requests to let Envoy finish warming
		// even though Nonce match and it looks like an ACK.
		if alwaysRespond {
			deltaLog.Infof("ADS:%s: FORCE RESPONSE %s for warming.", stype, con.ID())
			return true
		}

		deltaLog.Debugf("ADS:%s: ACK %s %s", stype, con.ID(), request.ResponseNonce)
		return false
	}
	deltaLog.Debugf("ADS:%s: RESOURCE CHANGE %s %s", stype, con.ID(), request.ResponseNonce)

	return true
}

// Push a Delta XDS resource for the given connection.
func (s *DiscoveryServer) pushDeltaXds(con *Connection, w *model.WatchedResource, req *model.PushRequest) error {
	if w == nil {
		return nil
	}
	gen := s.findGenerator(w.TypeUrl, con)
	if gen == nil {
		return nil
	}
	t0 := time.Now()

	originalW := w
	// If delta is set, client is requesting new resources or removing old ones. We should just generate the
	// new resources it needs, rather than the entire set of known resources.
	// Note: we do not need to account for unsubscribed resources as these are handled by parent removal;
	// See https://www.envoyproxy.io/docs/envoy/latest/api-docs/xds_protocol#deleting-resources.
	// This means if there are only removals, we will not respond.
	var logFiltered string
	if !req.Delta.IsEmpty() && !requiresResourceNamesModification(w.TypeUrl) {
		// Some types opt out of this and natively handle req.Delta
		logFiltered = " filtered:" + strconv.Itoa(len(w.ResourceNames)-len(req.Delta.Subscribed))
		w = &model.WatchedResource{
			TypeUrl:       w.TypeUrl,
			ResourceNames: req.Delta.Subscribed,
		}
	}

	var res model.Resources
	var deletedRes model.DeletedResources
	var logdata model.XdsLogDetails
	var usedDelta bool
	var err error
	switch g := gen.(type) {
	case model.XdsDeltaResourceGenerator:
		res, deletedRes, logdata, usedDelta, err = g.GenerateDeltas(con.proxy, req, w)
		if features.EnableUnsafeDeltaTest {
			fullRes, l, _ := g.Generate(con.proxy, originalW, req)
			s.compareDiff(con, originalW, fullRes, res, deletedRes, usedDelta, req.Delta, l.Incremental)
		}
	case model.XdsResourceGenerator:
		res, logdata, err = g.Generate(con.proxy, w, req)
	}
	if err != nil || (res == nil && deletedRes == nil) {
		return err
	}
	defer func() { recordPushTime(w.TypeUrl, time.Since(t0)) }()
	resp := &discovery.DeltaDiscoveryResponse{
		ControlPlane: ControlPlane(w.TypeUrl),
		TypeUrl:      w.TypeUrl,
		// TODO: send different version for incremental eds
		SystemVersionInfo: req.Push.PushVersion,
		Nonce:             nonce(req.Push.PushVersion),
		Resources:         res,
	}
	if usedDelta {
		resp.RemovedResources = deletedRes
	} else if req.Full {
		// similar to sotw
		removed := w.ResourceNames.Copy()
		for _, r := range res {
			removed.Delete(r.Name)
		}
		resp.RemovedResources = sets.SortedList(removed)
	}
	var newResourceNames sets.String
	if shouldSetWatchedResources(w) {
		// Set the new watched resources. Do not write to w directly, as it can be a copy from the 'filtered' logic above
		if usedDelta {
			// Apply the delta
			newResourceNames = w.ResourceNames.Copy().
				DeleteAll(resp.RemovedResources...)
			for _, r := range res {
				newResourceNames.Insert(r.Name)
			}
		} else {
			newResourceNames = resourceNamesSet(res)
		}
	}
	if neverRemoveDelta(w.TypeUrl) {
		resp.RemovedResources = nil
	}
	if len(resp.RemovedResources) > 0 {
		deltaLog.Debugf("ADS:%v REMOVE for node:%s %v", v3.GetShortType(w.TypeUrl), con.ID(), resp.RemovedResources)
	}

	configSize := ResourceSize(res)
	configSizeBytes.With(typeTag.Value(w.TypeUrl)).Record(float64(configSize))

	ptype := "PUSH"
	info := ""
	if logdata.Incremental {
		ptype = "PUSH INC"
	}
	if len(logdata.AdditionalInfo) > 0 {
		info = " " + logdata.AdditionalInfo
	}
	if len(logFiltered) > 0 {
		info += logFiltered
	}

	if err := con.sendDelta(resp, newResourceNames); err != nil {
		logger := deltaLog.Debugf
		if recordSendError(w.TypeUrl, err) {
			logger = deltaLog.Warnf
		}
		if deltaLog.DebugEnabled() {
			logger("%s: Send failure for node:%s resources:%d size:%s%s: %v",
				v3.GetShortType(w.TypeUrl), con.proxy.ID, len(res), util.ByteCount(configSize), info, err)
		}
		return err
	}

	switch {
	case !req.Full:
		if deltaLog.DebugEnabled() {
			deltaLog.Debugf("%s: %s%s for node:%s resources:%d size:%s%s",
				v3.GetShortType(w.TypeUrl), ptype, req.PushReason(), con.proxy.ID, len(res), util.ByteCount(configSize), info)
		}
	default:
		debug := ""
		if deltaLog.DebugEnabled() {
			// Add additional information to logs when debug mode enabled.
			debug = " nonce:" + resp.Nonce + " version:" + resp.SystemVersionInfo
		}
		deltaLog.Infof("%s: %s%s for node:%s resources:%d removed:%d size:%v%s%s",
			v3.GetShortType(w.TypeUrl), ptype, req.PushReason(), con.proxy.ID, len(res), len(resp.RemovedResources),
			util.ByteCount(ResourceSize(res)), info, debug)
	}

	return nil
}

func resourceNamesSet(res model.Resources) sets.Set[string] {
	return sets.New(slices.Map(res, func(r *discovery.Resource) string {
		return r.Name
	})...)
}

// requiresResourceNamesModification checks if a generator needs mutable access to w.ResourceNames.
// This is used when resources are spontaneously pushed during Delta XDS
func requiresResourceNamesModification(url string) bool {
	return url == v3.AddressType || url == v3.WorkloadType
}

// neverRemoveDelta checks if a type should never remove resources
func neverRemoveDelta(url string) bool {
	// https://github.com/envoyproxy/envoy/issues/32823
	// We want to garbage collect extensions when they are no longer referenced, rather than delete immediately
	return url == v3.ExtensionConfigurationType
}

// shouldSetWatchedResources indicates whether we should set the watched resources for a given type.
// for some type like `Address` we customly handle it in the generator
func shouldSetWatchedResources(w *model.WatchedResource) bool {
	if requiresResourceNamesModification(w.TypeUrl) {
		// These handle it directly in the generator
		return false
	}
	// Else fallback based on type
	return xds.IsWildcardTypeURL(w.TypeUrl)
}

func newDeltaConnection(peerAddr string, stream DeltaDiscoveryStream) *Connection {
	return &Connection{
		Connection:   xds.NewConnection(peerAddr, nil),
		deltaStream:  stream,
		deltaReqChan: make(chan *discovery.DeltaDiscoveryRequest, 1),
	}
}

// To satisfy methods that need DiscoveryRequest. Not suitable for real usage
func deltaToSotwRequest(request *discovery.DeltaDiscoveryRequest) *discovery.DiscoveryRequest {
	return &discovery.DiscoveryRequest{
		Node:          request.Node,
		ResourceNames: request.ResourceNamesSubscribe,
		TypeUrl:       request.TypeUrl,
		ResponseNonce: request.ResponseNonce,
		ErrorDetail:   request.ErrorDetail,
	}
}

// deltaWatchedResources returns current watched resources of delta xds
func deltaWatchedResources(existing sets.String, request *discovery.DeltaDiscoveryRequest) (sets.String, bool, bool) {
	res := existing
	if res == nil {
		res = sets.New[string]()
	}
	changed := false
	for _, r := range request.ResourceNamesSubscribe {
		if !res.InsertContains(r) {
			changed = true
		}
	}
	// This is set by Envoy on first request on reconnection so that we are aware of what Envoy knows
	// and can continue the xDS session properly.
	for r := range request.InitialResourceVersions {
		if !res.InsertContains(r) {
			changed = true
		}
	}
	for _, r := range request.ResourceNamesUnsubscribe {
		if res.DeleteContains(r) {
			changed = true
		}
	}
	wildcard := false
	// A request is wildcard if they explicitly subscribe to "*" or subscribe to nothing
	if res.Contains("*") {
		wildcard = true
		res.Delete("*")
	}
	// "if the client sends a request but has never explicitly subscribed to any resource names, the
	// server should treat that identically to how it would treat the client having explicitly
	// subscribed to *"
	// NOTE: this means you cannot subscribe to nothing, which is useful for on-demand loading; to workaround this
	// Istio clients will send and initial request both subscribing+unsubscribing to `*`.
	if len(request.ResourceNamesSubscribe) == 0 {
		wildcard = true
	}
	return res, wildcard, changed
}
