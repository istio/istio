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

package server

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"istio.io/istio/pkg/mcp/debug"
	"istio.io/istio/pkg/mcp/env"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/mcp/monitoring"
	"istio.io/istio/pkg/mcp/source"
)

var scope = log.RegisterScope("mcp", "mcp debugging", 0)

var (
	// For the purposes of rate limiting new connections, this controls how many
	// new connections are allowed as a burst every NEW_CONNECTION_FREQ.
	newConnectionBurstSize = env.Integer("NEW_CONNECTION_BURST_SIZE", 10)

	// For the purposes of rate limiting new connections, this controls how
	// frequently new bursts of connections are allowed.
	newConnectionFreq = env.Duration("NEW_CONNECTION_FREQ", 10*time.Millisecond)


	// Controls the rate limit frequency for re-pushing previously NACK'd pushes
	// for each type.
	nackLimitFreq = env.Duration("NACK_LIMIT_FREQ", 1*time.Second)

	// Controls the delay for re-retrying a configuration push if the previous
	// attempt was not possible, e.g. the lower-level serving layer was busy. This
	// should typically be set fairly small (order of milliseconds).
	retryPushDelay = env.Duration("RETRY_PUSH_DELAY", 10*time.Millisecond)
)

var _ mcp.AggregatedMeshConfigServiceServer = &Server{}

// Server implements the Mesh Configuration Protocol (MCP) gRPC server.
type Server struct {
	watcher      source.Watcher
	collections  []source.CollectionOptions
	nextStreamID int64
	// for auth check
	authCheck            AuthChecker
	connections          int64
	reporter             monitoring.Reporter
	newConnectionLimiter *rate.Limiter
}

// AuthChecker is used to check the transport auth info that is associated with each stream. If the function
// returns nil, then the connection will be allowed. If the function returns an error, then it will be
// percolated up to the gRPC stack.
//
// Note that it is possible that this method can be called with nil authInfo. This can happen either if there
// is no peer info, or if the underlying gRPC stream is insecure. The implementations should be resilient in
// this case and apply appropriate policy.
type AuthChecker interface {
	Check(authInfo credentials.AuthInfo) error
}

//type connState int
//const (
//	connActive connState = iota
//	connClosing
//	connError
//	connClosed
//)

// connection maintains per-stream connection admitter for a
// node. Access to the stream and watch admitter is serialized
// through request and response channels.
type connection struct {
	peerAddr string
	stream   mcp.AggregatedMeshConfigService_StreamAggregatedResourcesServer
	id       int64

	admitter *admitter
	//// The context for this connection. It's parent is the stream's context.
	//context context.Context
	//cancel  context.CancelFunc

	sendLimiter    *rate.Limiter
	receiveLimiter *rate.Limiter

	//// Tracks the current active requests that are in-flight. Used for bandwidth limiting of receive
	//// requests.
	//activeRequests *semaphore.Weighted

	// channel through which responses are accumulated. The channel can be closed, which is an indication
	// that send pump should exit.
	responseC chan *mcp.MeshConfigResponse

	//closed bool
	//err    error

	//watches  map[string]*watch
	reporter monitoring.Reporter
	//watcher  source.Watcher

	tracker *watchTracker

	mu sync.Mutex
}

// New creates a new gRPC server that implements the Mesh Configuration Protocol (MCP).
func New(options *source.Options, authChecker AuthChecker) *Server {
	s := &Server{
		watcher:              options.Watcher,
		collections:          options.CollectionsOptions,
		authCheck:            authChecker,
		reporter:             options.Reporter,
		newConnectionLimiter: rate.NewLimiter(rate.Every(newConnectionFreq), newConnectionBurstSize),
	}
	return s
}

