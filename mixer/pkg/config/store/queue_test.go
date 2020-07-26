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

package store

import (
	"fmt"
	"testing"

	"github.com/gogo/protobuf/proto"

	cfg "istio.io/api/policy/v1beta1"
)

func TestQueue(t *testing.T) {
	count := 10
	chin := make(chan BackendEvent)
	q := newQueue(chin, map[string]proto.Message{"Handler": &cfg.Handler{}})
	defer close(q.closec)
	donec := make(chan struct{})
	evs := []Event{}
	go func() {
		for ev := range q.chout {
			evs = append(evs, ev)
			if len(evs) >= count {
				break
			}
		}
		close(donec)
	}()
	for i := 0; i < count; i++ {
		chin <- BackendEvent{
			Type: Update,
			Key:  Key{Kind: "Handler", Namespace: "ns", Name: fmt.Sprintf("%d", i)},
			Value: &BackEndResource{
				Spec: map[string]interface{}{
					"name": "h1",
				},
			},
		}
	}
	<-donec
	if len(evs) != count {
		t.Errorf("Got %d Want %d", len(evs), count)
	}
	for i, ev := range evs {
		if ev.Name != fmt.Sprintf("%d", i) {
			t.Errorf("%d: Got name %s Want %d", i, ev.Name, i)
		}
	}
}

func TestQueueFail(t *testing.T) {
	chin := make(chan BackendEvent)
	q := newQueue(chin, map[string]proto.Message{"Handler": &cfg.Handler{}})
	defer close(q.closec)
	chin <- BackendEvent{
		Type:  Update,
		Key:   Key{Kind: "Unknown", Namespace: "ns", Name: "unknown"},
		Value: &BackEndResource{Spec: map[string]interface{}{"foo": "bar"}},
	}
	select {
	case ev := <-q.chout:
		t.Errorf("Got %+v, Want nothing", ev)
	default:
		// pass
	}
	chin <- BackendEvent{
		Type:  Update,
		Key:   Key{Kind: "Handler", Namespace: "ns", Name: "illformed"},
		Value: &BackEndResource{Spec: map[string]interface{}{"foo": "bar"}},
	}
	select {
	case ev := <-q.chout:
		t.Errorf("Got %+v, Want nothing", ev)
	default:
		// pass
	}
}

func TestQueueSync(t *testing.T) {
	count := 10
	chin := make(chan BackendEvent)
	q := newQueue(chin, map[string]proto.Message{"Handler": &cfg.Handler{}})
	defer close(q.closec)
	for i := 0; i < count; i++ {
		chin <- BackendEvent{
			Type:  Update,
			Key:   Key{Kind: "Handler", Namespace: "ns", Name: fmt.Sprintf("%d", i)},
			Value: &BackEndResource{},
		}
	}
	for i := 0; i < count; i++ {
		ev := <-q.chout
		if ev.Name != fmt.Sprintf("%d", i) {
			t.Errorf("Got name %s Want %d", ev.Name, i)
		}
	}
}

func TestQueueCancelClosesOutputChannel(t *testing.T) {
	chin := make(chan BackendEvent)
	q := newQueue(chin, map[string]proto.Message{"Handler": &cfg.Handler{}})
	donec := make(chan struct{})
	go func() {
		for range q.chout {
		}
		close(donec)
	}()
	close(q.closec)
	<-donec
}
