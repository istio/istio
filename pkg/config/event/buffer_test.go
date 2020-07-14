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

package event_test

import (
	"sync/atomic"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/testing/data"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
	"istio.io/istio/pkg/config/event"
)

func TestBuffer_Basics(t *testing.T) {
	g := NewGomegaWithT(t)

	s := &fixtures.Source{}
	acc := &fixtures.Accumulator{}

	b := event.WithBuffer(s)
	b.Dispatch(acc)

	s.Handlers.Handle(data.Event1Col1AddItem1)

	go b.Process()

	g.Eventually(acc.Events).Should(HaveLen(1))
	g.Eventually(acc.Events).Should(ContainElement(data.Event1Col1AddItem1))

	b.Stop()
}

func TestBuffer_Clear(t *testing.T) {
	g := NewGomegaWithT(t)

	s := &fixtures.Source{}
	acc := &fixtures.Accumulator{}

	b := event.WithBuffer(s)
	b.Dispatch(acc)

	s.Handlers.Handle(data.Event1Col1AddItem1)

	b.Clear()
	g.Consistently(acc.Events).Should(HaveLen(0))

	b.Stop()
}

func TestBuffer_DoubleProcess(t *testing.T) {
	g := NewGomegaWithT(t)

	s := &fixtures.Source{}
	acc := &fixtures.Accumulator{}

	b := event.WithBuffer(s)
	b.Dispatch(acc)

	s.Handlers.Handle(data.Event1Col1AddItem1)

	var ready int32
	var cnt int32
	go func() {
		atomic.AddInt32(&cnt, 1)
		atomic.AddInt32(&ready, 1)
		b.Process()
		atomic.AddInt32(&cnt, -1)
	}()
	go func() {
		atomic.AddInt32(&cnt, 1)
		atomic.AddInt32(&ready, 1)
		b.Process()
		atomic.AddInt32(&cnt, -1)
	}()

	// Wait for cnt to update.
	g.Eventually(func() int32 { return atomic.LoadInt32(&ready) }).Should(Equal(int32(2)))

	// Only one of the process calls should keep executing.
	g.Eventually(func() int32 { return atomic.LoadInt32(&cnt) }).Should(Equal(int32(1)))

	// Both process calls should exit.
	b.Stop()
	g.Eventually(func() int32 { return atomic.LoadInt32(&cnt) }).Should(Equal(int32(0)))
}

func TestBuffer_Stress(t *testing.T) {
	g := NewGomegaWithT(t)

	s := &fixtures.Source{}
	acc := &fixtures.Accumulator{}

	b := event.WithBuffer(s)
	b.Dispatch(acc)

	go b.Process()
	time.Sleep(time.Millisecond) // Yield

	var pumps int32
	var cnt int32
	var done int32
	pump := func() {
		atomic.AddInt32(&pumps, 1)
		for {
			b.Handle(data.Event1Col1AddItem1)
			atomic.AddInt32(&cnt, 1)
			if atomic.LoadInt32(&done) != 0 {
				break
			}
		}
		atomic.AddInt32(&pumps, -1)
	}

	for i := 0; i < 100; i++ {
		go pump()
	}

	g.Eventually(func() int32 { return atomic.LoadInt32(&pumps) }).Should(Equal(int32(100)))
	time.Sleep(time.Second) // Let the pumps run for a second.
	atomic.StoreInt32(&done, 1)
	g.Eventually(func() int32 { return atomic.LoadInt32(&pumps) }).Should(Equal(int32(0)))
	t.Logf("Pumped '%d' events.", atomic.LoadInt32(&cnt))
	g.Eventually(acc.Events, time.Second*5, time.Millisecond*100).Should(HaveLen(int(atomic.LoadInt32(&cnt))))
	b.Stop()
}
