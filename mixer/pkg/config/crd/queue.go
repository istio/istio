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

	"istio.io/mixer/pkg/config/store"
)

type eventQueue struct {
	ctx    context.Context
	cancel context.CancelFunc
	chout  chan store.Event
	chin   chan store.Event
}

func newQueue(ctx context.Context, cancel context.CancelFunc) *eventQueue {
	eq := &eventQueue{
		ctx:    ctx,
		cancel: cancel,
		chout:  make(chan store.Event),
		chin:   make(chan store.Event),
	}
	go eq.run()
	return eq
}

func (q *eventQueue) run() {
loop:
	for {
		select {
		case <-q.ctx.Done():
			break loop
		case ev := <-q.chin:
			evs := []store.Event{ev}
			for len(evs) > 0 {
				select {
				case <-q.ctx.Done():
					break loop
				case ev := <-q.chin:
					evs = append(evs, ev)
				case q.chout <- evs[0]:
					evs = evs[1:]
				}
			}
		}
	}
	close(q.chout)
}

func (q *eventQueue) Send(t store.ChangeType, key store.Key) {
	select {
	case <-q.ctx.Done():
	case q.chin <- store.Event{Key: key, Type: t}:
	}
}
