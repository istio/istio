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
	"errors"
	"fmt"
	"math"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/gogo/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/internal/test"
	"istio.io/istio/pkg/mcp/source"
	"istio.io/istio/pkg/mcp/testing/monitoring"
)

type mockConfigWatcher struct {
	mu        sync.Mutex
	counts    map[string]int
	responses map[string]*source.WatchResponse

	watchesCreated map[string]chan struct{}
	watches        map[string][]source.PushResponseFunc
	closeWatch     bool
}

func (config *mockConfigWatcher) Watch(req *source.Request, pushResponse source.PushResponseFunc) source.CancelWatchFunc {
	config.mu.Lock()
	defer config.mu.Unlock()

	config.counts[req.Collection]++

	if rsp, ok := config.responses[req.Collection]; ok {
		rsp.Collection = req.Collection
		pushResponse(rsp)
		return nil
	} else if config.closeWatch {
		pushResponse(nil)
		return nil
	} else {
		// save open watch channel for later
		config.watches[req.Collection] = append(config.watches[req.Collection], pushResponse)
	}

	if ch, ok := config.watchesCreated[req.Collection]; ok {
		ch <- struct{}{}
	}

	return func() {}
}

func (config *mockConfigWatcher) setResponse(response *source.WatchResponse) {
	config.mu.Lock()
	defer config.mu.Unlock()

	typeURL := response.Collection

	if watches, ok := config.watches[typeURL]; ok {
		for _, watch := range watches {
			watch(response)
		}
	} else {
		config.responses[typeURL] = response
	}
}

func makeMockConfigWatcher() *mockConfigWatcher {
	return &mockConfigWatcher{
		counts:         make(map[string]int),
		watchesCreated: make(map[string]chan struct{}),
		watches:        make(map[string][]source.PushResponseFunc),
		responses:      make(map[string]*source.WatchResponse),
	}
}

type mockStream struct {
	t         *testing.T
	recv      chan *mcp.MeshConfigRequest
	sent      chan *mcp.MeshConfigResponse
	nonce     int
	sendError bool
	recvError error
	grpc.ServerStream
}

func (stream *mockStream) Send(resp *mcp.MeshConfigResponse) error {
	// check that externalNonce is monotonically incrementing
	stream.nonce++
	if resp.Nonce != fmt.Sprintf("%d", stream.nonce) {
		stream.t.Errorf("Nonce => got %q, want %d", resp.Nonce, stream.nonce)
	}
	// check that version is set
	if resp.VersionInfo == "" {
		stream.t.Error("VersionInfo => got none, want non-empty")
	}
	// check resources are non-empty
	if len(resp.Resources) == 0 {
		stream.t.Error("Resources => got none, want non-empty")
	}
	// check that type URL matches in resources
	if resp.TypeUrl == "" {
		stream.t.Error("TypeUrl => got none, want non-empty")
	}
	stream.sent <- resp
	if stream.sendError {
		return errors.New("send error")
	}
	return nil
}

func (stream *mockStream) Recv() (*mcp.MeshConfigRequest, error) {
	if stream.recvError != nil {
		return nil, stream.recvError
	}
	req, more := <-stream.recv
	if !more {
		return nil, status.Error(codes.Canceled, "empty")
	}
	return req, nil
}

func (stream *mockStream) Context() context.Context {
	p := peer.Peer{Addr: &net.IPAddr{IP: net.IPv4(1, 2, 3, 4)}}
	return peer.NewContext(context.Background(), &p)
}

func makeMockStream(t *testing.T) *mockStream {
	return &mockStream{
		t:    t,
		sent: make(chan *mcp.MeshConfigResponse, 10),
		recv: make(chan *mcp.MeshConfigRequest, 10),
	}
}

