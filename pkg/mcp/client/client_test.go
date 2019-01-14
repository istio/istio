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

package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"istio.io/istio/pkg/mcp/internal/test"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/gogo/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	mcp "istio.io/api/mcp/v1alpha1"
	mcptestmon "istio.io/istio/pkg/mcp/testing/monitoring"
)

type testStream struct {
	sync.Mutex
	change map[string]*Change

	requestC        chan *mcp.MeshConfigRequest  // received from client
	responseC       chan *mcp.MeshConfigResponse // to-be-sent to client
	responseClosedC chan struct{}

	updateError bool
	sendError   int32
	recvError   int32

	grpc.ClientStream
}

func newTestStream() *testStream {
	return &testStream{
		requestC:        make(chan *mcp.MeshConfigRequest, 10),
		responseC:       make(chan *mcp.MeshConfigResponse, 10),
		responseClosedC: make(chan struct{}, 10),
		change:          make(map[string]*Change),
	}
}

func (ts *testStream) wantRequest(want *mcp.MeshConfigRequest) error {
	select {
	case got := <-ts.requestC:
		got = proto.Clone(got).(*mcp.MeshConfigRequest)
		return checkRequest(got, want)
	case <-time.After(time.Second):
		return fmt.Errorf("no request received")
	}
}

func (ts *testStream) sendResponseToClient(response *mcp.MeshConfigResponse) {
	if atomic.CompareAndSwapInt32(&ts.recvError, 1, 0) {
		ts.responseClosedC <- struct{}{}
	} else {
		ts.responseC <- response
	}
}

func (ts *testStream) IncrementalAggregatedResources(ctx context.Context, opts ...grpc.CallOption) (mcp.AggregatedMeshConfigService_IncrementalAggregatedResourcesClient, error) { // nolint: lll
	return nil, status.Errorf(codes.Unimplemented, "not implemented")
}

func (ts *testStream) StreamAggregatedResources(ctx context.Context, opts ...grpc.CallOption) (mcp.AggregatedMeshConfigService_StreamAggregatedResourcesClient, error) { // nolint: lll
	go func() {
		<-ctx.Done()
		ts.responseClosedC <- struct{}{}
	}()
	return ts, nil
}

func (ts *testStream) Send(request *mcp.MeshConfigRequest) error {
	if atomic.CompareAndSwapInt32(&ts.sendError, 1, 0) {
		return errors.New("send error")
	}
	select {
	case <-ts.responseClosedC:
		return errors.New("send error")
	case ts.requestC <- request:
		return nil
	}
}

func (ts *testStream) Recv() (*mcp.MeshConfigResponse, error) {
	if atomic.CompareAndSwapInt32(&ts.recvError, 1, 0) {
		return nil, errors.New("recv error")
	}
	select {
	case response, more := <-ts.responseC:
		if !more {
			return nil, io.EOF
		}
		return response, nil
	case <-ts.responseClosedC:
		return nil, io.EOF
	}
}

func (ts *testStream) Apply(change *Change) error {
	if ts.updateError {
		return errors.New("update error")
	}
	ts.Lock()
	defer ts.Unlock()
	ts.change[change.Collection] = change
	return nil
}

func checkRequest(got *mcp.MeshConfigRequest, want *mcp.MeshConfigRequest) error {
	// verify the presence of errorDetails and the error code. Ignore everything else.
	if got.ErrorDetail != nil {
		got.ErrorDetail.Message = ""
		got.ErrorDetail.Details = nil
	}
	if !reflect.DeepEqual(got, want) {
		return fmt.Errorf("bad request\n got %v \nwant %v", got, want)
	}

	return nil
}

var _ Updater = &testStream{}

var (
	key      = "node-id"
	metadata = map[string]string{"foo": "bar"}
	client   *mcp.SinkNode

	supportedCollections = []string{
		test.FakeType0Collection,
		test.FakeType1Collection,
		test.FakeType2Collection,
	}
)

func init() {
	client = &mcp.SinkNode{
		Id:          key,
		Annotations: map[string]string{},
	}
	for k, v := range metadata {
		client.Annotations[k] = v
	}
}

func makeRequest(collection, version, nonce string, errorCode codes.Code) *mcp.MeshConfigRequest {
	req := &mcp.MeshConfigRequest{
		SinkNode:      client,
		TypeUrl:       collection,
		VersionInfo:   version,
		ResponseNonce: nonce,
	}
	if errorCode != codes.OK {
		req.ErrorDetail = status.New(errorCode, "").Proto()
	}
	return req
}

