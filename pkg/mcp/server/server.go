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
	"os"
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
)

var (
	scope = log.RegisterScope("mcp", "mcp debugging", 0)
)

func envInt(name string, def int) int {
	if v := os.Getenv("NEW_CONNECTION_BURST_SIZE"); v != "" {
		if a, err := strconv.Atoi(v); err == nil {
			return a
		}
	}
	return def
}

func envDur(name string, def time.Duration) time.Duration {
	if v := os.Getenv("NEW_CONNECTION_FREQ"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

var (
	newConnectionBurstSize  = envInt("NEW_CONNECTION_BURST_SIZE", 10)
	newConnectionFreq       = envDur("NEW_CONNECTION_FREQ", 10*time.Millisecond)
	authFailureLogFreq      = envDur("AUTH_FAILURE_LOG_FREQ", time.Minute)
	authFailureLogBurstSize = envInt("AUTH_FAILURE_LOG_BURST_SIZE", 1)
	nackLimitFreq           = envDur("NACK_LIMIT_FREQ", 1*time.Second)
	retryPushDelay          = envDur("RETRY_PUSH_DELAY", 10*time.Millisecond)
)

// WatchResponse contains a versioned collection of pre-serialized resources.
type WatchResponse struct {
	TypeURL string

	// Version of the resources in the response for the given
	// type. The client responses with this version in subsequent
	// requests as an acknowledgment.
	Version string

	// Enveloped resources to be included in the response.
	Envelopes []*mcp.Envelope
}

type (
	// CancelWatchFunc allows the consumer to cancel a previous watch,
	// terminating the watch for the request.
	CancelWatchFunc func()

	// PushResponseFunc allows the consumer to push a response for the
	// corresponding watch.
	PushResponseFunc func(*WatchResponse)
)

// Watcher requests watches for configuration resources by node, last
// applied version, and type. The watch should send the responses when
// they are ready. The watch can be canceled by the consumer.
type Watcher interface {
	// Watch returns a new open watch for a non-empty request.
	//
	// Cancel is an optional function to release resources in the
	// producer. It can be called idempotently to cancel and release resources.
	Watch(*mcp.MeshConfigRequest, PushResponseFunc) CancelWatchFunc
}

var _ mcp.AggregatedMeshConfigServiceServer = &Server{}

// Server implements the Mesh Configuration Protocol (MCP) gRPC server.
type Server struct {
	watcher        Watcher
	supportedTypes []string
	nextStreamID   int64
	// for auth check
	authCheck                   AuthChecker
	checkFailureRecordLimiter   *rate.Limiter
	failureCountSinceLastRecord int
	connections                 int64
	reporter                    Reporter
	conLimiter                  *rate.Limiter
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

type mailboxState int

const (
	mailboxStateReady mailboxState = iota
	mailboxStateClosed
)

// watch maintains local state of the most recent watch per-type.
type watch struct {
	// only accessed from connection goroutine
	cancel          func()
	nonce           string // most recent nonce
	nonceVersionMap map[string]string

	// NOTE: do not hold `mu` when reading/writing to this channel
	mailboxReadyChan chan mailboxState

	mu                      sync.Mutex
	mailbox                 *WatchResponse
	mostRecentNackedVersion string
	timerRunning            bool
	timer                   *time.Timer
}

func (w *watch) schedulePush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.mailbox == nil {
		return
	}

	retry := false
	if w.mostRecentNackedVersion != "" && w.mailbox.Version == w.mostRecentNackedVersion {
		if !w.timerRunning {
			w.timerRunning = true
			w.timer = time.AfterFunc(nackLimitFreq, func() {
				w.mu.Lock()
				w.timerRunning = false
				w.mu.Unlock()

				select {
				case w.mailboxReadyChan <- mailboxStateReady:
				default:
					retry = true
				}
			})
		}
	} else {
		if w.timerRunning {
			w.timerRunning = false
			if w.timer.Stop() {
				<-w.timer.C
			}
		}
		select {
		case w.mailboxReadyChan <- mailboxStateReady:
		default:
			retry = true
		}
	}

	if retry {
		time.AfterFunc(retryPushDelay, w.schedulePush)
	}
}

func (w *watch) pushResponse(response *WatchResponse) {
	w.mu.Lock()
	w.mailbox = response
	w.mu.Unlock()

	if response == nil {
		w.mailboxReadyChan <- mailboxStateClosed
		return
	}

	w.schedulePush()
}

// connection maintains per-stream connection state for a
// client. Access to the stream and watch state is serialized
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
	watcher  Watcher

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
func New(watcher Watcher, supportedTypes []string, authChecker AuthChecker, reporter Reporter) *Server {
	s := &Server{
		watcher:        watcher,
		supportedTypes: supportedTypes,
		authCheck:      authChecker,
		reporter:       reporter,
		conLimiter:     rate.NewLimiter(rate.Every(newConnectionFreq), newConnectionBurstSize),
	}

	// Initialize record limiter for the auth checker.
	limit := rate.Every(authFailureLogFreq)
	limiter := rate.NewLimiter(limit, authFailureLogBurstSize)
	s.checkFailureRecordLimiter = limiter

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
		s.failureCountSinceLastRecord++
		if s.checkFailureRecordLimiter.Allow() {
			scope.Warnf("NewConnection: auth check failed: %v (repeated %d times).", err, s.failureCountSinceLastRecord)
			s.failureCountSinceLastRecord = 0
		}
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
	for _, typeURL := range s.supportedTypes {
		t := time.NewTimer(0)
		w := &watch{
			timer:            t,
			mailboxReadyChan: make(chan mailboxState, 1),
			nonceVersionMap:  make(map[string]string),
		}
		con.watches[typeURL] = w
		types = append(types, typeURL)
	}

	s.reporter.SetClientsTotal(atomic.AddInt64(&s.connections, 1))

	scope.Infof("MCP: connection %v: NEW, supported types: %#v", con, types)
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
			for state := range w.mailboxReadyChan {
				if state == mailboxStateClosed {
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
			resp := w.mailbox
			w.mailbox = nil
			w.mu.Unlock()

			// mailbox may have been cleared before we got to it
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
	s.reporter.SetClientsTotal(atomic.AddInt64(&s.connections, -1))
}

// String implements Stringer.String.
func (con *connection) String() string {
	return fmt.Sprintf("{addr=%v id=%v}", con.peerAddr, con.id)
}

func (con *connection) send(resp *WatchResponse) (string, error) {
	envelopes := make([]mcp.Envelope, 0, len(resp.Envelopes))
	for _, envelope := range resp.Envelopes {
		envelopes = append(envelopes, *envelope)
	}
	msg := &mcp.MeshConfigResponse{
		VersionInfo: resp.Version,
		Envelopes:   envelopes,
		TypeUrl:     resp.TypeURL,
	}

	// increment nonce
	con.streamNonce = con.streamNonce + 1
	msg.Nonce = strconv.FormatInt(con.streamNonce, 10)
	if err := con.stream.Send(msg); err != nil {
		con.reporter.RecordSendError(err, status.Code(err))

		return "", err
	}
	scope.Infof("MCP: connection %v: SEND version=%v nonce=%v", con, resp.Version, msg.Nonce)
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
		con.requestC <- req
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
	con.reporter.RecordRequestSize(req.TypeUrl, con.id, req.Size())

	w, ok := con.watches[req.TypeUrl]
	if !ok {
		return status.Errorf(codes.InvalidArgument, "unsupported type_url %q", req.TypeUrl)
	}

	// Reset on every request. Only the most recent NACK request (per
	// nonce) is tracked.
	w.mostRecentNackedVersion = ""

	// nonces can be reused across streams; we verify nonce only if nonce is not initialized
	if w.nonce == "" || w.nonce == req.ResponseNonce {
		if w.nonce == "" {
			scope.Infof("MCP: connection %v: WATCH for %v", con, req.TypeUrl)
		} else {
			if req.ErrorDetail != nil {
				scope.Warnf("MCP: connection %v: NACK type_url=%v version=%v with nonce=%q (w.nonce=%q) error=%#v", // nolint: lll
					con, req.TypeUrl, req.VersionInfo, req.ResponseNonce, w.nonce, req.ErrorDetail)
				con.reporter.RecordRequestNack(req.TypeUrl, con.id)

				if version, ok := w.nonceVersionMap[req.ResponseNonce]; ok {
					w.mu.Lock()
					w.mostRecentNackedVersion = version
					w.mu.Unlock()
				}
			} else {
				scope.Infof("MCP: connection %v ACK type_url=%q version=%q with nonce=%q",
					con, req.TypeUrl, req.VersionInfo, req.ResponseNonce)
				con.reporter.RecordRequestAck(req.TypeUrl, con.id)
			}
		}

		if w.cancel != nil {
			w.cancel()
		}
		w.cancel = con.watcher.Watch(req, w.pushResponse)
	} else {
		// This error path should not happen! Skip any requests that don't match the
		// latest watch's nonce value. These could be dup requests or out-of-order
		// requests from a buggy client.
		if req.ErrorDetail != nil {
			scope.Errorf("MCP: connection %v: STALE NACK type_url=%v version=%v with nonce=%q (w.nonce=%q) error=%#v", // nolint: lll
				con, req.TypeUrl, req.VersionInfo, req.ResponseNonce, w.nonce, req.ErrorDetail)
			con.reporter.RecordRequestNack(req.TypeUrl, con.id)
		} else {
			scope.Errorf("MCP: connection %v: STALE ACK type_url=%v version=%v with nonce=%q (w.nonce=%q)", // nolint: lll
				con, req.TypeUrl, req.VersionInfo, req.ResponseNonce, w.nonce)
			con.reporter.RecordRequestAck(req.TypeUrl, con.id)
		}
	}

	delete(w.nonceVersionMap, req.ResponseNonce)

	return nil
}

func (con *connection) pushServerResponse(w *watch, resp *WatchResponse) error {
	nonce, err := con.send(resp)
	if err != nil {
		return err
	}
	w.nonce = nonce
	w.nonceVersionMap[nonce] = resp.Version
	return nil
}
