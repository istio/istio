// Copyright 2019 Istio Authors
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
	"sync"
	"sync/atomic"
	"time"

	"github.com/gogo/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/mcp/monitoring"
)

const DefaultRetryPushDelay = 10 * time.Millisecond

var scope = log.RegisterScope("mcp", "mcp debugging", 0)

// Request is a temporary abstraction for the MCP client request which can
// be used with the mcp.MeshConfigRequest and mcp.RequestResources. It can
// be removed once we fully cutover to mcp.RequestResources.
type Request struct {
	Collection  string
	VersionInfo string
	SinkNode    *mcp.SinkNode
}

// WatchResponse contains a versioned collection of pre-serialized resources.
type WatchResponse struct {
	Collection string

	// Version of the resources in the response for the given
	// type. The client responses with this version in subsequent
	// requests as an acknowledgment.
	Version string

	// Resourced resources to be included in the response.
	Resources []*mcp.Resource
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
	Watch(*Request, PushResponseFunc) CancelWatchFunc
}

// Stream is for sending Resource messages and receiving RequestResources messages.
type Stream interface {
	Send(*mcp.Resources) error
	Recv() (*mcp.RequestResources, error)
	Context() context.Context
}

// Sources implements the resource source message exchange for MCP. It can be instantiated by client and server
// source implementations to manage the MCP message exchange.
type Source struct {
	watcher        Watcher
	collections    []string
	nextStreamID   int64
	reporter       monitoring.Reporter
	streamCount    int64
	retryPushDelay time.Duration
}

type Options struct {
	Watcher     Watcher
	Collections []string
	Reporter    monitoring.Reporter

	// Controls the delay for re-retrying a configuration push if the previous
	// attempt was not possible, e.errgrp. the lower-level serving layer was busy. This
	// should typically be set fairly small (order of milliseconds).
	RetryPushDelay time.Duration
}

// NewSource creates a new resource source.
func NewSource(options *Options) *Source {
	if options.RetryPushDelay == 0 {
		options.RetryPushDelay = DefaultRetryPushDelay
	}
	s := &Source{
		watcher:        options.Watcher,
		collections:    options.Collections,
		reporter:       options.Reporter,
		retryPushDelay: options.RetryPushDelay,
	}
	return s
}

