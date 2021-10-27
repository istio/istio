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
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/config/event"
	data2 "istio.io/istio/pkg/config/legacy/testing/data"
	fixtures2 "istio.io/istio/pkg/config/legacy/testing/fixtures"
)

func TestHandlers_Handle_Zero(t *testing.T) {
	g := NewWithT(t)
	hs := &event.Handlers{}
	g.Expect(hs.Size()).To(Equal(0))

	hs.Handle(data2.Event1Col1AddItem1)
}

func TestHandlers_Handle_One(t *testing.T) {
	g := NewWithT(t)

	hs := &event.Handlers{}

	h1 := &fixtures2.Accumulator{}
	hs.Add(h1)
	g.Expect(hs.Size()).To(Equal(1))

	hs.Handle(data2.Event1Col1AddItem1)

	g.Expect(h1.Events()).To(HaveLen(1))
	g.Expect(h1.Events()[0]).To(Equal(data2.Event1Col1AddItem1))
}

func TestHandlers_Handle_Multiple(t *testing.T) {
	g := NewWithT(t)

	hs := &event.Handlers{}

	h1 := &fixtures2.Accumulator{}
	hs.Add(h1)

	h2 := &fixtures2.Accumulator{}
	hs.Add(h2)
	g.Expect(hs.Size()).To(Equal(2))

	hs.Handle(data2.Event1Col1AddItem1)

	g.Expect(h1.Events()).To(HaveLen(1))
	g.Expect(h1.Events()[0]).To(Equal(data2.Event1Col1AddItem1))

	g.Expect(h2.Events()).To(HaveLen(1))
	g.Expect(h2.Events()[0]).To(Equal(data2.Event1Col1AddItem1))
}

func TestHandlers_Handle_Multiple_MultipleEvents(t *testing.T) {
	g := NewWithT(t)

	hs := &event.Handlers{}

	h1 := &fixtures2.Accumulator{}
	hs.Add(h1)

	h2 := &fixtures2.Accumulator{}
	hs.Add(h2)

	hs.Handle(data2.Event1Col1AddItem1)
	hs.Handle(data2.Event2Col1AddItem2)

	expected := []event.Event{data2.Event1Col1AddItem1, data2.Event2Col1AddItem2}

	g.Expect(h1.Events()).To(Equal(expected))
	g.Expect(h2.Events()).To(Equal(expected))
}

func TestHandlers_CombineHandlers_SentinelFirst(t *testing.T) {
	g := NewWithT(t)

	h1 := event.SentinelHandler()
	h2 := &fixtures2.Accumulator{}
	hs := event.CombineHandlers(h1, h2)

	g.Expect(hs).To(BeAssignableToTypeOf(&fixtures2.Accumulator{}))

	hs.Handle(data2.Event1Col1AddItem1)
	hs.Handle(data2.Event2Col1AddItem2)

	expected := []event.Event{data2.Event1Col1AddItem1, data2.Event2Col1AddItem2}

	g.Expect(h2.Events()).To(Equal(expected))
}

func TestHandlers_CombineHandlers_SentinelSecond(t *testing.T) {
	g := NewWithT(t)

	h1 := &fixtures2.Accumulator{}
	h2 := event.SentinelHandler()
	hs := event.CombineHandlers(h1, h2)

	g.Expect(hs).To(BeAssignableToTypeOf(&fixtures2.Accumulator{}))

	hs.Handle(data2.Event1Col1AddItem1)
	hs.Handle(data2.Event2Col1AddItem2)

	expected := []event.Event{data2.Event1Col1AddItem1, data2.Event2Col1AddItem2}

	g.Expect(h1.Events()).To(Equal(expected))
}
