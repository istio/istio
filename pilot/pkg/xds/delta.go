package xds

import (
	"errors"
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"time"
)

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
	// todo should this be a new constructor (newDeltaConnection)
	con := newConnection(peerAddr, nil, stream)
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
	reqChannel := make(chan *discovery.DeltaDiscoveryRequest, 1)
	go s.receiveDelta(con, reqChannel, &receiveError)

	// Wait for the proxy to be fully initialized before we start serving traffic. Because
	// initialization doesn't have dependencies that will block, there is no need to add any timeout
	// here. Prior to this explicit wait, we were implicitly waiting by receive() not sending to
	// reqChannel and the connection not being enqueued for pushes to pushChannel until the
	// initialization is complete.
	<-con.initialized

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
			err := s.processDeltaRequest(req, con)
			if err != nil {
				return err
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

func (s *DiscoveryServer) receiveDelta(con *Connection, reqChannel chan *discovery.DeltaDiscoveryRequest, errP *error) {
	defer func() {
		close(reqChannel)
		// Close the initialized channel, if its not already closed, to prevent blocking the stream
		select {
		case <-con.initialized:
		default:
			close(con.initialized)
		}
	}()
	firstReq := true
	for {
		req, err := con.deltaStream.Recv()
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
				if s.StatusGen != nil {
					s.StatusGen.OnDisconnect(con)
				}
				s.WorkloadEntryController.QueueUnregisterWorkload(con.proxy, con.Connect)
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

// processDeltaRequest is handling one delta request.
func (s *DiscoveryServer) processDeltaRequest(req *discovery.DeltaDiscoveryRequest, con *Connection) error {

	// todo preProcessRequest equivalent for deltas for health checking

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

	return s.pushDeltaXds(con, push, versionInfo(), con.Watched(req.TypeUrl), request)
}

func (s *DiscoveryServer) shouldRespondDelta(con *Connection, request *discovery.DeltaDiscoveryRequest) bool {
	stype := v3.GetShortType(request.TypeUrl)

	// If there is an error in request that means previous response is erroneous.
	// We do not have to respond in that case. In this case request's version info
	// will be different from the version sent. But it is fragile to rely on that.
	if request.ErrorDetail != nil {
		errCode := codes.Code(request.ErrorDetail.Code)
		adsLog.Warnf("ADS:%s: ACK ERROR %s %s:%s", stype, con.ConID, errCode.String(), request.ErrorDetail.GetMessage())
		incrementXDSRejects(request.TypeUrl, con.proxy.ID, errCode.String())
		if s.StatusGen != nil {
			s.StatusGen.OnNack(con.proxy, request)
		}
		con.proxy.Lock()
		con.proxy.WatchedResources[request.TypeUrl].NonceNacked = request.ResponseNonce
		con.proxy.Unlock()
		return false
	}

	// This is first request - initialize typeUrl watches.
	if request.ResponseNonce == "" {
		// is printing requested resource versions useful?
		adsLog.Debugf("ADS:%s: INIT %s %s %s", stype, con.ConID, request.InitialResourceVersions, request.ResponseNonce)
		con.proxy.Lock()
		// todo is there any use to store the last DeltaDiscoveryRequest?
		con.proxy.WatchedResources[request.TypeUrl] = &model.WatchedResource{TypeUrl: request.TypeUrl, ResourceNames: request.ResourceNamesSubscribe}
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
		adsLog.Debugf("ADS:%s: RECONNECT %s %s %s", stype, con.ConID, request.InitialResourceVersions, request.ResponseNonce)
		con.proxy.Lock()
		con.proxy.WatchedResources[request.TypeUrl] = &model.WatchedResource{TypeUrl: request.TypeUrl, ResourceNames: request.ResourceNamesSubscribe}
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
		//con.proxy.WatchedResources[request.TypeUrl].LastRequest = request
		con.proxy.Unlock()
		return false
	}

	// If it comes here, that means nonce match. This an ACK. We should record
	// the ack details and respond if there is a change in resource names.
	con.proxy.Lock()
	previousResources := con.proxy.WatchedResources[request.TypeUrl].ResourceNames
	//con.proxy.WatchedResources[request.TypeUrl].VersionAcked = request.VersionInfo
	con.proxy.WatchedResources[request.TypeUrl].NonceAcked = request.ResponseNonce
	con.proxy.WatchedResources[request.TypeUrl].NonceNacked = ""
	// add subscribe, remove unsubscribed
	removedSubs := generateDeltaSubcription(con.proxy.WatchedResources[request.TypeUrl].ResourceNames,
		request.ResourceNamesUnsubscribe)
	con.proxy.WatchedResources[request.TypeUrl].ResourceNames = append(removedSubs, request.ResourceNamesSubscribe...)
	//con.proxy.WatchedResources[request.TypeUrl].LastRequest = request
	con.proxy.Unlock()

	// Envoy can send two DiscoveryRequests with same version and nonce
	// when it detects a new resource. We should respond if they change.
	//if listEqualUnordered(previousResources, request.ResourceNames) {
	//	adsLog.Debugf("ADS:%s: ACK %s %s %s", stype, con.ConID, request.VersionInfo, request.ResponseNonce)
	//	return false
	//}
	adsLog.Debugf("ADS:%s: RESOURCE CHANGE previous resources: %v, new resources: %v %s %s %s", stype,
		previousResources, request.ResourceNames, con.ConID, request.VersionInfo, request.ResponseNonce)

	return true
}

func (s *DiscoveryServer) pushDeltaXds(con *Connection, push *model.PushContext,
	curVersion string, w *model.WatchedResource, req *model.PushRequest) error {
	if w == nil {
		return nil
	}
	gen := s.findGenerator(w.TypeUrl, con)
	if gen == nil {
		// todo shouldn't we return an error here?
		// doesnt this mean that the request that was sent had an unknown resource type?
		return nil
	}
	t0 := time.Now()
	res, err := gen.Generate(con.proxy, push, w, req)
	if err != nil || res == nil {
		if s.StatusReporter != nil {
			s.StatusReporter.RegisterEvent(con.ConID, w.TypeUrl, push.LedgerVersion)
		}
		return err
	}
	defer func() {
		recordPushTime(w.TypeUrl, time.Since(t0))
	}()

	deltaResources := convertResponseToDelta(curVersion, res)
	removedResources := generateResourceDiff(deltaResources,
		w.ResourceNames)

	deltaResp := &discovery.DeltaDiscoveryResponse{
		TypeUrl:           w.TypeUrl,
		SystemVersionInfo: curVersion,
		Nonce:             nonce(push.LedgerVersion),
		Resources:         deltaResources,
		RemovedResources:  removedResources,
	}

	if err := con.sendDelta(deltaResp); err != nil {
		recordSendError(w.TypeUrl, con.ConID, err)
		return err
	}

	if _, f := SkipLogTypes[w.TypeUrl]; !f {
		if adsLog.DebugEnabled() {
			// Add additional information to logs when debug mode enabled
			adsLog.Infof("%s: PUSH for node:%s resources:%d size:%s nonce:%v version:%v",
				v3.GetShortType(w.TypeUrl), con.proxy.ID, len(res), util.ByteCount(ResourceSize(res)), deltaResp.Nonce, deltaResp.SystemVersionInfo)
		} else {
			adsLog.Infof("%s: PUSH for node:%s resources:%d size:%s",
				v3.GetShortType(w.TypeUrl), con.proxy.ID, len(res), util.ByteCount(ResourceSize(res)))
		}
	}
	return nil
}

// returns resource names that do not appear in res that do appear in subscriptions
// todo this is wrong
func generateResourceDiff(resources []*discovery.Resource, subscriptions []string) []string {
	resourceNames := []string{}
	for _, res := range resources {
		resourceNames = append(resourceNames, res.Name)
	}
	// this is O(n^2) - can we make it faster?
	for i := 0; i < len(subscriptions); i++ {
		for _, resourceName := range resourceNames {
			if subscriptions[i] == resourceName {
				// found resource - remove
				// potential memory leak (?)
				subscriptions = append(subscriptions[:i], subscriptions[i+1:]...)
				i--
			}
		}
	}
	return subscriptions
}

// removes unsub from resources
func generateDeltaSubcription(resources []string, unsub []string) []string {
	for i := 0; i < len(resources); i++ {
		for _, rem := range unsub {
			if rem == resources[i] {
				resources = append(resources[:i], resources[i+1:]...)
				i--
			}
		}
	}
	return resources
}

// just for experimentation
func convertResponseToDelta(ver string, resources model.Resources) []*discovery.Resource {
	convert := []*discovery.Resource{}
	for _, r := range resources {
		var name string
		switch r.TypeUrl {
		case v3.ClusterType:
			aa := &cluster.Cluster{}
			ptypes.UnmarshalAny(r, aa)
			name = aa.Name
		case v3.ListenerType:
			aa := &listener.Listener{}
			ptypes.UnmarshalAny(r, aa)
			name = aa.Name
		case v3.EndpointType:
			aa := &endpoint.ClusterLoadAssignment{}
			ptypes.UnmarshalAny(r, aa)
			name = aa.ClusterName
		case v3.RouteType:
			aa := &route.RouteConfiguration{}
			ptypes.UnmarshalAny(r, aa)
			name = aa.Name
		}
		c := &discovery.Resource{
			Name:     name,
			Version:  ver,
			Resource: r,
		}
		convert = append(convert, c)
	}
	return convert
}