func TestMultipleRequests(t *testing.T) {
	config := makeMockConfigWatcher()

	config.setResponse(&source.WatchResponse{
		Collection: test.FakeType0Collection,
		Version:    "1",
		Resources:  []*mcp.Resource{test.Type0A[0].Resource},
	})

	// make a request
	stream := makeMockStream(t)
	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode: test.Node,
		TypeUrl:  test.FakeType0Collection,
	}

	options := &source.Options{
		Watcher:            config,
		CollectionsOptions: source.CollectionOptionsFromSlice(test.SupportedCollections),
		Reporter:           monitoring.NewInMemoryStatsContext(),
	}
	s := New(options, test.NewFakeAuthChecker())
	go func() {
		if err := s.StreamAggregatedResources(stream); err != nil {
			t.Errorf("Stream() => got %v, want no error", err)
		}
	}()

	var rsp *mcp.MeshConfigResponse

	// check a response
	select {
	case rsp = <-stream.sent:
		if want := map[string]int{test.FakeType0Collection: 1}; !reflect.DeepEqual(want, config.counts) {
			t.Errorf("watch counts => got %v, want %v", config.counts, want)
		}
	//case <-time.After(time.Second):
	//	t.Fatalf("got no response")
	}

	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode:      test.Node,
		TypeUrl:       test.FakeType0Collection,
		VersionInfo:   rsp.VersionInfo,
		ResponseNonce: rsp.Nonce,
	}

	// check a response
	select {
	case <-stream.sent:
		if want := map[string]int{test.FakeType0Collection: 2}; !reflect.DeepEqual(want, config.counts) {
			t.Errorf("watch counts => got %v, want %v", config.counts, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("got no response")
	}
}

type fakeAuthChecker struct {
	err error
}

func (f *fakeAuthChecker) Check(authInfo credentials.AuthInfo) error {
	return f.err
}

func TestAuthCheck_Failure(t *testing.T) {
	config := makeMockConfigWatcher()
	config.setResponse(&source.WatchResponse{
		Collection: test.FakeType0Collection,
		Version:    "1",
		Resources:  []*mcp.Resource{test.Type0A[0].Resource},
	})

	// make a request
	stream := makeMockStream(t)
	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode: test.Node,
		TypeUrl:  test.FakeType0Collection,
	}

	checker := test.NewFakeAuthChecker()
	checker.AllowError = errors.New("disallow")
	options := &source.Options{
		Watcher:            config,
		CollectionsOptions: source.CollectionOptionsFromSlice(test.SupportedCollections),
		Reporter:           monitoring.NewInMemoryStatsContext(),
	}
	s := New(options, checker)

	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		if err := s.StreamAggregatedResources(stream); err == nil {
			t.Error("Stream() => expected error not found")
		}
		wg.Done()
	}()

	wg.Wait()
}

