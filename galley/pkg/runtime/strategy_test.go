//  Copyright 2018 Istio Authors
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
	"testing"
	"time"
)

func TestPublishingStrategy_OnChange(t *testing.T) {
	s := newPublishingStrategyWithDefaults()

	t0 := time.Now()
	t1 := t0.Add(time.Second)

	now := t0
	s.nowFn = func() time.Time { return now }

	s.afterFuncFn = func(d time.Duration, fn func()) *time.Timer {
		// Use a very long time and an empty fn to avoid firing.
		return time.AfterFunc(d, func() {})
	}

	s.onChange()
	if s.firstEvent != t0 || s.latestEvent != t0 || s.timer == nil {
		t.Fatalf("Unexpected internal state: %+v", s)
	}

	// Call change again to see that firstEvent is not changed.
	now = t1
	s.onChange()
	if s.firstEvent != t0 || s.latestEvent != t1 || s.timer == nil {
		t.Fatalf("Unexpected internal state: %+v", s)
	}
}

func TestPublishingStrategy_OnTimer(t *testing.T) {
	s := newPublishingStrategyWithDefaults()

	// Capture t0, as a constant time.
	t0 := time.Now()
	var t1 time.Time // t1 will be captured later

	now := t0
	s.nowFn = func() time.Time { return now }

	var registeredFn func()
	s.afterFuncFn = func(d time.Duration, fn func()) *time.Timer {
		t1 = t0.Add(d)
		registeredFn = fn
		return time.AfterFunc(d, func() {})
	}

	s.onChange()

	// Simulate call of onTimer w/o quiesce or max timeout
	now = t1
	registeredFn()

	published := false
	select {
	case <-s.publish:
		published = true
	default:
	}
	if published {
		t.Fatal("strategy shouldn't have signalled to publish")
	}
}

func TestPublishingStrategy_OnTimer_Quiesce(t *testing.T) {
	s := newPublishingStrategyWithDefaults()

	// Capture t0, as a constant time.
	t0 := time.Now()

	now := t0
	s.nowFn = func() time.Time { return now }

	var registeredFn func()
	s.afterFuncFn = func(d time.Duration, fn func()) *time.Timer {
		registeredFn = fn
		return time.AfterFunc(d, func() {})
	}

	s.onChange()

	// Simulate quiesce
	now = t0.Add(defaultQuiesceDuration).Add(time.Nanosecond)
	registeredFn()

	published := false
	select {
	case <-s.publish:
		published = true
	default:
	}
	if !published {
		t.Fatal("strategy should have signalled to publish")
	}
}

func TestPublishingStrategy_OnTimer_MaxTimeout(t *testing.T) {
	s := newPublishingStrategyWithDefaults()

	// Capture t0, as a constant time.
	t0 := time.Now()

	now := t0
	s.nowFn = func() time.Time { return now }

	s.afterFuncFn = func(d time.Duration, fn func()) *time.Timer {
		return time.AfterFunc(d, func() {})
	}

	s.onChange()

	tEnd := now.Add(defaultMaxWaitDuration)
	// Imitate incoming events & timer fires upto the point of max wait time timeout.
	for ; now.Add(defaultTimerFrequency).Before(tEnd); now = now.Add(defaultTimerFrequency) {
		s.onChange()
		s.onTimer()

		published := false
		select {
		case <-s.publish:
			published = true
		default:
		}
		if published {
			t.Fatal("strategy should have not signalled to publish")
		}

	}

	now = now.Add(defaultTimerFrequency).Add(time.Nanosecond)
	s.onTimer()

	// There should be a publish now
	published := false
	select {
	case <-s.publish:
		published = true
	default:
	}
	if !published {
		t.Fatal("strategy should have signalled to publish")
	}
}

func TestPublishingStrategy_Reset(t *testing.T) {
	s := newPublishingStrategyWithDefaults()

	// Capture t0, as a constant time.
	t0 := time.Now()

	now := t0
	s.nowFn = func() time.Time { return now }

	s.afterFuncFn = func(d time.Duration, fn func()) *time.Timer {
		return time.AfterFunc(d, func() {})
	}

	s.onChange()

	s.reset()
	if s.timer != nil {
		t.Fatal("timer should have been stopped")
	}
	// Should not crash.
	s.reset()
}

func TestPublishingStrategy_DeadlockAvoidance(t *testing.T) {
	s := newPublishingStrategyWithDefaults()

	// Capture t0, as a constant time.
	t0 := time.Now()

	now := t0
	s.nowFn = func() time.Time { return now }

	s.afterFuncFn = func(d time.Duration, fn func()) *time.Timer {
		return time.AfterFunc(d, func() {})
	}

	s.onChange()

	now = now.Add(defaultMaxWaitDuration)
	s.onTimer()

	// Do not drain the publish channel

	s.onChange()

	now = now.Add(defaultMaxWaitDuration)
	s.onTimer()

	// Go through a locking operation
	s.onChange()
}
