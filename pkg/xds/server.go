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
	"strings"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	"istio.io/istio/pilot/pkg/features"
	istiogrpc "istio.io/istio/pilot/pkg/grpc"
	"istio.io/istio/pkg/model"
	"istio.io/istio/pkg/util/sets"
)

// ResourceDelta records the difference in requested resources by an XDS client
type ResourceDelta struct {
	// Subscribed indicates the client requested these additional resources
	Subscribed sets.String
	// Unsubscribed indicates the client no longer requires these resources
	Unsubscribed sets.String
}

var emptyResourceDelta = ResourceDelta{}

func (rd ResourceDelta) IsEmpty() bool {
	return len(rd.Subscribed) == 0 && len(rd.Unsubscribed) == 0
}

type Resources = []*discovery.Resource

func ResourcesToAny(r Resources) []*anypb.Any {
	a := make([]*anypb.Any, 0, len(r))
	for _, rr := range r {
		a = append(a, rr.Resource)
	}
	return a
}

// WatchedResource tracks an active DiscoveryRequest subscription.
type WatchedResource struct {
	// TypeUrl is copied from the DiscoveryRequest.TypeUrl that initiated watching this resource.
	// nolint
	TypeUrl string

	// ResourceNames tracks the list of resources that are actively watched.
	// For LDS and CDS, all resources of the TypeUrl type are watched if it is empty.
	// For endpoints the resource names will have list of clusters and for clusters it is empty.
	// For Delta Xds, all resources of the TypeUrl that a client has subscribed to.
	ResourceNames sets.String

	// Wildcard indicates the subscription is a wildcard subscription. This only applies to types that
	// allow both wildcard and non-wildcard subscriptions.
	Wildcard bool

	// NonceSent is the nonce sent in the last sent response. If it is equal with NonceAcked, the
	// last message has been processed. If empty: we never sent a message of this type.
	NonceSent string

	// NonceAcked is the last acked message.
	NonceAcked string

	// AlwaysRespond, if true, will ensure that even when a request would otherwise be treated as an
	// ACK, it will be responded to. This typically happens when a proxy reconnects to another instance of
	// Istiod. In that case, Envoy expects us to respond to EDS/RDS/SDS requests to finish warming of
	// clusters/listeners.
	// Typically, this should be set to 'false' after response; keeping it true would likely result in an endless loop.
	AlwaysRespond bool

	// LastSendTime tracks the last time we sent a message. This should change every time NonceSent changes.
	LastSendTime time.Time

	// LastError records the last error returned, if any. This is cleared on any successful ACK.
	LastError string

	// LastResources tracks the contents of the last push.
	// This field is extremely expensive to maintain and is typically disabled
	LastResources Resources
}

type Watcher interface {
	DeleteWatchedResource(url string)
	GetWatchedResource(url string) *WatchedResource
	NewWatchedResource(url string, names []string)
	UpdateWatchedResource(string, func(*WatchedResource) *WatchedResource)
	// GetID identifies an xDS client. This is different from a connection ID.
	GetID() string
}

// IsWildcardTypeURL checks whether a given type is a wildcard type
// https://www.envoyproxy.io/docs/envoy/latest/api-docs/xds_protocol#how-the-client-specifies-what-resources-to-return
// If the list of resource names becomes empty, that means that the client is no
// longer interested in any resources of the specified type. For Listener and
// Cluster resource types, there is also a “wildcard” mode, which is triggered
// when the initial request on the stream for that resource type contains no
// resource names.
func IsWildcardTypeURL(typeURL string) bool {
	switch typeURL {
	case model.SecretType, model.EndpointType, model.RouteType, model.ExtensionConfigurationType:
		// By XDS spec, these are not wildcard
		return false
	case model.ClusterType, model.ListenerType:
		// By XDS spec, these are wildcard
		return true
	default:
		// All of our internal types use wildcard semantics
		return true
	}
}

// DiscoveryStream is a server interface for XDS.
type DiscoveryStream = discovery.AggregatedDiscoveryService_StreamAggregatedResourcesServer

// Connection holds information about an xDS client connection. There may be more than one connection to the same client.
type Connection struct {
	// peerAddr is the address of the client, from network layer.
	peerAddr string

	// Time of connection, for debugging
	connectedAt time.Time

	// conID is the connection conID, used as a key in the connection table.
	// Currently based on the node name and a counter.
	conID string

	// Sending on this channel results in a push.
	pushChannel chan any

	// Both ADS and SDS streams implement this interface
	stream DiscoveryStream

	// initialized channel will be closed when proxy is initialized. Pushes, or anything accessing
	// the proxy, should not be started until this channel is closed.
	initialized chan struct{}

	// stop can be used to end the connection manually via debug endpoints. Only to be used for testing.
	stop chan struct{}

	// reqChan is used to receive discovery requests for this connection.
	reqChan chan *discovery.DiscoveryRequest

	// errorChan is used to process error during discovery request processing.
	errorChan chan error
}

