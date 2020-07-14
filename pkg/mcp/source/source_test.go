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
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/internal/test"
	"istio.io/istio/pkg/mcp/status"
	"istio.io/istio/pkg/mcp/testing/monitoring"
	"istio.io/pkg/log"
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
	watchResponses    map[string]*WatchResponse
	pushResponseFuncs map[string][]PushResponseFunc
	watchCreated      map[string]int
	client            bool
	closeWatch        bool
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
	if h.client == true {
		// Swallow trigger send for source client
		h.client = false
		return nil
	}
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

func (h *sourceTestHarness) setCloseWatch(closeWatch bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closeWatch = closeWatch
}

func (h *sourceTestHarness) watchesCreated(typeURL string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.watchCreated[typeURL]
}

func (h *sourceTestHarness) Watch(req *Request, pushResponse PushResponseFunc, peerAddr string) CancelWatchFunc {
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

	return func() {}
}

func (h *sourceTestHarness) injectWatchResponse(response *WatchResponse) {
	h.mu.Lock()
	defer h.mu.Unlock()

	collection := response.Collection

	if watches, ok := h.pushResponseFuncs[collection]; ok {
		for _, watch := range watches {
			watch(response)
		}
		delete(h.pushResponseFuncs, collection)
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

// nolint: unparam
func verifySentResourcesMultipleTypes(t *testing.T, h *sourceTestHarness, wantResources map[string]*mcp.Resources) map[string]*mcp.Resources {
	t.Helper()

	gotResources := make(map[string]*mcp.Resources)
	for {
		select {
		case got := <-h.resourcesChan:
			if _, ok := gotResources[got.Collection]; ok {
				t.Fatalf("gotResources duplicate response for type %v: %v", got.Collection, got)
			}
			gotResources[got.Collection] = got

			want, ok := wantResources[got.Collection]
			if !ok {
				t.Fatalf("gotResources unexpected response for type %v: %v", got.Collection, got)
			}

			if diff := cmp.Diff(*got, *want, cmpopts.IgnoreFields(mcp.Resources{}, "Nonce")); diff != "" {
				t.Fatalf("wrong responses for %v: \n got %+v \nwant %+v \n diff %v", got.Collection, got, want, diff)
			}

			if len(gotResources) == len(wantResources) {
				return gotResources
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for all responses: gotResources %v", gotResources)
		}
	}
}

func makeWatchResponse(collection string, version string, incremental bool, fakes ...*test.Fake) *WatchResponse { // nolint: unparam
	r := &WatchResponse{
		Collection: collection,
		Version:    version,
		Request: &Request{
			Collection:  collection,
			VersionInfo: version,
			SinkNode:    test.Node,
			incremental: incremental,
		},
	}
	for _, fake := range fakes {
		r.Resources = append(r.Resources, fake.Resource)
	}
	return r
}

func makeSourceUnderTest(w Watcher) *Source {
	fakeLimiter := test.NewFakePerConnLimiter()
	close(fakeLimiter.ErrCh)
	options := &Options{
		Watcher:            w,
		CollectionsOptions: CollectionOptionsFromSlice(test.SupportedCollections),
		Reporter:           monitoring.NewInMemoryStatsContext(),
		ConnRateLimiter:    fakeLimiter,
	}
	return New(options)
}

func TestSourceACKAddUpdateDelete(t *testing.T) {
	h := newSourceTestHarness(t)
	h.setContext(peer.NewContext(context.Background(), &peer.Peer{
		Addr: &net.IPAddr{IP: net.IPv4(192, 168, 1, 1)},
	}))

	s := makeSourceUnderTest(h)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		if err := s.ProcessStream(h); err != nil {
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
	}{
		{
			name:          "ack add A0",
			inject:        makeWatchResponse(test.FakeType0Collection, "1", false, test.Type0A[0]),
			wantResources: test.MakeResources(false, test.FakeType0Collection, "1", "1", nil, test.Type0A[0]),
			request:       test.MakeRequest(false, test.FakeType0Collection, "1", codes.OK),
		},
		{
			name:          "nack update A0",
			inject:        makeWatchResponse(test.FakeType0Collection, "2", false, test.Type0A[1]),
			wantResources: test.MakeResources(false, test.FakeType0Collection, "2", "2", nil, test.Type0A[1]),
			request:       test.MakeRequest(false, test.FakeType0Collection, "2", codes.InvalidArgument),
		},
		{
			name:          "ack update A0",
			inject:        makeWatchResponse(test.FakeType0Collection, "3", false, test.Type0A[1]),
			wantResources: test.MakeResources(false, test.FakeType0Collection, "3", "3", nil, test.Type0A[1]),
			request:       test.MakeRequest(false, test.FakeType0Collection, "3", codes.OK),
		},
		{
			name:          "delete A0",
			inject:        makeWatchResponse(test.FakeType0Collection, "4", false),
			wantResources: test.MakeResources(false, test.FakeType0Collection, "4", "4", nil),
			request:       test.MakeRequest(false, test.FakeType0Collection, "4", codes.OK),
		},
		{
			name:          "ack add A1 and A2",
			inject:        makeWatchResponse(test.FakeType0Collection, "5", false, test.Type0B[0], test.Type0C[0]),
			wantResources: test.MakeResources(false, test.FakeType0Collection, "5", "5", nil, test.Type0B[0], test.Type0C[0]),
			request:       test.MakeRequest(false, test.FakeType0Collection, "5", codes.OK),
		},
		{
			name:          "ack add0, update A1, keep A2",
			inject:        makeWatchResponse(test.FakeType0Collection, "6", false, test.Type0A[2], test.Type0B[1], test.Type0C[0]),
			wantResources: test.MakeResources(false, test.FakeType0Collection, "6", "6", nil, test.Type0A[2], test.Type0B[1], test.Type0C[0]),
			request:       test.MakeRequest(false, test.FakeType0Collection, "6", codes.OK),
		},
		{
			name:          "remove add A0 and update A1/A2 (incremental requested but source decides to use full-state)",
			inject:        makeWatchResponse(test.FakeType0Collection, "7", false, test.Type0B[2], test.Type0C[1]),
			wantResources: test.MakeResources(false, test.FakeType0Collection, "7", "7", nil, test.Type0B[2], test.Type0C[1]),
			request:       test.MakeRequest(true, test.FakeType0Collection, "7", codes.OK),
		},
	}

	// initial watch
	h.requestsChan <- test.MakeRequest(false, test.FakeType0Collection, "", codes.OK)

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
		if err := s.ProcessStream(h); err != nil {
			t.Errorf("Stream() => got %v, want no error", err)
		}
		wg.Done()
	}()

	// initial watch
	h.requestsChan <- test.MakeRequest(false, test.FakeType0Collection, "", codes.OK)

	// wait for watch to be created before injecting the response
	<-h.watchesCreatedChan[test.FakeType0Collection]

	h.injectWatchResponse(&WatchResponse{
		Collection: test.FakeType0Collection,
		Version:    "1",
		Resources:  []*mcp.Resource{test.Type0A[0].Resource},
	})

	verifySentResources(t, h, test.MakeResources(false, test.FakeType0Collection, "1", "1", nil, test.Type0A[0]))

	h.setRecvError(io.EOF)
	wg.Wait()
}

func TestSourceWatchClosed(t *testing.T) {
	h := newSourceTestHarness(t)
	h.setCloseWatch(true)
	h.requestsChan <- test.MakeRequest(false, test.FakeType0Collection, "", codes.OK)

	// check that response fails since watch gets closed
	s := makeSourceUnderTest(h)
	if err := s.ProcessStream(h); err == nil {
		t.Error("Stream() => got no error, want watch failed")
	}
}

func TestConnRateLimit(t *testing.T) {
	h := newSourceTestHarness(t)

	ctx, cancel := context.WithCancel(context.Background())
	type key int
	const ctxKey key = 0
	expectedCtx := "expectedCtx"
	h.ctx = context.WithValue(ctx, ctxKey, expectedCtx)

	h.requestsChan <- test.MakeRequest(false, test.FakeType0Collection, "", codes.OK)

	fakeLimiter := test.NewFakePerConnLimiter()

	s := makeSourceUnderTest(h)
	s.requestLimiter = fakeLimiter

	go s.ProcessStream(h)

	<-fakeLimiter.CreateCh
	c := <-fakeLimiter.WaitCh
	cancel()

	if len(fakeLimiter.CreateCh) != 0 {
		t.Error("Stream() => expected create to only get called once")
	}

	ctxVal := c.Value(ctxKey).(string)
	if ctxVal != expectedCtx {
		t.Errorf("Wait received wrong context: got %v want %v", ctxVal, expectedCtx)
	}

	if len(fakeLimiter.WaitCh) != 0 {
		t.Error("Stream() => expected Wait to only get called once")
	}
}

func TestConnRateLimitError(t *testing.T) {
	h := newSourceTestHarness(t)

	h.requestsChan <- test.MakeRequest(false, test.FakeType0Collection, "", codes.OK)

	fakeLimiter := test.NewFakePerConnLimiter()

	s := makeSourceUnderTest(h)
	s.requestLimiter = fakeLimiter

	errC := make(chan error)
	go func() {
		errC <- s.ProcessStream(h)
	}()

	expectedErr := "rate limiting went wrong"
	fakeLimiter.ErrCh <- errors.New(expectedErr)

	err := <-errC
	if err == nil || err.Error() != expectedErr {
		t.Errorf("Stream() => expected %v got %v", expectedErr, err)
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
	h.requestsChan <- test.MakeRequest(false, test.FakeType0Collection, "", codes.OK)

	// check that response fails since watch gets closed
	s := makeSourceUnderTest(h)
	if err := s.ProcessStream(h); err == nil {
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
	h.requestsChan <- test.MakeRequest(false, test.FakeType0Collection, "", codes.OK)

	options := &Options{
		Watcher:            h,
		CollectionsOptions: CollectionOptionsFromSlice(test.SupportedCollections),
		Reporter:           monitoring.NewInMemoryStatsContext(),
		ConnRateLimiter:    test.NewFakePerConnLimiter(),
	}
	// check that response fails since watch gets closed
	s := New(options)
	if err := s.ProcessStream(h); err == nil {
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
	h.requestsChan <- test.MakeRequest(false, "unsupportedtype", "", codes.OK)

	// check that response fails since watch gets closed
	s := makeSourceUnderTest(h)
	if err := s.ProcessStream(h); err == nil {
		t.Error("Stream() => got no error, want send error")
	}
}

func TestSourceStaleNonce(t *testing.T) {
	h := newSourceTestHarness(t)

	s := makeSourceUnderTest(h)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		if err := s.ProcessStream(h); err != nil {
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
	h.requestsChan <- test.MakeRequest(false, test.FakeType0Collection, "", codes.OK)

	verifySentResources(t, h, test.MakeResources(false, test.FakeType0Collection, "1", "1", nil, test.Type0A[0]))

	h.injectWatchResponse(&WatchResponse{
		Collection: test.FakeType0Collection,
		Version:    "2",
		Resources:  []*mcp.Resource{test.Type0A[1].Resource},
	})

	h.requestsChan <- test.MakeRequest(false, test.FakeType0Collection, "stale0", codes.OK)              // stale ACK
	h.requestsChan <- test.MakeRequest(false, test.FakeType0Collection, "stale1", codes.InvalidArgument) // stale NACK
	h.requestsChan <- test.MakeRequest(false, test.FakeType0Collection, "1", codes.OK)                   // valid ACK

	verifySentResources(t, h, test.MakeResources(false, test.FakeType0Collection, "2", "2", nil, test.Type0A[1]))

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
		if err := s.ProcessStream(h); err != nil {
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
	h.requestsChan <- test.MakeRequest(false, test.FakeType0Collection, "", codes.OK)
	h.requestsChan <- test.MakeRequest(false, test.FakeType1Collection, "", codes.OK)
	h.requestsChan <- test.MakeRequest(false, test.FakeType2Collection, "", codes.OK)

	verifySentResourcesMultipleTypes(t, h,
		map[string]*mcp.Resources{
			test.FakeType0Collection: test.MakeResources(false, test.FakeType0Collection, "1", "1", nil, test.Type0A[0]),
			test.FakeType1Collection: test.MakeResources(false, test.FakeType1Collection, "2", "2", nil, test.Type1A[0]),
			test.FakeType2Collection: test.MakeResources(false, test.FakeType2Collection, "3", "3", nil, test.Type2A[0]),
		},
	)

	h.setRecvError(io.EOF)
	wg.Wait()
}

func TestCalculateDelta(t *testing.T) {
	var (
		r0        = test.Type0A[0].Resource
		r0Updated = test.Type0A[1].Resource
		r1        = test.Type0B[0].Resource
		r2        = test.Type0C[0].Resource
	)

	cases := []struct {
		name        string
		current     []*mcp.Resource
		acked       map[string]string
		wantAdded   []mcp.Resource
		wantRemoved []string
	}{
		{
			name:      "empty acked set",
			current:   []*mcp.Resource{r0, r1, r2},
			acked:     map[string]string{},
			wantAdded: []mcp.Resource{*r0, *r1, *r2},
		},
		{
			name:    "incremental add",
			current: []*mcp.Resource{r0, r1, r2},
			acked: map[string]string{
				r0.Metadata.Name: r0.Metadata.Version,
			},
			wantAdded: []mcp.Resource{*r1, *r2},
		},
		{
			name:    "no-op push",
			current: []*mcp.Resource{r0, r1, r2},
			acked: map[string]string{
				r0.Metadata.Name: r0.Metadata.Version,
				r1.Metadata.Name: r1.Metadata.Version,
				r2.Metadata.Name: r2.Metadata.Version,
			},
			wantAdded: []mcp.Resource{},
		},
		{
			name:    "update existing",
			current: []*mcp.Resource{r0Updated, r1, r2},
			acked: map[string]string{
				r0.Metadata.Name: r0.Metadata.Version,
				r1.Metadata.Name: r1.Metadata.Version,
				r2.Metadata.Name: r2.Metadata.Version,
			},
			wantAdded: []mcp.Resource{*r0Updated},
		},
		{
			name:    "delete one",
			current: []*mcp.Resource{r1, r2},
			acked: map[string]string{
				r0.Metadata.Name: r0.Metadata.Version,
				r1.Metadata.Name: r1.Metadata.Version,
				r2.Metadata.Name: r2.Metadata.Version,
			},
			wantAdded:   []mcp.Resource{},
			wantRemoved: []string{r0.Metadata.Name},
		},
		{
			name:    "delete all",
			current: []*mcp.Resource{},
			acked: map[string]string{
				r0.Metadata.Name: r0.Metadata.Version,
				r1.Metadata.Name: r1.Metadata.Version,
				r2.Metadata.Name: r2.Metadata.Version,
			},
			wantAdded:   []mcp.Resource{},
			wantRemoved: []string{r0.Metadata.Name, r1.Metadata.Name, r2.Metadata.Name},
		},
		{
			name:    "add, update, and delete in the same push",
			current: []*mcp.Resource{r0Updated, r2},
			acked: map[string]string{
				r0.Metadata.Name: r0.Metadata.Version, // update
				r1.Metadata.Name: r1.Metadata.Version, // remove
			},
			wantAdded:   []mcp.Resource{*r0Updated, *r2},
			wantRemoved: []string{r1.Metadata.Name},
		},
	}

	sortAdded := cmp.Transformer("SortAdded", func(in []mcp.Resource) []mcp.Resource {
		out := append([]mcp.Resource(nil), in...) // copy
		sort.Slice(out, func(i, j int) bool { return out[i].Metadata.Name < out[j].Metadata.Name })
		return out
	})

	sortRemoved := cmp.Transformer("SortRemoved", func(in []string) []string {
		out := append([]string(nil), in...) // copy
		sort.Strings(out)
		return out
	})

	for i, c := range cases {
		t.Run(fmt.Sprintf("[%v] %v", i, c.name), func(tt *testing.T) {
			gotAdded, gotRemoved := calculateDelta(c.current, c.acked)

			if diff := cmp.Diff(gotAdded, c.wantAdded, sortAdded); diff != "" {
				tt.Errorf("wrong set of added resources: \n got %v \nwant %v\ndiff %v",
					gotAdded, c.wantAdded, diff)
			}

			if diff := cmp.Diff(gotRemoved, c.wantRemoved, sortRemoved); diff != "" {
				tt.Errorf("wrong set of removed resources: \n got %v \nwant %v\ndiff %v",
					gotRemoved, c.wantRemoved, diff)
			}
		})
	}
}

func TestSourceACKAddUpdateDelete_Incremental(t *testing.T) {
	h := newSourceTestHarness(t)
	h.setContext(peer.NewContext(context.Background(), &peer.Peer{
		Addr: &net.IPAddr{IP: net.IPv4(192, 168, 1, 1)},
	}))

	fakeLimiter := test.NewFakePerConnLimiter()
	close(fakeLimiter.ErrCh)
	options := &Options{
		Watcher:            h,
		CollectionsOptions: CollectionOptionsFromSlice(test.SupportedCollections),
		Reporter:           monitoring.NewInMemoryStatsContext(),
		ConnRateLimiter:    fakeLimiter,
	}
	for i := range options.CollectionsOptions {
		co := &options.CollectionsOptions[i]
		co.Incremental = true
	}
	s := New(options)

	for _, v := range s.collections {
		v.Incremental = true
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		if err := s.ProcessStream(h); err != nil {
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
	}{
		{
			name:   "ack add A0",
			inject: makeWatchResponse(test.FakeType0Collection, "1", true, test.Type0A[0]),
			// the first response can not be consider as incremental
			wantResources: test.MakeResources(false, test.FakeType0Collection, "1", "1", nil, test.Type0A[0]),
			request:       test.MakeRequest(true, test.FakeType0Collection, "1", codes.OK),
		},
		{
			name:          "nack update A0",
			inject:        makeWatchResponse(test.FakeType0Collection, "2", true, test.Type0A[1]),
			wantResources: test.MakeResources(true, test.FakeType0Collection, "2", "2", nil, test.Type0A[1]),
			request:       test.MakeRequest(true, test.FakeType0Collection, "2", codes.InvalidArgument),
		},
		{
			name:          "ack update A0",
			inject:        makeWatchResponse(test.FakeType0Collection, "3", true, test.Type0A[1]),
			wantResources: test.MakeResources(true, test.FakeType0Collection, "3", "3", nil, test.Type0A[1]),
			request:       test.MakeRequest(true, test.FakeType0Collection, "3", codes.OK),
		},
		{
			name:          "delete A0",
			inject:        makeWatchResponse(test.FakeType0Collection, "4", true),
			wantResources: test.MakeResources(true, test.FakeType0Collection, "4", "4", []string{test.Type0A[1].Metadata.Name}),
			request:       test.MakeRequest(true, test.FakeType0Collection, "4", codes.OK),
		},
		{
			name:          "ack add A1 and A2",
			inject:        makeWatchResponse(test.FakeType0Collection, "5", true, test.Type0B[0], test.Type0C[0]),
			wantResources: test.MakeResources(true, test.FakeType0Collection, "5", "5", nil, test.Type0B[0], test.Type0C[0]),
			request:       test.MakeRequest(true, test.FakeType0Collection, "5", codes.OK),
		},
		{
			name:          "ack add A0, update A1, keep A2",
			inject:        makeWatchResponse(test.FakeType0Collection, "6", true, test.Type0A[2], test.Type0B[1], test.Type0C[0]),
			wantResources: test.MakeResources(true, test.FakeType0Collection, "6", "6", nil, test.Type0A[2], test.Type0B[1]),
			request:       test.MakeRequest(true, test.FakeType0Collection, "6", codes.OK),
		},
		{
			name:          "keep A0, update A1, delete A2 (sink requested full-state)",
			inject:        makeWatchResponse(test.FakeType0Collection, "7", false, test.Type0A[2], test.Type0B[2]),
			wantResources: test.MakeResources(false, test.FakeType0Collection, "7", "7", nil, test.Type0A[2], test.Type0B[2]),
			request:       test.MakeRequest(false, test.FakeType0Collection, "7", codes.OK),
		},
		{
			name:          "remove A0, keep A1, add A2 (incremental after full-state)",
			inject:        makeWatchResponse(test.FakeType0Collection, "8", true, test.Type0B[2], test.Type0C[1]),
			wantResources: test.MakeResources(true, test.FakeType0Collection, "8", "8", []string{test.Type0A[2].Metadata.Name}, test.Type0C[1]),
			request:       test.MakeRequest(true, test.FakeType0Collection, "8", codes.OK),
		},
	}

	// initial watch
	h.requestsChan <- test.MakeRequest(false, test.FakeType0Collection, "", codes.OK)

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