func (src *Source) processStream(stream Stream) error {
	src.reporter.SetStreamCount(atomic.AddInt64(&src.streamCount, 1))
	defer src.reporter.SetStreamCount(atomic.AddInt64(&src.streamCount, -1))

	options := &remoteSinkOptions{
		stream:         stream,
		id:             atomic.AddInt64(&src.nextStreamID, 1),
		watcher:        src.watcher,
		reporter:       src.reporter,
		collections:    src.collections,
		retryPushDelay: src.retryPushDelay,
	}
	remote := newRemoteSink(options)

	scope.Infof("MCP: connection %v: NEW, supported collections: %#v", remote, src.collections)

	defer remote.close()
	go remote.receive()

	// fan-in per-type response channels into single response channel for the select loop below.
	responseChan := make(chan *watch, 1)
	for _, w := range remote.watches {
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

	// event loop
	for {
		select {
		case req, more := <-remote.requestC:
			if !more {
				return remote.reqError
			}

			if err := remote.processClientRequest(req); err != nil {
				return err
			}

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
			if err := remote.pushServerResponse(w, resp); err != nil {
				return err
			}
		}
	}
}

type newPushResponseState int

const (
	newPushResponseStateReady newPushResponseState = iota
	newPushResponseStateClosed
)

// watch maintains local push state of the most recent watch per-type.
type watch struct {
	// only accessed from connection goroutine
	cancel func()
	// nonce  string // most recent nonce

	// NOTE: do not hold `mu` when reading/writing to this channel.
	newPushResponseReadyChan chan newPushResponseState

	mu              sync.Mutex
	newPushResponse *WatchResponse
	timer           *time.Timer
	closed          bool
	acked           map[string]string // resources that exist at the sink; by name and version
	pending         *mcp.Resources
	retryPushDelay  time.Duration
}

func (w *watch) pushResponse(state newPushResponseState) {
	select {
	case w.newPushResponseReadyChan <- state:
	default:
		time.AfterFunc(w.retryPushDelay, w.schedulePush)
	}
}

func (w *watch) delayedPush() {
	w.mu.Lock()
	w.timer = nil
	w.mu.Unlock()

	w.pushResponse(newPushResponseStateReady)
}

// Try to schedule pushing a response to the client. The push may
// be re-scheduled as needed. Additional care is taken to rate limit
// re-pushing responses that were previously NACK'd. This avoid flooding
// the client with responses while also allowing transient NACK'd responses
// to be retried.
func (w *watch) schedulePush() {
	w.mu.Lock()

	// close the watch
	if w.closed {
		// unlock before channel write
		w.mu.Unlock()
		w.pushResponse(newPushResponseStateClosed)
		return
	}

	// no-op if the response has already been sent
	if w.newPushResponse == nil {
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

	w.pushResponse(newPushResponseStateReady)

}

// Save the pushed response in the newPushResponse and schedule a push. The push
// may be re-schedule as necessary but this should be transparent to the
// caller. The caller may provide a nil response to indicate that the watch
// should be closed.
func (w *watch) saveResponseAndSchedulePush(response *WatchResponse) {
	w.mu.Lock()
	w.newPushResponse = response
	if response == nil {
		w.closed = true
	}
	w.mu.Unlock()

	w.schedulePush()
}

type remoteSink struct {
	peerAddr string
	id       int64
	stream   Stream

	// unique nonce generator for req-resp pairs per xDS stream; the server
	// ignores stale nonces. nonce is only modified within send() function.
	streamNonce int64

	requestC chan *mcp.RequestResources // a channel for receiving incoming requests
	reqError error                      // holds error if request channel is closed
	watches  map[string]*watch          // per-type watches
	watcher  Watcher

	reporter monitoring.Reporter
}

type remoteSinkOptions struct {
	stream         Stream
	id             int64
	watcher        Watcher
	reporter       monitoring.Reporter
	collections    []string
	retryPushDelay time.Duration
}

func newRemoteSink(options *remoteSinkOptions) *remoteSink {
	peerAddr := "0.0.0.0"
	if peerInfo, ok := peer.FromContext(options.stream.Context()); ok {
		peerAddr = peerInfo.Addr.String()
	}

	rs := &remoteSink{
		stream:   options.stream,
		peerAddr: peerAddr,
		requestC: make(chan *mcp.RequestResources),
		watches:  make(map[string]*watch),
		watcher:  options.watcher,
		id:       options.id,
		reporter: options.reporter,
	}

	for _, collection := range options.collections {
		w := &watch{
			newPushResponseReadyChan: make(chan newPushResponseState, 1),
			acked:                    make(map[string]string),
			retryPushDelay:           options.retryPushDelay,
		}
		rs.watches[collection] = w
	}
	return rs
}

// String implements Stringer.String.
func (rs *remoteSink) String() string {
	return fmt.Sprintf("{addr=%v id=%v}", rs.peerAddr, rs.id)
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

func (rs *remoteSink) receive() {
	defer close(rs.requestC)
	for {
		req, err := rs.stream.Recv()
		if err != nil {
			code := status.Code(err)
			if code == codes.Canceled || err == io.EOF {
				scope.Infof("MCP: connection %v: TERMINATED %q", rs, err)
				return
			}
			rs.reporter.RecordRecvError(err, code)
			scope.Errorf("MCP: connection %v: TERMINATED with errors: %v", rs, err)
			// Save the stream error prior to closing the stream. The caller
			// should access the error after the channel closure.
			rs.reqError = err
			return
		}
		rs.requestC <- req
	}
}

func (rs *remoteSink) close() {
	scope.Infof("MCP: connection %v: CLOSED", rs)

	for _, w := range rs.watches {
		if w.cancel != nil {
			w.cancel()
		}
	}
}

func (rs *remoteSink) processClientRequest(req *mcp.RequestResources) error {
	rs.reporter.RecordRequestSize(req.Collection, rs.id, req.Size())

	w, ok := rs.watches[req.Collection]
	if !ok {
		return status.Errorf(codes.InvalidArgument, "unsupported collection %q", req.Collection)
	}

	nonce := ""
	if w.pending != nil {
		nonce = w.pending.Nonce
	}

	// nonces can be reused across streams; we verify nonce only if nonce is not initialized
	if nonce == "" || nonce == req.ResponseNonce {
		versionInfo := ""

		if w.pending == nil {
			scope.Infof("MCP: connection %v: WATCH for %v", rs, req.Collection)
		} else {
			versionInfo = w.pending.SystemVersionInfo

			if req.ErrorDetail != nil {
				scope.Warnf("MCP: connection %v: NACK collection=%v with version=%q nonce=%q (w.nonce=%q) error=%#v", // nolint: lll
					rs, req.Collection, req.ResponseNonce, versionInfo, nonce, req.ErrorDetail)
				rs.reporter.RecordRequestNack(req.Collection, rs.id, codes.Code(req.ErrorDetail.Code))
			} else {
				scope.Infof("MCP: connection %v ACK collection=%q with version=%q nonce=%q",
					rs, req.Collection, versionInfo, req.ResponseNonce)
				rs.reporter.RecordRequestAck(req.Collection, rs.id)

				for _, e := range w.pending.Resources {
					name, version := e.Metadata.Name, e.Metadata.Version
					if prev, ok := w.acked[e.Metadata.Name]; ok {
						scope.Debugf("MCP: ACK UPDATE name=%q version=%q (prev=%v_)", name, version, prev)
					} else {
						scope.Debugf("MCP: ACK ADD name=%q version=%q)", name, version)
					}
					w.acked[name] = version
				}
				for _, name := range w.pending.RemovedResources {
					scope.Debugf("MCP: ACK REMOVE name=%q)", name)
					delete(w.acked, name)
				}
			}
		}

		if w.cancel != nil {
			w.cancel()
		}

		sr := &Request{
			SinkNode:    req.SinkNode,
			Collection:  req.Collection,
			VersionInfo: versionInfo,
		}
		w.cancel = rs.watcher.Watch(sr, w.saveResponseAndSchedulePush)
	} else {
		// This error path should not happen! Skip any requests that don't match the
		// latest watch's nonce value. These could be dup requests or out-of-order
		// requests from a buggy client.
		if req.ErrorDetail != nil {
			scope.Errorf("MCP: connection %v: STALE NACK collection=%v with nonce=%q (expected nonce=%q) error=%#v", // nolint: lll
				rs, req.Collection, req.ResponseNonce, nonce, req.ErrorDetail)
			rs.reporter.RecordRequestNack(req.Collection, rs.id, codes.Code(req.ErrorDetail.Code))
		} else {
			scope.Errorf("MCP: connection %v: STALE ACK collection=%v with nonce=%q (expected nonce=%q)", // nolint: lll
				rs, req.Collection, req.ResponseNonce, nonce)
			rs.reporter.RecordRequestAck(req.Collection, rs.id)
		}
	}

	w.pending = nil

	return nil
}

func (rs *remoteSink) pushServerResponse(w *watch, resp *WatchResponse) error {
	added, removed := calculateDelta(resp.Resources, w.acked)

	msg := &mcp.Resources{
		SystemVersionInfo: resp.Version,
		Collection:        resp.Collection,
		Resources:         added,
		RemovedResources:  removed,
	}

	// increment nonce
	rs.streamNonce = rs.streamNonce + 1
	msg.Nonce = strconv.FormatInt(rs.streamNonce, 10)
	if err := rs.stream.Send(msg); err != nil {
		rs.reporter.RecordSendError(err, status.Code(err))
		return err
	}
	scope.Infof("MCP: connection %v: SEND version=%v nonce=%v", rs, resp.Version, msg.Nonce)
	w.pending = msg
	return nil
}
