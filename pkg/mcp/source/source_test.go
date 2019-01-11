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
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gogo/status"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/mcp/internal/test"
	"istio.io/istio/pkg/mcp/testing/monitoring"
)

func init() {
	scope.SetOutputLevel(log.DebugLevel)
}

var (
	errSend = errors.New("send error")
	errRecv = errors.New("recv error")
)

type sourceTestHarness struct {
	resourcesChan      chan *mcp.Resources
	requestsChan       chan *mcp.RequestResources
	watchesCreatedChan map[string]chan struct{}
	recvErrorChan      chan error

	t *testing.T

	mu                sync.Mutex
	openErr           error
	sendErr           error
	recvErr           error
	ctx               context.Context
	nonce             int
	closeWatch        bool
	watchResponses    map[string]*WatchResponse
	pushResponseFuncs map[string][]PushResponseFunc
	watchCreated      map[string]int
}

func newSourceTestHarness(t *testing.T) *sourceTestHarness {
	s := &sourceTestHarness{
		ctx:                context.Background(),
		t:                  t,
		watchCreated:       make(map[string]int),
		watchesCreatedChan: make(map[string]chan struct{}),
		pushResponseFuncs:  make(map[string][]PushResponseFunc),
		watchResponses:     make(map[string]*WatchResponse),
	}
	for _, typeURL := range test.SupportedCollections {
		s.watchesCreatedChan[typeURL] = make(chan struct{}, 10)
	}
	s.resetStream()
	return s
}

func (h *sourceTestHarness) resetStream() {
	h.resourcesChan = make(chan *mcp.Resources, 10)
	h.requestsChan = make(chan *mcp.RequestResources, 10)
	h.recvErrorChan = make(chan error, 10)
	h.nonce = 0
}

func (h *sourceTestHarness) Send(resources *mcp.Resources) error {
	// check that nonce is monotonically incrementing
	h.nonce++
	if resources.Nonce != fmt.Sprintf("%d", h.nonce) {
		h.t.Errorf("nonce => got %q, want %d", resources.Nonce, h.nonce)
	}
	// check that type URL matches in resources
	if resources.Collection == "" {
		h.t.Error("TypeURL => got none, want non-empty")
	}

	if h.sendErr != nil {
		return h.sendErr
	}

	h.resourcesChan <- resources
	return nil
}

func (h *sourceTestHarness) Recv() (*mcp.RequestResources, error) {
	h.mu.Lock()
	err := h.recvErr
	h.mu.Unlock()
	if err != nil {
		return nil, err
	}

	select {
	case err := <-h.recvErrorChan:
		return nil, err
	case req, more := <-h.requestsChan:
		if !more {
			return nil, io.EOF
		}
		return req, nil
	}
}

func (h *sourceTestHarness) Context() context.Context {
	return h.ctx
}

func (h *sourceTestHarness) setContext(ctx context.Context) {
	h.ctx = ctx
}

func (h *sourceTestHarness) setOpenError(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.openErr = err
}

func (h *sourceTestHarness) openError() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.openErr
}

func (h *sourceTestHarness) setSendError(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sendErr = err
}

func (h *sourceTestHarness) setRecvError(err error) {
	if err != nil {
		h.recvErrorChan <- err
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.recvErr = err
}

func (h *sourceTestHarness) setCloseWatch(close bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closeWatch = close
}

func (h *sourceTestHarness) watchesCreated(typeURL string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.watchCreated[typeURL]
}

func (h *sourceTestHarness) Watch(req *Request, pushResponse PushResponseFunc) CancelWatchFunc {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.watchCreated[req.Collection]++

	if rsp, ok := h.watchResponses[req.Collection]; ok {
		delete(h.watchResponses, req.Collection)
		rsp.Collection = req.Collection
		pushResponse(rsp)
		return nil
	} else if h.closeWatch {
		pushResponse(nil)
		return nil
	} else {
		// save open watch channel for later
		h.pushResponseFuncs[req.Collection] = append(h.pushResponseFuncs[req.Collection], pushResponse)
	}

	if ch, ok := h.watchesCreatedChan[req.Collection]; ok {
		ch <- struct{}{}
	}

	return func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.watchResponses, req.Collection)
	}
}

func (h *sourceTestHarness) injectWatchResponse(response *WatchResponse) {
	h.mu.Lock()
	defer h.mu.Unlock()

	collection := response.Collection

	if watches, ok := h.pushResponseFuncs[collection]; ok {
		for _, watch := range watches {
			watch(response)
		}
	} else {
		h.watchResponses[collection] = response
	}
}

func verifySentResources(t *testing.T, h *sourceTestHarness, want *mcp.Resources) {
	t.Helper()

	select {
	case got := <-h.resourcesChan:
		if diff := cmp.Diff(got, want); diff != "" {
			t.Fatalf("wrong set of responses: \n got %v \nwant %v \n diff %v", got, want, diff)
		}
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for response")
	}
}

