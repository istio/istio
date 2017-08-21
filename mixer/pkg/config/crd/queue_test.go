// Copyright 2017 Istio Authors
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

package crd

import (
	"context"
	"fmt"
	"testing"
	"time"

	"istio.io/mixer/pkg/config/store"
)

func TestQueue(t *testing.T) {
	count := 10
	ctx, cancel := context.WithCancel(context.Background())
	q := newQueue(ctx, cancel)
	defer q.cancel()
	donec := make(chan struct{})
	evs := []store.Event{}
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
		q.Send(store.Update, store.Key{Kind: "kind", Namespace: "ns", Name: fmt.Sprintf("%d", i)})
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

func TestQueueSync(t *testing.T) {
	count := 10
	ctx, cancel := context.WithCancel(context.Background())
	q := newQueue(ctx, cancel)
	defer q.cancel()
	for i := 0; i < count; i++ {
		q.Send(store.Update, store.Key{Kind: "kind", Namespace: "ns", Name: fmt.Sprintf("%d", i)})
	}
	for i := 0; i < count; i++ {
		ev := <-q.chout
		if ev.Name != fmt.Sprintf("%d", i) {
			t.Errorf("Got name %s Want %d", ev.Name, i)
		}
	}
}

func TestQueueCancel(t *testing.T) {
	count := 10
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*time.Duration(count)/2)
	q := newQueue(ctx, cancel)
	evs := []store.Event{}
	donec := make(chan struct{})
	go func() {
		for ev := range q.chout {
			evs = append(evs, ev)
		}
		close(donec)
	}()
	for i := 0; i < count; i++ {
		q.Send(store.Update, store.Key{Kind: "kind", Namespace: "ns", Name: fmt.Sprintf("%d", i)})
		time.Sleep(time.Millisecond)
	}
	<-donec
	if len(evs) > count/2 {
		t.Errorf("Got %d, Want <=%d", len(evs), count/2)
	}
}
