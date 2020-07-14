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

	"istio.io/istio/galley/pkg/config/testing/basicmeta"
	"istio.io/istio/galley/pkg/config/testing/data"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
	"istio.io/istio/pkg/config/event"
)

func TestRouter_Empty(t *testing.T) {
	s := event.NewRouter()
	// No crash
	s.Handle(data.Event1Col1AddItem1)
	s.Broadcast(data.Event1Col1DeleteItem1)
}

func TestRouter_Single_Handle(t *testing.T) {
	g := NewGomegaWithT(t)

	s := event.NewRouter()
	acc := &fixtures.Accumulator{}
	s = event.AddToRouter(s, basicmeta.K8SCollection1, acc)
	s.Handle(data.Event1Col1AddItem1)

	g.Expect(acc.Events()).To(HaveLen(1))
}

func TestRouter_Single_Handle_AddToNil(t *testing.T) {
	g := NewGomegaWithT(t)

	var s event.Router
	acc := &fixtures.Accumulator{}
	s = event.AddToRouter(s, basicmeta.K8SCollection1, acc)
	s.Handle(data.Event1Col1AddItem1)

	g.Expect(acc.Events()).To(HaveLen(1))
}

func TestRouter_Single_Handle_NoMatch(t *testing.T) {
	g := NewGomegaWithT(t)

	s := event.NewRouter()
	acc := &fixtures.Accumulator{}
	s = event.AddToRouter(s, basicmeta.Collection2, acc)
	s.Handle(data.Event1Col1AddItem1)

	g.Expect(acc.Events()).To(HaveLen(0))
}

func TestRouter_Single_MultiListener(t *testing.T) {
	g := NewGomegaWithT(t)

	s := event.NewRouter()
	acc1 := &fixtures.Accumulator{}
	acc2 := &fixtures.Accumulator{}
	s = event.AddToRouter(s, basicmeta.K8SCollection1, acc1)
	s = event.AddToRouter(s, basicmeta.K8SCollection1, acc2)
	s.Handle(data.Event1Col1AddItem1)

	g.Expect(acc1.Events()).To(HaveLen(1))
	g.Expect(acc2.Events()).To(HaveLen(1))
}

func TestRouter_Single_Broadcast(t *testing.T) {
	g := NewGomegaWithT(t)

	s := event.NewRouter()
	acc := &fixtures.Accumulator{}
	s = event.AddToRouter(s, basicmeta.K8SCollection1, acc)
	s.Broadcast(event.Event{Kind: event.Reset})

	g.Expect(acc.Events()).To(HaveLen(1))
}

func TestRouter_Multi_Handle(t *testing.T) {
	g := NewGomegaWithT(t)

	s := event.NewRouter()
	acc1 := &fixtures.Accumulator{}
	acc2 := &fixtures.Accumulator{}
	acc3 := &fixtures.Accumulator{}
	s = event.AddToRouter(s, basicmeta.K8SCollection1, acc1)
	s = event.AddToRouter(s, basicmeta.Collection2, acc2)
	s = event.AddToRouter(s, data.Foo, acc3)
	s.Handle(data.Event1Col1AddItem1)
	s.Handle(data.Event3Col2AddItem1)

	g.Expect(acc1.Events()).To(HaveLen(1))
	g.Expect(acc2.Events()).To(HaveLen(1))
	g.Expect(acc3.Events()).To(HaveLen(0))
}

func TestRouter_Multi_NoTarget(t *testing.T) {
	g := NewGomegaWithT(t)

	s := event.NewRouter()
	acc1 := &fixtures.Accumulator{}
	acc2 := &fixtures.Accumulator{}
	s = event.AddToRouter(s, basicmeta.K8SCollection1, acc1)
	s = event.AddToRouter(s, data.Foo, acc2)
	s.Handle(data.Event3Col2AddItem1)

	g.Expect(acc1.Events()).To(HaveLen(0))
	g.Expect(acc2.Events()).To(HaveLen(0))
}

func TestRouter_Multi_Broadcast(t *testing.T) {
	g := NewGomegaWithT(t)

	s := event.NewRouter()
	acc1 := &fixtures.Accumulator{}
	acc2 := &fixtures.Accumulator{}
	acc3 := &fixtures.Accumulator{}
	s = event.AddToRouter(s, basicmeta.K8SCollection1, acc1)
	s = event.AddToRouter(s, basicmeta.Collection2, acc2)
	s = event.AddToRouter(s, data.Foo, acc3)
	s.Broadcast(event.Event{Kind: event.Reset})

	g.Expect(acc1.Events()).To(HaveLen(1))
	g.Expect(acc2.Events()).To(HaveLen(1))
	g.Expect(acc3.Events()).To(HaveLen(1))
}

func TestRouter_Multi_Unknown_Panic(t *testing.T) {
	g := NewGomegaWithT(t)

	defer func() {
		r := recover()
		g.Expect(r).ToNot(BeNil())
	}()
	_ = event.AddToRouter(&unknownSelector{}, data.Foo, &fixtures.Accumulator{})
}

type unknownSelector struct{}

var _ event.Router = &unknownSelector{}

func (u *unknownSelector) Handle(event.Event)    {}
func (u *unknownSelector) Broadcast(event.Event) {}
