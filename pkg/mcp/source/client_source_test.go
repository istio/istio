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
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
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

func TestClientSource(t *testing.T) {
	h := &sourceHarness{
		sourceTestHarness: newSourceTestHarness(t),
	}

	options := &Options{
		Watcher:     h,
		Collections: test.SupportedCollections,
		Reporter:    monitoring.NewInMemoryStatsContext(),
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

	h.requestsChan <- test.MakeRequest(test.FakeType0Collection, "", codes.OK)
	h.requestsChan <- test.MakeRequest(test.FakeType1Collection, "", codes.OK)
	h.requestsChan <- test.MakeRequest(test.FakeType2Collection, "", codes.OK)

	verifySentResourcesMultipleTypes(t, h.sourceTestHarness,
		map[string]*mcp.Resources{
			test.FakeType0Collection: test.MakeResources(test.FakeType0Collection, "1", "1", nil, test.Type0A[0]),
			test.FakeType1Collection: test.MakeResources(test.FakeType1Collection, "2", "2", nil, test.Type1A[0]),
			test.FakeType2Collection: test.MakeResources(test.FakeType2Collection, "3", "3", nil, test.Type2A[0]),
		},
	)

	want := 1
	for _, typeURL := range []string{test.FakeType0Collection, test.FakeType1Collection, test.FakeType2Collection} {
		if got := h.watchesCreated(typeURL); got != want {
			t.Fatalf("Wrong number of %v watches created: got %v want %v", typeURL, got, want)
		}
	}

	reconnectChan := make(chan struct{}, 10)
	reconnectTestProbe = func() {
		reconnectChan <- struct{}{}
	}
	defer func() { reconnectTestProbe = nil }()

	h.setOpenError(errors.New("fake connection error"))
	h.recvErrorChan <- errRecv
	<-reconnectChan
}
