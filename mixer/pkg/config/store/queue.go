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

package store

import (
	"context"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/glog"
)

// The size of the buffer for the outbound channel for the queue.
const choutBufSize = 10

type eventQueue struct {
	ctx   context.Context
	chout chan Event
	chin  <-chan BackendEvent
	kinds map[string]proto.Message
}

func newQueue(ctx context.Context, chin <-chan BackendEvent, kinds map[string]proto.Message) *eventQueue {
	eq := &eventQueue{
		ctx:   ctx,
		chout: make(chan Event, choutBufSize),
		chin:  chin,
		kinds: kinds,
	}
	go eq.run()
	return eq
}

func (q *eventQueue) convertValue(ev BackendEvent) (Event, error) {
	pbSpec, err := cloneMessage(ev.Kind, q.kinds)
	if err != nil {
		return Event{}, err
	}
	if ev.Value == nil {
		return Event{Key: ev.Key, Type: ev.Type}, nil
	}
	if err = convert(ev.Key, ev.Value.Spec, pbSpec); err != nil {
		return Event{}, err
	}
	return Event{Key: ev.Key, Type: ev.Type, Value: &Resource{
		Metadata: ev.Value.Metadata,
		Spec:     pbSpec,
	}}, nil
}

func (q *eventQueue) run() {
loop:
	for {
		select {
		case <-q.ctx.Done():
			break loop
		case ev := <-q.chin:
			converted, err := q.convertValue(ev)
			if err != nil {
				glog.Errorf("Failed to convert %s an event: %v", ev.Key, err)
				break
			}
			evs := []Event{converted}
			for len(evs) > 0 {
				select {
				case <-q.ctx.Done():
					break loop
				case ev := <-q.chin:
					converted, err = q.convertValue(ev)
					if err != nil {
						glog.Errorf("Failed to convert %s an event: %v", ev.Key, err)
						break
					}
					evs = append(evs, converted)
				case q.chout <- evs[0]:
					evs = evs[1:]
				}
			}
		}
	}
	close(q.chout)
}
