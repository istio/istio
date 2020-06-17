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

package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	cfgpb "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/runtime/config/constant"
	"istio.io/istio/mixer/pkg/runtime/testing/data"
	"istio.io/pkg/attribute"
	"istio.io/pkg/pool"
	"istio.io/pkg/probe"
)

var egp = pool.NewGoroutinePool(1, true)
var hgp = pool.NewGoroutinePool(1, true)
var adapters = data.BuildAdapters(nil)
var templates = data.BuildTemplates(nil)

func TestRuntime_Basic(t *testing.T) {
	s := &mockStore{}

	rt := New(
		s,
		templates,
		adapters, "istio-system",
		egp,
		hgp,
		true,
		[]string{metav1.NamespaceAll},
	)

	d := rt.Dispatcher()
	if d == nil {
		t.Fatalf("Dispatcher is nil")
	}

	err := rt.StartListening()
	if err != nil {
		t.Fatalf("error at StartListening: %v", err)
	}

	err = rt.StartListening()
	if err == nil {
		t.Fatal("should have returned error when trying to listen twice.")
	}

	if !s.watchCalled {
		t.Fatal("should have started listening to the store.")
	}

	rt.StopListening()

	// Do not attempt a restart as Store documentation calls out

	s.watchCalled = false
	err = rt.StartListening()
	if err != nil {
		t.Fatalf("error during 2nd StartListening: %v", err)
	}

	if !s.watchCalled {
		t.Fatal("watch was not called during 2nd StartListening")
	}
}

func TestRuntime_ErrorDuringWatch(t *testing.T) {
	s := &mockStore{}
	s.watchErrorToReturn = errors.New("error during watch")

	rt := New(
		s,
		templates,
		adapters, "istio-system",
		egp,
		hgp,
		true,
		[]string{metav1.NamespaceAll},
	)

	err := rt.StartListening()
	if err == nil {
		t.Fatal("expected error during StartListening was not received")
	}
}

func TestRuntime_OnConfigChange(t *testing.T) {
	s := &mockStore{
		listResultToReturn: map[store.Key]*store.Resource{},
	}

	rt := New(
		s,
		templates,
		adapters, "istio-system",
		egp,
		hgp,
		true,
		[]string{metav1.NamespaceAll},
	)

	err := rt.StartListening()
	if err != nil {
		t.Fatalf("error at StartListening: %v", err)
	}

	events := []*store.Event{
		{
			Type: store.Update,
			Key:  store.Key{Kind: constant.AttributeManifestKind, Name: "attrs"},
			Value: &store.Resource{
				Spec: &cfgpb.AttributeManifest{
					Name: "attrs",
					Attributes: map[string]*cfgpb.AttributeManifest_AttributeInfo{
						"foo": {
							ValueType: cfgpb.STRING,
						},
					},
				},
			},
		},
	}
	rt.onConfigChange(events)

	snapshot, _ := rt.ephemeral.BuildSnapshot()

	// expect the newly declared attribute to be received by the ephemeral state of the runtime, as part
	// of listening.
	expected := `
ID: 3
TemplatesStatic:
  Name: tapa
  Name: tcheck
  Name: tcheckoutput
  Name: thalt
  Name: tquota
  Name: treport
AdaptersStatic:
  Name: acheck
  Name: acheckoutput
  Name: apa
  Name: aquota
  Name: areport
HandlersStatic:
InstancesStatic:
Rules:
Attributes:
  foo: STRING
  prefix.generated.string: STRING
  value: STRING
`
	if strings.TrimSpace(expected) != strings.TrimSpace(snapshot.String()) {
		t.Fatalf("snapshot mismatch. got:\n%v\n, wanted:\n%v\n", snapshot, expected)

	}
}

