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

package strategy

import (
	"sync/atomic"
	"testing"
	"time"

	. "github.com/onsi/gomega"
)

func TestStrategy_StartStop(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewDebounce(time.Minute, time.Millisecond*500)

	var called int32
	s.Start(func() {
		atomic.StoreInt32(&called, 1)
	})
	s.Stop()

	s.Start(func() {
		atomic.StoreInt32(&called, 1)
	})
	s.Stop()

	g.Expect(atomic.LoadInt32(&called)).To(Equal(int32(0)))
}

func TestStrategy_DoubleStart(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewDebounce(time.Minute, time.Millisecond*500)

	var called int32
	s.Start(func() {
		atomic.StoreInt32(&called, 1)
	})
	s.Start(func() {
		atomic.StoreInt32(&called, 1)
	})
	s.Stop()

	g.Expect(atomic.LoadInt32(&called)).To(Equal(int32(0)))
}

func TestStrategy_DoubleStop(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewDebounce(time.Hour, time.Millisecond*500)

	var called int32
	s.Start(func() {
		atomic.StoreInt32(&called, 1)
	})
	s.Stop()
	s.Stop()

	g.Expect(atomic.LoadInt32(&called)).To(Equal(int32(0)))
}

func TestStrategy_ChangeBeforeStart(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewDebounce(time.Millisecond*100, time.Millisecond)
	s.OnChange()
	s.OnChange()

	var called int32
	s.Start(func() {
		atomic.StoreInt32(&called, 1)
	})
	defer s.Stop()

	time.Sleep(time.Millisecond * 500)
	g.Expect(atomic.LoadInt32(&called)).To(Equal(int32(0)))
}

func TestStrategy_FireEvent(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewDebounce(time.Millisecond*200, time.Millisecond*100)

	var called int32
	s.Start(func() {
		atomic.StoreInt32(&called, 1)
	})
	defer s.Stop()

	s.OnChange()

	time.Sleep(time.Millisecond * 210)
	g.Expect(atomic.LoadInt32(&called)).To(Equal(int32(1)))
}

func TestStrategy_StopBeforeMaxTimeout(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewDebounce(time.Second*5, time.Second)

	var called int32
	s.Start(func() {
		atomic.StoreInt32(&called, 1)
	})

	s.OnChange()
	time.Sleep(time.Millisecond * 200)
	s.Stop()

	g.Expect(atomic.LoadInt32(&called)).To(Equal(int32(0)))
}

func TestStrategy_ChangeBeforeQuiesce(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewDebounce(time.Second*5, time.Second)

	var called int32
	s.Start(func() {
		atomic.StoreInt32(&called, 1)
	})

	s.OnChange()
	time.Sleep(time.Millisecond * 10)
	s.OnChange()

	s.Stop()

	g.Expect(atomic.LoadInt32(&called)).To(Equal(int32(0)))
}

func TestStrategy_MaxTimeout(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewDebounce(time.Second, time.Millisecond*500)

	var called int32
	s.Start(func() {
		atomic.StoreInt32(&called, 1)
	})

	for i := 0; i < 120; i++ {
		s.OnChange()
		time.Sleep(time.Millisecond * 10)
	}
	s.Stop()

	g.Expect(atomic.LoadInt32(&called)).To(Equal(int32(1)))
}

func TestStrategy_NewWithDefaults(t *testing.T) {
	g := NewGomegaWithT(t)
	s := NewDebounceWithDefaults()
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