func (s *Server) newConnection(stream mcp.AggregatedMeshConfigService_StreamAggregatedResourcesServer) (*connection, error) {
	peerAddr := "0.0.0.0"

	peerInfo, ok := peer.FromContext(stream.Context())
	if ok {
		peerAddr = peerInfo.Addr.String()
	} else {
		scope.Warnf("No peer info found on the incoming stream.")
		peerInfo = nil
	}

	var authInfo credentials.AuthInfo
	if peerInfo != nil {
		authInfo = peerInfo.AuthInfo
	}

	if err := s.authCheck.Check(authInfo); err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "Authentication failure: %v", err)
	}

	var types []string
	for _, collection := range s.collections {
		types = append(types, collection.Name)
	}

	//ctx, cancel := context.WithCancel(stream.Context())
	con := &connection{
		admitter: newAdmitter(stream.Context()),
		id:       atomic.AddInt64(&s.nextStreamID, 1),
		stream:   stream,
		reporter: s.reporter,
		//watcher:  s.watcher,
		peerAddr: peerAddr,
		//context:  ctx,
		//cancel:   cancel,
		//watches:  make(map[string]*watch),
		//activeRequests: semaphore.NewWeighted(int64(len(s.collections) + 2)), // TODO: Magic number
		responseC:      make(chan *mcp.MeshConfigResponse, len(s.collections)+2), // TODO: Magic number
		sendLimiter:    rate.NewLimiter(rate.Every(time.Millisecond), 10),        // TODO: Magic values
		receiveLimiter: rate.NewLimiter(rate.Every(time.Millisecond*2), 10),      // TODO: Magic values
	}

	con.tracker = newWatchTracker(s.watcher, types, con.onResponseReady)

	//for _, c := range s.collections {
	//	con.watches[c.Name] = &watch{
	//		nonceVersionMap: make(map[string]string),
	//	}
	//}

	s.reporter.SetStreamCount(atomic.AddInt64(&s.connections, 1))

	scope.Debugf("MCP: connection %v: NEW, supported types: %#v", con, types)
	return con, nil
}

// IncrementalAggregatedResources implements bidirectional streaming method for incremental MCP.
func (s *Server) IncrementalAggregatedResources(stream mcp.AggregatedMeshConfigService_IncrementalAggregatedResourcesServer) error { // nolint: lll
	return status.Errorf(codes.Unimplemented, "not implemented")
}

// StreamAggregatedResources implements bidirectional streaming method for MCP.
func (s *Server) StreamAggregatedResources(stream mcp.AggregatedMeshConfigService_StreamAggregatedResourcesServer) error { // nolint: lll
	con, err := s.newConnection(stream)
	if err != nil {
		return err
	}

	defer s.closeConnection(con)

	// start a send loop to pump responses
	debug.Go("con.sendPump", con.sendPump)

	// start a receive loop to receive and process messages
	debug.Go("con.receivePump", con.receivePump)

	err = con.admitter.wait()

	// cause sendPump to exit. Receive pump either already exited, or will exit once
	// the whole stream is closed.
	close(con.responseC)

	return err
}

func (s *Server) closeConnection(con *connection) {
	s.reporter.SetStreamCount(atomic.AddInt64(&s.connections, -1))
}

// receivePump is a loop for receiving messages and processing them.
func (con *connection) receivePump() {

	for {
		if err := con.receiveLimiter.Wait(con.stream.Context()); err != nil {
			scope.Debugf("MCP: connection %v: error in receive limiter: %v", con, err)
			return
		}

		request, err := con.stream.Recv()
		if err != nil {
			code := status.Code(err)
			if code == codes.Canceled || err == io.EOF { // TODO: is handling canceled, the right thing?
				scope.Infof("MCP: connection %v: TERMINATED %q", con, err)
				con.admitter.block()
			} else {
				con.reporter.RecordRecvError(err, code)
				scope.Errorf("MCP: connection %v: TERMINATED with errors: %v", con, err)
				con.abort(err)
			}
			return
		}

		if err = con.process(request); err != nil {
			con.abort(err)
			return
		}
	}
}

