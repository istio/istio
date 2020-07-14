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
	"fmt"
	"io"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/gogo/protobuf/types"
	"github.com/golang/sync/errgroup"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/codes"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/internal/test"
	"istio.io/istio/pkg/mcp/testing/monitoring"
)

var (
	errSend   = errors.New("send error")
	errUpdate = errors.New("update error")
)

type sinkTestHarness struct {
	resourcesChan chan *mcp.Resources
	requestsChan  chan *mcp.RequestResources
	recvErrorChan chan error
	sendErrorChan chan error

	sendError   error
	recvError   error
	updateError error
	ctx         context.Context

	mu                 sync.Mutex
	openErr            error
	changes            map[string]*Change
	changeUpdatedChans chan struct{}
}

func (h *sinkTestHarness) Context() context.Context {
	return h.ctx
}

func (h *sinkTestHarness) setContext(ctx context.Context) {
	h.ctx = ctx
}

func (h *sinkTestHarness) setOpenError(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.openErr = err
}

func (h *sinkTestHarness) openError() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.openErr
}

func (h *sinkTestHarness) setSendError(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sendError = err
}

func (h *sinkTestHarness) setRecvError(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recvError = err
}

func (h *sinkTestHarness) setUpdateError(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.updateError = err
}

func (h *sinkTestHarness) Apply(change *Change) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.changes[change.Collection] = change
	h.changeUpdatedChans <- struct{}{}

	if h.updateError != nil {
		return h.updateError
	}
	return nil
}

func (h *sinkTestHarness) Send(req *mcp.RequestResources) error {
	h.mu.Lock()
	err := h.sendError
	h.mu.Unlock()

	if err != nil {
		return err
	}

	select {
	case h.requestsChan <- req:
		return nil
	case err := <-h.sendErrorChan:
		return err
	}
}

func (h *sinkTestHarness) Close() {
	h.sendErrorChan <- io.EOF
	h.recvErrorChan <- io.EOF
}

func (h *sinkTestHarness) Recv() (*mcp.Resources, error) {
	h.mu.Lock()
	err := h.recvError
	h.mu.Unlock()

	if err != nil {
		return nil, err
	}
	select {
	case err := <-h.recvErrorChan:
		return nil, err
	case resources := <-h.resourcesChan:
		return resources, nil
	}
}

func (h *sinkTestHarness) sendFakeResources(response *mcp.Resources) {
	h.mu.Lock()
	err := h.recvError
	h.mu.Unlock()

	if err != nil {
		h.recvErrorChan <- err
	} else {
		h.resourcesChan <- response
	}
}

func (h *sinkTestHarness) resetSavedChanges() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.changes = make(map[string]*Change)
}
func newSinkTestHarness() *sinkTestHarness {
	return &sinkTestHarness{
		resourcesChan:      make(chan *mcp.Resources, 10),
		requestsChan:       make(chan *mcp.RequestResources, 10),
		changes:            make(map[string]*Change),
		recvErrorChan:      make(chan error, 10),
		sendErrorChan:      make(chan error, 10),
		changeUpdatedChans: make(chan struct{}, 10),
		ctx:                context.Background(),
	}
}

type sinkHarness struct {
	sink         *Sink
	errgrp       errgroup.Group
	responseDone chan struct{}
	*sinkTestHarness
}

func newHarness() *sinkHarness {
	h := &sinkHarness{
		sinkTestHarness: newSinkTestHarness(),
		responseDone:    make(chan struct{}),
	}

	options := &Options{
		CollectionOptions: CollectionOptionsFromSlice(test.SupportedCollections),
		Updater:           h,
		ID:                test.NodeID,
		Metadata:          test.NodeMetadata,
		Reporter:          monitoring.NewInMemoryStatsContext(),
	}
	h.sink = New(options)

	handleResponseDoneProbe = func() { h.responseDone <- struct{}{} }

	return h
}

