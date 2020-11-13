package xds

import (
	"errors"
	"fmt"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
)

type XdsServer struct {
	// Authenticate, wait until ready, etc
	InitializeStream func(*Connection) error

	ProcessRequest func(con *Connection, req *discovery.DiscoveryRequest) error
	PushConnection func(con *Connection, pushRequest *model.PushRequest) error

	// Generators allow customizing the generated config, based on the client metadata.
	// Key is the generator type - will match the Generator metadata to set the per-connection
	// default generator, or the combination of Generator metadata and TypeUrl to select a
	// different generator for a type.
	// Normal istio clients use the default generator - will not be impacted by this.
	Generators map[string]model.XdsResourceGenerator

	OnConnection func(con *Connection)
	OnDisconnect func(con *Connection)
	OnNack       func(proxy *model.Proxy, request *discovery.DiscoveryRequest)
}

func (s *XdsServer) findGenerator(typeURL string, con *Connection) model.XdsResourceGenerator {
	if g, f := s.Generators[con.proxy.Metadata.Generator+"/"+typeURL]; f {
		return g
	}

	if g, f := s.Generators[typeURL]; f {
		return g
	}

	// XdsResourceGenerator is the default generator for this connection. We want to allow
	// some types to use custom generators - for example EDS.
	g := con.proxy.XdsResourceGenerator
	if g == nil {
		// TODO move this to just directly using the resource TypeUrl
		g = s.Generators["api"] // default to "MCP" generators - any type supported by store
	}
	return g
}

// Push an XDS resource for the given connection. Configuration will be generated
// based on the passed in generator. Based on the updates field, generators may
// choose to send partial or even no response if there are no changes.
func (s *XdsServer) PushXds(con *Connection, push *model.PushContext,
	currentVersion string, w *model.WatchedResource, req *model.PushRequest) error {
	if w == nil {
		return nil
	}
	gen := s.findGenerator(w.TypeUrl, con)
	if gen == nil {
		return nil
	}

	t0 := time.Now()

	cl := gen.Generate(con.proxy, push, w, req)
	if cl == nil {
		// If we have nothing to send, report that we got an ACK for this version.
		//if s.StatusReporter != nil {
		//	s.StatusReporter.RegisterEvent(con.ConID, w.TypeUrl, push.Version)
		//}
		return nil // No push needed.
	}
	defer func() { recordPushTime(w.TypeUrl, time.Since(t0)) }()

	resp := &discovery.DiscoveryResponse{
		TypeUrl:     w.TypeUrl,
		VersionInfo: currentVersion,
		Nonce:       nonce(push.Version),
		Resources:   cl,
	}

	// Approximate size by looking at the Any marshaled size. This avoids high cost
	// proto.Size, at the expense of slightly under counting.
	size := 0
	for _, r := range cl {
		size += len(r.Value)
	}

	err := con.send(resp)
	if err != nil {
		recordSendError(w.TypeUrl, con.ConID, err)
		return err
	}

	// Some types handle logs inside Generate, skip them here
	if _, f := SkipLogTypes[w.TypeUrl]; !f {
		adsLog.Infof("%s: PUSH for node:%s resources:%d size:%s", v3.GetShortType(w.TypeUrl), con.proxy.ID, len(cl), util.ByteCount(size))
	}
	return nil
}

func getPeerAddress(stream DiscoveryStream) string {
	ctx := stream.Context()
	peerAddr := "0.0.0.0"
	if peerInfo, ok := peer.FromContext(ctx); ok {
		peerAddr = peerInfo.Addr.String()
	}
	return peerAddr
}

// StreamAggregatedResources implements the ADS interface.
func (s *XdsServer) Stream(stream DiscoveryStream) error {
	con := newConnection(stream)
	if err := s.InitializeStream(con); err != nil {
		return err
	}

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
			err := s.ProcessRequest(con, req)
			if err != nil {
				return err
			}

		case pushEv := <-con.pushChannel:
			// TODO: possible race condition: if a config change happens while the envoy
			// was getting the initial config, between LDS and RDS, the push will miss the
			// monitored 'routes'. Same for CDS/EDS interval. It is very tricky to handle
			// due to the protocol - but the periodic push recovers from it.
			err := s.PushConnection(con, pushEv.pushRequest)
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
func (s *XdsServer) ShouldRespond(con *Connection, request *discovery.DiscoveryRequest) bool {
	stype := v3.GetShortType(request.TypeUrl)

	// If there is an error in request that means previous response is erroneous.
	// We do not have to respond in that case. In this case request's version info
	// will be different from the version sent. But it is fragile to rely on that.
	if request.ErrorDetail != nil {
		errCode := codes.Code(request.ErrorDetail.Code)
		adsLog.Warnf("ADS:%s: ACK ERROR %s %s:%s", stype, con.ConID, errCode.String(), request.ErrorDetail.GetMessage())
		incrementXDSRejects(request.TypeUrl, con.proxy.ID, errCode.String())
		s.OnNack(con.proxy, request)
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

func (s *XdsServer) receive(con *Connection, reqChannel chan *discovery.DiscoveryRequest, errP *error) {
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
				s.OnDisconnect(con)
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

// update the node associated with the connection, after receiving a a packet from envoy, also adds the connection
// to the tracking map.
func (s *XdsServer) initConnection(node *core.Node, con *Connection) error {
	proxy, err := s.initProxy(node)
	if err != nil {
		return err
	}

	// Based on node metadata and version, we can associate a different generator.
	// TODO: use a map of generators, so it's easily customizable and to avoid deps
	proxy.WatchedResources = map[string]*model.WatchedResource{}

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

	s.OnConnection(con)

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

	return nil
}

// initProxy initializes the Proxy from node.
func (s *XdsServer) initProxy(node *core.Node) (*model.Proxy, error) {
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

	// Discover supported IP Versions of proxy so that appropriate config can be delivered.
	proxy.DiscoverIPVersions()

	return proxy, nil
}