func NewConnection(peerAddr string, stream DiscoveryStream) Connection {
	return Connection{
		pushChannel: make(chan any),
		initialized: make(chan struct{}),
		stop:        make(chan struct{}),
		reqChan:     make(chan *discovery.DiscoveryRequest, 1),
		errorChan:   make(chan error, 1),
		peerAddr:    peerAddr,
		connectedAt: time.Now(),
		stream:      stream,
	}
}

func (conn *Connection) InitializedCh() chan struct{} {
	return conn.initialized
}

func (conn *Connection) PushCh() chan any {
	return conn.pushChannel
}

func (conn *Connection) StopCh() chan struct{} {
	return conn.stop
}

func (conn *Connection) ErrorCh() chan error {
	return conn.errorChan
}

func (conn *Connection) StreamDone() <-chan struct{} {
	return conn.stream.Context().Done()
}

func (conn *Connection) ID() string {
	return conn.conID
}

func (conn *Connection) Peer() string {
	return conn.peerAddr
}

func (conn *Connection) SetID(id string) {
	conn.conID = id
}

func (conn *Connection) ConnectedAt() time.Time {
	return conn.connectedAt
}

func (conn *Connection) Stop() {
	close(conn.stop)
}

func (conn *Connection) MarkInitialized() {
	close(conn.initialized)
}

// ConnectionContext is used by the RPC event loop to respond to requests and pushes.
type ConnectionContext interface {
	XdsConnection() *Connection
	Watcher() Watcher
	// Initialize checks the first request.
	Initialize(node *core.Node) error
	// Close discards the connection.
	Close()
	// Process responds to a discovery request.
	Process(req *discovery.DiscoveryRequest) error
	// Push responds to a push event queue
	Push(ev any) error
}

func Stream(ctx ConnectionContext) error {
	con := ctx.XdsConnection()
	// Do not call: defer close(con.pushChannel). The push channel will be garbage collected
	// when the connection is no longer used. Closing the channel can cause subtle race conditions
	// with push. According to the spec: "It's only necessary to close a channel when it is important
	// to tell the receiving goroutines that all data have been sent."

	// Block until either a request is received or a push is triggered.
	// We need 2 go routines because 'read' blocks in Recv().
	go Receive(ctx)

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
				if err := ctx.Process(req); err != nil {
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
				if err := ctx.Process(req); err != nil {
					return err
				}
			} else {
				// Remote side closed connection or error processing the request.
				return <-con.errorChan
			}
		case pushEv := <-con.pushChannel:
			err := ctx.Push(pushEv)
			if err != nil {
				return err
			}
		case <-con.stop:
			return nil
		}
	}
}