func (h *sinkHarness) delete() {
	handleResponseDoneProbe = nil
}

func (h *sinkHarness) verifyAppliedChanges(t *testing.T, collection string, want *Change) {
	t.Helper()

	h.mu.Lock()
	defer h.mu.Unlock()

	got := h.changes[collection]

	if diff := cmp.Diff(got, want, cmpopts.EquateEmpty()); diff != "" {
		t.Fatalf("bad change for %q: \n got %#v \nwant %#v\ndiff %v", collection, got, want, diff)
	}
}

func (h *sinkHarness) verifySingleRequestSent(t *testing.T, resources *mcp.RequestResources) {
	t.Helper()

	resourcesByType := map[string]*mcp.RequestResources{
		resources.Collection: resources,
	}
	h.verifyMultipleRequestsSent(t, resourcesByType)
}

func (h *sinkHarness) verifyMultipleRequestsSent(t *testing.T, wantRequests map[string]*mcp.RequestResources) {
	t.Helper()

	gotRequests := make(map[string]*mcp.RequestResources)
	for i := 0; i < len(wantRequests); i++ {
		select {
		case got := <-h.requestsChan:
			// verify the presence of errorDetails and the error code. Ignore everything else.
			if got.ErrorDetail != nil {
				got.ErrorDetail.Message = ""
				got.ErrorDetail.Details = nil
			}

			gotRequests[got.Collection] = got
		case <-time.After(time.Second):
			t.Fatalf("missing requests: got %v want %v", len(gotRequests), len(wantRequests))
		}
	}
	if diff := cmp.Diff(gotRequests, wantRequests); diff != "" {
		t.Fatalf("bad requests\n got %#v \nwant %#v\n diff %v", gotRequests, wantRequests, diff)
	}
}

func (h *sinkHarness) openStream(t *testing.T) {
	t.Helper()

	h.sinkTestHarness = newSinkTestHarness()

	h.errgrp = errgroup.Group{}
	h.errgrp.Go(func() error {
		err := h.sink.ProcessStream(h)
		return err
	})
}

func (h *sinkHarness) closeStream(t *testing.T) {
	t.Helper()
	h.Close()
}

func (h *sinkHarness) verifyInitialRequest(t *testing.T) {
	t.Helper()

	h.verifyInitialRequestOnResume(t, map[string]map[string]string{}, map[string]*Change{})
}

func (h *sinkHarness) verifyInitialRequestOnResume(t *testing.T, initialResourceVersions map[string]map[string]string, changes map[string]*Change) { // nolint: lll
	t.Helper()

	// verify the initial set of empty requests are sent for each supported type
	want := make(map[string]*mcp.RequestResources)
	for _, typeURL := range test.SupportedCollections {
		want[typeURL] = &mcp.RequestResources{
			SinkNode:                test.Node,
			Collection:              typeURL,
			InitialResourceVersions: initialResourceVersions[typeURL],
		}
	}
	h.verifyMultipleRequestsSent(t, want)

	// verify no responses are received and no changes are applied
	for _, collection := range test.SupportedCollections {
		h.verifyAppliedChanges(t, collection, changes[collection])
	}
}

func (h *sinkHarness) closeStreamAndVerifyReturnValue(t *testing.T, wantError error) {
	t.Helper()

	h.closeStream(t)
	h.verifyStreamReturnValue(t, wantError)
}

func (h *sinkHarness) verifyStreamReturnValue(t *testing.T, wantError error) {
	t.Helper()

	if err := h.errgrp.Wait(); err != wantError {
		t.Fatalf("unexpected error on stream close: got %q want %q", err, wantError)
	}
}

func makeChange(collection, version string, inc bool, removed []string, fakes ...*test.Fake) *Change { // nolint: unparam
	change := &Change{
		Collection:        collection,
		Removed:           removed,
		Incremental:       inc,
		SystemVersionInfo: version,
	}
	for _, fake := range fakes {
		change.Objects = append(change.Objects, &Object{
			TypeURL:  fake.TypeURL,
			Metadata: fake.Metadata,
			Body:     fake.Proto,
		})
	}
	return change
}

