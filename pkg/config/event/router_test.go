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
	basicmeta2 "istio.io/istio/pkg/config/legacy/testing/basicmeta"
	data2 "istio.io/istio/pkg/config/legacy/testing/data"
	fixtures2 "istio.io/istio/pkg/config/legacy/testing/fixtures"
)

func TestRouter_Empty(t *testing.T) {
	s := event.NewRouter()
	// No crash
	s.Handle(data2.Event1Col1AddItem1)
	s.Broadcast(data2.Event1Col1DeleteItem1)
}

func TestRouter_Single_Handle(t *testing.T) {
	g := NewWithT(t)

	s := event.NewRouter()
	acc := &fixtures2.Accumulator{}
	s = event.AddToRouter(s, basicmeta2.K8SCollection1, acc)
	s.Handle(data2.Event1Col1AddItem1)

	g.Expect(acc.Events()).To(HaveLen(1))
}

func TestRouter_Single_Handle_AddToNil(t *testing.T) {
	g := NewWithT(t)

	var s event.Router
	acc := &fixtures2.Accumulator{}
	s = event.AddToRouter(s, basicmeta2.K8SCollection1, acc)
	s.Handle(data2.Event1Col1AddItem1)

	g.Expect(acc.Events()).To(HaveLen(1))
}

func TestRouter_Single_Handle_NoMatch(t *testing.T) {
	g := NewWithT(t)

	s := event.NewRouter()
	acc := &fixtures2.Accumulator{}
	s = event.AddToRouter(s, basicmeta2.Collection2, acc)
	s.Handle(data2.Event1Col1AddItem1)

	g.Expect(acc.Events()).To(HaveLen(0))
}

func TestRouter_Single_MultiListener(t *testing.T) {
	g := NewWithT(t)

	s := event.NewRouter()
	acc1 := &fixtures2.Accumulator{}
	acc2 := &fixtures2.Accumulator{}
	s = event.AddToRouter(s, basicmeta2.K8SCollection1, acc1)
	s = event.AddToRouter(s, basicmeta2.K8SCollection1, acc2)
	s.Handle(data2.Event1Col1AddItem1)

	g.Expect(acc1.Events()).To(HaveLen(1))
	g.Expect(acc2.Events()).To(HaveLen(1))
}

func TestRouter_Single_Broadcast(t *testing.T) {
	g := NewWithT(t)

	s := event.NewRouter()
	acc := &fixtures2.Accumulator{}
	s = event.AddToRouter(s, basicmeta2.K8SCollection1, acc)
	s.Broadcast(event.Event{Kind: event.Reset})

	g.Expect(acc.Events()).To(HaveLen(1))
}

func TestRouter_Multi_Handle(t *testing.T) {
	g := NewWithT(t)

	s := event.NewRouter()
	acc1 := &fixtures2.Accumulator{}
	acc2 := &fixtures2.Accumulator{}
	acc3 := &fixtures2.Accumulator{}
	s = event.AddToRouter(s, basicmeta2.K8SCollection1, acc1)
	s = event.AddToRouter(s, basicmeta2.Collection2, acc2)
	s = event.AddToRouter(s, data2.Foo, acc3)
	s.Handle(data2.Event1Col1AddItem1)
	s.Handle(data2.Event3Col2AddItem1)

	g.Expect(acc1.Events()).To(HaveLen(1))
	g.Expect(acc2.Events()).To(HaveLen(1))
	g.Expect(acc3.Events()).To(HaveLen(0))
}

func TestRouter_Multi_NoTarget(t *testing.T) {
	g := NewWithT(t)

	s := event.NewRouter()
	acc1 := &fixtures2.Accumulator{}
	acc2 := &fixtures2.Accumulator{}
	s = event.AddToRouter(s, basicmeta2.K8SCollection1, acc1)
	s = event.AddToRouter(s, data2.Foo, acc2)
	s.Handle(data2.Event3Col2AddItem1)

	g.Expect(acc1.Events()).To(HaveLen(0))
	g.Expect(acc2.Events()).To(HaveLen(0))
}

func TestRouter_Multi_Broadcast(t *testing.T) {
	g := NewWithT(t)

	s := event.NewRouter()
	acc1 := &fixtures2.Accumulator{}
	acc2 := &fixtures2.Accumulator{}
	acc3 := &fixtures2.Accumulator{}
	s = event.AddToRouter(s, basicmeta2.K8SCollection1, acc1)
	s = event.AddToRouter(s, basicmeta2.Collection2, acc2)
	s = event.AddToRouter(s, data2.Foo, acc3)
	s.Broadcast(event.Event{Kind: event.Reset})

	g.Expect(acc1.Events()).To(HaveLen(1))
	g.Expect(acc2.Events()).To(HaveLen(1))
	g.Expect(acc3.Events()).To(HaveLen(1))
}

func TestRouter_Multi_Unknown_Panic(t *testing.T) {
	g := NewWithT(t)

	defer func() {
		r := recover()
		g.Expect(r).ToNot(BeNil())
	}()
	_ = event.AddToRouter(&unknownSelector{}, data2.Foo, &fixtures2.Accumulator{})
}

type unknownSelector struct{}

var _ event.Router = &unknownSelector{}

func (u *unknownSelector) Handle(event.Event)    {}
func (u *unknownSelector) Broadcast(event.Event) {}
