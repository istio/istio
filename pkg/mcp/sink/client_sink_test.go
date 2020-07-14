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
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/internal/test"
	"istio.io/istio/pkg/mcp/testing/monitoring"
)

type clientHarness struct {
	grpc.ClientStream
	*sinkTestHarness
}

func (h *clientHarness) EstablishResourceStream(ctx context.Context, opts ...grpc.CallOption) (mcp.ResourceSource_EstablishResourceStreamClient, error) {
	return h, h.openError()
}

// avoid ambiguity between grpc.ClientStream and test.sinkTestHarness
func (h *clientHarness) Context() context.Context {
	return h.sinkTestHarness.Context()
}

func TestClientSink(t *testing.T) {
	h := &clientHarness{
		sinkTestHarness: newSinkTestHarness(),
	}
	options := &Options{
		CollectionOptions: CollectionOptionsFromSlice(test.SupportedCollections),
		Updater:           h,
		ID:                test.NodeID,
		Metadata:          test.NodeMetadata,
		Reporter:          monitoring.NewInMemoryStatsContext(),
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
		h.setOpenError(errors.New("done"))
		h.recvErrorChan <- io.EOF
		h.sendErrorChan <- io.EOF
		cancel()
		wg.Wait()
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

	prevDelay := reestablishStreamDelay
	reestablishStreamDelay = 100 * time.Millisecond
	defer func() { reestablishStreamDelay = prevDelay }()

	reconnectChan := make(chan struct{}, 1)
	c.reconnectTestProbe = func() {
		select {
		case reconnectChan <- struct{}{}:
		default:
		}
	}

	h.changes[test.FakeType0Collection] = nil

	// force a disconnect and unsuccessful reconnects
	h.setOpenError(errors.New("fake connection error"))
	h.recvErrorChan <- errors.New("non-EOF error")
	<-reconnectChan

	// allow connection to succeed
	h.setOpenError(nil)
	h.recvErrorChan <- io.EOF
	<-reconnectChan

	want = &Change{
		Collection: test.FakeType0Collection,
		Objects: []*Object{
			{
				TypeURL:  test.FakeType0TypeURL,
				Metadata: test.Type0A[1].Metadata,
				Body:     test.Type0A[1].Proto,
			},
			{
				TypeURL:  test.FakeType0TypeURL,
				Metadata: test.Type0B[1].Metadata,
				Body:     test.Type0B[1].Proto,
			},
		},
	}

	h.resourcesChan <- &mcp.Resources{
		Collection: test.FakeType0Collection,
		Nonce:      "n1",
		Resources: []mcp.Resource{
			*test.Type0A[1].Resource,
			*test.Type0B[1].Resource,
		},
	}

	<-h.changeUpdatedChans

	h.mu.Lock()
	got = h.changes[test.FakeType0Collection]
	h.mu.Unlock()

	if diff := cmp.Diff(got, want); diff != "" {
		t.Fatalf("wrong change on second update: \n got %v \nwant %v \ndiff %v", got, want, diff)
	}
}
