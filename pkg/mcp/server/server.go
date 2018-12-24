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
	"strconv"
	"sync"
	"sync/atomic"
	"time"

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

type newPushResponseState int

const (
	newPushResponseStateReady newPushResponseState = iota
	newPushResponseStateClosed
)

// watch maintains local push state of the most recent watch per-type.
type watch struct {
	// only accessed from connection goroutine
	cancel          func()
	nonce           string // most recent nonce
	nonceVersionMap map[string]string

	// NOTE: do not hold `mu` when reading/writing to this channel.
	newPushResponseReadyChan chan newPushResponseState

	mu                      sync.Mutex
	newPushResponse         *source.WatchResponse
	mostRecentNackedVersion string
	timer                   *time.Timer
	closed                  bool
}

func (w *watch) delayedPush() {
	w.mu.Lock()
	w.timer = nil
	w.mu.Unlock()

	select {
	case w.newPushResponseReadyChan <- newPushResponseStateReady:
	default:
		time.AfterFunc(retryPushDelay, w.schedulePush)
	}
}

// Try to schedule pushing a response to the node. The push may
// be re-scheduled as needed. Additional care is taken to rate limit
// re-pushing responses that were previously NACK'd. This avoid flooding
// the node with responses while also allowing transient NACK'd responses
// to be retried.
func (w *watch) schedulePush() {
	w.mu.Lock()

	// close the watch
	if w.closed {
		// unlock before channel write
		w.mu.Unlock()

		select {
		case w.newPushResponseReadyChan <- newPushResponseStateClosed:
		default:
			time.AfterFunc(retryPushDelay, w.schedulePush)
		}
		return
	}

	// no-op if the response has already be sent
	if w.newPushResponse == nil {
		w.mu.Unlock()
		return
	}

	// delay re-sending a previously nack'd response
	if w.newPushResponse.Version == w.mostRecentNackedVersion {
		if w.timer == nil {
			w.timer = time.AfterFunc(nackLimitFreq, w.delayedPush)
		}
		w.mu.Unlock()
		return
	}

	// Otherwise, try to schedule the response to be sent.
	if w.timer != nil {
		if !w.timer.Stop() {
			<-w.timer.C
		}
		w.timer = nil
	}
	// unlock before channel write
	w.mu.Unlock()
	select {
	case w.newPushResponseReadyChan <- newPushResponseStateReady:
	default:
		time.AfterFunc(retryPushDelay, w.schedulePush)
	}

}

// Save the pushed response in the newPushResponse and schedule a push. The push
// may be re-schedule as necessary but this should be transparent to the
// caller. The caller may provide a nil response to indicate that the watch
// should be closed.
func (w *watch) saveResponseAndSchedulePush(response *source.WatchResponse) {
	w.mu.Lock()
	w.newPushResponse = response
	if response == nil {
		w.closed = true
	}
	w.mu.Unlock()

	w.schedulePush()
}

// connection maintains per-stream connection state for a
// node. Access to the stream and watch state is serialized
// through request and response channels.
type connection struct {
	peerAddr string
	stream   mcp.AggregatedMeshConfigService_StreamAggregatedResourcesServer
	id       int64

	// unique nonce generator for req-resp pairs per xDS stream; the server
	// ignores stale nonces. nonce is only modified within send() function.
	streamNonce int64

	requestC chan *mcp.MeshConfigRequest // a channel for receiving incoming requests
	reqError error                       // holds error if request channel is closed
	watches  map[string]*watch           // per-type watches
	watcher  source.Watcher

	reporter Reporter
}

// Reporter is used to report metrics for an MCP server.
type Reporter interface {
	SetClientsTotal(clients int64)
	RecordSendError(err error, code codes.Code)
	RecordRecvError(err error, code codes.Code)
	RecordRequestSize(typeURL string, connectionID int64, size int)
	RecordRequestAck(typeURL string, connectionID int64)
	RecordRequestNack(typeURL string, connectionID int64)
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

	con := &connection{
		stream:   stream,
		peerAddr: peerAddr,
		requestC: make(chan *mcp.MeshConfigRequest),
		watches:  make(map[string]*watch),
		watcher:  s.watcher,
		id:       atomic.AddInt64(&s.nextStreamID, 1),
		reporter: s.reporter,
	}

	var types []string
	for _, collection := range s.collections {
		w := &watch{
			newPushResponseReadyChan: make(chan newPushResponseState, 1),
			nonceVersionMap:          make(map[string]string),
		}
		con.watches[collection.Name] = w
		types = append(types, collection.Name)
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
	go con.receive()

	// fan-in per-type response channels into single response channel for the select loop below.
	responseChan := make(chan *watch, 1)
	for _, w := range con.watches {
		go func(w *watch) {
			for state := range w.newPushResponseReadyChan {
				if state == newPushResponseStateClosed {
					break
				}
				responseChan <- w
			}

			// Any closed watch can close the overall connection. Use
			// `nil` value to indicate a closed state to the run loop
			// below instead of closing the channel to avoid closing
			// the channel multiple times.
			responseChan <- nil
		}(w)
	}

	for {
		select {
		case w, more := <-responseChan:
			if !more || w == nil {
				return status.Error(codes.Unavailable, "server canceled watch")
			}

			w.mu.Lock()
			resp := w.newPushResponse
			w.newPushResponse = nil
			w.mu.Unlock()

			// newPushResponse may have been cleared before we got to it
			if resp == nil {
				break
			}
			if err := con.pushServerResponse(w, resp); err != nil {
				return err
			}
		case req, more := <-con.requestC:
			if !more {
				return con.reqError
			}
			if err := con.processClientRequest(req); err != nil {
				return err
			}
		case <-stream.Context().Done():
			scope.Debugf("MCP: connection %v: stream done, err=%v", con, stream.Context().Err())
			return stream.Context().Err()
		}
	}
}

func (s *Server) closeConnection(con *connection) {
	con.close()
	s.reporter.SetStreamCount(atomic.AddInt64(&s.connections, -1))
}

// String implements Stringer.String.
func (con *connection) String() string {
	return fmt.Sprintf("{addr=%v id=%v}", con.peerAddr, con.id)
}

func (con *connection) send(resp *source.WatchResponse) (string, error) {
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
	if err := con.stream.Send(msg); err != nil {
		con.reporter.RecordSendError(err, status.Code(err))

		return "", err
	}
	scope.Debugf("MCP: connection %v: SEND version=%v nonce=%v", con, resp.Version, msg.Nonce)
	return msg.Nonce, nil
}

func (con *connection) receive() {
	defer close(con.requestC)
	for {
		req, err := con.stream.Recv()
		if err != nil {
			code := status.Code(err)
			if code == codes.Canceled || err == io.EOF {
				scope.Infof("MCP: connection %v: TERMINATED %q", con, err)
				return
			}
			con.reporter.RecordRecvError(err, code)
			scope.Errorf("MCP: connection %v: TERMINATED with errors: %v", con, err)
			// Save the stream error prior to closing the stream. The caller
			// should access the error after the channel closure.
			con.reqError = err
			return
		}
		select {
		case con.requestC <- req:
		case <-con.stream.Context().Done():
			scope.Debugf("MCP: connection %v: stream done, err=%v", con, con.stream.Context().Err())
			return
		}
	}
}

func (con *connection) close() {
	scope.Infof("MCP: connection %v: CLOSED", con)

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
