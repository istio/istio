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

package contextgraph

import (
	"context"
	"testing"
	"time"

	gax "github.com/googleapis/gax-go"
	"google.golang.org/api/option"

	"istio.io/istio/mixer/adapter/stackdriver/config"
	"istio.io/istio/mixer/adapter/stackdriver/helper"
	contextgraph "istio.io/istio/mixer/adapter/stackdriver/internal/cloud.google.com/go/contextgraph/apiv1alpha1"
	contextgraphpb "istio.io/istio/mixer/adapter/stackdriver/internal/google.golang.org/genproto/googleapis/cloud/contextgraph/v1alpha1"
	env "istio.io/istio/mixer/pkg/adapter/test"
	edgepb "istio.io/istio/mixer/template/edge"
)

type mockMG struct {
	GMCalled bool
}

func (m *mockMG) GenerateMetadata() helper.Metadata {
	m.GMCalled = true
	return helper.Metadata{}
}

func TestNewBuilder(t *testing.T) {
	var mg *mockMG
	b := NewBuilder(mg).(*builder)
	if mg != b.mg {
		t.Error("NewBuilder did not set passed mg.")
	}
}

func TestSetAdapterConfig(t *testing.T) {
	c := &config.Params{
		ProjectId: "myid",
	}
	mg := &mockMG{
		GMCalled: false,
	}
	b := NewBuilder(mg).(*builder)
	b.SetAdapterConfig(c)

	if b.projectID != c.ProjectId {
		t.Error("Expected builder projectID to be set, wasn't")
	}

	if mg.GMCalled == false {
		t.Error("Expected GenerateMetadata to be called, wasn't")
	}
}

type mockLogger struct{}

func (m mockLogger) Infof(format string, args ...interface{}) {}

func (m mockLogger) Warningf(format string, args ...interface{}) {}

func (m mockLogger) Errorf(format string, args ...interface{}) error { return nil }

func (m mockLogger) Debugf(format string, args ...interface{}) {}

func (m mockLogger) InfoEnabled() bool { return false }

func (m mockLogger) WarnEnabled() bool { return false }

func (m mockLogger) ErrorEnabled() bool { return false }

func (m mockLogger) DebugEnabled() bool { return false }

type mockNC struct {
	NCCalled bool
}

func (m *mockNC) NewClient(ctx context.Context, opts ...option.ClientOption) (*contextgraph.Client, error) {
	m.NCCalled = true
	return nil, nil
}

func TestBuild(t *testing.T) {
	m := &mockNC{
		NCCalled: false,
	}
	b := &builder{
		newClient: m.NewClient,
		projectID: "myid",
		zone:      "myzone",
		cluster:   "mycluster",
		cfg:       &config.Params{ProjectId: "myid"},
	}

	mEnv := env.NewEnv(t)

	han, err := b.Build(context.TODO(), mEnv)
	h := han.(*handler)
	if err != nil {
		t.Errorf("Build returned unexpected err: %v", err)
	}

	// ScheduleDaemon should have been called, need to signal
	// h.cacheAndSend to exit
	h.quit <- 0
	done := mEnv.GetDoneChan()

	timeout := make(chan bool, 1)
	go func() {
		time.Sleep(1 * time.Second)
		timeout <- true
	}()

	select {
	case <-done:
		// Good
	case <-timeout:
		t.Error("Builder did not correctly ScheduleDaemon, or h.cacheAndSend did not exit")
	}

	if m.NCCalled == false {
		t.Error("Expected NewClient to be called, wasn't")
	}

	if h.meshUID != "myid/myzone/mycluster" {
		t.Errorf("Expected mesh uid to be : myid/myzone/mycluster, got: %s", h.meshUID)
	}
}

func TestBuildWithMeshID(t *testing.T) {
	m := &mockNC{}
	b := &builder{
		newClient: m.NewClient,
		projectID: "myid",
		zone:      "myzone",
		cluster:   "mycluster",
		cfg:       &config.Params{ProjectId: "myid", MeshUid: "what-a-mesh"},
	}

	mEnv := env.NewEnv(t)

	han, err := b.Build(context.TODO(), mEnv)
	h := han.(*handler)
	if err != nil {
		t.Errorf("Build returned unexpected err: %v", err)
	}

	if got, want := h.meshUID, "what-a-mesh"; got != want {
		t.Errorf("handler.meshUID: got %q, want %q", got, want)
	}
}

func TestHandleEdge(t *testing.T) {
	h := &handler{
		traffics:  make(chan trafficAssertion, 1),
		meshUID:   "meshid",
		projectID: "projid",
		zone:      "zone",
		cluster:   "clus",
	}
	i := []*edgepb.Instance{
		{
			SourceUid:                    "foo/ns/podname",
			SourceOwner:                  "foo/ns/deploy/name",
			SourceWorkloadName:           "name",
			SourceWorkloadNamespace:      "ns",
			DestinationUid:               "foo/ns/podname",
			DestinationOwner:             "foo/ns/deploy/name",
			DestinationWorkloadName:      "name",
			DestinationWorkloadNamespace: "ns",
			ContextProtocol:              "tcp",
			ApiProtocol:                  "Unknown",
			Timestamp:                    time.Unix(1337, 0),
		},
	}
	if err := h.HandleEdge(context.TODO(), i); err != nil {
		t.Errorf("HandleEdge returned unexpected err: %v", err)
	}
	ta := <-h.traffics
	if ta.contextProtocol != "tcp" {
		t.Errorf("Context Protocol wrong, expected tcp, got: %s", ta.contextProtocol)
	}
}

type mockClient struct {
	Request  *contextgraphpb.AssertBatchRequest
	ABCalled bool
}

func (m *mockClient) fakeAssertBatch(
	ctx context.Context,
	req *contextgraphpb.AssertBatchRequest,
	opts ...gax.CallOption) (*contextgraphpb.AssertBatchResponse, error) {
	m.Request = req
	m.ABCalled = true
	return nil, nil
}

func TestSend(t *testing.T) {
	tChan := make(chan time.Time)
	m := &mockClient{
		ABCalled: false,
	}
	h := &handler{
		traffics: make(chan trafficAssertion),
		sendTick: &time.Ticker{
			C: tChan,
		},
		quit:        make(chan int),
		env:         env.NewEnv(t),
		entityCache: newEntityCache(mockLogger{}),
		edgeCache:   newEdgeCache(mockLogger{}),
		assertBatch: m.fakeAssertBatch,
	}

	go h.cacheAndSend(context.Background())
	h.traffics <- trafficAssertion{}
	tChan <- time.Now()
	h.quit <- 0

	if m.ABCalled == false {
		t.Fatalf("AssertBatch expected to be called, wasn't")
	}

	if m.Request.EntityPresentAssertions[0].Entity.ContainerFullName != "//cloudresourcemanager.googleapis.com/projects/" {
		t.Errorf("ContainerFullName incorrect, want: //cloudresourcemanager.googleapis.com/projects/ got: %v",
			m.Request.EntityPresentAssertions[0].Entity.ContainerFullName)
	}
}