// process an incoming request. If this is not a valid request, an error is returned. If the request is valid
// but has otherwise has other problems (i.e. invalid nonce), then it is ignored and nil is returned.
func (con *connection) process(request *mcp.MeshConfigRequest) error {
	con.mu.Lock()
	defer con.mu.Unlock()

	collection := request.TypeUrl

	con.reporter.RecordRequestSize(collection, con.id, request.Size())

	if !con.tracker.hasCollection(collection) {
		// No collection, short-circuit and let the receive pump close the connection.
		return status.Errorf(codes.InvalidArgument, "unsupported collection %q", collection)
	}

	if request.ErrorDetail == nil {
		con.reporter.RecordRequestAck(collection, con.id)
		scope.Debugf("MCP: connection %v ACK collection=%q version=%q with externalNonce=%q",
			con, collection, request.VersionInfo, request.ResponseNonce)

	} else {
		con.reporter.RecordRequestNack(collection, con.id, codes.Code(request.ErrorDetail.Code))
		scope.Warnf("MCP: connection %v: NACK collection=%v version=%v with externalNonce=%q (w.externalNonce=%q) error=%#v", // nolint: lll
			con, collection, request.VersionInfo, request.ResponseNonce, con.tracker.nonceOf(collection), request.ErrorDetail)
	}

	if !con.admitter.enter() {
		return nil
	}

	// Regardless of whether it was an ack or nack, start a new watch with the latest nonce.
	if !con.tracker.watch(collection, request.ResponseNonce, request.SinkNode) {
		// Watch hasn't been started. We might be in the process of shutting down. Simply ignore the
		// request
		// TODO: Log

		con.admitter.leave()
	}

	return nil
}

// sendPump is a loop over a response channel for sending responses out. Any error to send out things will
// cause the connection to be marked as being in error.
func (con *connection) sendPump() {

	for response := range con.responseC {
		if err := con.sendLimiter.Wait(con.stream.Context()); err != nil {
			scope.Errorf("Error waiting in send limiter: %v", err)
			// Wait will return error when con.context is closed. Do not use that as an error signal
			return
		}

		if err := con.stream.Send(response); err != nil {
			con.reporter.RecordSendError(err, status.Code(err))
			scope.Errorf("Error sending response: %v")
			con.abort(err)
			return
		}

		scope.Debugf("MCP: connection %v: SEND version=%v externalNonce=%v",
			con, response.VersionInfo, response.Nonce)
	}
}

func (con *connection) onResponseReady(nonce nonce, r *source.WatchResponse) {
	response := con.prepareResponse(nonce, r)
	select {
	case con.responseC <- response:
	default:
		// Unable to push response to channel. Shutdown
		con.abort(fmt.Errorf("send channel full"))
	}
}

func (con *connection) prepareResponse(nonce nonce, resp *source.WatchResponse) *mcp.MeshConfigResponse {
	resources := make([]mcp.Resource, 0, len(resp.Resources))
	for _, resource := range resp.Resources {
		resources = append(resources, *resource)
	}
	msg := &mcp.MeshConfigResponse{
		VersionInfo: resp.Version,
		Resources:   resources,
		TypeUrl:     resp.Collection,
	}

	// increment externalNonce
	msg.Nonce = nonce.String()
	return msg
}

func (con *connection) abort(err error) {
	con.mu.Lock()
	defer con.mu.Unlock()

	con.admitter.abort(err)
	con.tracker.abort()
}

//
//func (con *connection) shutdown(err error) {
//	con.mu.Lock()
//	defer con.mu.Unlock()
//
//	abort := false
//
//	con.closed = true
//	if con.err == nil && err != nil {
//		abort = true
//		con.err = err
//	}
//
//	if abort {
//		con.tracker.abort()
//		con.cancel()
//		close(con.responseC)
//	} else {
//		con.tracker.shutdown()
//	}
//}

//func (con *connection) wait() {
//	<-con.context.Done()
//}

//func (con *connection) wait() {
//	<- con.context.Done()
//}
//	if !con.closed {
//		con.err = err
//		abort = err != nil
//	} else {
//	}
//
//	if !con.closed && err != nil && con.err{
//		con.closed = true
//		con.err = err
//	} else {
//		if con.err == nil && err != nil {
//			con.err = err
//		}
//	}
//}

