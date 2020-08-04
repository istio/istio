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
	"io"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/internal/test"
	"istio.io/istio/pkg/mcp/status"
	"istio.io/istio/pkg/mcp/testing/monitoring"
)

type serverHarness struct {
	grpc.ServerStream
	*sourceTestHarness
}

func (*serverHarness) SendHeader(metadata.MD) error {
	return nil
}

// avoid ambiguity between grpc.ServerStream and test.sourceTestHarness
func (h *serverHarness) Context() context.Context {
	return h.sourceTestHarness.Context()
}

func TestServerSinkRateLimitter(t *testing.T) {
	h := &serverHarness{
		sourceTestHarness: newSourceTestHarness(t),
	}

	fakeLimiter := test.NewFakeRateLimiter()
	authChecker := test.NewFakeAuthChecker()
	options := &Options{
		Watcher:            h,
		CollectionsOptions: CollectionOptionsFromSlice(test.SupportedCollections),
		Reporter:           monitoring.NewInMemoryStatsContext(),
		ConnRateLimiter:    test.NewFakePerConnLimiter(),
	}
	s := NewServer(options, &ServerOptions{AuthChecker: authChecker})
	s.rateLimiter = fakeLimiter

	// when rate limit returns an error
	errc := make(chan error)
	go func() {
		errc <- s.EstablishResourceStream(h)
	}()

	expectedErr := "something went wrong while waiting"

	fakeLimiter.WaitErr <- errors.New(expectedErr)

	err := <-errc
	if err == nil || err.Error() != expectedErr {
		t.Fatalf("Expected error from Wait: got %v want %v ", err, expectedErr)
	}
}

func TestServerSource(t *testing.T) {
	h := &serverHarness{
		sourceTestHarness: newSourceTestHarness(t),
	}

	authChecker := test.NewFakeAuthChecker()
	rateLimiter := test.NewFakePerConnLimiter()
	// force ratelimiter Wait to return nil by closing WaitErr
	close(rateLimiter.ErrCh)
	options := &Options{
		Watcher:            h,
		CollectionsOptions: CollectionOptionsFromSlice(test.SupportedCollections),
		Reporter:           monitoring.NewInMemoryStatsContext(),
		ConnRateLimiter:    rateLimiter,
	}

	fakeLimiter := test.NewFakeRateLimiter()
	// force ratelimiter Wait to return nil by closing WaitErr
	close(fakeLimiter.WaitErr)

	srvOptions := &ServerOptions{
		AuthChecker: authChecker,
		RateLimiter: fakeLimiter,
	}
	s := NewServer(options, srvOptions)

	errc := make(chan error)
	go func() {
		errc <- s.EstablishResourceStream(h)
	}()

	h.setRecvError(io.EOF)
	err := <-errc
	if err != nil {
		t.Fatalf("Stream exited with error: got %v", err)
	}

	// ProcessStream error
	wantError := errors.New("unknown")
	h.setRecvError(wantError)
	go func() {
		errc <- s.EstablishResourceStream(h)
	}()
	err = <-errc
	if err != wantError {
		t.Fatalf("Stream exited with error: got %v want %v", err, wantError)
	}

	h.setContext(peer.NewContext(context.Background(), &peer.Peer{
		Addr:     &net.IPAddr{IP: net.IPv4(192, 168, 1, 1)},
		AuthInfo: authChecker,
	}))
	authChecker.AllowError = errors.New("not allowed")
	err = s.EstablishResourceStream(h)
	if s, ok := status.FromError(err); !ok || s.Code() != codes.Unauthenticated {
		t.Fatalf("Connection should have failed: got %v want %v", err, nil)
	}

	authChecker.AllowError = nil

	h.resetStream()
	h.setRecvError(nil)

	go func() {
		errc <- s.EstablishResourceStream(h)
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

	h.requestsChan <- test.MakeRequest(false, test.FakeType0Collection, "", codes.OK)
	h.requestsChan <- test.MakeRequest(false, test.FakeType1Collection, "", codes.OK)
	h.requestsChan <- test.MakeRequest(false, test.FakeType2Collection, "", codes.OK)

	verifySentResourcesMultipleTypes(t, h.sourceTestHarness,
		map[string]*mcp.Resources{
			test.FakeType0Collection: test.MakeResources(false, test.FakeType0Collection, "1", "1", nil, test.Type0A[0]),
			test.FakeType1Collection: test.MakeResources(false, test.FakeType1Collection, "2", "2", nil, test.Type1A[0]),
			test.FakeType2Collection: test.MakeResources(false, test.FakeType2Collection, "3", "3", nil, test.Type2A[0]),
		},
	)

	want := 1
	for _, typeURL := range []string{test.FakeType0Collection, test.FakeType1Collection, test.FakeType2Collection} {
		if got := h.watchesCreated(typeURL); got != want {
			t.Fatalf("Wrong number of %v watches created: got %v want %v", typeURL, got, want)
		}
	}
}