func TestRuntime_InFlightRequestsDuringConfigChange(t *testing.T) {
	s := &mockStore{
		listResultToReturn: map[store.Key]*store.Resource{},
	}

	l := data.Logger{}
	commenceCh := make(chan struct{})
	receiveCh := make(chan struct{})

	adapters := data.BuildAdapters(&l)
	templates := data.BuildTemplates(&l, data.FakeTemplateSettings{
		Name: "tcheck", CommenceSignalChannel: commenceCh, ReceivedCallChannel: receiveCh})
	rt := New(
		s,
		templates,
		adapters, "istio-system",
		egp,
		hgp,
		true,
		[]string{metav1.NamespaceAll},
	)

	err := rt.StartListening()
	if err != nil {
		t.Fatalf("error at StartListening: %v", err)
	}

	// create a basic set of config entries using update events. There is a handler and a rule for check.
	events := []*store.Event{
		{
			Type: store.Update,
			Key:  store.Key{Kind: constant.AttributeManifestKind, Name: "attrs"},
			Value: &store.Resource{
				Spec: &cfgpb.AttributeManifest{
					Attributes: map[string]*cfgpb.AttributeManifest_AttributeInfo{
						"identityAttr": {
							ValueType: cfgpb.STRING,
						},
					},
				},
			},
		},
		{
			Type: store.Update,
			Key:  store.Key{Kind: "acheck", Name: "hcheck", Namespace: "istio-system"},
			Value: &store.Resource{
				Spec: &types.Struct{},
			},
		},
		{
			Type: store.Update,
			Key:  store.Key{Kind: "tcheck", Name: "icheck", Namespace: "istio-system"},
			Value: &store.Resource{
				Spec: &types.Struct{},
			},
		},
		{
			Type: store.Update,
			Key:  store.Key{Kind: "rule", Name: "rule1", Namespace: "istio-system"},
			Value: &store.Resource{
				Spec: &cfgpb.Rule{
					Actions: []*cfgpb.Action{
						{
							Handler: "hcheck.acheck.istio-system",
							Instances: []string{
								"icheck.tcheck.istio-system",
							},
						},
					},
				},
			},
		},
	}

	// publish the events to the runtime.
	rt.onConfigChange(events)

	// start a dispatch session, which will block until we signal it to commence.
	bag := attribute.GetMutableBagForTesting(map[string]interface{}{
		"identityAttr": "svc.istio-system",
	})
	callComplete := false
	callCompleteCh := make(chan struct{})
	callErr := errors.New("call haven't completed yet")
	go func() {
		_, callErr = rt.Dispatcher().Check(context.Background(), bag)

		callComplete = true
		callCompleteCh <- struct{}{}
	}()

	// wait until the template signals that the call is received. The call will be blocked until we signal it.
	<-receiveCh

	// force a config change and unloading of the handler.
	events = []*store.Event{
		{
			Type: store.Delete,
			Key:  store.Key{Kind: "acheck", Name: "hcheck", Namespace: "istio-system"},
		},
		{
			Type: store.Delete,
			Key:  store.Key{Kind: "tcheck", Name: "icheck", Namespace: "istio-system"},
		},
		{
			Type: store.Delete,
			Key:  store.Key{Kind: "rule", Name: "rule1", Namespace: "istio-system"},
		},
	}
	rt.onConfigChange(events)

	// wait longer than the cleanup algorithm's wait period.
	time.Sleep(time.Second * 10)

	if callComplete {
		t.Fatal("Call shouldn't have completed before it is released by the framework")
	}

	// signal template the complete the call.
	commenceCh <- struct{}{}

	// wait until the call fully completes.
	<-callCompleteCh

	// the call should have completed without any errors.
	if callErr != nil {
		t.Fatalf("There shouldn't be an error returned from call: %v", callErr)
	}
}

type mockStore struct {
	initKinds            map[string]proto.Message
	initErrorToReturn    error
	watchChannelToReturn chan store.Event
	watchErrorToReturn   error
	listResultToReturn   map[store.Key]*store.Resource

	initCalled  bool
	watchCalled bool
	listCalled  bool
}

var _ store.Store = &mockStore{}

func (m *mockStore) Stop() {
}

func (m *mockStore) Init(kinds map[string]proto.Message) error {
	m.initCalled = true
	m.initKinds = kinds

	return m.initErrorToReturn
}

func (m *mockStore) WaitForSynced(time.Duration) error {
	return nil
}

// Watch creates a channel to receive the events. A store can conduct a single
// watch channel at the same time. Multiple calls lead to an error.
func (m *mockStore) Watch() (<-chan store.Event, error) {
	m.watchCalled = true

	return m.watchChannelToReturn, m.watchErrorToReturn
}

// Get returns a resource's spec to the key.
func (m *mockStore) Get(key store.Key) (*store.Resource, error) {
	return nil, nil
}

// List returns the whole mapping from key to resource specs in the store.
func (m *mockStore) List() map[store.Key]*store.Resource {
	m.listCalled = true
	return m.listResultToReturn
}

func (m *mockStore) RegisterProbe(c probe.Controller, name string) {

}

type mockProto struct {
}

var _ proto.Message = &mockProto{}

func (m *mockProto) Reset()         {}
func (m *mockProto) String() string { return "" }
func (m *mockProto) ProtoMessage()  {}
