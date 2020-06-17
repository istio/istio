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

package source

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"sync/atomic"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/internal"
	"istio.io/istio/pkg/mcp/monitoring"
	"istio.io/istio/pkg/mcp/rate"
	"istio.io/istio/pkg/mcp/status"
	"istio.io/pkg/log"
)

var scope = log.RegisterScope("mcp", "mcp debugging", 0)

// Request is a temporary abstraction for the MCP node request which can
// be used with the mcp.MeshConfigRequest and mcp.RequestResources. It can
// be removed once we fully cutover to mcp.RequestResources.
type Request struct {
	Collection string

	// Most recent version was that ACK/NACK'd by the sink
	VersionInfo string
	SinkNode    *mcp.SinkNode

	// hidden
	incremental bool
}

// WatchResponse contains a versioned collection of pre-serialized resources.
type WatchResponse struct {
	Collection string

	// Version of the resources in the response for the given
	// type. The node responses with this version in subsequent
	// requests as an acknowledgment.
	Version string

	// Resourced resources to be included in the response.
	Resources []*mcp.Resource

	// The original request for triggered this response
	Request *Request
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
	Watch(*Request, PushResponseFunc, string) CancelWatchFunc
}

// CollectionOptions configures the per-collection updates.
type CollectionOptions struct {
	// Name of the collection, e.g. istio/networking/v1alpha3/VirtualService
	Name string

	// When true, the source is allowed to push incremental updates to the sink.
	// Incremental updates are only used if the sink requests it (per request)
	// and the source decides to make use of it.
	Incremental bool
}

// CollectionOptionsFromSlice returns a slice of collection options from
// a slice of collection names.
func CollectionOptionsFromSlice(names []string) []CollectionOptions {
	options := make([]CollectionOptions, 0, len(names))
	for _, name := range names {
		options = append(options, CollectionOptions{
			Name: name,
		})
	}
	return options
}

// Options contains options for configuring MCP sources.
type Options struct {
	Watcher            Watcher
	CollectionsOptions []CollectionOptions
	Reporter           monitoring.Reporter
	ConnRateLimiter    rate.LimitFactory
}

// Stream is for sending Resource messages and receiving RequestResources messages.
type Stream interface {
	Send(*mcp.Resources) error
	Recv() (*mcp.RequestResources, error)
	Context() context.Context
}

// Source implements the resource source message exchange for MCP.
// It can be instantiated by client and server
type Source struct {
	// nextStreamID and connections must be at start of struct to ensure 64bit alignment for atomics on
	// 32bit architectures. See also: https://golang.org/pkg/sync/atomic/#pkg-note-BUG
	nextStreamID   int64
	connections    int64
	watcher        Watcher
	collections    []CollectionOptions
	reporter       monitoring.Reporter
	requestLimiter rate.LimitFactory
}

// watch maintains local push state of the most recent watch per-type.
type watch struct {
	// only accessed from connection goroutine
	cancel          func()
	ackedVersionMap map[string]string // resources that exist at the sink; by name and version
	pending         *mcp.Resources
	incremental     bool
}

// connection maintains per-stream connection state for a
// node. Access to the stream and watch state is serialized
// through request and response channels.
type connection struct {
	peerAddr string
	stream   Stream
	id       int64

	// unique nonce generator for req-resp pairs per xDS stream; the server
	// ignores stale nonces. nonce is only modified within send() function.
	streamNonce int64

	requestC chan *mcp.RequestResources // a channel for receiving incoming requests
	reqError error                      // holds error if request channel is closed
	watches  map[string]*watch          // per-type watch state
	watcher  Watcher

	reporter monitoring.Reporter
	limiter  rate.Limit

	queue *internal.UniqueQueue
}

// New creates a new resource source.
func New(options *Options) *Source {
	s := &Source{
		watcher:        options.Watcher,
		collections:    options.CollectionsOptions,
		reporter:       options.Reporter,
		requestLimiter: options.ConnRateLimiter,
	}
	return s
}