func TestAuthCheck_Success(t *testing.T) {
	config := makeMockConfigWatcher()

	config.setResponse(&source.WatchResponse{
		Collection: test.FakeType0Collection,
		Version:    "1",
		Resources:  []*mcp.Resource{test.Type0A[0].Resource},
	})

	// make a request
	stream := makeMockStream(t)
	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode: test.Node,
		TypeUrl:  test.FakeType0Collection,
	}

	options := &source.Options{
		Watcher:            config,
		CollectionsOptions: source.CollectionOptionsFromSlice(test.SupportedCollections),
		Reporter:           monitoring.NewInMemoryStatsContext(),
	}
	s := New(options, test.NewFakeAuthChecker())
	go func() {
		if err := s.StreamAggregatedResources(stream); err != nil {
			t.Errorf("Stream() => got %v, want no error", err)
		}
	}()

	var rsp *mcp.MeshConfigResponse

	// check a response
	select {
	case rsp = <-stream.sent:
		if want := map[string]int{test.FakeType0Collection: 1}; !reflect.DeepEqual(want, config.counts) {
			t.Errorf("watch counts => got %v, want %v", config.counts, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("got no response")
	}

	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode:      test.Node,
		TypeUrl:       test.FakeType0Collection,
		VersionInfo:   rsp.VersionInfo,
		ResponseNonce: rsp.Nonce,
	}

	// check a response
	select {
	case <-stream.sent:
		if want := map[string]int{test.FakeType0Collection: 2}; !reflect.DeepEqual(want, config.counts) {
			t.Errorf("watch counts => got %v, want %v", config.counts, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("got no response")
	}
}

func TestWatchBeforeResponsesAvailable(t *testing.T) {
	config := makeMockConfigWatcher()
	config.watchesCreated[test.FakeType0Collection] = make(chan struct{})

	// make a request
	stream := makeMockStream(t)
	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode: test.Node,
		TypeUrl:  test.FakeType0Collection,
	}

	options := &source.Options{
		Watcher:            config,
		CollectionsOptions: source.CollectionOptionsFromSlice(test.SupportedCollections),
		Reporter:           monitoring.NewInMemoryStatsContext(),
	}
	s := New(options, test.NewFakeAuthChecker())
	go func() {
		if err := s.StreamAggregatedResources(stream); err != nil {
			t.Errorf("Stream() => got %v, want no error", err)
		}
	}()

	<-config.watchesCreated[test.FakeType0Collection]
	config.setResponse(&source.WatchResponse{
		Collection: test.FakeType0Collection,
		Version:    "1",
		Resources:  []*mcp.Resource{test.Type0A[0].Resource},
	})

	// check a response
	select {
	case <-stream.sent:
		close(stream.recv)
		if want := map[string]int{test.FakeType0Collection: 1}; !reflect.DeepEqual(want, config.counts) {
			t.Errorf("watch counts => got %v, want %v", config.counts, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("got no response")
	}
}

func TestWatchClosed(t *testing.T) {
	config := makeMockConfigWatcher()
	config.closeWatch = true

	// make a request
	stream := makeMockStream(t)
	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode: test.Node,
		TypeUrl:  test.FakeType0Collection,
	}

	// check that response fails since watch gets active
	options := &source.Options{
		Watcher:            config,
		CollectionsOptions: source.CollectionOptionsFromSlice(test.SupportedCollections),
		Reporter:           monitoring.NewInMemoryStatsContext(),
	}
	s := New(options, test.NewFakeAuthChecker())
	if err := s.StreamAggregatedResources(stream); err == nil {
		t.Error("Stream() => got no error, want watch failed")
	}
	close(stream.recv)
}

func TestSendError(t *testing.T) {
	config := makeMockConfigWatcher()
	config.setResponse(&source.WatchResponse{
		Collection: test.FakeType0Collection,
		Version:    "1",
		Resources:  []*mcp.Resource{test.Type0A[0].Resource},
	})

	// make a request
	stream := makeMockStream(t)
	stream.sendError = true
	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode: test.Node,
		TypeUrl:  test.FakeType0Collection,
	}

	options := &source.Options{
		Watcher:            config,
		CollectionsOptions: source.CollectionOptionsFromSlice(test.SupportedCollections),
		Reporter:           monitoring.NewInMemoryStatsContext(),
	}
	s := New(options, test.NewFakeAuthChecker())
	// check that response fails since watch gets active
	if err := s.StreamAggregatedResources(stream); err == nil {
		t.Error("Stream() => got no error, want send error")
	}

	close(stream.recv)
}

func TestReceiveError(t *testing.T) {
	config := makeMockConfigWatcher()

	config.setResponse(&source.WatchResponse{
		Collection: test.FakeType0Collection,
		Version:    "1",
		Resources:  []*mcp.Resource{test.Type0A[0].Resource},
	})

	// make a request
	stream := makeMockStream(t)
	stream.recvError = status.Error(codes.Internal, "internal receive error")
	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode: test.Node,
		TypeUrl:  test.FakeType0Collection,
	}

	// check that response fails since watch gets active
	options := &source.Options{
		Watcher:            config,
		CollectionsOptions: source.CollectionOptionsFromSlice(test.SupportedCollections),
		Reporter:           monitoring.NewInMemoryStatsContext(),
	}
	s := New(options, test.NewFakeAuthChecker())
	if err := s.StreamAggregatedResources(stream); err == nil {
		t.Error("Stream() => got no error, want send error")
	}

	close(stream.recv)
}

func TestUnsupportedTypeError(t *testing.T) {
	config := makeMockConfigWatcher()

	config.setResponse(&source.WatchResponse{
		Collection: "unsupportedCollection",
		Version:    "1",
		Resources:  []*mcp.Resource{test.Type0A[0].Resource},
	})

	// make a request
	stream := makeMockStream(t)
	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode: test.Node,
		TypeUrl:  "unsupportedtype",
	}

	// check that response fails since watch gets active
	options := &source.Options{
		Watcher:            config,
		CollectionsOptions: source.CollectionOptionsFromSlice(test.SupportedCollections),
		Reporter:           monitoring.NewInMemoryStatsContext(),
	}
	s := New(options, test.NewFakeAuthChecker())
	if err := s.StreamAggregatedResources(stream); err == nil {
		t.Error("Stream() => got no error, want send error")
	}

	close(stream.recv)
}