//func (con *connection) markClosed() {
//	con.mu.Lock()
//	defer con.mu.Unlock()
//	if con.closed {
//		con.closed = true
//		con.err = nil
//	}
//}
//
//func (con *connection) markAsError(err error) {
//	con.mu.Lock()
//	defer con.mu.Unlock()
//	con.closed = true
//	con.err = err
//	con.cancel()
//}

// String implements Stringer.String.
func (con *connection) String() string {
	return fmt.Sprintf("{addr=%v id=%v}", con.peerAddr, con.id)
}

//func (con *connection) send(resp *source.WatchResponse) (string, error) {
//	resources := make([]mcp.Resource, 0, len(resp.Resources))
//	for _, resource := range resp.Resources {
//		resources = append(resources, *resource)
//	}
//	msg := &mcp.MeshConfigResponse{
//		VersionInfo: resp.Version,
//		Resources:   resources,
//		TypeUrl:     resp.Collection,
//	}
//
//	// increment externalNonce
//	con.streamNonce = con.streamNonce + 1
//	msg.Nonce = strconv.FormatInt(con.streamNonce, 10)
//	if err := con.stream.Send(msg); err != nil {
//		con.reporter.RecordSendError(err, status.Code(err))
//
//		return "", err
//	}
//	scope.Debugf("MCP: connection %v: SEND version=%v externalNonce=%v", con, resp.Version, msg.Nonce)
//	return msg.Nonce, nil
//}
//
//func (con *connection) receivePump() {
//	for {
//		if err := con.receiveLimiter.Wait(con.context); err != nil {
//			scope.Debugf("MCP: connection %v: error in receive limiter: %v", con, err)
//			return
//		}
//
//		if err := con.activeRequests.Acquire(con.context, 1); err != nil {
//			scope.Debugf("MCP: connection %v: error in activeRequests.Acquire: %v", con, err)
//			return
//		}
//
//		request, err := con.stream.Recv()
//		if err != nil {
//			code := status.Code(err)
//			if code == codes.Canceled || err == io.EOF { // TODO: is handling canceled, the right thing?
//				scope.Infof("MCP: connection %v: TERMINATED %q", con, err)
//				con.active = true // TODO: make thread safe
//				// Do not call cancel. Send pump should finish and terminate.
//				return
//			}
//			con.reporter.RecordRecvError(err, code)
//			scope.Errorf("MCP: connection %v: TERMINATED with errors: %v", con, err)
//			// Save the stream error prior to closing the stream.
//			con.err = err
//			con.active = true // TODO: make thread safe
//			con.cancel()
//			return
//		}
//
//		if err := con.process(request); err != nil {
//			con.err = err
//			con.cancel()
//			con.active = true // TODO: make thread safe
//			return
//		}
//	}
//
//	//defer close(con.requestC)
//	//for {
//	//	req, err := con.stream.Recv()
//	//	if err != nil {
//	//		code := status.Code(err)
//	//		if code == codes.Canceled || err == io.EOF {
//	//			scope.Infof("MCP: connection %v: TERMINATED %q", con, err)
//	//			return
//	//		}
//	//		con.reporter.RecordRecvError(err, code)
//	//		scope.Errorf("MCP: connection %v: TERMINATED with errors: %v", con, err)
//	//		// Save the stream error prior to closing the stream. The caller
//	//		// should access the error after the channel closure.
//	//		con.err = err
//	//		return
//	//	}
//	//	select {
//	//	case con.requestC <- req:
//	//	case <-con.stream.Context().Done():
//	//		scope.Debugf("MCP: connection %v: stream done, err=%v", con, con.stream.Context().Err())
//	//		return
//	//	}
//	//}
//}

