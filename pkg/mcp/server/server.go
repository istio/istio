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
	"context"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/semaphore"
	"golang.org/x/time/rate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/mcp/env"
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

// connection maintains per-stream connection state for a
// node. Access to the stream and watch state is serialized
// through request and response channels.
type connection struct {
	peerAddr string
	stream   mcp.AggregatedMeshConfigService_StreamAggregatedResourcesServer
	id       int64

	context context.Context
	cancel  context.CancelFunc

	sendLimiter    *rate.Limiter
	receiveLimiter *rate.Limiter

	// Tracks the current active requests that are in-flight. Used for bandwidth limiting of receive
	// requests.
	activeRequests *semaphore.Weighted

	closed bool
	responseC      chan *mcp.MeshConfigResponse

	// unique nonce generator for req-resp pairs per xDS stream; the server
	// ignores stale nonces. nonce is only modified within send() function.
	streamNonce int64

	err error

	tracker *watchTracker

	reporter monitoring.Reporter

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

	ctx, cancel := context.WithCancel(stream.Context())
	con := &connection{
		stream:   stream,
		peerAddr: peerAddr,
		//requestC:    make(chan *mcp.MeshConfigRequest),
		context:        ctx,
		cancel:         cancel,
		activeRequests: semaphore.NewWeighted(int64(len(s.collections)+2)), // TODO: Magic number
		responseC:      make(chan *mcp.MeshConfigResponse),
		sendLimiter:    rate.NewLimiter(rate.Every(time.Millisecond), 10), // TODO: Magic values
		receiveLimiter: rate.NewLimiter(rate.Every(time.Millisecond * 2), 10), // TODO: Magic values

		tracker: newWatchTracker(s.watcher, types),

		id:             atomic.AddInt64(&s.nextStreamID, 1),
		reporter:       s.reporter,
	}


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
	goWithDebug("con.sendPump", con.sendPump)
	goWithDebug("con.receivePump", con.receivePump)

	<-con.context.Done()
	return con.err
}

func (s *Server) closeConnection(con *connection) {
	con.close()
	s.reporter.SetStreamCount(atomic.AddInt64(&s.connections, -1))
}

func (con *connection) receivePump() {
	for {
		if err := con.receiveLimiter.Wait(con.context); err != nil {
			scope.Debugf("MCP: connection %v: error in receive limiter: %v", con, err)
			return
		}

		if err := con.activeRequests.Acquire(con.context, 1); err != nil {
			scope.Debugf("MCP: connection %v: error in activeRequests.Acquire: %v", con, err)
			return
		}

		request, err := con.stream.Recv()
		if err != nil {
			code := status.Code(err)
			if code == codes.Canceled || err == io.EOF { // TODO: is handling canceled, the right thing?
				scope.Infof("MCP: connection %v: TERMINATED %q", con, err)
				con.markClosed()
				return
			}
			con.reporter.RecordRecvError(err, code)
			scope.Errorf("MCP: connection %v: TERMINATED with errors: %v", con, err)
			con.markAsError(err)
			return
		}

		processed, err := con.process(request)
		if err != nil {
			con.markAsError(err)
			return
		}

		if !processed {
			con.activeRequests.Release(1)
		}
	}
}

func (con *connection) process(request *mcp.MeshConfigRequest) (bool, error) {
	collection := request.TypeUrl

	con.reporter.RecordRequestSize(collection, con.id, request.Size())

	w, found := con.tracker.getWatch(collection)
	if !found {
		return false, status.Errorf(codes.InvalidArgument, "unsupported collection %q", collection)
	}

	if !w.registerRequest(request.ResponseNonce, request.ErrorDetail == nil) {
		// We couldn't mark
		return false, nil
	}

	if request.ErrorDetail == nil {
		con.reporter.RecordRequestAck(collection, con.id)
		scope.Debugf("MCP: connection %v ACK collection=%q version=%q with nonce=%q",
			con, collection, request.VersionInfo, request.ResponseNonce)

	} else {
		con.reporter.RecordRequestNack(collection, con.id, codes.Code(request.ErrorDetail.Code))
		scope.Warnf("MCP: connection %v: NACK collection=%v version=%v with nonce=%q (w.nonce=%q) error=%#v", // nolint: lll
			con, collection, request.VersionInfo, request.ResponseNonce, w.nonce, request.ErrorDetail)
	}

	w.beginWatch(con.tracker.watcher, request.VersionInfo)

	return nil
}

func (con *connection) markClosed() {
	con.mu.Lock()
	defer con.mu.Unlock()
	con.closed = true
}

func (con *connection) markAsError(err error) {
	con.mu.Lock()
	defer con.mu.Unlock()
	con.closed = true
	con.err = err
	con.cancel()
}

