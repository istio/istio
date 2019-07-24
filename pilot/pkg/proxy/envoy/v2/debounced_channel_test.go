// Copyright 2019 Istio Authors
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

package v2_test

import (
	"math/rand"
	"testing"
	"time"

	v2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
)

func newBoolDebouncedChannel(debounceAfter time.Duration, debounceMax time.Duration, initialAccumulator bool,
	merge func(accumulator bool, value bool) bool) *v2.DebouncedChannel {

	return v2.NewDebouncedChannel(debounceAfter, debounceMax, initialAccumulator,
		func(accumulator v2.DebouncedEvent, value v2.DebouncedEvent) v2.DebouncedEvent {
			return merge(accumulator.(bool), value.(bool))
		})
}

func TestDebouncedChannelSimple(t *testing.T) {

	dc := newBoolDebouncedChannel(0, 0, true, func(accumulator bool, value bool) bool {
		return accumulator || value
	})

	expectNoEvent(t, dc)

	dc.Enqueue(true)

	expectEvent(t, dc, true)

	expectNoEvent(t, dc)
}

func TestDebouncedChannelMultiple(t *testing.T) {

	dc := newBoolDebouncedChannel(0, 0, false, func(accumulator bool, value bool) bool {
		return accumulator || value
	})

	expectNoEvent(t, dc)

	// first
	dc.Enqueue(false)

	// second
	dc.Enqueue(true)
	dc.Enqueue(false)

	expectEvent(t, dc, false)
	expectEvent(t, dc, true)

	expectNoEvent(t, dc)
}

func TestDebouncedChannelMultipleWithDebounce(t *testing.T) {

	dc := newBoolDebouncedChannel(100*time.Millisecond, 1*time.Hour, false, func(accumulator bool, value bool) bool {
		return accumulator || value
	})

	expectNoEvent(t, dc)

	dc.Enqueue(false)
	dc.Enqueue(true)
	dc.Enqueue(false)

	expectNoEvent(t, dc)

	expectEventBlocking(t, dc, true)

	expectNoEvent(t, dc)
}

func TestDebouncedChannelMultipleWithDebounceMax(t *testing.T) {

	dc := newBoolDebouncedChannel(100*time.Hour, 100*time.Millisecond, false, func(accumulator bool, value bool) bool {
		return accumulator || value
	})

	expectNoEvent(t, dc)

	dc.Enqueue(false)
	dc.Enqueue(true)
	dc.Enqueue(false)

	expectNoEvent(t, dc)

	expectEventBlocking(t, dc, true)

	expectNoEvent(t, dc)
}

func TestDebouncedChannelReentrant(t *testing.T) {

	const cycles = 1
	stop := make(chan bool)

	dc := newBoolDebouncedChannel(10*time.Millisecond, 100*time.Millisecond, false, func(accumulator bool, value bool) bool {
		return accumulator || value
	})

	go func() {
		for {
			dc.Enqueue(true)
			time.Sleep(time.Duration(rand.Int63n(110)) * time.Millisecond)
			select {
			case <-stop:
				return
			default:
				break
			}
		}
	}()

	for i := 0; i < cycles; i++ {
		select {
		case notification := <-dc.C:
			time.Sleep(time.Duration(rand.Int63n(110)) * time.Millisecond)
			notification.Ack()
			break
		case <-time.After(2 * time.Second):
			t.Fatalf("No event received within 2 seconds")
		}
	}
	stop <- true

}

func expectNoEvent(t *testing.T, dc *v2.DebouncedChannel) {
	select {
	case <-dc.C:
		t.Fatalf("unexpected event emitted")
	default:
		// ok
	}
}

func expectEvent(t *testing.T, dc *v2.DebouncedChannel, expected bool) {
	actual := !expected
	select {
	case notification := <-dc.C:
		actual = notification.Event.(bool)
		notification.Ack()
		break
	default:
		t.Fatalf("No event emitted")
	}
	if actual != expected {
		t.Fatalf("Invalid value for dequeued element: %t", actual)
	}
}

func expectEventBlocking(t *testing.T, dc *v2.DebouncedChannel, expected bool) {
	actual := !expected
	select {
	case notification := <-dc.C:
		actual = notification.Event.(bool)
		notification.Ack()
		break
	case <-time.After(1 * time.Second):
		t.Fatalf("No event emitted (timeout)")
	}
	if actual != expected {
		t.Fatalf("Invalid value for dequeued element: %t", actual)
	}
}
