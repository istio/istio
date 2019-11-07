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
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/testing/data"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
)

func TestTransformer_Basics(t *testing.T) {
	g := NewGomegaWithT(t)

	inputs := collection.Names{collection.NewName("foo"), collection.NewName("bar")}
	outputs := collection.Names{collection.NewName("boo"), collection.NewName("baz")}

	var started, stopped bool
	xform := event.NewFnTransform(
		inputs,
		outputs,
		func() { started = true },
		func() { stopped = true },
		func(e event.Event, h event.Handler) {},
	)

	g.Expect(xform.Inputs()).To(Equal(inputs))
	g.Expect(xform.Outputs()).To(Equal(outputs))

	g.Expect(started).To(BeFalse())
	g.Expect(stopped).To(BeFalse())

	xform.Start()
	g.Expect(started).To(BeTrue())
	g.Expect(stopped).To(BeFalse())

	xform.Stop()
	g.Expect(stopped).To(BeTrue())
}

func TestTransformer_Selection(t *testing.T) {
	g := NewGomegaWithT(t)

	foo := collection.NewName("foo")
	bar := collection.NewName("bar")
	boo := collection.NewName("boo")
	baz := collection.NewName("baz")
	inputs := collection.Names{foo, bar}
	outputs := collection.Names{boo, baz}

	xform := event.NewFnTransform(
		inputs,
		outputs,
		nil,
		nil,
		func(e event.Event, h event.Handler) {
			// Simply translate events
			if e.IsSource(foo) {
				h.Handle(e.WithSource(boo))
			}
			if e.IsSource(bar) {
				h.Handle(e.WithSource(baz))
			}
		},
	)

	accBoo := &fixtures.Accumulator{}
	accBaz := &fixtures.Accumulator{}
	xform.DispatchFor(boo, accBoo)
	xform.DispatchFor(baz, accBaz)

	xform.Start()

	xform.Handle(data.Event1Col1AddItem1.WithSource(foo))
	xform.Handle(data.Event1Col1AddItem1.WithSource(bar))

	g.Expect(accBoo.Events()).To(ConsistOf(
		data.Event1Col1AddItem1.WithSource(boo),
	))
	g.Expect(accBaz.Events()).To(ConsistOf(
		data.Event1Col1AddItem1.WithSource(baz),
	))
}

func TestTransformer_InvalidEvent(t *testing.T) {
	g := NewGomegaWithT(t)

	foo := collection.NewName("foo")
	bar := collection.NewName("bar")
	inputs := collection.Names{foo}
	outputs := collection.Names{bar}

	xform := event.NewFnTransform(
		inputs,
		outputs,
		nil,
		nil,
		func(e event.Event, h event.Handler) {
			// Simply translate events
			if e.IsSource(foo) {
				h.Handle(e.WithSource(bar))
			}
		},
	)

	acc := &fixtures.Accumulator{}
	xform.DispatchFor(bar, acc)

	xform.Start()

	xform.Handle(data.Event1Col1AddItem1.WithSource(bar))

	g.Expect(acc.Events()).To(BeEmpty())
}

func TestTransformer_Reset(t *testing.T) {
	g := NewGomegaWithT(t)

	foo := collection.NewName("foo")
	bar := collection.NewName("bar")
	baz := collection.NewName("baz")
	inputs := collection.Names{foo}
	outputs := collection.Names{bar, baz}

	xform := event.NewFnTransform(
		inputs,
		outputs,
		nil,
		nil,
		func(e event.Event, h event.Handler) {
			// Simply translate events
			if e.IsSource(foo) {
				h.Handle(e.WithSource(bar))
			}
		},
	)

	accBar := &fixtures.Accumulator{} // it is a trap!
	xform.DispatchFor(bar, accBar)
	accBaz := &fixtures.Accumulator{}
	xform.DispatchFor(baz, accBaz)

	xform.Start()

	xform.Handle(event.Event{Kind: event.Reset})

	g.Expect(accBar.Events()).To(ConsistOf(
		event.Event{Kind: event.Reset},
	))
	g.Expect(accBar.Events()).To(ConsistOf(
		event.Event{Kind: event.Reset},
	))
}

func TestTransformer_FullSync(t *testing.T) {
	g := NewGomegaWithT(t)

	foo := collection.NewName("foo")
	bar := collection.NewName("bar")
	boo := collection.NewName("boo")
	baz := collection.NewName("baz")
	inputs := collection.Names{foo, bar}
	outputs := collection.Names{boo, baz}

	xform := event.NewFnTransform(
		inputs,
		outputs,
		nil,
		nil,
		func(e event.Event, h event.Handler) {
			// Simply translate events
			if e.IsSource(foo) {
				h.Handle(e.WithSource(boo))
			}
			if e.IsSource(bar) {
				h.Handle(e.WithSource(baz))
			}
		},
	)

	accBoo := &fixtures.Accumulator{}
	accBaz := &fixtures.Accumulator{}
	xform.DispatchFor(boo, accBoo)
	xform.DispatchFor(baz, accBaz)

	xform.Start()

	xform.Handle(event.FullSyncFor(foo))
	g.Expect(accBoo.Events()).To(BeEmpty())
	g.Expect(accBaz.Events()).To(BeEmpty())

	xform.Handle(event.FullSyncFor(bar))
	g.Expect(accBoo.Events()).To(ConsistOf(event.FullSyncFor(boo)))
	g.Expect(accBaz.Events()).To(ConsistOf(event.FullSyncFor(baz)))

	// redo
	accBoo.Clear()
	accBaz.Clear()
	xform.Stop()
	xform.Start()

	xform.Handle(event.FullSyncFor(bar))
	g.Expect(accBoo.Events()).To(BeEmpty())
	g.Expect(accBaz.Events()).To(BeEmpty())

	xform.Handle(event.FullSyncFor(foo))
	g.Expect(accBoo.Events()).To(ConsistOf(event.FullSyncFor(boo)))
	g.Expect(accBaz.Events()).To(ConsistOf(event.FullSyncFor(baz)))
}
