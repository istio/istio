//  Copyright 2019 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package server

import (
	"context"
	"sync"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/source"
)

type watchTracker struct {
	watches   map[string]*watch
}

type watch struct {
	cancel          func()
	nonce           string // most recent nonce
	nonceVersionMap map[string]string

	mostRecentNackedVersion string
	//	closed                  bool
	mu    sync.Mutex
}

func newWatchTracker(w source.Watcher, collections []string) *watchTracker {
	t := &watchTracker{
		watcher:   w,
		watches:   make(map[string]*watch, len(collections)),
		responses: make(map[string]*source.WatchResponse),
	}

	for _, c := range collections {
		t.watches[c] = &watch{
			nonceVersionMap: make(map[string]string),
		}
	}

	return t
}

func (t *watchTracker) getWatch(collection string) (*watch, bool) {
	w, ok := t.watches[collection]
	return w, ok
	//if !ok {
	//	return nil, status.Errorf(codes.InvalidArgument, "unsupported collection %q", collection)
	//}
	//return w, nil
}

func (w *watch) registerRequest(responseNonce string, isNack bool) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	delete(w.nonceVersionMap, responseNonce)
	w.mostRecentNackedVersion = ""

	if w.nonce != "" && w.nonce != responseNonce {
	// This error path should not happen! Skip any requests that don't match the
	// latest watch's nonce value. These could be dup requests or out-of-order
	// requests from a buggy node.
	// TODO: log
		return false
	}

	if w.cancel != nil {
		w.cancel()
		w = nil
	}

	return true
}

func (w *watch) markNack(responseNonce string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	delete(w.nonceVersionMap, responseNonce)
	w.mostRecentNackedVersion = ""

	if version, ok := w.nonceVersionMap[responseNonce]; ok {
		w.mostRecentNackedVersion = version
	}

	if w.cancel != nil {
		w.cancel()
	}

	return true
}

func (t *watchTracker) ack(collection string, sinkNode *mcp.SinkNode, responseNonce, versionInfo string) error {
	w, err := t.getWatch(collection)
	if err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	delete(w.nonceVersionMap, responseNonce)
	w.mostRecentNackedVersion = ""

	if w.nonce != "" && w.nonce != responseNonce {
		// This error path should not happen! Skip any requests that don't match the
		// latest watch's nonce value. These could be dup requests or out-of-order
		// requests from a buggy node.
		return nil
	}

	if w.cancel != nil {
		w.cancel()
	}

	sr := &source.Request{
		SinkNode:    sinkNode,
		Collection:  collection,
		VersionInfo: versionInfo,
	}
	w.cancel = t.watcher.Watch(sr, t.handlePush)

	return nil
}

func (t *watchTracker) nack(collection string, sinkNode *mcp.SinkNode, responseNonce, versionInfo string) error {
	w, err := t.getWatch(collection)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	delete(w.nonceVersionMap, responseNonce)
	w.mostRecentNackedVersion = ""

	if version, ok := w.nonceVersionMap[responseNonce]; ok {
		w.mostRecentNackedVersion = version
	}

	if w.cancel != nil {
		w.cancel()
	}

	sr := &source.Request{
		SinkNode:    sinkNode,
		Collection:  collection,
		VersionInfo: versionInfo,
	}
	w.cancel = t.watcher.Watch(sr, t.handlePush)
	return nil
}

func (t *watchTracker) handlePush(r *source.WatchResponse) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if r == nil {
		delete(t.responses, r.Collection)
		return
	}

	t.responses[r.Collection] = r
	if t.ready != nil {
		close(t.ready)
	}
}

func (t *watchTracker) waitForNext(ctx context.Context) (*source.WatchResponse, error) {
	t.mu.Lock()
	ch := make(chan struct{})
	t.ready = ch
	t.mu.Unlock()

	select {
		case <- ch:

		case <-ctx.Done():
			return nil, ctx.Err()
	}
}