type testStep struct {
	name        string
	resources   *mcp.Resources
	wantRequest *mcp.RequestResources
	wantChange  *Change

	updateError error
	sendError   error
	recvError   error
}

func (h *sinkHarness) executeTestSequence(t *testing.T, steps []testStep) {
	t.Helper()

	for i, step := range steps {
		name := fmt.Sprintf("[%v] %v", i, step.name)
		passed := t.Run(name, func(tt *testing.T) {
			h.resetSavedChanges()
			h.setUpdateError(step.updateError)
			h.setSendError(step.sendError)
			h.setRecvError(step.recvError)

			h.sendFakeResources(step.resources)

			if step.recvError == nil {
				<-h.responseDone
			}

			if step.wantRequest != nil {
				h.verifySingleRequestSent(tt, step.wantRequest)
			}

			if step.wantChange != nil {
				h.verifyAppliedChanges(tt, step.wantChange.Collection, step.wantChange)
			}
		})
		if !passed {
			t.Fatalf("step %v failed", name)
		}
	}
}

func TestSinkACKAddUpdateDelete(t *testing.T) {
	steps := []testStep{
		{
			name:        "ACK test.Type0 add A",
			resources:   test.MakeResources(false, test.FakeType0Collection, "type0/v0", "type0/n0", nil, test.Type0A[0]),
			wantRequest: test.MakeRequest(false, test.FakeType0Collection, "type0/n0", codes.OK),
			wantChange:  makeChange(test.FakeType0Collection, "type0/v0", false, nil, test.Type0A[0]),
		},
		{
			name:        "ACK test.Type1 add A",
			resources:   test.MakeResources(false, test.FakeType1Collection, "type1/v0", "type1/n0", nil, test.Type1A[0]),
			wantRequest: test.MakeRequest(false, test.FakeType1Collection, "type1/n0", codes.OK),
			wantChange:  makeChange(test.FakeType1Collection, "type1/v0", false, nil, test.Type1A[0]),
		},
		{
			name:        "ACK test.Type2 add A",
			resources:   test.MakeResources(false, test.FakeType2Collection, "type2/v0", "type2/n0", nil, test.Type2A[0]),
			wantRequest: test.MakeRequest(false, test.FakeType2Collection, "type2/n0", codes.OK),
			wantChange:  makeChange(test.FakeType2Collection, "type2/v0", false, nil, test.Type2A[0]),
		},
		{
			name:        "ACK update A add B",
			resources:   test.MakeResources(false, test.FakeType0Collection, "type0/v1", "type0/n1", nil, test.Type0A[1], test.Type0B[0]),
			wantRequest: test.MakeRequest(false, test.FakeType0Collection, "type0/n1", codes.OK),
			wantChange:  makeChange(test.FakeType0Collection, "type0/v1", false, nil, test.Type0A[1], test.Type0B[0]),
		},
		{
			name:        "ACK remove A update B add C",
			resources:   test.MakeResources(false, test.FakeType0Collection, "type0/v2", "type0/n2", nil, test.Type0B[1], test.Type0C[0]),
			wantRequest: test.MakeRequest(false, test.FakeType0Collection, "type0/n2", codes.OK),
			wantChange:  makeChange(test.FakeType0Collection, "type0/v2", false, nil, test.Type0B[1], test.Type0C[0]),
		},
		{
			name:        "ACK remove B",
			resources:   test.MakeResources(false, test.FakeType0Collection, "type0/v3", "type0/n3", nil),
			wantRequest: test.MakeRequest(false, test.FakeType0Collection, "type0/n3", codes.OK),
			wantChange:  makeChange(test.FakeType0Collection, "type0/v3", false, nil),
		},
		{
			name:        "ACK remove B again",
			resources:   test.MakeResources(false, test.FakeType0Collection, "type0/v4", "type0/n4", nil),
			wantRequest: test.MakeRequest(false, test.FakeType0Collection, "type0/n4", codes.OK),
			wantChange:  makeChange(test.FakeType0Collection, "type0/v4", false, nil),
		},
		{
			name:        "ACK update C",
			resources:   test.MakeResources(false, test.FakeType0Collection, "type0/v5", "type0/n5", nil, test.Type0C[1]),
			wantRequest: test.MakeRequest(false, test.FakeType0Collection, "type0/n5", codes.OK),
			wantChange:  makeChange(test.FakeType0Collection, "type0/v5", false, nil, test.Type0C[1]),
		},
	}

	h := newHarness()
	h.openStream(t)

	h.verifyInitialRequest(t)
	h.executeTestSequence(t, steps)
	h.closeStreamAndVerifyReturnValue(t, io.EOF)

	info := h.sink.SnapshotRequestInfo()
	got := len(info)
	want := len(steps) + len(h.sink.Collections()) // plus initial requests
	if got != want {
		t.Fatalf("wrong number of snapshot request info: got %v want %v", got, want)
	}
}