// String implements Stringer.String.
func (con *connection) String() string {
	return fmt.Sprintf("{addr=%v id=%v}", con.peerAddr, con.id)
}

func (con *connection) sendPump() {
loop:
	for {
		if err := con.sendLimiter.Wait(con.context); err != nil {
			scope.Errorf("Error waiting in send limiter: %v", err)
			// If the context is cancelled, then the following select should detect and exit the loop.
		}

		select {
		case <-con.context.Done():
			scope.Debugf("MCP: connection %v: Exiting the send pump (err: %v)", con, con.context.Err())
			break loop

		case response := <-con.responseC:
			if err := con.stream.Send(response); err != nil {
				con.reporter.RecordSendError(err, status.Code(err))
				scope.Errorf("Error sending response: %v")
				con.err = err // TODO: not thread safe
				con.cancel()
				break loop
			}
			scope.Debugf("MCP: connection %v: SEND version=%v nonce=%v",
				con, response.VersionInfo, response.Nonce)
			con.activeRequests.Release(1)
		}
	}
}

func (con *connection) prepareResponse(resp *source.WatchResponse) *mcp.MeshConfigResponse {
	resources := make([]mcp.Resource, 0, len(resp.Resources))
	for _, resource := range resp.Resources {
		resources = append(resources, *resource)
	}
	msg := &mcp.MeshConfigResponse{
		VersionInfo: resp.Version,
		Resources:   resources,
		TypeUrl:     resp.Collection,
	}

	// increment nonce
	con.streamNonce = con.streamNonce + 1
	msg.Nonce = strconv.FormatInt(con.streamNonce, 10)
	return msg
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
//	// increment nonce
//	con.streamNonce = con.streamNonce + 1
//	msg.Nonce = strconv.FormatInt(con.streamNonce, 10)
//	if err := con.stream.Send(msg); err != nil {
//		con.reporter.RecordSendError(err, status.Code(err))
//
//		return "", err
//	}
//	scope.Debugf("MCP: connection %v: SEND version=%v nonce=%v", con, resp.Version, msg.Nonce)
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
//				con.closed = true // TODO: make thread safe
//				// Do not call cancel. Send pump should finish and terminate.
//				return
//			}
//			con.reporter.RecordRecvError(err, code)
//			scope.Errorf("MCP: connection %v: TERMINATED with errors: %v", con, err)
//			// Save the stream error prior to closing the stream.
//			con.err = err
//			con.closed = true // TODO: make thread safe
//			con.cancel()
//			return
//		}
//
//		if err := con.process(request); err != nil {
//			con.err = err
//			con.cancel()
//			con.closed = true // TODO: make thread safe
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
//		scope.Debugf("MCP: connection %v ACK collection=%q version=%q with nonce=%q",
//			con, collection, request.VersionInfo, request.ResponseNonce)
//
//		if err := con.tracker.ack(collection, request.SinkNode, request.ResponseNonce, request.VersionInfo); err != nil {
//			return err
//		}
//	} else {
//		con.reporter.RecordRequestNack(collection, con.id, codes.Code(request.ErrorDetail.Code))
//		scope.Warnf("MCP: connection %v: NACK collection=%v version=%v with nonce=%q (w.nonce=%q) error=%#v", // nolint: lll
//			con, collection, request.VersionInfo, request.ResponseNonce, w.nonce, request.ErrorDetail)
//
//		if err := con.tracker.nack(collection, request.SinkNode, request.ResponseNonce, request.VersionInfo); err != nil {
//			return err
//		}
//	}
//
//
//	//
//	//// Reset on every request. Only the most recent NACK request (per
//	//// nonce) is tracked.
//	//w.mostRecentNackedVersion = ""
//	//
//	//// nonces can be reused across streams; we verify nonce only if nonce is not initialized
//	//if w.nonce == "" || w.nonce == request.ResponseNonce {
//	//	if w.nonce == "" {
//	//		scope.Infof("MCP: connection %v: WATCH for %v", con, collection)
//	//	} else {
//	//		if request.ErrorDetail != nil {
//	//			scope.Warnf("MCP: connection %v: NACK collection=%v version=%v with nonce=%q (w.nonce=%q) error=%#v", // nolint: lll
//	//				con, collection, request.VersionInfo, request.ResponseNonce, w.nonce, request.ErrorDetail)
//	//
//	//			con.reporter.RecordRequestNack(collection, con.id, codes.Code(request.ErrorDetail.Code))
//	//
//	//			if version, ok := w.nonceVersionMap[request.ResponseNonce]; ok {
//	//				w.mu.Lock()
//	//				w.mostRecentNackedVersion = version
//	//				w.mu.Unlock()
//	//			}
//	//		} else {
//	//			scope.Debugf("MCP: connection %v ACK collection=%q version=%q with nonce=%q",
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
//	//	// latest watch's nonce value. These could be dup requests or out-of-order
//	//	// requests from a buggy node.
//	//	if request.ErrorDetail != nil {
//	//		scope.Errorf("MCP: connection %v: STALE NACK collection=%v version=%v with nonce=%q (w.nonce=%q) error=%#v", // nolint: lll
//	//			con, collection, request.VersionInfo, request.ResponseNonce, w.nonce, request.ErrorDetail)
//	//		con.reporter.RecordRequestNack(collection, con.id, codes.Code(request.ErrorDetail.Code))
//	//	} else {
//	//		scope.Errorf("MCP: connection %v: STALE ACK collection=%v version=%v with nonce=%q (w.nonce=%q)", // nolint: lll
//	//			con, collection, request.VersionInfo, request.ResponseNonce, w.nonce)
//	//		con.reporter.RecordRequestAck(collection, con.id)
//	//	}
//	//
//	//	return fmt.Errorf("stale (N)ACK: collection=%v version=%v with nonce=%q (w.nonce=%q)",
//	//		collection, request.VersionInfo, request.ResponseNonce, w.nonce)
//	//}
//	//
//	//delete(w.nonceVersionMap, request.ResponseNonce)
//	return nil
//}

func (con *connection) close() {
	scope.Infof("MCP: connection %v: CLOSED", con)

	con.cancel()

	for _, w := range con.watches {
		if w.cancel != nil {
			w.cancel()
		}
	}
}

func (con *connection) processClientRequest(req *mcp.MeshConfigRequest) error {
	collection := req.TypeUrl

	con.reporter.RecordRequestSize(collection, con.id, req.Size())

	w, ok := con.watches[collection]
	if !ok {
		return status.Errorf(codes.InvalidArgument, "unsupported collection %q", collection)
	}

	// Reset on every request. Only the most recent NACK request (per
	// nonce) is tracked.
	w.mostRecentNackedVersion = ""

	// nonces can be reused across streams; we verify nonce only if nonce is not initialized
	if w.nonce == "" || w.nonce == req.ResponseNonce {
		if w.nonce == "" {
			scope.Infof("MCP: connection %v: WATCH for %v", con, collection)
		} else {
			if req.ErrorDetail != nil {
				scope.Warnf("MCP: connection %v: NACK collection=%v version=%v with nonce=%q (w.nonce=%q) error=%#v", // nolint: lll
					con, collection, req.VersionInfo, req.ResponseNonce, w.nonce, req.ErrorDetail)

				con.reporter.RecordRequestNack(collection, con.id, codes.Code(req.ErrorDetail.Code))

				if version, ok := w.nonceVersionMap[req.ResponseNonce]; ok {
					w.mu.Lock()
					w.mostRecentNackedVersion = version
					w.mu.Unlock()
				}
			} else {
				scope.Debugf("MCP: connection %v ACK collection=%q version=%q with nonce=%q",
					con, collection, req.VersionInfo, req.ResponseNonce)
				con.reporter.RecordRequestAck(collection, con.id)
			}
		}

		if w.cancel != nil {
			w.cancel()
		}

		sr := &source.Request{
			SinkNode:    req.SinkNode,
			Collection:  collection,
			VersionInfo: req.VersionInfo,
		}
		w.cancel = con.watcher.Watch(sr, w.saveResponseAndSchedulePush)
	} else {
		// This error path should not happen! Skip any requests that don't match the
		// latest watch's nonce value. These could be dup requests or out-of-order
		// requests from a buggy node.
		if req.ErrorDetail != nil {
			scope.Errorf("MCP: connection %v: STALE NACK collection=%v version=%v with nonce=%q (w.nonce=%q) error=%#v", // nolint: lll
				con, collection, req.VersionInfo, req.ResponseNonce, w.nonce, req.ErrorDetail)
			con.reporter.RecordRequestNack(collection, con.id, codes.Code(req.ErrorDetail.Code))
		} else {
			scope.Errorf("MCP: connection %v: STALE ACK collection=%v version=%v with nonce=%q (w.nonce=%q)", // nolint: lll
				con, collection, req.VersionInfo, req.ResponseNonce, w.nonce)
			con.reporter.RecordRequestAck(collection, con.id)
		}
	}

	delete(w.nonceVersionMap, req.ResponseNonce)

	return nil
}

func (con *connection) pushServerResponse(w *watch, resp *source.WatchResponse) error {
	nonce, err := con.send(resp)
	if err != nil {
		return err
	}
	w.nonce = nonce
	w.nonceVersionMap[nonce] = resp.Version
	return nil
}