func (s *Source) newConnection(stream Stream) *connection {
	peerAddr := "0.0.0.0"

	peerInfo, ok := peer.FromContext(stream.Context())
	if ok {
		peerAddr = peerInfo.Addr.String()
	} else {
		scope.Warnf("No peer info found on the incoming stream.")
		peerInfo = nil
	}

	con := &connection{
		stream:   stream,
		peerAddr: peerAddr,
		requestC: make(chan *mcp.RequestResources),
		watches:  make(map[string]*watch),
		watcher:  s.watcher,
		id:       atomic.AddInt64(&s.nextStreamID, 1),
		reporter: s.reporter,
		limiter:  s.requestLimiter.Create(),
		queue:    internal.NewUniqueScheduledQueue(len(s.collections)),
	}

	collections := make([]string, 0, len(s.collections))
	for i := range s.collections {
		collection := s.collections[i]
		w := &watch{
			ackedVersionMap: make(map[string]string),
			incremental:     collection.Incremental,
		}
		con.watches[collection.Name] = w
		collections = append(collections, collection.Name)
	}

	s.reporter.SetStreamCount(atomic.AddInt64(&s.connections, 1))

	scope.Infof("MCP: connection %v: NEW (ResourceSource), supported collections: %#v", con, collections)

	return con
}

func (s *Source) ProcessStream(stream Stream) error {
	con := s.newConnection(stream)

	defer s.closeConnection(con)
	go con.receive()

	for {
		select {
		case <-con.queue.Ready():
			collection, item, ok := con.queue.Dequeue()
			if !ok {
				break
			}

			resp := item.(*WatchResponse)

			w, ok := con.watches[collection]
			if !ok {
				scope.Errorf("unknown collection in dequeued watch response: %v", collection)
				break // bug?
			}

			// the response may have been cleared before we got to it
			if resp != nil {
				if err := con.pushServerResponse(w, resp); err != nil {
					return err
				}
			}
		case req, more := <-con.requestC:
			if !more {
				return con.reqError
			}
			if con.limiter != nil {
				if err := con.limiter.Wait(stream.Context()); err != nil {
					return err
				}

			}
			if err := con.processClientRequest(req); err != nil {
				return err
			}
		case <-con.queue.Done():
			scope.Debugf("MCP: connection %v: stream done", con)
			return status.Error(codes.Unavailable, "server canceled watch")
		}
	}
}

func (s *Source) closeConnection(con *connection) {
	con.close()
	s.reporter.SetStreamCount(atomic.AddInt64(&s.connections, -1))
}

// String implements Stringer.String.
func (con *connection) String() string {
	return fmt.Sprintf("{addr=%v id=%v}", con.peerAddr, con.id)
}

// Queue the response for sending in the dispatch loop. The caller may provide
// a nil response to indicate that the watch should be closed.
func (con *connection) queueResponse(resp *WatchResponse) {
	if resp == nil {
		con.queue.Close()
	} else {
		con.queue.Enqueue(resp.Collection, resp)
	}
}

func calculateDelta(current []*mcp.Resource, acked map[string]string) (added []mcp.Resource, removed []string) {
	// TODO - consider storing desired state as a map to make this faster
	desired := make(map[string]*mcp.Resource, len(current))

	// compute diff
	for _, envelope := range current {
		prevVersion, exists := acked[envelope.Metadata.Name]

		if !exists {
			// new
			added = append(added, *envelope)
		} else if prevVersion != envelope.Metadata.Version {
			// update
			added = append(added, *envelope)
		}
		// tracking for delete
		desired[envelope.Metadata.Name] = envelope
	}

	for name := range acked {
		if _, exists := desired[name]; !exists {
			removed = append(removed, name)
		}
	}

	return added, removed
}

func (con *connection) pushServerResponse(w *watch, resp *WatchResponse) error {
	var (
		added   []mcp.Resource
		removed []string
	)

	// send an incremental update if enabled for this collection and the most
	// recent request from the sink requested it.
	var incremental bool
	if w.incremental && resp.Request.incremental {
		incremental = true
	}

	if incremental {
		added, removed = calculateDelta(resp.Resources, w.ackedVersionMap)
	} else {
		for _, resource := range resp.Resources {
			added = append(added, *resource)
		}
	}

	msg := &mcp.Resources{
		SystemVersionInfo: resp.Version,
		Collection:        resp.Collection,
		Resources:         added,
		RemovedResources:  removed,
		Incremental:       con.streamNonce > 0 && incremental, // the first response was not consider as incremental
	}

	// increment nonce
	con.streamNonce++
	msg.Nonce = strconv.FormatInt(con.streamNonce, 10)
	if err := con.stream.Send(msg); err != nil {
		con.reporter.RecordSendError(err, status.Code(err))
		return err
	}
	scope.Debugf("MCP: connection %v: SEND collection=%v version=%v nonce=%v inc=%v",
		con, resp.Collection, resp.Version, msg.Nonce, msg.Incremental)
	w.pending = msg
	return nil
}