func makeWatchResponse(collection string, version string, fakes ...*test.Fake) *WatchResponse { // nolint: unparam
	r := &WatchResponse{
		Collection: collection,
		Version:    version,
	}
	for _, fake := range fakes {
		r.Resources = append(r.Resources, fake.Resource)
	}
	return r
}

func makeSourceUnderTest(w Watcher) *Source {
	options := &Options{
		Watcher:     w,
		Collections: test.SupportedCollections,
		Reporter:    monitoring.NewInMemoryStatsContext(),
	}
	return NewSource(options)
}

func TestSourceACKWithIncrementalAddUpdateDelete(t *testing.T) {
	h := newSourceTestHarness(t)
	h.setContext(peer.NewContext(context.Background(), &peer.Peer{
		Addr: &net.IPAddr{IP: net.IPv4(192, 168, 1, 1)},
	}))

	s := makeSourceUnderTest(h)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		if err := s.processStream(h); err != nil {
			t.Errorf("Stream() => got %v, want no error", err)
		}
		wg.Done()
	}()

	defer func() {
		h.setRecvError(io.EOF)
		wg.Wait()
	}()

	steps := []struct {
		name          string
		request       *mcp.RequestResources
		wantResources *mcp.Resources
		inject        *WatchResponse
		version       string
	}{
		{
			name:          "ack add A",
			inject:        makeWatchResponse(test.FakeType0Collection, "1", test.Type0A[0]),
			wantResources: test.MakeResources(test.FakeType0Collection, "1", "1", nil, test.Type0A[0]),
			request:       test.MakeRequest(test.FakeType0Collection, "1", codes.OK),
			version:       "v1",
		},
		{
			name:          "nack update A",
			inject:        makeWatchResponse(test.FakeType0Collection, "2", test.Type0A[1]),
			wantResources: test.MakeResources(test.FakeType0Collection, "2", "2", nil, test.Type0A[1]),
			request:       test.MakeRequest(test.FakeType0Collection, "2", codes.InvalidArgument),
			version:       "v3",
		},
		{
			name:          "ack update A",
			inject:        makeWatchResponse(test.FakeType0Collection, "3", test.Type0A[1]),
			wantResources: test.MakeResources(test.FakeType0Collection, "3", "3", nil, test.Type0A[1]),
			request:       test.MakeRequest(test.FakeType0Collection, "3", codes.OK),
			version:       "v4",
		},
		{
			name:          "delete A",
			inject:        makeWatchResponse(test.FakeType0Collection, "4"),
			wantResources: test.MakeResources(test.FakeType0Collection, "4", "4", []string{test.Type0A[0].Metadata.Name}),
			request:       test.MakeRequest(test.FakeType0Collection, "4", codes.OK),
			version:       "v5",
		},
	}

	// initial watch
	h.requestsChan <- test.MakeRequest(test.FakeType0Collection, "", codes.OK)

	for i, step := range steps {
		passed := t.Run(fmt.Sprintf("[%v] %s", i, step.name), func(tt *testing.T) {
			h.injectWatchResponse(step.inject)
			verifySentResources(tt, h, step.wantResources)
			h.requestsChan <- step.request
		})
		if !passed {
			t.Fatal("subtest failed")
		}
	}

}

func TestSourceWatchBeforeResponsesAvailable(t *testing.T) {
	h := newSourceTestHarness(t)

	s := makeSourceUnderTest(h)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		if err := s.processStream(h); err != nil {
			t.Errorf("Stream() => got %v, want no error", err)
		}
		wg.Done()
	}()

	// initial watch
	h.requestsChan <- test.MakeRequest(test.FakeType0Collection, "", codes.OK)

	// wait for watch to be created before injecting the response
	<-h.watchesCreatedChan[test.FakeType0Collection]

	h.injectWatchResponse(&WatchResponse{
		Collection: test.FakeType0Collection,
		Version:    "1",
		Resources:  []*mcp.Resource{test.Type0A[0].Resource},
	})

	verifySentResources(t, h, test.MakeResources(test.FakeType0Collection, "1", "1", nil, test.Type0A[0]))

	h.setRecvError(io.EOF)
	wg.Wait()
}

func TestSourceWatchClosed(t *testing.T) {
	h := newSourceTestHarness(t)
	h.setCloseWatch(true)
	h.requestsChan <- test.MakeRequest(test.FakeType0Collection, "", codes.OK)

	// check that response fails since watch gets closed
	s := makeSourceUnderTest(h)
	if err := s.processStream(h); err == nil {
		t.Error("Stream() => got no error, want watch failed")
	}
}

func TestSourceSendError(t *testing.T) {
	h := newSourceTestHarness(t)
	h.injectWatchResponse(&WatchResponse{
		Collection: test.FakeType0Collection,
		Version:    "1",
		Resources:  []*mcp.Resource{test.Type0A[0].Resource},
	})

	h.setSendError(errSend)
	// initial watch
	h.requestsChan <- test.MakeRequest(test.FakeType0Collection, "", codes.OK)

	// check that response fails since watch gets closed
	s := makeSourceUnderTest(h)
	if err := s.processStream(h); err == nil {
		t.Error("Stream() => got no error, want send error")
	}
}

