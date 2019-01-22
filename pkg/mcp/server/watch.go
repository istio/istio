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
	"sync"
	"sync/atomic"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/source"
)

type pushResponseGuard struct {
	w source.Watcher
}

var _ source.Watcher = &pushResponseGuard{}

func (g *pushResponseGuard) Watch(request *source.Request, fn source.PushResponseFunc) source.CancelWatchFunc {
	done := int32(0)

	return g.w.Watch(request, func(response *source.WatchResponse) {
		if !atomic.CompareAndSwapInt32(&done, 0, 1) {
			// TODO: borked, log
			return
		}
		fn(response)
	})
}

type watchTracker struct {
	externalNonces *nonceSource
	watchNonces    *nonceSource
	watches        map[string]*watch
	watcher        source.Watcher
	pushFn         func(nonce, *source.WatchResponse)
}

type watch struct {
	cancel          func()
	externalNonce   nonce // most recent nonce that was sent to the client as a response.
	watchNonce      nonce // nonce for the currently in-flight watch request against the watcher
	nonceVersionMap map[nonce]string
	lastNackVersion string

	mu sync.Mutex
}

func newWatchTracker(w source.Watcher, collections []string, fn func(nonce, *source.WatchResponse)) *watchTracker {
	t := &watchTracker{
		externalNonces: &nonceSource{},
		watchNonces:    &nonceSource{},
		watcher:        w,
		watches:        make(map[string]*watch, len(collections)),
		pushFn:         fn,
	}

	for _, c := range collections {
		t.watches[c] = &watch{
			nonceVersionMap: make(map[nonce]string),
		}
	}

	return t
}

func (t *watchTracker) hasCollection(collection string) bool {
	_, found := t.watches[collection]
	return found
}