func Receive(ctx ConnectionContext) {
	con := ctx.XdsConnection()
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
			if istiogrpc.GRPCErrorType(err) != istiogrpc.UnexpectedError {
				log.Infof("ADS: %q %s terminated", con.peerAddr, con.conID)
				return
			}
			con.errorChan <- err
			log.Errorf("ADS: %q %s terminated with error: %v", con.peerAddr, con.conID, err)
			TotalXDSInternalErrors.Increment()
			return
		}
		// This should be only set for the first request. The node id may not be set - for example malicious clients.
		if firstRequest {
			// probe happens before envoy sends first xDS request
			if req.TypeUrl == model.HealthInfoType {
				log.Warnf("ADS: %q %s send health check probe before normal xDS request", con.peerAddr, con.conID)
				continue
			}
			firstRequest = false
			if req.Node == nil || req.Node.Id == "" {
				con.errorChan <- status.New(codes.InvalidArgument, "missing node information").Err()
				return
			}
			if err := ctx.Initialize(req.Node); err != nil {
				con.errorChan <- err
				return
			}
			defer ctx.Close()
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

// ShouldRespond determines whether this request needs to be responded back. It applies the ack/nack rules as per xds protocol
// using WatchedResource for previous state and discovery request for the current state.
func ShouldRespond(w Watcher, id string, request *discovery.DiscoveryRequest) (bool, ResourceDelta) {
	stype := model.GetShortType(request.TypeUrl)

	// If there is an error in request that means previous response is erroneous.
	// We do not have to respond in that case. In this case request's version info
	// will be different from the version sent. But it is fragile to rely on that.
	if request.ErrorDetail != nil {
		errCode := codes.Code(request.ErrorDetail.Code)
		log.Warnf("ADS:%s: ACK ERROR %s %s:%s", stype, id, errCode.String(), request.ErrorDetail.GetMessage())
		IncrementXDSRejects(request.TypeUrl, w.GetID(), errCode.String())
		w.UpdateWatchedResource(request.TypeUrl, func(wr *WatchedResource) *WatchedResource {
			wr.LastError = request.ErrorDetail.GetMessage()
			return wr
		})
		return false, emptyResourceDelta
	}

	if shouldUnsubscribe(request) {
		log.Debugf("ADS:%s: UNSUBSCRIBE %s %s %s", stype, id, request.VersionInfo, request.ResponseNonce)
		w.DeleteWatchedResource(request.TypeUrl)
		return false, emptyResourceDelta
	}

	previousInfo := w.GetWatchedResource(request.TypeUrl)
	// This can happen in two cases:
	// 1. When Envoy starts for the first time, it sends an initial Discovery request to Istiod.
	// 2. When Envoy reconnects to a new Istiod that does not have information about this typeUrl
	// i.e. non empty response nonce.
	// We should always respond with the current resource names.
	if request.ResponseNonce == "" || previousInfo == nil {
		log.Debugf("ADS:%s: INIT/RECONNECT %s %s %s", stype, id, request.VersionInfo, request.ResponseNonce)
		w.NewWatchedResource(request.TypeUrl, request.ResourceNames)
		return true, emptyResourceDelta
	}

	// If there is mismatch in the nonce, that is a case of expired/stale nonce.
	// A nonce becomes stale following a newer nonce being sent to Envoy.
	// previousInfo.NonceSent can be empty if we previously had shouldRespond=true but didn't send any resources.
	if request.ResponseNonce != previousInfo.NonceSent {
		if features.EnableUnsafeAssertions && previousInfo.NonceSent == "" {
			// Assert we do not end up in an invalid state
			log.Fatalf("ADS:%s: REQ %s Expired nonce received %s, but we never sent any nonce", stype,
				id, request.ResponseNonce)
		}
		log.Debugf("ADS:%s: REQ %s Expired nonce received %s, sent %s", stype,
			id, request.ResponseNonce, previousInfo.NonceSent)
		ExpiredNonce.With(typeTag.Value(model.GetMetricType(request.TypeUrl))).Increment()
		return false, emptyResourceDelta
	}

	// If it comes here, that means nonce match.
	var previousResources sets.String
	var cur sets.String
	var alwaysRespond bool
	w.UpdateWatchedResource(request.TypeUrl, func(wr *WatchedResource) *WatchedResource {
		// Clear last error, we got an ACK.
		wr.LastError = ""
		previousResources = wr.ResourceNames
		wr.NonceAcked = request.ResponseNonce
		wr.ResourceNames = sets.New(request.ResourceNames...)
		cur = wr.ResourceNames
		alwaysRespond = wr.AlwaysRespond
		wr.AlwaysRespond = false
		return wr
	})

	// Envoy can send two DiscoveryRequests with same version and nonce.
	// when it detects a new resource. We should respond if they change.
	removed := previousResources.Difference(cur)
	added := cur.Difference(previousResources)

	// We should always respond "alwaysRespond" marked requests to let Envoy finish warming
	// even though Nonce match and it looks like an ACK.
	if alwaysRespond {
		log.Infof("ADS:%s: FORCE RESPONSE %s for warming.", stype, id)
		return true, emptyResourceDelta
	}

	if len(removed) == 0 && len(added) == 0 {
		log.Debugf("ADS:%s: ACK %s %s %s", stype, id, request.VersionInfo, request.ResponseNonce)
		return false, emptyResourceDelta
	}
	log.Debugf("ADS:%s: RESOURCE CHANGE added %v removed %v %s %s %s", stype,
		added, removed, id, request.VersionInfo, request.ResponseNonce)

	// For non wildcard resource, if no new resources are subscribed, it means we do not need to push.
	if !IsWildcardTypeURL(request.TypeUrl) && len(added) == 0 {
		return false, emptyResourceDelta
	}

	return true, ResourceDelta{
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
	return len(request.ResourceNames) == 0 && !IsWildcardTypeURL(request.TypeUrl)
}

func Send(ctx ConnectionContext, res *discovery.DiscoveryResponse) error {
	conn := ctx.XdsConnection()
	sendResponse := func() error {
		start := time.Now()
		defer func() { RecordSendTime(time.Since(start)) }()
		return conn.stream.Send(res)
	}
	err := sendResponse()
	if err == nil {
		if res.Nonce != "" && !strings.HasPrefix(res.TypeUrl, model.DebugType) {
			ctx.Watcher().UpdateWatchedResource(res.TypeUrl, func(wr *WatchedResource) *WatchedResource {
				if wr == nil {
					wr = &WatchedResource{TypeUrl: res.TypeUrl}
				}
				wr.NonceSent = res.Nonce
				wr.LastSendTime = time.Now()
				return wr
			})
		}
	} else if status.Convert(err).Code() == codes.DeadlineExceeded {
		log.Infof("Timeout writing %s: %v", conn.conID, model.GetShortType(res.TypeUrl))
		ResponseWriteTimeouts.Increment()
	}
	return err
}
