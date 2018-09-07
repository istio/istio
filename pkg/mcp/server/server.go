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
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"context"

	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/log"
)

var (
	scope = log.RegisterScope("mcp", "mcp debugging", 0)
)

// channel depth for mcp response
const responseChanDepth = 1

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

// CancelWatchFunc allows the consumer to cancel a previous watch,
// terminating the watch for the request.
type CancelWatchFunc func()

// Watcher requests watches for configuration resources by node, last
// applied version, and type. The watch should send the responses when
// they are ready. The watch can be canceled by the consumer.
type Watcher interface {
	// Watch returns a new open watch for a non-empty request.
	//
	// Immediate responses should be returned to the caller along
	// with an optional cancel function. Asynchronous responses should
	// be delivered through the write-only WatchResponse channel. If the
	// channel is closed prior to cancellation of the watch, an
	// unrecoverable error has occurred in the producer, and the consumer
	// should close the corresponding stream.
	//
	// Cancel is an optional function to release resources in the
	// producer. It can be called idempotently to cancel and release resources.
	Watch(*mcp.MeshConfigRequest, chan<- *WatchResponse) (*WatchResponse, CancelWatchFunc)
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
	connections    int32
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

// watch maintains local state of the most recent watch per-type.
type watch struct {
	cancel       func()
	nonce        string
	responseChan chan *WatchResponse
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
}

// New creates a new gRPC server that implements the Mesh Configuration Protocol (MCP). A nil authCheck
// implies all incoming connections should be allowed.
func New(watcher Watcher, supportedTypes []string, authChecker AuthChecker) *Server {
	s := &Server{
		watcher:        watcher,
		supportedTypes: supportedTypes,
		authCheck:      authChecker,
	}

	if authChecker != nil {
		// Initialize record limiter for the auth checker.
		limit := rate.Every(1 * time.Minute) // TODO: make this configurable?
		limiter := rate.NewLimiter(limit, 1)
		s.checkFailureRecordLimiter = limiter
	}

	return s
}

func (s *Server) newConnection(stream mcp.AggregatedMeshConfigService_StreamAggregatedResourcesServer) (*connection, error) {
	peerAddr := "0.0.0.0"

	peerInfo, ok := peer.FromContext(stream.Context())
	if ok {
		peerAddr = peerInfo.Addr.String()
	} else {
		log.Warnf("No peer info found on the incoming stream.")
		peerInfo = nil
	}

	var authInfo credentials.AuthInfo
	if peerInfo != nil {
		authInfo = peerInfo.AuthInfo
	}

	if err := s.authCheck.Check(authInfo); err != nil {
		s.failureCountSinceLastRecord++
		if s.checkFailureRecordLimiter.Allow() {
			log.Warnf("NewConnection: auth check failed: %v (repeated %d times).", err, s.failureCountSinceLastRecord)
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
	}

	var types []string
	for _, typeURL := range s.supportedTypes {
		con.watches[typeURL] = &watch{
			responseChan: make(chan *WatchResponse, responseChanDepth),
		}
		types = append(types, typeURL)
	}

	atomic.AddInt32(&s.connections, 1)
	stats.Record(context.Background(), ClientCount.M(int64(atomic.LoadInt32(&s.connections))))

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
	responseChan := make(chan *WatchResponse)
	for _, ch := range con.watches {
		go func(in chan *WatchResponse) {
			for v := range in {
				responseChan <- v
			}
		}(ch.responseChan)
	}

	for {
		select {
		case resp, more := <-responseChan:
			if !more || resp == nil {
				return status.Errorf(codes.Unavailable, "server canceled watch: more=%v resp=%v",
					more, resp)
			}
			if err := con.pushServerResponse(resp); err != nil {
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
	atomic.AddInt32(&s.connections, -1)
	stats.Record(context.Background(), ClientCount.M(int64(s.connections)))
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
		ctx, err := tag.New(context.Background(),
			tag.Insert(ErrorTag, err.Error()),
			tag.Insert(ErrorCodeTag, strconv.FormatUint(uint64(status.Code(err)), 10)))
		stats.Record(ctx, SendFailures.M(1))
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
			ctx, err := tag.New(context.Background(),
				tag.Insert(ErrorTag, err.Error()),
				tag.Insert(ErrorCodeTag, strconv.FormatUint(uint64(code), 10)))
			stats.Record(ctx, RecvFailures.M(1))
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

	for _, watch := range con.watches {
		if watch.cancel != nil {
			watch.cancel()
		}
	}
}

func (con *connection) processClientRequest(req *mcp.MeshConfigRequest) error {
	ctx, err := tag.New(context.Background(),
		tag.Insert(TypeURLTag, req.TypeUrl),
		tag.Insert(ConnectionIDTag, strconv.FormatInt(con.id, 10)))
	stats.Record(ctx, RequestSizesBytes.M(int64(req.Size())))
	if err != nil {
		scope.Errorf("MCP: error creating monitoring context. %v", err)
	}
	watch, ok := con.watches[req.TypeUrl]
	if !ok {
		return status.Errorf(codes.InvalidArgument, "unsupported type_url %q", req.TypeUrl)
	}

	// nonces can be reused across streams; we verify nonce only if nonce is not initialized
	if watch.nonce == "" || watch.nonce == req.ResponseNonce {
		if watch.nonce == "" {
			scope.Debugf("MCP: connection %v: WATCH for %v", con, req.TypeUrl)
		} else {
<<<<<<< HEAD
			scope.Debugf("MCP: connection %v ACK type_url=%q version=%q with nonce=%q",
				con, req.TypeUrl, req.VersionInfo, req.ResponseNonce)
=======
			scope.Debugf("MCP: connection %v ACK version=%q with nonce=%q",
				con, req.VersionInfo, req.ResponseNonce)
			stats.Record(ctx, RequestAcks.M(1))
>>>>>>> add opencensus metrics to mcp/server
		}

		if watch.cancel != nil {
			watch.cancel()
		}
		var resp *WatchResponse
		resp, watch.cancel = con.watcher.Watch(req, watch.responseChan)
		if resp != nil {
			nonce, err := con.send(resp)
			if err != nil {
				return err
			}
			watch.nonce = nonce
		}
	} else {
		scope.Warnf("MCP: connection %v: NACK type_url=%v version=%v with nonce=%q (watch.nonce=%q) error=%#v",
			con, req.TypeUrl, req.VersionInfo, req.ResponseNonce, watch.nonce, req.ErrorDetail)
		stats.Record(ctx, RequestNacks.M(1))
	}
	return nil
}

func (con *connection) pushServerResponse(resp *WatchResponse) error {
	nonce, err := con.send(resp)
	if err != nil {
		return err
	}

	watch, ok := con.watches[resp.TypeURL]
	if !ok {
		scope.Errorf("MCP: connection %v: internal error: received push response for unsupported type: %v",
			con, resp.TypeURL)
		return status.Errorf(codes.Internal,
			"failed to update internal stream nonce for %v",
			resp.TypeURL)
	}
	watch.nonce = nonce
	return nil
}
