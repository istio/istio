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

package strategy

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"
)

func TestStrategy_StartStop(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewDebounce(&NoopReporter{}, time.Minute, time.Millisecond*500)

	called := false
	s.Start(func() {
		called = true
	})
	s.Stop()

	s.Start(func() {
		called = true
	})
	s.Stop()

	g.Expect(called).To(BeFalse())
}

func TestStrategy_DoubleStart(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewDebounce(&NoopReporter{}, time.Minute, time.Millisecond*500)

	called := false
	s.Start(func() {
		called = true
	})
	s.Start(func() {
		called = true
	})
	s.Stop()

	g.Expect(called).To(BeFalse())
}

func TestStrategy_DoubleStop(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewDebounce(&NoopReporter{}, time.Hour, time.Millisecond*500)

	called := false
	s.Start(func() {
		called = true
	})
	s.Stop()
	s.Stop()

	g.Expect(called).To(BeFalse())
}

func TestStrategy_ChangeBeforeStart(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewDebounce(&NoopReporter{}, time.Millisecond*100, time.Millisecond)
	s.OnChange()
	s.OnChange()

	called := false
	s.Start(func() {
		called = true
	})
	defer s.Stop()

	time.Sleep(time.Millisecond * 500)
	g.Expect(called).To(BeFalse())
}

func TestStrategy_FireEvent(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewDebounce(&NoopReporter{}, time.Millisecond*200, time.Millisecond*100)

	called := false
	s.Start(func() {
		called = true
	})
	defer s.Stop()

	s.OnChange()

	time.Sleep(time.Millisecond * 210)
	g.Expect(called).To(BeTrue())
}

func TestStrategy_StopBeforeMaxTimeout(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewDebounce(&NoopReporter{}, time.Second*5, time.Second)

	called := false
	s.Start(func() {
		called = true
	})

	s.OnChange()
	time.Sleep(time.Millisecond * 200)
	s.Stop()

	g.Expect(called).To(BeFalse())
}

func TestStrategy_ChangeBeforeQuiesce(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewDebounce(&NoopReporter{}, time.Second*5, time.Second)

	called := false
	s.Start(func() {
		called = true
	})

	s.OnChange()
	time.Sleep(time.Millisecond * 10)
	s.OnChange()

	s.Stop()

	g.Expect(called).To(BeFalse())
}

func TestStrategy_MaxTimeout(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewDebounce(&NoopReporter{}, time.Second, time.Millisecond*500)

	called := false
	s.Start(func() {
		called = true
	})

	for i := 0; i < 120; i++ {
		s.OnChange()
		time.Sleep(time.Millisecond * 10)
	}
	s.Stop()

	g.Expect(called).To(BeTrue())
}

func TestStrategy_NewWithDefaults(t *testing.T) {
	g := NewGomegaWithT(t)
	s := NewDebounceWithDefaults(&NoopReporter{})
	g.Expect(s.quiesceDuration).To(Equal(defaultQuiesceDuration))
	g.Expect(s.maxWaitDuration).To(Equal(defaultMaxWaitDuration))
}

func TestDrainCh(t *testing.T) {
	ch := make(chan struct{}, 1)
	ch <- struct{}{}

	drainCh(ch)
	select {
	case <-ch:
		t.Fail()
	default:
	}
}

func TestDrainCh_Empty(t *testing.T) {
	ch := make(chan struct{}, 1)
	drainCh(ch)
	// Does not block or crash
}

func TestDrainCh_Closed(t *testing.T) {
	ch := make(chan struct{}, 1)
	ch <- struct{}{}
	close(ch)
	drainCh(ch)
	// Does not block or crash
}

func TestDrainTimeCh(t *testing.T) {
	ch := make(chan time.Time, 1)
	ch <- time.Now()

	drainTimeCh(ch)
	select {
	case <-ch:
		t.Fail()
	default:
	}
}

func TestDrainTimeCh_Empty(t *testing.T) {
	ch := make(chan time.Time, 1)
	drainTimeCh(ch)
	// Does not block or crash
}

func TestDrainTimeCh_Closed(t *testing.T) {
	ch := make(chan time.Time, 1)
	ch <- time.Now()
	close(ch)
	drainTimeCh(ch)
	// Does not block or crash
}