func makeResponse(collection, version, nonce string, resources ...*mcp.Resource) *mcp.MeshConfigResponse {
	r := &mcp.MeshConfigResponse{
		TypeUrl:     collection,
		VersionInfo: version,
		Nonce:       nonce,
	}
	for _, resource := range resources {
		r.Resources = append(r.Resources, *resource)
	}
	return r
}

func TestSingleTypeCases(t *testing.T) {
	ts := newTestStream()

	c := New(ts, supportedCollections, ts, key, metadata, mcptestmon.NewInMemoryClientStatsContext())
	ctx, cancelClient := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		c.Run(ctx)
		wg.Done()
	}()

	defer func() {
		cancelClient()
		wg.Wait()
	}()

	// Check metadata fields first
	if !reflect.DeepEqual(c.Metadata(), metadata) {
		t.Fatalf("metadata mismatch: got:\n%v\nwanted:\n%v\n", c.metadata, metadata)
	}

	if c.ID() != key {
		t.Fatalf("id mismatch: got\n%v\nwanted:\n%v\n", c.ID(), key)
	}

	if !reflect.DeepEqual(c.Collections(), supportedCollections) {
		t.Fatalf("type url mismatch: got:\n%v\nwanted:\n%v\n", c.Collections(), supportedCollections)
	}

	wantInitial := make(map[string]*mcp.MeshConfigRequest)
	for _, collection := range supportedCollections {
		wantInitial[collection] = makeRequest(collection, "", "", codes.OK)
	}
	gotInitial := make(map[string]*mcp.MeshConfigRequest)
	for i := 0; i < len(supportedCollections); i++ {
		select {
		case got := <-ts.requestC:
			gotInitial[got.TypeUrl] = got
		case <-time.After(time.Second):
			t.Fatalf("no initial request received: got %v of %v", len(gotInitial), len(wantInitial))
		}
	}
	if !reflect.DeepEqual(gotInitial, wantInitial) {
		t.Fatalf("bad initial requests\n got %v \nwant %v", gotInitial, wantInitial)
	}

	steps := []struct {
		name         string
		sendResponse *mcp.MeshConfigResponse
		wantRequest  *mcp.MeshConfigRequest
		wantChange   *Change
		wantJournal  []RecentRequestInfo
		updateError  bool
	}{
		{
			name:         "ACK request (type0)",
			sendResponse: makeResponse(test.FakeType0Collection, "type0/v0", "type0/n0", test.Type0A[0].Resource),
			wantRequest:  makeRequest(test.FakeType0Collection, "type0/v0", "type0/n0", codes.OK),
			wantChange: &Change{
				Collection: test.FakeType0Collection,
				Objects: []*Object{{
					TypeURL:  test.FakeType0TypeURL,
					Metadata: test.Type0A[0].Metadata,
					Body:     test.Type0A[0].Proto,
				}},
			},
		},
		{
			name:         "ACK request (type1)",
			sendResponse: makeResponse(test.FakeType1Collection, "type1/v0", "type1/n0", test.Type1A[0].Resource),
			wantRequest:  makeRequest(test.FakeType1Collection, "type1/v0", "type1/n0", codes.OK),
			wantChange: &Change{
				Collection: test.FakeType1Collection,
				Objects: []*Object{{
					TypeURL:  test.FakeType1TypeURL,
					Metadata: test.Type1A[0].Metadata,
					Body:     test.Type1A[0].Proto,
				}},
			},
		},
		{
			name:         "ACK request (type2)",
			sendResponse: makeResponse(test.FakeType2Collection, "type2/v0", "type2/n0", test.Type2A[0].Resource),
			wantRequest:  makeRequest(test.FakeType2Collection, "type2/v0", "type2/n0", codes.OK),
			wantChange: &Change{
				Collection: test.FakeType2Collection,
				Objects: []*Object{{
					TypeURL:  test.FakeType2TypeURL,
					Metadata: test.Type2A[0].Metadata,
					Body:     test.Type2A[0].Proto,
				}},
			},
		},
		{
			name:         "NACK request (unsupported type_url)",
			sendResponse: makeResponse(test.FakeType0Collection+"Garbage", "type0/v1", "type0/n1", test.Type0A[0].Resource),
			wantRequest:  makeRequest(test.FakeType0Collection+"Garbage", "", "type0/n1", codes.Unimplemented),
			wantChange: &Change{
				Collection: test.FakeType0Collection,
				Objects: []*Object{{
					TypeURL:  test.FakeType0TypeURL,
					Metadata: test.Type0A[0].Metadata,
					Body:     test.Type0A[0].Proto,
				}},
			},
		},
		{
			name:         "NACK request (unmarshal error)",
			sendResponse: makeResponse(test.FakeType0Collection, "type0/v1", "type0/n2", test.BadUnmarshal.Resource),
			wantRequest:  makeRequest(test.FakeType0Collection, "type0/v0", "type0/n2", codes.Unknown),
			wantChange: &Change{
				Collection: test.FakeType0Collection,
				Objects: []*Object{{
					TypeURL:  test.FakeType0TypeURL,
					Metadata: test.Type0A[0].Metadata,
					Body:     test.Type0A[0].Proto,
				}},
			},
		},
		{
			name:         "NACK request (client updater rejected changes)",
			updateError:  true,
			sendResponse: makeResponse(test.FakeType0Collection, "type0/v1", "type0/n3", test.Type0A[0].Resource),
			wantRequest:  makeRequest(test.FakeType0Collection, "type0/v0", "type0/n3", codes.InvalidArgument),
			wantChange: &Change{
				Collection: test.FakeType0Collection,
				Objects: []*Object{{
					TypeURL:  test.FakeType0TypeURL,
					Metadata: test.Type0A[0].Metadata,
					Body:     test.Type0A[0].Proto,
				}},
			},
		},
		{
			name:         "ACK request after previous NACKs",
			sendResponse: makeResponse(test.FakeType0Collection, "type0/v1", "type0/n3", test.Type0A[1].Resource, test.Type0A[2].Resource),
			wantRequest:  makeRequest(test.FakeType0Collection, "type0/v1", "type0/n3", codes.OK),
			wantChange: &Change{
				Collection: test.FakeType0Collection,
				Objects: []*Object{{
					TypeURL:  test.FakeType0TypeURL,
					Metadata: test.Type0A[1].Metadata,
					Body:     test.Type0A[1].Proto,
				}, {
					TypeURL:  test.FakeType0TypeURL,
					Metadata: test.Type0A[2].Metadata,
					Body:     test.Type0A[2].Proto,
				}},
			},
			wantJournal: nil,
		},
	}

	// install probe to monitor when the client is finished handling responses
	responseDone := make(chan struct{})
	handleResponseDoneProbe = func() { responseDone <- struct{}{} }
	defer func() { handleResponseDoneProbe = nil }()

	for _, step := range steps {
		ts.updateError = step.updateError

		ts.sendResponseToClient(step.sendResponse)
		<-responseDone
		if diff := cmp.Diff(ts.change[step.wantChange.Collection], step.wantChange); diff != "" {
			t.Fatalf("%v: bad client change: \n got %#v \nwant %#v\n diff %v",
				step.name, ts.change[step.wantChange.Collection], step.wantChange, diff)
		}

		if err := ts.wantRequest(step.wantRequest); err != nil {
			t.Fatalf("%v: failed to receive correct request: %v", step.name, err)
		}

		entries := c.SnapshotRequestInfo()
		if len(entries) == 0 {
			t.Fatal("No journal entries not found.")
		}

		lastEntry := entries[len(entries)-1]
		if err := checkRequest(lastEntry.Request.toMeshConfigRequest(), step.wantRequest); err != nil {
			t.Fatalf("%v: failed to publish the right journal entries: %v", step.name, err)
		}
	}
}