//func (con *connection) process(request *mcp.MeshConfigRequest) error {
//	collection := request.TypeUrl
//
//	con.reporter.RecordRequestSize(collection, con.id, request.Size())
//
//	if request.ErrorDetail == nil {
//		con.reporter.RecordRequestAck(collection, con.id)
//		scope.Debugf("MCP: connection %v ACK collection=%q version=%q with externalNonce=%q",
//			con, collection, request.VersionInfo, request.ResponseNonce)
//
//		if err := con.tracker.ack(collection, request.SinkNode, request.ResponseNonce, request.VersionInfo); err != nil {
//			return err
//		}
//	} else {
//		con.reporter.RecordRequestNack(collection, con.id, codes.Code(request.ErrorDetail.Code))
//		scope.Warnf("MCP: connection %v: NACK collection=%v version=%v with externalNonce=%q (w.externalNonce=%q) error=%#v", // nolint: lll
//			con, collection, request.VersionInfo, request.ResponseNonce, w.externalNonce, request.ErrorDetail)
//
//		if err := con.tracker.nack(collection, request.SinkNode, request.ResponseNonce, request.VersionInfo); err != nil {
//			return err
//		}
//	}
//
//
//	//
//	//// Reset on every request. Only the most recent NACK request (per
//	//// externalNonce) is tracked.
//	//w.lastNackVersion = ""
//	//
//	//// externalNonces can be reused across streams; we verify externalNonce only if externalNonce is not initialized
//	//if w.externalNonce == "" || w.externalNonce == request.ResponseNonce {
//	//	if w.externalNonce == "" {
//	//		scope.Infof("MCP: connection %v: WATCH for %v", con, collection)
//	//	} else {
//	//		if request.ErrorDetail != nil {
//	//			scope.Warnf("MCP: connection %v: NACK collection=%v version=%v with externalNonce=%q (w.externalNonce=%q) error=%#v", // nolint: lll
//	//				con, collection, request.VersionInfo, request.ResponseNonce, w.externalNonce, request.ErrorDetail)
//	//
//	//			con.reporter.RecordRequestNack(collection, con.id, codes.Code(request.ErrorDetail.Code))
//	//
//	//			if version, ok := w.nonceVersionMap[request.ResponseNonce]; ok {
//	//				w.mu.Lock()
//	//				w.lastNackVersion = version
//	//				w.mu.Unlock()
//	//			}
//	//		} else {
//	//			scope.Debugf("MCP: connection %v ACK collection=%q version=%q with externalNonce=%q",
//	//				con, collection, request.VersionInfo, request.ResponseNonce)
//	//			con.reporter.RecordRequestAck(collection, con.id)
//	//		}
//	//	}
//	//
//	//	if w.cancel != nil {
//	//		w.cancel()
//	//	}
//	//
//	//	sr := &source.Request{
//	//		SinkNode:    request.SinkNode,
//	//		Collection:  collection,
//	//		VersionInfo: request.VersionInfo,
//	//	}
//	//	w.cancel = con.tracker.watcher.Watch(sr, func(r *source.WatchResponse) {
//	//		response := con.prepareResponse(r)
//	//		w.cancel()
//	//		w.cancel = nil
//	//		select {
//	//		case con.responseC <- response:
//	//		default:
//	//			con.err = fmt.Errorf("channel full") // TODO: Error
//	//		}
//	//		con.pushServerResponse(w, )
//	//	})
//
//	//} else {
//	//	// This error path should not happen! Skip any requests that don't match the
//	//	// latest watch's externalNonce value. These could be dup requests or out-of-order
//	//	// requests from a buggy node.
//	//	if request.ErrorDetail != nil {
//	//		scope.Errorf("MCP: connection %v: STALE NACK collection=%v version=%v with externalNonce=%q (w.externalNonce=%q) error=%#v", // nolint: lll
//	//			con, collection, request.VersionInfo, request.ResponseNonce, w.externalNonce, request.ErrorDetail)
//	//		con.reporter.RecordRequestNack(collection, con.id, codes.Code(request.ErrorDetail.Code))
//	//	} else {
//	//		scope.Errorf("MCP: connection %v: STALE ACK collection=%v version=%v with externalNonce=%q (w.externalNonce=%q)", // nolint: lll
//	//			con, collection, request.VersionInfo, request.ResponseNonce, w.externalNonce)
//	//		con.reporter.RecordRequestAck(collection, con.id)
//	//	}
//	//
//	//	return fmt.Errorf("stale (N)ACK: collection=%v version=%v with externalNonce=%q (w.externalNonce=%q)",
//	//		collection, request.VersionInfo, request.ResponseNonce, w.externalNonce)
//	//}
//	//
//	//delete(w.nonceVersionMap, request.ResponseNonce)
//	return nil
//}
//
//func (con *connection) close() {
//	scope.Infof("MCP: connection %v: CLOSED", con)
//
//	con.cancel()
//
//	for _, w := range con.watches {
//		if w.cancel != nil {
//			w.cancel()
//		}
//	}
//}
//
//func (con *connection) processClientRequest(req *mcp.MeshConfigRequest) error {
//	collection := req.TypeUrl
//
//	con.reporter.RecordRequestSize(collection, con.id, req.Size())
//
//	w, ok := con.watches[collection]
//	if !ok {
//		return status.Errorf(codes.InvalidArgument, "unsupported collection %q", collection)
//	}
//
//	// Reset on every request. Only the most recent NACK request (per
//	// externalNonce) is tracked.
//	w.lastNackVersion = ""
//
//	// externalNonces can be reused across streams; we verify externalNonce only if externalNonce is not initialized
//	if w.externalNonce == "" || w.externalNonce == req.ResponseNonce {
//		if w.externalNonce == "" {
//			scope.Infof("MCP: connection %v: WATCH for %v", con, collection)
//		} else {
//			if req.ErrorDetail != nil {
//				scope.Warnf("MCP: connection %v: NACK collection=%v version=%v with externalNonce=%q (w.externalNonce=%q) error=%#v", // nolint: lll
//					con, collection, req.VersionInfo, req.ResponseNonce, w.externalNonce, req.ErrorDetail)
//
//				con.reporter.RecordRequestNack(collection, con.id, codes.Code(req.ErrorDetail.Code))
//
//				if version, ok := w.nonceVersionMap[req.ResponseNonce]; ok {
//					w.mu.Lock()
//					w.lastNackVersion = version
//					w.mu.Unlock()
//				}
//			} else {
//				scope.Debugf("MCP: connection %v ACK collection=%q version=%q with externalNonce=%q",
//					con, collection, req.VersionInfo, req.ResponseNonce)
//				con.reporter.RecordRequestAck(collection, con.id)
//			}
//		}
//
//		if w.cancel != nil {
//			w.cancel()
//		}
//
//		sr := &source.Request{
//			SinkNode:    req.SinkNode,
//			Collection:  collection,
//			VersionInfo: req.VersionInfo,
//		}
//		w.cancel = con.watcher.Watch(sr, w.saveResponseAndSchedulePush)
//	} else {
//		// This error path should not happen! Skip any requests that don't match the
//		// latest watch's externalNonce value. These could be dup requests or out-of-order
//		// requests from a buggy node.
//		if req.ErrorDetail != nil {
//			scope.Errorf("MCP: connection %v: STALE NACK collection=%v version=%v with externalNonce=%q (w.externalNonce=%q) error=%#v", // nolint: lll
//				con, collection, req.VersionInfo, req.ResponseNonce, w.externalNonce, req.ErrorDetail)
//			con.reporter.RecordRequestNack(collection, con.id, codes.Code(req.ErrorDetail.Code))
//		} else {
//			scope.Errorf("MCP: connection %v: STALE ACK collection=%v version=%v with externalNonce=%q (w.externalNonce=%q)", // nolint: lll
//				con, collection, req.VersionInfo, req.ResponseNonce, w.externalNonce)
//			con.reporter.RecordRequestAck(collection, con.id)
//		}
//	}
//
//	delete(w.nonceVersionMap, req.ResponseNonce)
//
//	return nil
//}
//
//func (con *connection) pushServerResponse(w *watch, resp *source.WatchResponse) error {
//	nonce, err := con.send(resp)
//	if err != nil {
//		return err
//	}
//	w.externalNonce = nonce
//	w.nonceVersionMap[nonce] = resp.Version
//	return nil
//}