func (con *connection) receive() {
	defer close(con.requestC)
	for {
		req, err := con.stream.Recv()
		if err != nil {
			if err == io.EOF {
				scope.Infof("MCP: connection %v: TERMINATED %q", con, err)
				return
			}
			con.reporter.RecordRecvError(err, status.Code(err))
			scope.Errorf("MCP: connection %v: TERMINATED with errors: %v", con, err)
			// Save the stream error prior to closing the stream. The caller
			// should access the error after the channel closure.
			con.reqError = err
			return
		}
		select {
		case con.requestC <- req:
		case <-con.queue.Done():
			scope.Debugf("MCP: connection %v: stream done", con)
			return
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

func (con *connection) processClientRequest(req *mcp.RequestResources) error {
	if isTriggerResponse(req) {
		return nil
	}

	collection := req.Collection

	con.reporter.RecordRequestSize(collection, con.id, internal.ProtoSize(req))

	w, ok := con.watches[collection]
	if !ok {
		return status.Errorf(codes.InvalidArgument, "unsupported collection %q", collection)
	}

	// nonces can be reused across streams; we verify nonce only if it initialized
	if req.ResponseNonce == "" || w.pending.GetNonce() == req.ResponseNonce {
		versionInfo := ""

		if w.pending == nil {
			scope.Infof("MCP: connection %v: inc=%v WATCH for %v", con, req.Incremental, collection)
		} else {
			versionInfo = w.pending.SystemVersionInfo
			if req.ErrorDetail != nil {
				scope.Warnf("MCP: connection %v: NACK collection=%v version=%q with nonce=%q error=%#v inc=%v", // nolint: lll
					con, collection, req.ResponseNonce, versionInfo, req.ErrorDetail, req.Incremental)
				con.reporter.RecordRequestNack(collection, con.id, codes.Code(req.ErrorDetail.Code))
			} else {
				scope.Infof("MCP: connection %v ACK collection=%v with version=%q nonce=%q inc=%v",
					con, collection, versionInfo, req.ResponseNonce, req.Incremental)
				con.reporter.RecordRequestAck(collection, con.id)

				internal.UpdateResourceVersionTracking(w.ackedVersionMap, w.pending)
			}

			// clear the pending request after we finished processing the corresponding response.
			w.pending = nil
		}

		if w.cancel != nil {
			w.cancel()
		}

		sr := &Request{
			SinkNode:    req.SinkNode,
			Collection:  collection,
			VersionInfo: versionInfo,
			incremental: req.Incremental,
		}
		w.cancel = con.watcher.Watch(sr, con.queueResponse, con.peerAddr)
	} else {
		// This error path should not happen! Skip any requests that don't match the
		// latest watch's nonce. These could be dup requests or out-of-order
		// requests from a buggy node.
		if req.ErrorDetail != nil {
			scope.Errorf("MCP: connection %v: STALE NACK collection=%v with nonce=%q (expected nonce=%q) error=%+v inc=%v", // nolint: lll
				con, collection, req.ResponseNonce, w.pending.GetNonce(), req.ErrorDetail, req.Incremental)
			con.reporter.RecordRequestNack(collection, con.id, codes.Code(req.ErrorDetail.Code))
		} else {
			scope.Errorf("MCP: connection %v: STALE ACK collection=%v with nonce=%q (expected nonce=%q) inc=%v", // nolint: lll
				con, collection, req.ResponseNonce, w.pending.GetNonce(), req.Incremental)
			con.reporter.RecordRequestAck(collection, con.id)
		}
	}

	return nil
}
