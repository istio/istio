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

package events

import (
	"testing"
	"time"

	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	DefaultTimeout = 2 * time.Second
	DefaultPeriod  = 100 * time.Millisecond
)

var (
	defaultOptions = []retry.Option{retry.Timeout(DefaultTimeout), retry.Delay(DefaultPeriod)}
)

// ChannelHandler creates an EventHandler that adds the event to the provided channel.
func ChannelHandler(ch chan resource.Event) resource.EventHandler {
	return func(e resource.Event) {
		ch <- e
	}
}

// ExpectOne polls the channel and ensures that only a single event is available. Fails the test
// if the number of events != 1.
func ExpectOne(t *testing.T, ch chan resource.Event, options ...retry.Option) resource.Event {
	t.Helper()
	e := Expect(t, ch, options...)

	// Use the default options for checking for none. This is to avoid long delays when the caller
	// increases the polling timeout.
	ExpectNone(t, ch, defaultOptions...)
	return e
}

// Expect polls the channel for the next event and returns it. Fails the test if no event found.
func Expect(t *testing.T, ch chan resource.Event, options ...retry.Option) resource.Event {
	t.Helper()

	e := Poll(ch, options...)
	if e == nil {
		t.Fatalf("timed out waiting for event")
	}
	return *e
}

// ExpectNone polls the channel and fails the test if any events are available.
func ExpectNone(t *testing.T, ch chan resource.Event, options ...retry.Option) {
	t.Helper()
	e := Poll(ch, options...)
	if e != nil {
		t.Fatalf("expected no events, found: %v", e)
	}
}

// Poll polls the channel to see if there is an event waiting. Returns either the next event or nil.
func Poll(ch chan resource.Event, options ...retry.Option) *resource.Event {
	// Add the default options first, then the arguments (if provided). Since options are applied
	// in-order, this allows the defaults to be overridden.
	options = append(append([]retry.Option{}, defaultOptions...), options...)
	e, err := retry.Do(func() (result interface{}, completed bool, err error) {
		select {
		case e := <-ch:
			return &e, true, nil
		default:
			return nil, false, nil
		}
	}, options...)

	if err != nil {
		return nil
	}

	return e.(*resource.Event)
}