//
//// Reset on every request. Only the most recent NACK request (per
//// nonce) is tracked.
//w.mostRecentNackedVersion = ""
//
//// nonces can be reused across streams; we verify nonce only if nonce is not initialized
//if w.nonce == "" || w.nonce == request.ResponseNonce {
//if w.nonce == "" {
//scope.Infof("MCP: connection %v: WATCH for %v", con, collection)
//} else {
//if request.ErrorDetail != nil {
//scope.Warnf("MCP: connection %v: NACK collection=%v version=%v with nonce=%q (w.nonce=%q) error=%#v", // nolint: lll
//con, collection, request.VersionInfo, request.ResponseNonce, w.nonce, request.ErrorDetail)
//
//con.reporter.RecordRequestNack(collection, con.id, codes.Code(request.ErrorDetail.Code))
//
//if version, ok := w.nonceVersionMap[request.ResponseNonce]; ok {
//w.mu.Lock()
//w.mostRecentNackedVersion = version
//w.mu.Unlock()
//}
//} else {
//scope.Debugf("MCP: connection %v ACK collection=%q version=%q with nonce=%q",
//con, collection, request.VersionInfo, request.ResponseNonce)
//con.reporter.RecordRequestAck(collection, con.id)
//}
//}
//
//if w.cancel != nil {
//w.cancel()
//}
//
//sr := &source.Request{
//SinkNode:    request.SinkNode,
//Collection:  collection,
//VersionInfo: request.VersionInfo,
//}
//w.cancel = con.tracker.watcher.Watch(sr, func(r *source.WatchResponse) {
//response := con.prepareResponse(r)
//w.cancel()
//w.cancel = nil
//select {
//case con.responseC <- response:
//default:
//con.err = fmt.Errorf("channel full") // TODO: Error
//}
//con.pushServerResponse(w, )
//})

//type newPushResponseState int
//
//const (
//	newPushResponseStateReady newPushResponseState = iota
//	newPushResponseStateClosed
//)
//
//// watch maintains local push state of the most recent watch per-type.
//type watch struct {
//	// only accessed from connection goroutine
//	cancel          func()
//	nonce           string // most recent nonce
//	nonceVersionMap map[string]string
//
//	// NOTE: do not hold `mu` when reading/writing to this channel.
//	newPushResponseReadyChan chan newPushResponseState
//
//	mu                      sync.Mutex
//	newPushResponse         *source.WatchResponse
//	mostRecentNackedVersion string
//	timer                   *time.Timer
//	closed                  bool
//}
//
//func (w *watch) delayedPush() {
//	w.mu.Lock()
//	w.timer = nil
//	w.mu.Unlock()
//
//	select {
//	case w.newPushResponseReadyChan <- newPushResponseStateReady:
//	default:
//		time.AfterFunc(retryPushDelay, w.schedulePush)
//	}
//}
//
//// Try to schedule pushing a response to the node. The push may
//// be re-scheduled as needed. Additional care is taken to rate limit
//// re-pushing responses that were previously NACK'd. This avoid flooding
//// the node with responses while also allowing transient NACK'd responses
//// to be retried.
//func (w *watch) schedulePush() {
//	w.mu.Lock()
//
//	// close the watch
//	if w.closed {
//		// unlock before channel write
//		w.mu.Unlock()
//		select {
//		case w.newPushResponseReadyChan <- newPushResponseStateClosed:
//		default:
//			time.AfterFunc(retryPushDelay, w.schedulePush)
//		}
//		return
//	}
//
//	// no-op if the response has already be sent
//	if w.newPushResponse == nil {
//		w.mu.Unlock()
//		return
//	}
//
//	// delay re-sending a previously nack'd response
//	if w.newPushResponse.Version == w.mostRecentNackedVersion {
//		if w.timer == nil {
//			w.timer = time.AfterFunc(nackLimitFreq, w.delayedPush)
//		}
//		w.mu.Unlock()
//		return
//	}
//
//	// Otherwise, try to schedule the response to be sent.
//	if w.timer != nil {
//		if !w.timer.Stop() {
//			<-w.timer.C
//		}
//		w.timer = nil
//	}
//	// unlock before channel write
//	w.mu.Unlock()
//	select {
//	case w.newPushResponseReadyChan <- newPushResponseStateReady:
//	default:
//		time.AfterFunc(retryPushDelay, w.schedulePush)
//	}
//
//}

//
//// Save the pushed response in the newPushResponse and schedule a push. The push
//// may be re-schedule as necessary but this should be transparent to the
//// caller. The caller may provide a nil response to indicate that the watch
//// should be closed.
//func (w *watch) saveResponseAndSchedulePush(response *source.WatchResponse) {
//	w.mu.Lock()
//	r := w.
//	//w.newPushResponse = response
//	//if response == nil {
//	//	w.closed = true
//	//}
//	w.mu.Unlock()
//
//	w.schedulePush()
//}
