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

package event_test

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/testing/data"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
)

func TestSelector_Empty(t *testing.T) {
	s := event.NewSelector()
	// No crash
	s.Handle(data.Event1Col1AddItem1)
	s.Broadcast(data.Event1Col1DeleteItem1)
}

func TestSelector_Single_Handle(t *testing.T) {
	g := NewGomegaWithT(t)

	s := event.NewSelector()
	acc := &fixtures.Accumulator{}
	s = event.AddToSelector(s, data.Collection1, acc)
	s.Handle(data.Event1Col1AddItem1)

	g.Expect(acc.Events()).To(HaveLen(1))
}

func TestSelector_Single_Handle_AddToNil(t *testing.T) {
	g := NewGomegaWithT(t)

	var s event.Selector
	acc := &fixtures.Accumulator{}
	s = event.AddToSelector(s, data.Collection1, acc)
	s.Handle(data.Event1Col1AddItem1)

	g.Expect(acc.Events()).To(HaveLen(1))
}

func TestSelector_Single_Handle_NoMatch(t *testing.T) {
	g := NewGomegaWithT(t)

	s := event.NewSelector()
	acc := &fixtures.Accumulator{}
	s = event.AddToSelector(s, data.Collection2, acc)
	s.Handle(data.Event1Col1AddItem1)

	g.Expect(acc.Events()).To(HaveLen(0))
}

func TestSelector_Single_MultiListener(t *testing.T) {
	g := NewGomegaWithT(t)

	s := event.NewSelector()
	acc1 := &fixtures.Accumulator{}
	acc2 := &fixtures.Accumulator{}
	s = event.AddToSelector(s, data.Collection1, acc1)
	s = event.AddToSelector(s, data.Collection1, acc2)
	s.Handle(data.Event1Col1AddItem1)

	g.Expect(acc1.Events()).To(HaveLen(1))
	g.Expect(acc2.Events()).To(HaveLen(1))
}

func TestSelector_Single_Broadcast(t *testing.T) {
	g := NewGomegaWithT(t)

	s := event.NewSelector()
	acc := &fixtures.Accumulator{}
	s = event.AddToSelector(s, data.Collection1, acc)
	s.Broadcast(event.Event{Kind: event.Reset})

	g.Expect(acc.Events()).To(HaveLen(1))
}

func TestSelector_Multi_Handle(t *testing.T) {
	g := NewGomegaWithT(t)

	s := event.NewSelector()
	acc1 := &fixtures.Accumulator{}
	acc2 := &fixtures.Accumulator{}
	acc3 := &fixtures.Accumulator{}
	s = event.AddToSelector(s, data.Collection1, acc1)
	s = event.AddToSelector(s, data.Collection2, acc2)
	s = event.AddToSelector(s, data.Collection3, acc3)
	s.Handle(data.Event1Col1AddItem1)
	s.Handle(data.Event3Col2AddItem1)

	g.Expect(acc1.Events()).To(HaveLen(1))
	g.Expect(acc2.Events()).To(HaveLen(1))
	g.Expect(acc3.Events()).To(HaveLen(0))
}

func TestSelector_Multi_NoTarget(t *testing.T) {
	g := NewGomegaWithT(t)

	s := event.NewSelector()
	acc1 := &fixtures.Accumulator{}
	acc2 := &fixtures.Accumulator{}
	s = event.AddToSelector(s, data.Collection1, acc1)
	s = event.AddToSelector(s, data.Collection3, acc2)
	s.Handle(data.Event3Col2AddItem1)

	g.Expect(acc1.Events()).To(HaveLen(0))
	g.Expect(acc2.Events()).To(HaveLen(0))
}

func TestSelector_Multi_Broadcast(t *testing.T) {
	g := NewGomegaWithT(t)

	s := event.NewSelector()
	acc1 := &fixtures.Accumulator{}
	acc2 := &fixtures.Accumulator{}
	acc3 := &fixtures.Accumulator{}
	s = event.AddToSelector(s, data.Collection1, acc1)
	s = event.AddToSelector(s, data.Collection2, acc2)
	s = event.AddToSelector(s, data.Collection3, acc3)
	s.Broadcast(event.Event{Kind: event.Reset})

	g.Expect(acc1.Events()).To(HaveLen(1))
	g.Expect(acc2.Events()).To(HaveLen(1))
	g.Expect(acc3.Events()).To(HaveLen(1))
}

func TestSelector_Multi_Unknown_Panic(t *testing.T) {
	g := NewGomegaWithT(t)

	defer func() {
		r := recover()
		g.Expect(r).ToNot(BeNil())
	}()
	_ = event.AddToSelector(&unknownSelector{}, data.Collection3, &fixtures.Accumulator{})
}

type unknownSelector struct{}

var _ event.Selector = &unknownSelector{}

func (u *unknownSelector) Handle(e event.Event)    {}
func (u *unknownSelector) Broadcast(e event.Event) {}
