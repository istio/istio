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

package sink

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"

	"istio.io/istio/pkg/mcp/status"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/internal/test"
	"istio.io/istio/pkg/mcp/testing/monitoring"
)

type serverHarness struct {
	grpc.ServerStream
	*sinkTestHarness
}

// avoid ambiguity between grpc.ServerStream and test.sinkTestHarness
func (h *serverHarness) Context() context.Context {
	return h.sinkTestHarness.Context()
}

func TestServerSinkRateLimitter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type key int
	const ctxKey key = 0
	expectedCtx := "expectedCtx"

	testHarness := newSinkTestHarness()
	testHarness.ctx = context.WithValue(ctx, ctxKey, expectedCtx)

	h := &serverHarness{
		sinkTestHarness: testHarness,
	}

	sinkOptions := &Options{
		CollectionOptions: CollectionOptionsFromSlice(test.SupportedCollections),
		Updater:           h,
		ID:                test.NodeID,
		Metadata:          test.NodeMetadata,
		Reporter:          monitoring.NewInMemoryStatsContext(),
	}

	authChecker := test.NewFakeAuthChecker()
	fakeLimiter := test.NewFakeRateLimiter()

	// force ratelimiter Wait to return nil by closing WaitErr
	close(fakeLimiter.WaitErr)

	serverOpts := &ServerOptions{
		AuthChecker: authChecker,
		RateLimiter: fakeLimiter,
	}

	s := NewServer(sinkOptions, serverOpts)

	go s.EstablishResourceStream(h)
	c := <-fakeLimiter.WaitCh

	ctxVal := c.Value(ctxKey).(string)
	if ctxVal != expectedCtx {
		t.Errorf("Wait received wrong context: got %v want %v", ctxVal, expectedCtx)
	}

	if len(fakeLimiter.WaitCh) != 0 {
		t.Error("Stream() => expected Wait to only get called once")
	}
}

func TestServerSinkRateLimitterError(t *testing.T) {
	testHarness := newSinkTestHarness()

	h := &serverHarness{
		sinkTestHarness: testHarness,
	}

	sinkOptions := &Options{
		CollectionOptions: CollectionOptionsFromSlice(test.SupportedCollections),
		Updater:           h,
		ID:                test.NodeID,
		Metadata:          test.NodeMetadata,
		Reporter:          monitoring.NewInMemoryStatsContext(),
	}

	authChecker := test.NewFakeAuthChecker()
	fakeLimiter := test.NewFakeRateLimiter()

	serverOpts := &ServerOptions{
		AuthChecker: authChecker,
		RateLimiter: fakeLimiter,
	}

	s := NewServer(sinkOptions, serverOpts)

	expectedErr := "rate limiting gone wrong"
	fakeLimiter.WaitErr <- errors.New("rate limiting gone wrong")

	errC := make(chan error)
	go func() {
		errC <- s.EstablishResourceStream(h)
	}()

	err := <-errC
	if err == nil || err.Error() != expectedErr {
		t.Fatalf("Expected error from Wait: got %v want %v ", err, expectedErr)
	}
}
func TestServerSink(t *testing.T) {
	h := &serverHarness{
		sinkTestHarness: newSinkTestHarness(),
	}

	authChecker := test.NewFakeAuthChecker()
	sinkOptions := &Options{
		CollectionOptions: CollectionOptionsFromSlice(test.SupportedCollections),
		Updater:           h,
		ID:                test.NodeID,
		Metadata:          test.NodeMetadata,
		Reporter:          monitoring.NewInMemoryStatsContext(),
	}

	rateLimiter := test.NewFakeRateLimiter()
	// force ratelimiter Wait to return nil by closing WaitErr
	close(rateLimiter.WaitErr)

	serverOpts := &ServerOptions{
		AuthChecker: authChecker,
		RateLimiter: rateLimiter,
	}
	s := NewServer(sinkOptions, serverOpts)

	errc := make(chan error)
	go func() {
		errc <- s.EstablishResourceStream(h)
	}()

	want := &Change{
		Collection: test.FakeType0Collection,
		Objects: []*Object{
			{
				TypeURL:  test.FakeType0TypeURL,
				Metadata: test.Type0A[0].Metadata,
				Body:     test.Type0A[0].Proto,
			},
			{
				TypeURL:  test.FakeType0TypeURL,
				Metadata: test.Type0B[0].Metadata,
				Body:     test.Type0B[0].Proto,
			},
		},
	}

	h.resourcesChan <- &mcp.Resources{
		Collection: test.FakeType0Collection,
		Nonce:      "n0",
		Resources: []mcp.Resource{
			*test.Type0A[0].Resource,
			*test.Type0B[0].Resource,
		},
	}

	<-h.changeUpdatedChans
	h.mu.Lock()
	got := h.changes[test.FakeType0Collection]
	h.mu.Unlock()
	if diff := cmp.Diff(got, want); diff != "" {
		t.Fatalf("wrong change on first update: \n got %v \nwant %v \ndiff %v", got, want, diff)
	}

	h.recvErrorChan <- io.EOF
	err := <-errc
	if err != nil {
		t.Fatalf("Stream exited with error: got %v", err)
	}

	h.sinkTestHarness = newSinkTestHarness()

	// multiple connections
	go func() {
		errc <- s.EstablishResourceStream(h)
	}()

	// wait for first connection to succeed and change to be applied
	h.resourcesChan <- &mcp.Resources{
		Collection: test.FakeType0Collection,
		Nonce:      "n0",
		Resources: []mcp.Resource{
			*test.Type0A[0].Resource,
		},
	}

	<-h.changeUpdatedChans

	h.setContext(peer.NewContext(context.Background(), &peer.Peer{
		Addr:     &net.IPAddr{IP: net.IPv4(192, 168, 1, 1)},
		AuthInfo: authChecker,
	}))

	// error disconnect
	h.recvErrorChan <- errors.New("unknown error")
	err = <-errc
	if err == nil {
		t.Fatal("Stream exited without error")
	}

	authChecker.AllowError = errors.New("not allowed")

	err = s.EstablishResourceStream(h)
	if s, ok := status.FromError(err); !ok || s.Code() != codes.Unauthenticated {
		t.Fatalf("Connection should have failed: got %v want %v", err, nil)
	}
}
