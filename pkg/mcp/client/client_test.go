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
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/gogo/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	mcp "istio.io/api/mcp/v1alpha1"
)

type testStream struct {
	sync.Mutex
	change map[string]*Change

	requestC  chan *mcp.MeshConfigRequest  // received from client
	responseC chan *mcp.MeshConfigResponse // to-be-sent to client

	updateError bool
	sendError   bool
	recvError   bool

	grpc.ClientStream
}

func newTestStream() *testStream {
	return &testStream{
		requestC:  make(chan *mcp.MeshConfigRequest, 10),
		responseC: make(chan *mcp.MeshConfigResponse, 10),
		change:    make(map[string]*Change),
	}
}

func (ts *testStream) close() {
	close(ts.requestC)
	close(ts.responseC)
}

func (ts *testStream) wantRequest(messageName string, want *mcp.MeshConfigRequest) error {
	select {
	case got := <-ts.requestC:
		got = proto.Clone(got).(*mcp.MeshConfigRequest)
		return checkRequest(got, want)
	case <-time.After(time.Second):
		return fmt.Errorf("no request received")
	}
}

func (ts *testStream) sendResponseToClient(response *mcp.MeshConfigResponse) {
	ts.responseC <- response
}

func (ts *testStream) StreamAggregatedResources(ctx context.Context, opts ...grpc.CallOption) (mcp.AggregatedMeshConfigService_StreamAggregatedResourcesClient, error) { // nolint: lll
	return ts, nil
}

func (ts *testStream) Send(request *mcp.MeshConfigRequest) error {
	if ts.sendError {
		return errors.New("send error")
	}
	ts.requestC <- request
	return nil
}

func (ts *testStream) Recv() (*mcp.MeshConfigResponse, error) {
	if ts.recvError {
		return nil, errors.New("recv error")
	}
	response, more := <-ts.responseC
	if !more {
		return nil, io.EOF
	}
	return response, nil
}

