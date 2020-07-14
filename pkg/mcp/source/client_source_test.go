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
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/internal/test"
	"istio.io/istio/pkg/mcp/testing/monitoring"
)

type sourceHarness struct {
	grpc.ClientStream
	*sourceTestHarness
}

func (h *sourceHarness) EstablishResourceStream(ctx context.Context, opts ...grpc.CallOption) (mcp.ResourceSink_EstablishResourceStreamClient, error) {
	return h, h.openError()
}

// avoid ambiguity between grpc.ServerStream and test.sourceTestHarness
func (h *sourceHarness) Context() context.Context {
	return h.sourceTestHarness.Context()
}

func TestClientSource(t *testing.T) {
	h := &sourceHarness{
		sourceTestHarness: newSourceTestHarness(t),
	}
	h.client = true
	fakeLimiter := test.NewFakePerConnLimiter()
	close(fakeLimiter.ErrCh)
	options := &Options{
		Watcher:            h,
		CollectionsOptions: CollectionOptionsFromSlice(test.SupportedCollections),
		Reporter:           monitoring.NewInMemoryStatsContext(),
		ConnRateLimiter:    fakeLimiter,
	}
	c := NewClient(h, options)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		c.Run(ctx)
		wg.Done()
	}()
	defer func() {
		cancel()
		wg.Wait()
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

	h.requestsChan <- test.MakeRequest(false, triggerCollection, "", codes.Unimplemented)
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

	waiting := make(chan bool)
	proceed := make(chan bool)
	reconnectTestProbe = func() {
		waiting <- true
		<-proceed
	}
	defer func() { reconnectTestProbe = nil }()

	h.setOpenError(errors.New("fake connection error"))
	h.recvErrorChan <- errRecv
	<-waiting
	proceed <- true

	<-waiting
	h.setOpenError(nil)
	h.client = true
	h.requestsChan <- test.MakeRequest(false, "", "", codes.OK)
	proceed <- true

	<-waiting
	h.setRecvError(errors.New("fake recv error"))
	h.client = true
	proceed <- true
}
