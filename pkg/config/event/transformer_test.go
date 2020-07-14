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

	"istio.io/istio/galley/pkg/config/testing/data"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/schema/collection"
)

func TestTransformer_Basics(t *testing.T) {
	g := NewGomegaWithT(t)

	inputs := collection.NewSchemasBuilder().MustAdd(data.Foo).MustAdd(data.Bar).Build()
	outputs := collection.NewSchemasBuilder().MustAdd(data.Boo).MustAdd(data.Baz).Build()

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

	inputs := collection.NewSchemasBuilder().MustAdd(data.Foo).MustAdd(data.Bar).Build()
	outputs := collection.NewSchemasBuilder().MustAdd(data.Boo).MustAdd(data.Baz).Build()

	xform := event.NewFnTransform(
		inputs,
		outputs,
		nil,
		nil,
		func(e event.Event, h event.Handler) {
			// Simply translate events
			if e.IsSource(data.Foo.Name()) {
				h.Handle(e.WithSource(data.Boo))
			}
			if e.IsSource(data.Bar.Name()) {
				h.Handle(e.WithSource(data.Baz))
			}
		},
	)

	accBoo := &fixtures.Accumulator{}
	accBaz := &fixtures.Accumulator{}
	xform.DispatchFor(data.Boo, accBoo)
	xform.DispatchFor(data.Baz, accBaz)

	xform.Start()

	xform.Handle(data.Event1Col1AddItem1.WithSource(data.Foo))
	xform.Handle(data.Event1Col1AddItem1.WithSource(data.Bar))

	g.Expect(accBoo.Events()).To(ConsistOf(
		data.Event1Col1AddItem1.WithSource(data.Boo),
	))
	g.Expect(accBaz.Events()).To(ConsistOf(
		data.Event1Col1AddItem1.WithSource(data.Baz),
	))
}

func TestTransformer_InvalidEvent(t *testing.T) {
	g := NewGomegaWithT(t)

	inputs := collection.NewSchemasBuilder().MustAdd(data.Foo).Build()
	outputs := collection.NewSchemasBuilder().MustAdd(data.Bar).Build()

	xform := event.NewFnTransform(
		inputs,
		outputs,
		nil,
		nil,
		func(e event.Event, h event.Handler) {
			// Simply translate events
			if e.IsSource(data.Foo.Name()) {
				h.Handle(e.WithSource(data.Bar))
			}
		},
	)

	acc := &fixtures.Accumulator{}
	xform.DispatchFor(data.Bar, acc)

	xform.Start()

	xform.Handle(data.Event1Col1AddItem1.WithSource(data.Bar))

	g.Expect(acc.Events()).To(BeEmpty())
}

func TestTransformer_Reset(t *testing.T) {
	g := NewGomegaWithT(t)

	inputs := collection.NewSchemasBuilder().MustAdd(data.Foo).Build()
	outputs := collection.NewSchemasBuilder().MustAdd(data.Bar).MustAdd(data.Baz).Build()

	xform := event.NewFnTransform(
		inputs,
		outputs,
		nil,
		nil,
		func(e event.Event, h event.Handler) {
			// Simply translate events
			if e.IsSource(data.Foo.Name()) {
				h.Handle(e.WithSource(data.Bar))
			}
		},
	)

	accBar := &fixtures.Accumulator{} // it is a trap!
	xform.DispatchFor(data.Bar, accBar)
	accBaz := &fixtures.Accumulator{}
	xform.DispatchFor(data.Baz, accBaz)

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

	inputs := collection.NewSchemasBuilder().MustAdd(data.Foo).MustAdd(data.Bar).Build()
	outputs := collection.NewSchemasBuilder().MustAdd(data.Boo).MustAdd(data.Baz).Build()

	xform := event.NewFnTransform(
		inputs,
		outputs,
		nil,
		nil,
		func(e event.Event, h event.Handler) {
			// Simply translate events
			if e.IsSource(data.Foo.Name()) {
				h.Handle(e.WithSource(data.Boo))
			}
			if e.IsSource(data.Bar.Name()) {
				h.Handle(e.WithSource(data.Baz))
			}
		},
	)

	accBoo := &fixtures.Accumulator{}
	accBaz := &fixtures.Accumulator{}
	xform.DispatchFor(data.Boo, accBoo)
	xform.DispatchFor(data.Baz, accBaz)

	xform.Start()

	xform.Handle(event.FullSyncFor(data.Foo))
	g.Expect(accBoo.Events()).To(BeEmpty())
	g.Expect(accBaz.Events()).To(BeEmpty())

	xform.Handle(event.FullSyncFor(data.Bar))
	g.Expect(accBoo.Events()).To(ConsistOf(event.FullSyncFor(data.Boo)))
	g.Expect(accBaz.Events()).To(ConsistOf(event.FullSyncFor(data.Baz)))

	// redo
	accBoo.Clear()
	accBaz.Clear()
	xform.Stop()
	xform.Start()

	xform.Handle(event.FullSyncFor(data.Bar))
	g.Expect(accBoo.Events()).To(BeEmpty())
	g.Expect(accBaz.Events()).To(BeEmpty())

	xform.Handle(event.FullSyncFor(data.Foo))
	g.Expect(accBoo.Events()).To(ConsistOf(event.FullSyncFor(data.Boo)))
	g.Expect(accBaz.Events()).To(ConsistOf(event.FullSyncFor(data.Baz)))
}
