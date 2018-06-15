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
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2"
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	mcp "istio.io/api/config/mcp/v1alpha1"
)

type mockConfigWatcher struct {
	mu        sync.Mutex
	counts    map[string]int
	responses map[string]*WatchResponse

	watchesCreated map[string]chan struct{}
	watches        map[string][]chan<- *WatchResponse
	closeWatch     bool
}

func (config *mockConfigWatcher) Watch(req *v2.DiscoveryRequest, out chan<- *WatchResponse) (*WatchResponse, CancelWatchFunc) {
	config.mu.Lock()
	defer config.mu.Unlock()

	config.counts[req.TypeUrl] += 1

	if rsp, ok := config.responses[req.TypeUrl]; ok {
		rsp.TypeURL = req.TypeUrl
		return rsp, nil
	} else if config.closeWatch {
		close(out)
	} else {
		// save open watch channel for later
		config.watches[req.TypeUrl] = append(config.watches[req.TypeUrl], out)
	}

	if ch, ok := config.watchesCreated[req.TypeUrl]; ok {
		ch <- struct{}{}
	}

	return nil, func() {}
}

func (config *mockConfigWatcher) setResponse(response *WatchResponse) {
	config.mu.Lock()
	defer config.mu.Unlock()

	typeURL := response.TypeURL

	if watches, ok := config.watches[typeURL]; ok {
		for _, watch := range watches {
			watch <- response
		}
	} else {
		config.responses[typeURL] = response
	}
}

func makeMockConfigWatcher() *mockConfigWatcher {
	return &mockConfigWatcher{
		counts:         make(map[string]int),
		watchesCreated: make(map[string]chan struct{}),
		watches:        make(map[string][]chan<- *WatchResponse),
		responses:      make(map[string]*WatchResponse),
	}
}

type mockStream struct {
	t         *testing.T
	recv      chan *v2.DiscoveryRequest
	sent      chan *v2.DiscoveryResponse
	nonce     int
	sendError bool
	recvError error
	grpc.ServerStream
}

