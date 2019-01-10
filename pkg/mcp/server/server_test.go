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

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/gogo/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"

	mcp "istio.io/api/mcp/v1alpha1"
	mcptestmon "istio.io/istio/pkg/mcp/testing/monitoring"
)

type mockConfigWatcher struct {
	mu        sync.Mutex
	counts    map[string]int
	responses map[string]*WatchResponse

	watchesCreated map[string]chan struct{}
	watches        map[string][]PushResponseFunc
	closeWatch     bool
}

func (config *mockConfigWatcher) Watch(req *mcp.MeshConfigRequest, pushResponse PushResponseFunc) CancelWatchFunc {
	config.mu.Lock()
	defer config.mu.Unlock()

	config.counts[req.TypeUrl]++

	if rsp, ok := config.responses[req.TypeUrl]; ok {
		rsp.TypeURL = req.TypeUrl
		pushResponse(rsp)
		return nil
	} else if config.closeWatch {
		pushResponse(nil)
		return nil
	} else {
		// save open watch channel for later
		config.watches[req.TypeUrl] = append(config.watches[req.TypeUrl], pushResponse)
	}

	if ch, ok := config.watchesCreated[req.TypeUrl]; ok {
		ch <- struct{}{}
	}

	return func() {}
}

func (config *mockConfigWatcher) setResponse(response *WatchResponse) {
	config.mu.Lock()
	defer config.mu.Unlock()

	typeURL := response.TypeURL

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
		watches:        make(map[string][]PushResponseFunc),
		responses:      make(map[string]*WatchResponse),
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
	// check that nonce is monotonically incrementing
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
	for _, resource := range resp.Resources {
		got := resource.Body.TypeUrl
		if got != resp.TypeUrl {
			stream.t.Errorf("TypeUrl => got %q, want %q", got, resp.TypeUrl)
		}
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

// fake protobuf types

type fakeTypeBase struct{ Info string }

func (f fakeTypeBase) Reset()                   {}
func (f fakeTypeBase) String() string           { return f.Info }
func (f fakeTypeBase) ProtoMessage()            {}
func (f fakeTypeBase) Marshal() ([]byte, error) { return []byte(f.Info), nil }

type fakeType0 struct{ fakeTypeBase }
type fakeType1 struct{ fakeTypeBase }
type fakeType2 struct{ fakeTypeBase }

const (
	typePrefix      = "type.googleapis.com/"
	fakeType0Prefix = "istio.io.galley.pkg.mcp.server.fakeType0"
	fakeType1Prefix = "istio.io.galley.pkg.mcp.server.fakeType1"
	fakeType2Prefix = "istio.io.galley.pkg.mcp.server.fakeType2"

	fakeType0TypeURL = typePrefix + fakeType0Prefix
	fakeType1TypeURL = typePrefix + fakeType1Prefix
	fakeType2TypeURL = typePrefix + fakeType2Prefix
)

func mustMarshalAny(pb proto.Message) *types.Any {
	a, err := types.MarshalAny(pb)
	if err != nil {
		panic(err.Error())
	}
	return a
}

func init() {
	proto.RegisterType((*fakeType0)(nil), fakeType0Prefix)
	proto.RegisterType((*fakeType1)(nil), fakeType1Prefix)
	proto.RegisterType((*fakeType2)(nil), fakeType2Prefix)

	fakeResource0 = &mcp.Resource{
		Metadata: &mcp.Metadata{Name: "f0"},
		Body:     mustMarshalAny(&fakeType0{fakeTypeBase{"f0"}}),
	}
	fakeResource1 = &mcp.Resource{
		Metadata: &mcp.Metadata{Name: "f1"},
		Body:     mustMarshalAny(&fakeType1{fakeTypeBase{"f1"}}),
	}
	fakeResource2 = &mcp.Resource{
		Metadata: &mcp.Metadata{Name: "f2"},
		Body:     mustMarshalAny(&fakeType2{fakeTypeBase{"f2"}}),
	}
}

var (
	node = &mcp.SinkNode{
		Id: "test-id",
	}

	fakeResource0 *mcp.Resource
	fakeResource1 *mcp.Resource
	fakeResource2 *mcp.Resource

	WatchResponseTypes = []string{
		fakeType0TypeURL,
		fakeType1TypeURL,
		fakeType2TypeURL,
	}
)

func TestMultipleRequests(t *testing.T) {
	config := makeMockConfigWatcher()
	config.setResponse(&WatchResponse{
		TypeURL:   fakeType0TypeURL,
		Version:   "1",
		Resources: []*mcp.Resource{fakeResource0},
	})

	// make a request
	stream := makeMockStream(t)
	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode: node,
		TypeUrl:  fakeType0TypeURL,
	}

	s := New(config, WatchResponseTypes, NewAllowAllChecker(), mcptestmon.NewInMemoryServerStatsContext())
	go func() {
		if err := s.StreamAggregatedResources(stream); err != nil {
			t.Errorf("Stream() => got %v, want no error", err)
		}
	}()

	var rsp *mcp.MeshConfigResponse

	// check a response
	select {
	case rsp = <-stream.sent:
		if want := map[string]int{fakeType0TypeURL: 1}; !reflect.DeepEqual(want, config.counts) {
			t.Errorf("watch counts => got %v, want %v", config.counts, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("got no response")
	}

	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode:      node,
		TypeUrl:       fakeType0TypeURL,
		VersionInfo:   rsp.VersionInfo,
		ResponseNonce: rsp.Nonce,
	}

	// check a response
	select {
	case <-stream.sent:
		if want := map[string]int{fakeType0TypeURL: 2}; !reflect.DeepEqual(want, config.counts) {
			t.Errorf("watch counts => got %v, want %v", config.counts, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("got no response")
	}
}

func TestAuthCheck_Failure(t *testing.T) {
	config := makeMockConfigWatcher()
	config.setResponse(&WatchResponse{
		TypeURL:   fakeType0TypeURL,
		Version:   "1",
		Resources: []*mcp.Resource{fakeResource0},
	})

	// make a request
	stream := makeMockStream(t)
	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode: node,
		TypeUrl:  fakeType0TypeURL,
	}

	checker := NewListAuthChecker()
	s := New(config, WatchResponseTypes, checker, mcptestmon.NewInMemoryServerStatsContext())
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

type fakeAuthChecker struct{}

func (f *fakeAuthChecker) Check(authInfo credentials.AuthInfo) error {
	return nil
}

func TestAuthCheck_Success(t *testing.T) {
	config := makeMockConfigWatcher()
	config.setResponse(&WatchResponse{
		TypeURL:   fakeType0TypeURL,
		Version:   "1",
		Resources: []*mcp.Resource{fakeResource0},
	})

	// make a request
	stream := makeMockStream(t)
	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode: node,
		TypeUrl:  fakeType0TypeURL,
	}

	s := New(config, WatchResponseTypes, &fakeAuthChecker{}, mcptestmon.NewInMemoryServerStatsContext())
	go func() {
		if err := s.StreamAggregatedResources(stream); err != nil {
			t.Errorf("Stream() => got %v, want no error", err)
		}
	}()

	var rsp *mcp.MeshConfigResponse

	// check a response
	select {
	case rsp = <-stream.sent:
		if want := map[string]int{fakeType0TypeURL: 1}; !reflect.DeepEqual(want, config.counts) {
			t.Errorf("watch counts => got %v, want %v", config.counts, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("got no response")
	}

	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode:      node,
		TypeUrl:       fakeType0TypeURL,
		VersionInfo:   rsp.VersionInfo,
		ResponseNonce: rsp.Nonce,
	}

	// check a response
	select {
	case <-stream.sent:
		if want := map[string]int{fakeType0TypeURL: 2}; !reflect.DeepEqual(want, config.counts) {
			t.Errorf("watch counts => got %v, want %v", config.counts, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("got no response")
	}
}

func TestWatchBeforeResponsesAvailable(t *testing.T) {
	config := makeMockConfigWatcher()
	config.watchesCreated[fakeType0TypeURL] = make(chan struct{})

	// make a request
	stream := makeMockStream(t)
	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode: node,
		TypeUrl:  fakeType0TypeURL,
	}

	s := New(config, WatchResponseTypes, NewAllowAllChecker(), mcptestmon.NewInMemoryServerStatsContext())
	go func() {
		if err := s.StreamAggregatedResources(stream); err != nil {
			t.Errorf("Stream() => got %v, want no error", err)
		}
	}()

	<-config.watchesCreated[fakeType0TypeURL]
	config.setResponse(&WatchResponse{
		TypeURL:   fakeType0TypeURL,
		Version:   "1",
		Resources: []*mcp.Resource{fakeResource0},
	})

	// check a response
	select {
	case <-stream.sent:
		close(stream.recv)
		if want := map[string]int{fakeType0TypeURL: 1}; !reflect.DeepEqual(want, config.counts) {
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
		SinkNode: node,
		TypeUrl:  fakeType0TypeURL,
	}

	// check that response fails since watch gets closed
	s := New(config, WatchResponseTypes, NewAllowAllChecker(), mcptestmon.NewInMemoryServerStatsContext())
	if err := s.StreamAggregatedResources(stream); err == nil {
		t.Error("Stream() => got no error, want watch failed")
	}
	close(stream.recv)
}

func TestSendError(t *testing.T) {
	config := makeMockConfigWatcher()
	config.setResponse(&WatchResponse{
		TypeURL:   fakeType0TypeURL,
		Version:   "1",
		Resources: []*mcp.Resource{fakeResource0},
	})

	// make a request
	stream := makeMockStream(t)
	stream.sendError = true
	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode: node,
		TypeUrl:  fakeType0TypeURL,
	}

	s := New(config, WatchResponseTypes, NewAllowAllChecker(), mcptestmon.NewInMemoryServerStatsContext())
	// check that response fails since watch gets closed
	if err := s.StreamAggregatedResources(stream); err == nil {
		t.Error("Stream() => got no error, want send error")
	}

	close(stream.recv)
}

func TestReceiveError(t *testing.T) {
	config := makeMockConfigWatcher()
	config.setResponse(&WatchResponse{
		TypeURL:   fakeType0TypeURL,
		Version:   "1",
		Resources: []*mcp.Resource{fakeResource0},
	})

	// make a request
	stream := makeMockStream(t)
	stream.recvError = status.Error(codes.Internal, "internal receive error")
	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode: node,
		TypeUrl:  fakeType0TypeURL,
	}

	// check that response fails since watch gets closed
	s := New(config, WatchResponseTypes, NewAllowAllChecker(), mcptestmon.NewInMemoryServerStatsContext())
	if err := s.StreamAggregatedResources(stream); err == nil {
		t.Error("Stream() => got no error, want send error")
	}

	close(stream.recv)
}

func TestUnsupportedTypeError(t *testing.T) {
	config := makeMockConfigWatcher()
	config.setResponse(&WatchResponse{
		TypeURL:   "unsupportedType",
		Version:   "1",
		Resources: []*mcp.Resource{fakeResource0},
	})

	// make a request
	stream := makeMockStream(t)
	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode: node,
		TypeUrl:  "unsupportedtype",
	}

	// check that response fails since watch gets closed
	s := New(config, WatchResponseTypes, NewAllowAllChecker(), mcptestmon.NewInMemoryServerStatsContext())
	if err := s.StreamAggregatedResources(stream); err == nil {
		t.Error("Stream() => got no error, want send error")
	}

	close(stream.recv)
}

func TestStaleNonce(t *testing.T) {
	config := makeMockConfigWatcher()
	config.setResponse(&WatchResponse{
		TypeURL:   fakeType0TypeURL,
		Version:   "1",
		Resources: []*mcp.Resource{fakeResource0},
	})

	stream := makeMockStream(t)
	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode: node,
		TypeUrl:  fakeType0TypeURL,
	}
	stop := make(chan struct{})
	s := New(config, WatchResponseTypes, NewAllowAllChecker(), mcptestmon.NewInMemoryServerStatsContext())
	go func() {
		if err := s.StreamAggregatedResources(stream); err != nil {
			t.Errorf("StreamAggregatedResources() => got %v, want no error", err)
		}
		// should be two watches called
		if want := map[string]int{fakeType0TypeURL: 2}; !reflect.DeepEqual(want, config.counts) {
			t.Errorf("watch counts => got %v, want %v", config.counts, want)
		}
		close(stop)
	}()
	select {
	case <-stream.sent:
		// stale request
		stream.recv <- &mcp.MeshConfigRequest{
			SinkNode:      node,
			TypeUrl:       fakeType0TypeURL,
			ResponseNonce: "xyz",
		}
		// fresh request
		stream.recv <- &mcp.MeshConfigRequest{
			VersionInfo:   "1",
			SinkNode:      node,
			TypeUrl:       fakeType0TypeURL,
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
	config.setResponse(&WatchResponse{
		TypeURL:   fakeType0TypeURL,
		Version:   "1",
		Resources: []*mcp.Resource{fakeResource0},
	})
	config.setResponse(&WatchResponse{
		TypeURL:   fakeType1TypeURL,
		Version:   "2",
		Resources: []*mcp.Resource{fakeResource1},
	})
	config.setResponse(&WatchResponse{
		TypeURL:   fakeType2TypeURL,
		Version:   "3",
		Resources: []*mcp.Resource{fakeResource2},
	})

	stream := makeMockStream(t)
	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode: node,
		TypeUrl:  fakeType0TypeURL,
	}
	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode: node,
		TypeUrl:  fakeType1TypeURL,
	}
	stream.recv <- &mcp.MeshConfigRequest{
		SinkNode: node,
		TypeUrl:  fakeType2TypeURL,
	}

	s := New(config, WatchResponseTypes, NewAllowAllChecker(), mcptestmon.NewInMemoryServerStatsContext())
	go func() {
		if err := s.StreamAggregatedResources(stream); err != nil {
			t.Errorf("StreamAggregatedResources() => got %v, want no error", err)
		}
	}()

	want := map[string]int{
		fakeType0TypeURL: 1,
		fakeType1TypeURL: 1,
		fakeType2TypeURL: 1,
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
	stream.recv <- &mcp.MeshConfigRequest{SinkNode: node}

	s := New(config, WatchResponseTypes, NewAllowAllChecker(), mcptestmon.NewInMemoryServerStatsContext())
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
	s := New(config, WatchResponseTypes, NewAllowAllChecker(), mcptestmon.NewInMemoryServerStatsContext())

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
			SinkNode:      node,
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
	sendRequest(fakeType0TypeURL, "", "", nil)

	nonces := make(map[string]bool)
	var prevNonce string

	for _, s := range steps {
		numResponses := 0
		finish := time.Now().Add(s.repeatDuration)
		first := true
		for {
			if first {
				first = false
				config.setResponse(&WatchResponse{
					TypeURL:   fakeType0TypeURL,
					Version:   s.pushedVersion,
					Resources: []*mcp.Resource{fakeResource0},
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
				t.Fatalf("%v: reused nonce in response: %v", s.name, response.Nonce)
			}
			nonces[response.Nonce] = true

			prevNonce = response.Nonce

			sendRequest(fakeType0TypeURL, prevNonce, s.ackedVersion, s.errDetails)

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