func TestSourceReceiveError(t *testing.T) {
	h := newSourceTestHarness(t)
	h.injectWatchResponse(&WatchResponse{
		Collection: test.FakeType0Collection,
		Version:    "1",
		Resources:  []*mcp.Resource{test.Type0A[0].Resource},
	})

	h.setRecvError(status.Error(codes.Internal, "internal receive error"))
	// initial watch
	h.requestsChan <- test.MakeRequest(test.FakeType0Collection, "", codes.OK)

	options := &Options{
		Watcher:     h,
		Collections: test.SupportedCollections,
		Reporter:    monitoring.NewInMemoryStatsContext(),
	}
	// check that response fails since watch gets closed
	s := NewSource(options)
	if err := s.processStream(h); err == nil {
		t.Error("Stream() => got no error, want send error")
	}
}

func TestSourceUnsupportedTypeError(t *testing.T) {
	h := newSourceTestHarness(t)
	h.injectWatchResponse(&WatchResponse{
		Collection: "unsupportedType",
		Version:    "1",
		Resources:  []*mcp.Resource{test.Type0A[0].Resource},
	})

	// initial watch with bad type
	h.requestsChan <- test.MakeRequest("unsupportedtype", "", codes.OK)

	// check that response fails since watch gets closed
	s := makeSourceUnderTest(h)
	if err := s.processStream(h); err == nil {
		t.Error("Stream() => got no error, want send error")
	}
}

func TestSourceStaleNonce(t *testing.T) {
	h := newSourceTestHarness(t)

	s := makeSourceUnderTest(h)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		if err := s.processStream(h); err != nil {
			t.Errorf("StreamAggregatedResources() => got %v, want no error", err)
		}
		wg.Done()
	}()

	h.injectWatchResponse(&WatchResponse{
		Collection: test.FakeType0Collection,
		Version:    "1",
		Resources:  []*mcp.Resource{test.Type0A[0].Resource},
	})

	// initial watch
	h.requestsChan <- test.MakeRequest(test.FakeType0Collection, "", codes.OK)

	verifySentResources(t, h, test.MakeResources(test.FakeType0Collection, "1", "1", nil, test.Type0A[0]))

	h.injectWatchResponse(&WatchResponse{
		Collection: test.FakeType0Collection,
		Version:    "2",
		Resources:  []*mcp.Resource{test.Type0A[1].Resource},
	})

	h.requestsChan <- test.MakeRequest(test.FakeType0Collection, "stale0", codes.OK)              // stale ACK
	h.requestsChan <- test.MakeRequest(test.FakeType0Collection, "stale1", codes.InvalidArgument) // stale NACK
	h.requestsChan <- test.MakeRequest(test.FakeType0Collection, "1", codes.OK)                   // valid ACK

	verifySentResources(t, h, test.MakeResources(test.FakeType0Collection, "2", "2", nil, test.Type0A[1]))

	h.setRecvError(io.EOF)
	wg.Wait()

	want := 2
	if got := h.watchesCreated(test.FakeType0Collection); got != want {
		t.Fatalf("Wrong number of watches created: got %v want %v", got, want)
	}
}

func TestSourceConcurrentRequestsForMultipleTypes(t *testing.T) {
	h := newSourceTestHarness(t)

	s := makeSourceUnderTest(h)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		if err := s.processStream(h); err != nil {
			t.Errorf("StreamAggregatedResources() => got %v, want no error", err)
		}
		wg.Done()
	}()

	h.injectWatchResponse(&WatchResponse{
		Collection: test.FakeType0Collection,
		Version:    "1",
		Resources:  []*mcp.Resource{test.Type0A[0].Resource},
	})
	h.injectWatchResponse(&WatchResponse{
		Collection: test.FakeType1Collection,
		Version:    "2",
		Resources:  []*mcp.Resource{test.Type1A[0].Resource},
	})
	h.injectWatchResponse(&WatchResponse{
		Collection: test.FakeType2Collection,
		Version:    "3",
		Resources:  []*mcp.Resource{test.Type2A[0].Resource},
	})

	// initial watch
	h.requestsChan <- test.MakeRequest(test.FakeType0Collection, "", codes.OK)
	h.requestsChan <- test.MakeRequest(test.FakeType1Collection, "", codes.OK)
	h.requestsChan <- test.MakeRequest(test.FakeType2Collection, "", codes.OK)

	verifySentResourcesMultipleTypes(t, h,
		map[string]*mcp.Resources{
			test.FakeType0Collection: test.MakeResources(test.FakeType0Collection, "1", "1", nil, test.Type0A[0]),
			test.FakeType1Collection: test.MakeResources(test.FakeType1Collection, "2", "2", nil, test.Type1A[0]),
			test.FakeType2Collection: test.MakeResources(test.FakeType2Collection, "3", "3", nil, test.Type2A[0]),
		},
	)

	h.setRecvError(io.EOF)
	wg.Wait()
}