func (stream *mockStream) Send(resp *v2.DiscoveryResponse) error {
	// check that nonce is monotonically incrementing
	stream.nonce = stream.nonce + 1
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
	for _, res := range resp.Resources {
		var envelope mcp.Envelope
		if err := types.UnmarshalAny(&res, &envelope); err != nil {
			stream.t.Errorf("Failed to unmarshal envelope: %v", err)
		}

		got := envelope.Resource.TypeUrl
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

func (stream *mockStream) Recv() (*v2.DiscoveryRequest, error) {
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
		sent: make(chan *v2.DiscoveryResponse, 10),
		recv: make(chan *v2.DiscoveryRequest, 10),
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

	fakeType0TypeUrl = typePrefix + fakeType0Prefix
	fakeType1TypeUrl = typePrefix + fakeType1Prefix
	fakeType2TypeUrl = typePrefix + fakeType2Prefix
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

	fakeResource0 = &mcp.Envelope{
		Metadata: &mcp.Metadata{Name: "f0"},
		Resource: mustMarshalAny(&fakeType0{fakeTypeBase{"f0"}}),
	}
	fakeResource1 = &mcp.Envelope{
		Metadata: &mcp.Metadata{Name: "f1"},
		Resource: mustMarshalAny(&fakeType1{fakeTypeBase{"f1"}}),
	}
	fakeResource2 = &mcp.Envelope{
		Metadata: &mcp.Metadata{Name: "f2"},
		Resource: mustMarshalAny(&fakeType2{fakeTypeBase{"f2"}}),
	}
}

var (
	node = &core.Node{
		Id:      "test-id",
		Cluster: "test-cluster",
	}

	fakeResource0 *mcp.Envelope
	fakeResource1 *mcp.Envelope
	fakeResource2 *mcp.Envelope

	WatchResponseTypes = []string{
		fakeType0TypeUrl,
		fakeType1TypeUrl,
		fakeType2TypeUrl,
	}
)

func TestMultipleRequests(t *testing.T) {
	config := makeMockConfigWatcher()
	config.setResponse(&WatchResponse{
		TypeURL:   fakeType0TypeUrl,
		Version:   "1",
		Resources: []*mcp.Envelope{fakeResource0},
	})

	// make a request
	stream := makeMockStream(t)
	stream.recv <- &v2.DiscoveryRequest{
		Node:    node,
		TypeUrl: fakeType0TypeUrl,
	}

	s := New(config, WatchResponseTypes)
	go func() {
		if err := s.StreamAggregatedResources(stream); err != nil {
			t.Errorf("Stream() => got %v, want no error", err)
		}
	}()

	var rsp *xdsapi.DiscoveryResponse

	// check a response
	select {
	case rsp = <-stream.sent:
		if want := map[string]int{fakeType0TypeUrl: 1}; !reflect.DeepEqual(want, config.counts) {
			t.Errorf("watch counts => got %v, want %v", config.counts, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("got no response")
	}

	stream.recv <- &v2.DiscoveryRequest{
		Node:          node,
		TypeUrl:       fakeType0TypeUrl,
		VersionInfo:   rsp.VersionInfo,
		ResponseNonce: rsp.Nonce,
	}

	// check a response
	select {
	case <-stream.sent:
		if want := map[string]int{fakeType0TypeUrl: 2}; !reflect.DeepEqual(want, config.counts) {
			t.Errorf("watch counts => got %v, want %v", config.counts, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("got no response")
	}
}

func TestWatchBeforeResponsesAvailable(t *testing.T) {
	config := makeMockConfigWatcher()
	config.watchesCreated[fakeType0TypeUrl] = make(chan struct{})

	// make a request
	stream := makeMockStream(t)
	stream.recv <- &v2.DiscoveryRequest{
		Node:    node,
		TypeUrl: fakeType0TypeUrl,
	}

	s := New(config, WatchResponseTypes)
	go func() {
		if err := s.StreamAggregatedResources(stream); err != nil {
			t.Errorf("Stream() => got %v, want no error", err)
		}
	}()

	<-config.watchesCreated[fakeType0TypeUrl]
	config.setResponse(&WatchResponse{
		TypeURL:   fakeType0TypeUrl,
		Version:   "1",
		Resources: []*mcp.Envelope{fakeResource0},
	})

	// check a response
	select {
	case <-stream.sent:
		close(stream.recv)
		if want := map[string]int{fakeType0TypeUrl: 1}; !reflect.DeepEqual(want, config.counts) {
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
	stream.recv <- &v2.DiscoveryRequest{
		Node:    node,
		TypeUrl: fakeType0TypeUrl,
	}

	// check that response fails since watch gets closed
	s := New(config, WatchResponseTypes)
	if err := s.StreamAggregatedResources(stream); err == nil {
		t.Error("Stream() => got no error, want watch failed")
	}

	close(stream.recv)
}

func TestSendError(t *testing.T) {
	config := makeMockConfigWatcher()
	config.setResponse(&WatchResponse{
		TypeURL:   fakeType0TypeUrl,
		Version:   "1",
		Resources: []*mcp.Envelope{fakeResource0},
	})

	// make a request
	stream := makeMockStream(t)
	stream.sendError = true
	stream.recv <- &v2.DiscoveryRequest{
		Node:    node,
		TypeUrl: fakeType0TypeUrl,
	}

	s := New(config, WatchResponseTypes)
	// check that response fails since watch gets closed
	if err := s.StreamAggregatedResources(stream); err == nil {
		t.Error("Stream() => got no error, want send error")
	}

	close(stream.recv)
}

func TestReceiveError(t *testing.T) {
	config := makeMockConfigWatcher()
	config.setResponse(&WatchResponse{
		TypeURL:   fakeType0TypeUrl,
		Version:   "1",
		Resources: []*mcp.Envelope{fakeResource0},
	})

	// make a request
	stream := makeMockStream(t)
	stream.recvError = status.Error(codes.Internal, "internal receive error")
	stream.recv <- &v2.DiscoveryRequest{
		Node:    node,
		TypeUrl: fakeType0TypeUrl,
	}

	// check that response fails since watch gets closed
	s := New(config, WatchResponseTypes)
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
		Resources: []*mcp.Envelope{fakeResource0},
	})

	// make a request
	stream := makeMockStream(t)
	stream.recv <- &v2.DiscoveryRequest{
		Node:    node,
		TypeUrl: "unsupportedtype",
	}

	// check that response fails since watch gets closed
	s := New(config, WatchResponseTypes)
	if err := s.StreamAggregatedResources(stream); err == nil {
		t.Error("Stream() => got no error, want send error")
	}

	close(stream.recv)
}

func TestStaleNonce(t *testing.T) {
	config := makeMockConfigWatcher()
	config.setResponse(&WatchResponse{
		TypeURL:   fakeType0TypeUrl,
		Version:   "1",
		Resources: []*mcp.Envelope{fakeResource0},
	})

	stream := makeMockStream(t)
	stream.recv <- &v2.DiscoveryRequest{
		Node:    node,
		TypeUrl: fakeType0TypeUrl,
	}
	stop := make(chan struct{})
	s := New(config, WatchResponseTypes)
	go func() {
		if err := s.StreamAggregatedResources(stream); err != nil {
			t.Errorf("StreamAggregatedResources() => got %v, want no error", err)
		}
		// should be two watches called
		if want := map[string]int{fakeType0TypeUrl: 2}; !reflect.DeepEqual(want, config.counts) {
			t.Errorf("watch counts => got %v, want %v", config.counts, want)
		}
		close(stop)
	}()
	select {
	case <-stream.sent:
		// stale request
		stream.recv <- &v2.DiscoveryRequest{
			Node:          node,
			TypeUrl:       fakeType0TypeUrl,
			ResponseNonce: "xyz",
		}
		// fresh request
		stream.recv <- &v2.DiscoveryRequest{
			VersionInfo:   "1",
			Node:          node,
			TypeUrl:       fakeType0TypeUrl,
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
		TypeURL:   fakeType0TypeUrl,
		Version:   "1",
		Resources: []*mcp.Envelope{fakeResource0},
	})
	config.setResponse(&WatchResponse{
		TypeURL:   fakeType1TypeUrl,
		Version:   "2",
		Resources: []*mcp.Envelope{fakeResource1},
	})
	config.setResponse(&WatchResponse{
		TypeURL:   fakeType2TypeUrl,
		Version:   "3",
		Resources: []*mcp.Envelope{fakeResource2},
	})

	stream := makeMockStream(t)
	stream.recv <- &v2.DiscoveryRequest{
		Node:    node,
		TypeUrl: fakeType0TypeUrl,
	}
	stream.recv <- &v2.DiscoveryRequest{
		Node:    node,
		TypeUrl: fakeType1TypeUrl,
	}
	stream.recv <- &v2.DiscoveryRequest{
		Node:    node,
		TypeUrl: fakeType2TypeUrl,
	}

	s := New(config, WatchResponseTypes)
	go func() {
		if err := s.StreamAggregatedResources(stream); err != nil {
			t.Errorf("StreamAggregatedResources() => got %v, want no error", err)
		}
	}()

	want := map[string]int{
		fakeType0TypeUrl: 1,
		fakeType1TypeUrl: 1,
		fakeType2TypeUrl: 1,
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
	close(stream.recv)
}

func TestAggregateRequestType(t *testing.T) {
	config := makeMockConfigWatcher()

	stream := makeMockStream(t)
	stream.recv <- &v2.DiscoveryRequest{Node: node}

	s := New(config, WatchResponseTypes)
	if err := s.StreamAggregatedResources(stream); err == nil {
		t.Error("StreamAggregatedResources() => got nil, want an error")
	}
	close(stream.recv)
}