func (ts *testStream) Apply(change *Change) error {
	if ts.updateError {
		return errors.New("update error")
	}
	ts.Lock()
	defer ts.Unlock()
	ts.change[change.MessageName] = change
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

// fake protobuf types

type fakeTypeBase struct{ Info string }

func (f *fakeTypeBase) Reset()                   {}
func (f *fakeTypeBase) String() string           { return f.Info }
func (f *fakeTypeBase) ProtoMessage()            {}
func (f *fakeTypeBase) Marshal() ([]byte, error) { return []byte(f.Info), nil }
func (f *fakeTypeBase) Unmarshal(in []byte) error {
	f.Info = string(in)
	return nil
}

type fakeType0 struct{ fakeTypeBase }
type fakeType1 struct{ fakeTypeBase }
type fakeType2 struct{ fakeTypeBase }

type unmarshalErrorType struct{ fakeTypeBase }

func (f *unmarshalErrorType) Unmarshal(in []byte) error { return errors.New("unmarshal_error") }

const (
	typePrefix                = "type.googleapis.com/"
	fakeType0MessageName      = "istio.io.galley.pkg.mcp.server.fakeType0"
	fakeType1MessageName      = "istio.io.galley.pkg.mcp.server.fakeType1"
	fakeType2MessageName      = "istio.io.galley.pkg.mcp.server.fakeType2"
	unmarshalErrorMessageName = "istio.io.galley.pkg.mcp.server.unmarshalErrorType"

	fakeType0TypeURL = typePrefix + fakeType0MessageName
	fakeType1TypeURL = typePrefix + fakeType1MessageName
	fakeType2TypeURL = typePrefix + fakeType2MessageName
)

var (
	key      = "node-id"
	metadata = map[string]string{"foo": "bar"}
	client   *mcp.Client

	supportedMessageNames = []string{
		fakeType0MessageName,
		fakeType1MessageName,
		fakeType2MessageName,
	}

	supportedTypeUrls = []string{
		typePrefix + fakeType0MessageName,
		typePrefix + fakeType1MessageName,
		typePrefix + fakeType2MessageName,
	}

	fake0_0      = &fakeType0{fakeTypeBase{"f0_0"}}
	fake0_1      = &fakeType0{fakeTypeBase{"f0_1"}}
	fake0_2      = &fakeType0{fakeTypeBase{"f0_2"}}
	fake1        = &fakeType1{fakeTypeBase{"f1"}}
	fake2        = &fakeType2{fakeTypeBase{"f2"}}
	badUnmarshal = &unmarshalErrorType{fakeTypeBase{"unmarshal_error"}}

	// initialized in init()
	fakeResource0_0      *mcp.Envelope
	fakeResource0_1      *mcp.Envelope
	fakeResource0_2      *mcp.Envelope
	fakeResource1        *mcp.Envelope
	fakeResource2        *mcp.Envelope
	badUnmarshalEnvelope *mcp.Envelope
)

func mustMarshalAny(pb proto.Message) *types.Any {
	a, err := types.MarshalAny(pb)
	if err != nil {
		panic(err.Error())
	}
	return a
}

func init() {
	proto.RegisterType((*fakeType0)(nil), fakeType0MessageName)
	proto.RegisterType((*fakeType1)(nil), fakeType1MessageName)
	proto.RegisterType((*fakeType2)(nil), fakeType2MessageName)
	proto.RegisterType((*fakeType2)(nil), fakeType2MessageName)
	proto.RegisterType((*unmarshalErrorType)(nil), unmarshalErrorMessageName)

	fakeResource0_0 = &mcp.Envelope{
		Metadata: &mcp.Metadata{Name: "f0_0"},
		Resource: mustMarshalAny(fake0_0),
	}
	fakeResource0_1 = &mcp.Envelope{
		Metadata: &mcp.Metadata{Name: "f0_1"},
		Resource: mustMarshalAny(fake0_1),
	}
	fakeResource0_2 = &mcp.Envelope{
		Metadata: &mcp.Metadata{Name: "f0_2"},
		Resource: mustMarshalAny(fake0_2),
	}
	fakeResource1 = &mcp.Envelope{
		Metadata: &mcp.Metadata{Name: "f1"},
		Resource: mustMarshalAny(fake1),
	}
	fakeResource2 = &mcp.Envelope{
		Metadata: &mcp.Metadata{Name: "f2"},
		Resource: mustMarshalAny(fake2),
	}
	badUnmarshalEnvelope = &mcp.Envelope{
		Metadata: &mcp.Metadata{Name: "unmarshal_error"},
		Resource: mustMarshalAny(badUnmarshal),
	}

	client = &mcp.Client{
		Id:       key,
		Metadata: &types.Struct{Fields: map[string]*types.Value{}},
	}
	for k, v := range metadata {
		client.Metadata.Fields[k] = &types.Value{Kind: &types.Value_StringValue{v}}
	}
}

func makeRequest(typeURL, version, nonce string, errorCode codes.Code) *mcp.MeshConfigRequest {
	req := &mcp.MeshConfigRequest{
		Client:        client,
		TypeUrl:       typeURL,
		VersionInfo:   version,
		ResponseNonce: nonce,
	}
	if errorCode != codes.OK {
		req.ErrorDetail = status.New(errorCode, "").Proto()
	}
	return req
}

func makeResponse(typeURL, version, nonce string, envelopes ...*mcp.Envelope) *mcp.MeshConfigResponse {
	r := &mcp.MeshConfigResponse{
		TypeUrl:     typeURL,
		VersionInfo: version,
		Nonce:       nonce,
	}
	for _, envelope := range envelopes {
		r.Envelopes = append(r.Envelopes, *envelope)
	}
	return r
}

// Verify reconnect on send error
// Verify reconnect on receive error

func TestSingleTypeCases(t *testing.T) {
	ts := newTestStream()

	c := New(ts, supportedMessageNames, ts, key, metadata)
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
		ts.close()
	}()

	// Check metadata fields first
	if !reflect.DeepEqual(c.Metadata(), metadata) {
		t.Fatalf("metadata mismatch: got:\n%v\nwanted:\n%v\n", c.metadata, metadata)
	}

	if c.ID() != key {
		t.Fatalf("id mismatch: got\n%v\nwanted:\n%v\n", c.ID(), key)
	}

	if !reflect.DeepEqual(c.SupportedTypeURLs(), supportedTypeUrls) {
		t.Fatalf("type url mismatch: got:\n%v\nwanted:\n%v\n", c.SupportedTypeURLs(), supportedTypeUrls)
	}

	wantInitial := make(map[string]*mcp.MeshConfigRequest)
	for _, messageName := range supportedMessageNames {
		typeURL := typePrefix + messageName
		wantInitial[typeURL] = makeRequest(typeURL, "", "", codes.OK)
	}
	gotInitial := make(map[string]*mcp.MeshConfigRequest)
	for i := 0; i < len(supportedMessageNames); i++ {
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
			sendResponse: makeResponse(fakeType0TypeURL, "type0/v0", "type0/n0", fakeResource0_0),
			wantRequest:  makeRequest(fakeType0TypeURL, "type0/v0", "type0/n0", codes.OK),
			wantChange: &Change{
				MessageName: fakeType0MessageName,
				Objects: []*Object{{
					MessageName: fakeType0MessageName,
					Metadata:    fakeResource0_0.Metadata,
					Resource:    fake0_0,
					Version:     "type0/v0",
				}},
			},
		},
		{
			name:         "ACK request (type1)",
			sendResponse: makeResponse(fakeType1TypeURL, "type1/v0", "type1/n0", fakeResource1),
			wantRequest:  makeRequest(fakeType1TypeURL, "type1/v0", "type1/n0", codes.OK),
			wantChange: &Change{
				MessageName: fakeType1MessageName,
				Objects: []*Object{{
					MessageName: fakeType1MessageName,
					Metadata:    fakeResource1.Metadata,
					Resource:    fake1,
					Version:     "type1/v0",
				}},
			},
		},
		{
			name:         "ACK request (type2)",
			sendResponse: makeResponse(fakeType2TypeURL, "type2/v0", "type2/n0", fakeResource2),
			wantRequest:  makeRequest(fakeType2TypeURL, "type2/v0", "type2/n0", codes.OK),
			wantChange: &Change{
				MessageName: fakeType2MessageName,
				Objects: []*Object{{
					MessageName: fakeType2MessageName,
					Metadata:    fakeResource2.Metadata,
					Resource:    fake2,
					Version:     "type2/v0",
				}},
			},
		},
		{
			name:         "NACK request (unsupported type_url)",
			sendResponse: makeResponse(fakeType0TypeURL+"Garbage", "type0/v1", "type0/n1", fakeResource0_0),
			wantRequest:  makeRequest(fakeType0TypeURL+"Garbage", "", "type0/n1", codes.Unimplemented),
			wantChange: &Change{
				MessageName: fakeType0MessageName,
				Objects: []*Object{{
					MessageName: fakeType0MessageName,
					Metadata:    fakeResource0_0.Metadata,
					Resource:    fake0_0,
					Version:     "type0/v0",
				}},
			},
		},
		{
			name:         "NACK request (unmarshal error)",
			sendResponse: makeResponse(fakeType0TypeURL, "type0/v1", "type0/n2", badUnmarshalEnvelope),
			wantRequest:  makeRequest(fakeType0TypeURL, "type0/v0", "type0/n2", codes.Unknown),
			wantChange: &Change{
				MessageName: fakeType0MessageName,
				Objects: []*Object{{
					MessageName: fakeType0MessageName,
					Metadata:    fakeResource0_0.Metadata,
					Resource:    fake0_0,
					Version:     "type0/v0",
				}},
			},
		},
		{
			name:         "NACK request (response type_url does not match resource type_url)",
			sendResponse: makeResponse(fakeType0TypeURL, "type0/v1", "type0/n3", fakeResource1),
			wantRequest:  makeRequest(fakeType0TypeURL, "type0/v0", "type0/n3", codes.InvalidArgument),
			wantChange: &Change{
				MessageName: fakeType0MessageName,
				Objects: []*Object{{
					MessageName: fakeType0MessageName,
					Metadata:    fakeResource0_0.Metadata,
					Resource:    fake0_0,
					Version:     "type0/v0",
				}},
			},
		},
		{
			name:         "NACK request (client updater rejected changes)",
			updateError:  true,
			sendResponse: makeResponse(fakeType0TypeURL, "type0/v1", "type0/n3", fakeResource0_0),
			wantRequest:  makeRequest(fakeType0TypeURL, "type0/v0", "type0/n3", codes.InvalidArgument),
			wantChange: &Change{
				MessageName: fakeType0MessageName,
				Objects: []*Object{{
					MessageName: fakeType0MessageName,
					Metadata:    fakeResource0_0.Metadata,
					Resource:    fake0_0,
					Version:     "type0/v0",
				}},
			},
		},
		{
			name:         "ACK request after previous NACKs",
			sendResponse: makeResponse(fakeType0TypeURL, "type0/v1", "type0/n3", fakeResource0_1, fakeResource0_2),
			wantRequest:  makeRequest(fakeType0TypeURL, "type0/v1", "type0/n3", codes.OK),
			wantChange: &Change{
				MessageName: fakeType0MessageName,
				Objects: []*Object{{
					MessageName: fakeType0MessageName,
					Metadata:    fakeResource0_1.Metadata,
					Resource:    fake0_1,
					Version:     "type0/v1",
				}, {
					MessageName: fakeType0MessageName,
					Metadata:    fakeResource0_2.Metadata,
					Resource:    fake0_2,
					Version:     "type0/v1",
				}},
			},
			wantJournal: nil,
		},
	}

	// install probe to monitor when the client is finished handling responses
	responseDone := make(chan struct{})
	handleResponseDoneProbe = func() {
		responseDone <- struct{}{}
	}
	defer func() { handleResponseDoneProbe = nil }()

	for _, step := range steps {
		ts.updateError = step.updateError

		ts.sendResponseToClient(step.sendResponse)
		<-responseDone
		if !reflect.DeepEqual(ts.change[step.wantChange.MessageName], step.wantChange) {
			t.Fatalf("%v: bad client change: \n got %#v \nwant %#v", step.name, ts.change, step.wantChange)
		}

		if err := ts.wantRequest(fakeType0MessageName, step.wantRequest); err != nil {
			t.Fatalf("%v: failed to receive correct request: %v", step.name, err)
		}

		entries := c.SnapshotRequestInfo()
		if len(entries) == 0 {
			t.Fatal("No journal entries not found.")
		}
		lastEntry := entries[len(entries)-1]
		if err := checkRequest(lastEntry.Request, step.wantRequest); err != nil {
			t.Fatalf("%v: failed to publish the right journal entries: %v", step.name, err)
		}
	}
}