func TestReconnect(t *testing.T) {
	ts := newTestStream()

	c := New(ts, []string{test.FakeType0Collection}, ts, key, metadata, mcptestmon.NewInMemoryClientStatsContext())
	ctx, cancelClient := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		c.Run(ctx)
		wg.Done()
	}()

	defer func() {
		cancelClient()
		wg.Wait()
	}()

	steps := []struct {
		name         string
		sendResponse *mcp.MeshConfigResponse
		wantRequest  *mcp.MeshConfigRequest
		wantChange   *Change
		sendError    bool
		recvError    bool
	}{
		{
			name:         "Initial request (type0)",
			sendResponse: nil, // client initiates the exchange
			wantRequest:  makeRequest(test.FakeType0Collection, "", "", codes.OK),
			wantChange: &Change{
				Collection: test.FakeType0Collection,
				Objects: []*Object{{
					TypeURL:  test.FakeType0TypeURL,
					Metadata: test.Type0A[0].Metadata,
					Body:     test.Type0A[0].Proto,
				}},
			},
		},
		{
			name:         "ACK request (type0)",
			sendResponse: makeResponse(test.FakeType0Collection, "type0/v0", "type0/n0", test.Type0A[0].Resource),
			wantRequest:  makeRequest(test.FakeType0Collection, "type0/v0", "type0/n0", codes.OK),
			wantChange: &Change{
				Collection: test.FakeType0Collection,
				Objects: []*Object{{
					TypeURL:  test.FakeType0TypeURL,
					Metadata: test.Type0A[0].Metadata,
					Body:     test.Type0A[0].Proto,
				}},
			},
		},
		{
			name:         "send error",
			sendResponse: makeResponse(test.FakeType0Collection, "type0/v1", "type0/n1", test.Type0A[1].Resource),
			wantRequest:  makeRequest(test.FakeType0Collection, "", "", codes.OK),
			wantChange: &Change{
				Collection: test.FakeType0Collection,
				Objects: []*Object{{
					TypeURL:  test.FakeType0TypeURL,
					Metadata: test.Type0A[0].Metadata,
					Body:     test.Type0A[0].Proto,
				}},
			},
			sendError: true,
		},
		{
			name:         "ACK request after reconnect on send error",
			sendResponse: makeResponse(test.FakeType0Collection, "type0/v1", "type0/n1", test.Type0A[1].Resource),
			wantRequest:  makeRequest(test.FakeType0Collection, "type0/v1", "type0/n1", codes.OK),
			wantChange: &Change{
				Collection: test.FakeType0Collection,
				Objects: []*Object{{
					TypeURL:  test.FakeType0TypeURL,
					Metadata: test.Type0A[1].Metadata,
					Body:     test.Type0A[1].Proto,
				}},
			},
		},
		{
			name:         "recv error",
			sendResponse: makeResponse(test.FakeType0Collection, "type0/v2", "type0/n2", test.Type0A[2].Resource),
			wantRequest:  makeRequest(test.FakeType0Collection, "", "", codes.OK),
			wantChange: &Change{
				Collection: test.FakeType0Collection,
				Objects: []*Object{{
					TypeURL:  test.FakeType0TypeURL,
					Metadata: test.Type0A[1].Metadata,
					Body:     test.Type0A[1].Proto,
				}},
			},
			recvError: true,
		},
		{
			name:         "ACK request after reconnect on recv error",
			sendResponse: makeResponse(test.FakeType0Collection, "type0/v2", "type0/n2", test.Type0A[2].Resource),
			wantRequest:  makeRequest(test.FakeType0Collection, "type0/v2", "type0/n2", codes.OK),
			wantChange: &Change{
				Collection: test.FakeType0Collection,
				Objects: []*Object{{
					TypeURL:  test.FakeType0TypeURL,
					Metadata: test.Type0A[2].Metadata,
					Body:     test.Type0A[2].Proto,
				}},
			},
		},
	}

	// install probe to monitor when the client is finished handling responses
	responseDone := make(chan struct{})
	handleResponseDoneProbe = func() { responseDone <- struct{}{} }
	prevDelay := reestablishStreamDelay
	reestablishStreamDelay = 10 * time.Millisecond

	defer func() {
		handleResponseDoneProbe = nil
		reestablishStreamDelay = prevDelay
	}()

	for _, step := range steps {
		if step.sendError {
			atomic.StoreInt32(&ts.sendError, 1)
		} else {
			atomic.StoreInt32(&ts.sendError, 0)
		}
		if step.recvError {
			atomic.StoreInt32(&ts.recvError, 1)
		} else {
			atomic.StoreInt32(&ts.recvError, 0)
		}

		if step.sendResponse != nil {
			ts.sendResponseToClient(step.sendResponse)

			if !step.recvError {
				<-responseDone
			}

			if !step.sendError {
				if !reflect.DeepEqual(ts.change[step.wantChange.Collection], step.wantChange) {
					t.Fatalf("%v: bad client change: \n got %#v \nwant %#v",
						step.name, ts.change[step.wantChange.Collection].Objects[0], step.wantChange.Objects[0])
				}
			}
		}

		if err := ts.wantRequest(step.wantRequest); err != nil {
			t.Fatalf("%v: failed to receive correct request: %v", step.name, err)
		}
	}
}

func TestInMemoryUpdater(t *testing.T) {
	u := NewInMemoryUpdater()

	o := u.Get("foo")
	if len(o) != 0 {
		t.Fatalf("Unexpected items in updater: %v", o)
	}

	c := Change{
		Collection: "foo",
		Objects: []*Object{
			{
				TypeURL: "foo",
				Metadata: &mcp.Metadata{
					Name: "bar",
				},
				Body: &types.Empty{},
			},
		},
	}

	err := u.Apply(&c)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	o = u.Get("foo")
	if len(o) != 1 {
		t.Fatalf("expected item not found: %v", o)
	}

	if o[0].Metadata.Name != "bar" {
		t.Fatalf("expected name not found on object: %v", o)
	}
}