func TestSinkNACK(t *testing.T) {
	steps := []testStep{
		{
			name:        "ACK add A",
			resources:   test.MakeResources(false, test.FakeType0Collection, "type0/v0", "type0/n0", nil, test.Type0A[0]),
			wantRequest: test.MakeRequest(false, test.FakeType0Collection, "type0/n0", codes.OK),
			wantChange:  makeChange(test.FakeType0Collection, "type0/v0", false, nil, test.Type0A[0]),
		},
		{
			name:        "NACK unsupported type_url",
			resources:   test.MakeResources(false, test.FakeType0Collection+"Garbage", "type0/v1", "type0/n1", nil, test.Type0A[1]),
			wantRequest: test.MakeRequest(false, test.FakeType0Collection+"Garbage", "type0/n1", codes.Unimplemented),
		},
		{
			name:        "NACK unmarshal error",
			resources:   test.MakeResources(false, test.FakeType0Collection, "type0/v2", "type0/n2", nil, test.BadUnmarshal),
			wantRequest: test.MakeRequest(false, test.FakeType0Collection, "type0/n2", codes.Unknown),
		},
		{
			name:        "NACK updater rejected change",
			resources:   test.MakeResources(false, test.FakeType0Collection, "type0/v4", "type0/n4", nil, test.Type0A[1]),
			wantRequest: test.MakeRequest(false, test.FakeType0Collection, "type0/n4", codes.InvalidArgument),
			updateError: errUpdate,
		},
		{
			name:        "ACK update A",
			resources:   test.MakeResources(false, test.FakeType0Collection, "type0/v5", "type0/n5", nil, test.Type0A[1]),
			wantRequest: test.MakeRequest(false, test.FakeType0Collection, "type0/n5", codes.OK),
			wantChange:  makeChange(test.FakeType0Collection, "type0/v5", false, nil, test.Type0A[1]),
		},
	}

	h := newHarness()
	defer h.delete()

	h.openStream(t)

	h.verifyInitialRequest(t)
	h.executeTestSequence(t, steps)
	h.closeStreamAndVerifyReturnValue(t, io.EOF)
}