func TestStaleNonce(t *testing.T) {
	config := makeMockConfigWatcher()

	config.setResponse(&source.WatchResponse{
		Collection: test.FakeType0Collection,
		Version:    "1",
		Resources:  []*mcp.Resource{test.Type0A[0].Resource},
	})

	stream := makeMockStream(t)
	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode: test.Node,
		TypeUrl:  test.FakeType0Collection,
	}
	stop := make(chan struct{})
	options := &source.Options{
		Watcher:            config,
		CollectionsOptions: source.CollectionOptionsFromSlice(test.SupportedCollections),
		Reporter:           monitoring.NewInMemoryStatsContext(),
	}
	s := New(options, test.NewFakeAuthChecker())
	go func() {
		if err := s.StreamAggregatedResources(stream); err != nil {
			t.Errorf("StreamAggregatedResources() => got %v, want no error", err)
		}
		// should be two watches called
		if want := map[string]int{test.FakeType0Collection: 2}; !reflect.DeepEqual(want, config.counts) {
			t.Errorf("watch counts => got %v, want %v", config.counts, want)
		}
		close(stop)
	}()
	select {
	case <-stream.sent:
		// stale request
		stream.recv <- &mcp.MeshConfigRequest{
			SinkNode:      test.Node,
			TypeUrl:       test.FakeType0Collection,
			ResponseNonce: "xyz",
		}
		// fresh request
		stream.recv <- &mcp.MeshConfigRequest{
			VersionInfo:   "1",
			SinkNode:      test.Node,
			TypeUrl:       test.FakeType0Collection,
			ResponseNonce: "1",
		}
		close(stream.recv)
	case <-time.After(time.Second):
		t.Fatalf("got %d messages on the stream, not 4", stream.nonce)
	}
	<-stop
}

func TestAggregatedHandlers(t *testing.T) {
	config := makeMockConfigWatcher()

	config.setResponse(&source.WatchResponse{
		Collection: test.FakeType0Collection,
		Version:    "1",
		Resources:  []*mcp.Resource{test.Type0A[0].Resource},
	})
	config.setResponse(&source.WatchResponse{
		Collection: test.FakeType1Collection,
		Version:    "2",
		Resources:  []*mcp.Resource{test.Type1A[0].Resource},
	})
	config.setResponse(&source.WatchResponse{
		Collection: test.FakeType2Collection,
		Version:    "3",
		Resources:  []*mcp.Resource{test.Type2A[0].Resource},
	})

	stream := makeMockStream(t)
	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode: test.Node,
		TypeUrl:  test.FakeType0Collection,
	}
	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode: test.Node,
		TypeUrl:  test.FakeType1Collection,
	}
	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode: test.Node,
		TypeUrl:  test.FakeType2Collection,
	}

	options := &source.Options{
		Watcher:            config,
		CollectionsOptions: source.CollectionOptionsFromSlice(test.SupportedCollections),
		Reporter:           monitoring.NewInMemoryStatsContext(),
	}
	s := New(options, test.NewFakeAuthChecker())
	go func() {
		if err := s.StreamAggregatedResources(stream); err != nil {
			t.Errorf("StreamAggregatedResources() => got %v, want no error", err)
		}
	}()

	want := map[string]int{
		test.FakeType0Collection: 1,
		test.FakeType1Collection: 1,
		test.FakeType2Collection: 1,
	}

	count := 0
	for {
		select {
		case <-stream.sent:
			count++
			if count >= len(want) {
				close(stream.recv)
				if !reflect.DeepEqual(want, config.counts) {
					t.Errorf("watch counts => got %v, want %v", config.counts, want)
				}
				// got all messages
				return
			}
		case <-time.After(time.Second):
			t.Fatalf("got %d mesages on the stream, not %v", count, len(want))
		}
	}
}

func TestAggregateRequestType(t *testing.T) {
	config := makeMockConfigWatcher()

	stream := makeMockStream(t)
	stream.recv <- &mcp.MeshConfigRequest{SinkNode: test.Node}

	options := &source.Options{
		Watcher:            config,
		CollectionsOptions: source.CollectionOptionsFromSlice(test.SupportedCollections),
		Reporter:           monitoring.NewInMemoryStatsContext(),
	}
	s := New(options, test.NewFakeAuthChecker())
	if err := s.StreamAggregatedResources(stream); err == nil {
		t.Error("StreamAggregatedResources() => got nil, want an error")
	}
	close(stream.recv)
}