func (t *watchTracker) nonceOf(collection string) string {
	w, ok := t.watches[collection]
	if !ok {
		return ""
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.externalNonce.String()
}

func (t *watchTracker) watch(collection, prevNonce string, node *mcp.SinkNode) bool {
	w, ok := t.watches[collection]
	if !ok {
		// Shouldn't happen, the caller should have checked hasCollection.
		return false
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// Check whether this is the first externalNonce, or a externalNonce that we expected.
	if w.externalNonce.isValid() && w.externalNonce.String() != prevNonce {
		// This is not a request/externalNonce we've expected.
		return false
	}

	// Nonces match, but we already have an active watch, so ignore this incoming request (dupe?).
	if w.watchNonce.isValid() {
		// we're receiving the request with the same externalNonce twice. This should not happen
		return false
	}

	// From here on, we've accepted the request, and will continue ahead with a watch.

	w.lastNackVersion = ""
	prevVersion := w.nonceVersionMap[w.externalNonce]
	delete(w.nonceVersionMap, w.externalNonce)

	w.externalNonce = t.externalNonces.next()
	watchNonce := t.watchNonces.next()
	w.watchNonce = watchNonce

	r := source.Request{
		SinkNode:    node,
		VersionInfo: prevVersion,
		Collection:  collection,
	}
	w.cancel = t.watcher.Watch(&r, func(r *source.WatchResponse) {
		// TODO: We shouldn't need this go
		go t.pushResponse(w, watchNonce, r)
	})

	return true
}

func (t *watchTracker) pushResponse(w *watch, nonce nonce, r *source.WatchResponse) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.watchNonce != nonce {
		// We're not getting called correctly by the watcher.
		return
	}

	w.watchNonce = invalidNonce
	w.cancel = nil
	w.nonceVersionMap[w.externalNonce] = r.Version

	t.pushFn(w.externalNonce, r)
}

func (t *watchTracker) abort() {
	for _, w := range t.watches {
		w.mu.Lock()
		if w.cancel != nil {
			w.cancel()
			w.cancel = nil
		}
		w.watchNonce = invalidNonce
		w.mu.Unlock()
	}
}

//
//func (t *watchTracker) getWatch(collection string) (*watch, bool) {
//	w, ok := t.watches[collection]
//	return w, ok
//}
//
//func (w *watch) isExpectedNonce(responseNonce string) bool {
//	w.mu.Lock()
//	defer w.mu.Unlock()
//	// Either this is the first request (hence tracker's externalNonce is empty), or externalNonces match
//	return w.externalNonce == "" || w.externalNonce == responseNonce
//}
//
//func (w *watch) registerRequest(responseNonce string, isNack bool) bool {
//	w.mu.Lock()
//	defer w.mu.Unlock()
//
//	delete(w.nonceVersionMap, responseNonce)
//	w.lastNackVersion = ""
//
//	if w.externalNonce != "" && w.externalNonce != responseNonce {
//		// This error path should not happen! Skip any requests that don't match the
//		// latest watch's externalNonce value. These could be dup requests or out-of-order
//		// requests from a buggy node.
//		// TODO: log
//		return false
//	}
//
//	if w.cancel != nil {
//		w.cancel()
//		w = nil
//	}
//
//	return true
//}
//
//func (w *watch) markNack(responseNonce string) bool {
//	w.mu.Lock()
//	defer w.mu.Unlock()
//
//	delete(w.nonceVersionMap, responseNonce)
//	w.lastNackVersion = ""
//
//	if version, ok := w.nonceVersionMap[responseNonce]; ok {
//		w.lastNackVersion = version
//	}
//
//	if w.cancel != nil {
//		w.cancel()
//	}
//
//	return true
//}
//
//func (t *watchTracker) ack(collection string, sinkNode *mcp.SinkNode, responseNonce, versionInfo string) error {
//	w, err := t.getWatch(collection)
//	if err != nil {
//		return err
//	}
//
//	w.mu.Lock()
//	defer w.mu.Unlock()
//
//	delete(w.nonceVersionMap, responseNonce)
//	w.lastNackVersion = ""
//
//	if w.externalNonce != "" && w.externalNonce != responseNonce {
//		// This error path should not happen! Skip any requests that don't match the
//		// latest watch's externalNonce value. These could be dup requests or out-of-order
//		// requests from a buggy node.
//		return nil
//	}
//
//	if w.cancel != nil {
//		w.cancel()
//	}
//
//	sr := &source.Request{
//		SinkNode:    sinkNode,
//		Collection:  collection,
//		VersionInfo: versionInfo,
//	}
//	w.cancel = t.watcher.Watch(sr, t.handlePush)
//
//	return nil
//}
//
//func (t *watchTracker) nack(collection string, sinkNode *mcp.SinkNode, responseNonce, versionInfo string) error {
//	w, err := t.getWatch(collection)
//	if err != nil {
//		return err
//	}
//	w.mu.Lock()
//	defer w.mu.Unlock()
//
//	delete(w.nonceVersionMap, responseNonce)
//	w.lastNackVersion = ""
//
//	if version, ok := w.nonceVersionMap[responseNonce]; ok {
//		w.lastNackVersion = version
//	}
//
//	if w.cancel != nil {
//		w.cancel()
//	}
//
//	sr := &source.Request{
//		SinkNode:    sinkNode,
//		Collection:  collection,
//		VersionInfo: versionInfo,
//	}
//	w.cancel = t.watcher.Watch(sr, t.handlePush)
//	return nil
//}
//
//func (t *watchTracker) handlePush(r *source.WatchResponse) {
//	t.mu.Lock()
//	defer t.mu.Unlock()
//
//	if r == nil {
//		delete(t.responses, r.Collection)
//		return
//	}
//
//	t.responses[r.Collection] = r
//	if t.ready != nil {
//		close(t.ready)
//	}
//}
//
//func (t *watchTracker) waitForNext(ctx context.Context) (*source.WatchResponse, error) {
//	t.mu.Lock()
//	ch := make(chan struct{})
//	t.ready = ch
//	t.mu.Unlock()
//
//	select {
//	case <-ch:
//
//	case <-ctx.Done():
//		return nil, ctx.Err()
//	}
//}

//
//// Reset on every request. Only the most recent NACK request (per
//// externalNonce) is tracked.
//w.lastNackVersion = ""
//
//// externalNonces can be reused across streams; we verify externalNonce only if externalNonce is not initialized
//if w.externalNonce == "" || w.externalNonce == request.ResponseNonce {
//if w.externalNonce == "" {
//scope.Infof("MCP: connection %v: WATCH for %v", con, collection)
//} else {
//if request.ErrorDetail != nil {
//scope.Warnf("MCP: connection %v: NACK collection=%v version=%v with externalNonce=%q (w.externalNonce=%q) error=%#v", // nolint: lll
//con, collection, request.VersionInfo, request.ResponseNonce, w.externalNonce, request.ErrorDetail)
//
//con.reporter.RecordRequestNack(collection, con.id, codes.Code(request.ErrorDetail.Code))
//
//if version, ok := w.nonceVersionMap[request.ResponseNonce]; ok {
//w.mu.Lock()
//w.lastNackVersion = version
//w.mu.Unlock()
//}
//} else {
//scope.Debugf("MCP: connection %v ACK collection=%q version=%q with externalNonce=%q",
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
//// watch maintains local push admitter of the most recent watch per-type.
//type watch struct {
//	// only accessed from connection goroutine
//	cancel          func()
//	externalNonce           string // most recent externalNonce
//	nonceVersionMap map[string]string
//
//	// NOTE: do not hold `mu` when reading/writing to this channel.
//	newPushResponseReadyChan chan newPushResponseState
//
//	mu                      sync.Mutex
//	newPushResponse         *source.WatchResponse
//	lastNackVersion string
//	timer                   *time.Timer
//	active                  bool
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
//	if w.active {
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
//	if w.newPushResponse.Version == w.lastNackVersion {
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
//// should be active.
//func (w *watch) saveResponseAndSchedulePush(response *source.WatchResponse) {
//	w.mu.Lock()
//	r := w.
//	//w.newPushResponse = response
//	//if response == nil {
//	//	w.active = true
//	//}
//	w.mu.Unlock()
//
//	w.schedulePush()
//}