func TestSinkResume(t *testing.T) {
	steps0 := []testStep{
		{
			name:        "ACK add test.Type0 A B",
			resources:   test.MakeResources(false, test.FakeType0Collection, "type0/v0", "type0/n0", nil, test.Type0A[0], test.Type0B[0]),
			wantRequest: test.MakeRequest(false, test.FakeType0Collection, "type0/n0", codes.OK),
			wantChange:  makeChange(test.FakeType0Collection, "type0/v0", false, nil, test.Type0A[0], test.Type0B[0]),
		},
		{
			name:        "ACK add test.Type1 A",
			resources:   test.MakeResources(false, test.FakeType1Collection, "type1/v0", "type1/n0", nil, test.Type1A[0]),
			wantRequest: test.MakeRequest(false, test.FakeType1Collection, "type1/n0", codes.OK),
			wantChange:  makeChange(test.FakeType1Collection, "type1/v0", false, nil, test.Type1A[0]),
		},
	}

	steps1 := []testStep{
		{
			name:        "ACK remove test.Type0 A B",
			resources:   test.MakeResources(false, test.FakeType0Collection, "type0/v0", "type0/n0", nil),
			wantRequest: test.MakeRequest(false, test.FakeType0Collection, "type0/n0", codes.OK),
			wantChange:  makeChange(test.FakeType0Collection, "type0/v0", false, nil),
		},
		{
			name:        "ACK remove test.Type1 A",
			resources:   test.MakeResources(false, test.FakeType1Collection, "type1/v0", "type1/n0", nil),
			wantRequest: test.MakeRequest(false, test.FakeType1Collection, "type1/n0", codes.OK),
			wantChange:  makeChange(test.FakeType1Collection, "type1/v0", false, nil),
		},
	}

	h := newHarness()
	defer h.delete()

	// Verify initial resource pushed
	h.openStream(t)
	h.verifyInitialRequest(t)
	h.executeTestSequence(t, steps0)

	// Verify previously pushes resources are included in requests when resuming the stream.
	h.closeStreamAndVerifyReturnValue(t, io.EOF)
	h.openStream(t)
	wantInitialResourceVersions := map[string]map[string]string{
		// TODO enable this or create a new test for incrementally resuming a session
		//test.FakeType0Collection: map[string]string{
		//	test.Type0A[0].Metadata.Name: test.Type0A[0].Metadata.Version,
		//	test.Type0B[0].Metadata.Name: test.Type0B[0].Metadata.Version,
		//},
		//test.FakeType1Collection: map[string]string{
		//	test.Type1A[0].Metadata.Name: test.Type1A[0].Metadata.Version,
		//},
	}
	h.verifyInitialRequestOnResume(t, wantInitialResourceVersions, nil)

	// Delete all of the resources ...
	h.executeTestSequence(t, steps1)
	h.closeStreamAndVerifyReturnValue(t, io.EOF)

	// ... and verify that no resource versions are included in the subsequent re-connection requests
	h.openStream(t)
	h.verifyInitialRequest(t)
	h.closeStreamAndVerifyReturnValue(t, io.EOF)
}

func TestSinkSendError(t *testing.T) {
	steps0 := []testStep{
		{
			name:        "ACK add A",
			resources:   test.MakeResources(false, test.FakeType0Collection, "type0/v0", "type0/n0", nil, test.Type0A[0]),
			wantRequest: test.MakeRequest(false, test.FakeType0Collection, "type0/n0", codes.OK),
			wantChange:  makeChange(test.FakeType0Collection, "type0/v0", false, nil, test.Type0A[0]),
		},
		{
			name:        "send error",
			resources:   test.MakeResources(false, test.FakeType0Collection, "type0/v1", "type0/n1", nil, test.Type0A[1]),
			wantRequest: nil, // ACK request dropped on send error
			wantChange:  makeChange(test.FakeType0Collection, "type0/v1", false, nil, test.Type0A[1]),
			sendError:   errSend,
		},
	}

	h := newHarness()
	defer h.delete()

	// verify send error stops the stream
	h.openStream(t)
	h.verifyInitialRequest(t)
	h.executeTestSequence(t, steps0)
	h.verifyStreamReturnValue(t, errSend)
}