func TestRateLimitNACK(t *testing.T) {
	origNackLimitFreq := nackLimitFreq
	nackLimitFreq = 1 * time.Millisecond
	defer func() {
		nackLimitFreq = origNackLimitFreq
	}()

	config := makeMockConfigWatcher()
	options := &source.Options{
		Watcher:            config,
		CollectionsOptions: source.CollectionOptionsFromSlice(test.SupportedCollections),
		Reporter:           monitoring.NewInMemoryStatsContext(),
	}
	s := New(options, test.NewFakeAuthChecker())

	stream := makeMockStream(t)
	go func() {
		if err := s.StreamAggregatedResources(stream); err != nil {
			t.Errorf("StreamAggregatedResources() => got %v, want no error", err)
		}
	}()

	var (
		nackFloodRepeatDuration   = time.Millisecond * 500
		nackFloodMaxWantResponses = int(nackFloodRepeatDuration/nackLimitFreq) + 1
		nackFloodMinWantResponses = nackFloodMaxWantResponses / 2

		ackFloodRepeatDuration   = time.Millisecond * 500
		ackFloodMaxWantResponses = math.MaxInt64
		ackFloodMinWantResponses = int(ackFloodRepeatDuration/nackLimitFreq) + 1
	)

	steps := []struct {
		name           string
		pushedVersion  string
		ackedVersion   string
		minResponses   int
		maxResponses   int
		repeatDuration time.Duration
		errDetails     error
	}{
		{
			name:          "initial push",
			pushedVersion: "1",
			ackedVersion:  "1",
			minResponses:  1,
			maxResponses:  1,
		},
		{
			name:          "first ack",
			pushedVersion: "2",
			ackedVersion:  "2",
			minResponses:  1,
			maxResponses:  1,
		},
		{
			name:           "verify nack flood is rate limited",
			pushedVersion:  "3",
			ackedVersion:   "2",
			errDetails:     errors.New("nack"),
			minResponses:   nackFloodMinWantResponses,
			maxResponses:   nackFloodMaxWantResponses,
			repeatDuration: nackFloodRepeatDuration,
		},
		{
			name:           "verify back-to-back ack is not rate limited",
			pushedVersion:  "4",
			ackedVersion:   "4", // resuse version to simplify test code below
			minResponses:   ackFloodMinWantResponses,
			maxResponses:   ackFloodMaxWantResponses,
			repeatDuration: ackFloodRepeatDuration,
		},
	}

	sendRequest := func(typeURL, nonce, version string, err error) {
		req := &mcp.MeshConfigRequest{
			SinkNode:      test.Node,
			TypeUrl:       typeURL,
			ResponseNonce: nonce,
			VersionInfo:   version,
		}
		if err != nil {
			errorDetails, _ := status.FromError(err)
			req.ErrorDetail = errorDetails.Proto()
		}
		stream.recv <- req
	}

	// initial watch request
	sendRequest(test.FakeType0Collection, "", "", nil)

	nonces := make(map[string]bool)
	var prevNonce string

	for _, s := range steps {
		numResponses := 0
		finish := time.Now().Add(s.repeatDuration)
		first := true
		for {
			if first {
				first = false

				config.setResponse(&source.WatchResponse{
					Collection: test.FakeType0Collection,
					Version:    s.pushedVersion,
					Resources:  []*mcp.Resource{test.Type0A[0].Resource},
				})
			}

			var response *mcp.MeshConfigResponse
			select {
			case response = <-stream.sent:
				numResponses++
			case <-time.After(time.Second):
				t.Fatalf("%v: timed out waiting for response", s.name)
			}

			if response.VersionInfo != s.pushedVersion {
				t.Fatalf("%v: wrong response version: got %v want %v", s.name, response.VersionInfo, s.pushedVersion)
			}
			if _, ok := nonces[response.Nonce]; ok {
				t.Fatalf("%v: reused externalNonce in response: %v", s.name, response.Nonce)
			}
			nonces[response.Nonce] = true

			prevNonce = response.Nonce

			sendRequest(test.FakeType0Collection, prevNonce, s.ackedVersion, s.errDetails)

			if time.Now().After(finish) {
				break
			}
		}
		if numResponses < s.minResponses || numResponses > s.maxResponses {
			t.Fatalf("%v: wrong number of responses: got %v want[%v,%v]",
				s.name, numResponses, s.minResponses, s.maxResponses)
		}
	}
}