func TestSinkRecvError(t *testing.T) {
	stepsErrorEOF := []testStep{
		{
			name:        "ACK add A",
			resources:   test.MakeResources(false, test.FakeType0Collection, "type0/v0", "type0/n0", nil, test.Type0A[0]),
			wantRequest: test.MakeRequest(false, test.FakeType0Collection, "type0/n0", codes.OK),
			wantChange:  makeChange(test.FakeType0Collection, "type0/v0", false, nil, test.Type0A[0]),
		},
		{
			name:      "recv error EOF",
			resources: test.MakeResources(false, test.FakeType0Collection, "type0/v1", "type0/n1", nil, test.Type0A[1]),
			recvError: io.EOF,
		},
	}

	unknownError := errors.New("unknown")
	stepsErrorUnknown := []testStep{
		{
			name:        "ACK update A",
			resources:   test.MakeResources(false, test.FakeType0Collection, "type0/v0", "type0/n0", nil, test.Type0A[1]),
			wantRequest: test.MakeRequest(false, test.FakeType0Collection, "type0/n0", codes.OK),
			wantChange:  makeChange(test.FakeType0Collection, "type0/v0", false, nil, test.Type0A[1]),
		},
		{
			name:      "recv error unknown",
			resources: test.MakeResources(false, test.FakeType0Collection, "type0/v1", "type0/n1", nil, test.Type0A[2]),
			recvError: unknownError,
		},
	}

	h := newHarness()
	defer h.delete()

	// verify an EOF recv error stops the stream
	h.openStream(t)
	h.verifyInitialRequest(t)
	h.executeTestSequence(t, stepsErrorEOF)
	h.verifyStreamReturnValue(t, io.EOF)

	// verify an unknown recv error stops the stream
	h.closeStreamAndVerifyReturnValue(t, io.EOF)
	h.openStream(t)
	h.verifyInitialRequestOnResume(t, nil, nil)
	h.executeTestSequence(t, stepsErrorUnknown)
	h.verifyStreamReturnValue(t, unknownError)
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

func TestSink_MetadataID(t *testing.T) {
	options := &Options{
		CollectionOptions: CollectionOptionsFromSlice(test.SupportedCollections),
		Updater:           NewInMemoryUpdater(),
		ID:                test.NodeID,
		Metadata:          test.NodeMetadata,
		Reporter:          monitoring.NewInMemoryStatsContext(),
	}
	sink := New(options)

	gotID := sink.ID()
	if gotID != test.NodeID {
		t.Errorf("wrong ID: got %v want %v", gotID, test.NodeID)
	}

	gotMetadata := sink.Metadata()
	if diff := cmp.Diff(gotMetadata, test.NodeMetadata); diff != "" {
		t.Errorf("wrong Metadata: got %v want %v", gotMetadata, test.NodeMetadata)
	}
}

func TestCreateInitialRequests(t *testing.T) {
	options := &Options{
		CollectionOptions: CollectionOptionsFromSlice(test.SupportedCollections),
		Updater:           NewInMemoryUpdater(),
		ID:                test.NodeID,
		Metadata:          test.NodeMetadata,
		Reporter:          monitoring.NewInMemoryStatsContext(),
	}
	sink := New(options)

	want := []*mcp.RequestResources{
		test.MakeRequest(false, test.FakeType0Collection, "", codes.OK),
		test.MakeRequest(false, test.FakeType1Collection, "", codes.OK),
		test.MakeRequest(false, test.FakeType2Collection, "", codes.OK),
	}
	got := sink.createInitialRequests()
	sort.Slice(got, func(i, j int) bool { return got[i].Collection < got[j].Collection })

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("wrong requests with incremental disabled: \n got %v \nwant %v diff %v", got, want, diff)
	}

	for i := 0; i < len(test.SupportedCollections); i++ {
		want[i].Incremental = true
		want[i].InitialResourceVersions = map[string]string{"foo": "v1"}
	}
	for _, state := range sink.state {
		state.requestIncremental = true
		state.versions["foo"] = "v1"
	}

	got = sink.createInitialRequests()
	sort.Slice(got, func(i, j int) bool { return got[i].Collection < got[j].Collection })

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("wrong requests with incemental enabled: \n got %v \nwant %v diff %v", got, want, diff)
	}
}
