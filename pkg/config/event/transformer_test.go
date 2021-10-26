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
	data2 "istio.io/istio/pkg/config/legacy/testing/data"
	fixtures2 "istio.io/istio/pkg/config/legacy/testing/fixtures"

	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/schema/collection"
)

func TestTransformer_Basics(t *testing.T) {
	g := NewWithT(t)

	inputs := collection.NewSchemasBuilder().MustAdd(data2.Foo).MustAdd(data2.Bar).Build()
	outputs := collection.NewSchemasBuilder().MustAdd(data2.Boo).MustAdd(data2.Baz).Build()

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
	g := NewWithT(t)

	inputs := collection.NewSchemasBuilder().MustAdd(data2.Foo).MustAdd(data2.Bar).Build()
	outputs := collection.NewSchemasBuilder().MustAdd(data2.Boo).MustAdd(data2.Baz).Build()

	xform := event.NewFnTransform(
		inputs,
		outputs,
		nil,
		nil,
		func(e event.Event, h event.Handler) {
			// Simply translate events
			if e.IsSource(data2.Foo.Name()) {
				h.Handle(e.WithSource(data2.Boo))
			}
			if e.IsSource(data2.Bar.Name()) {
				h.Handle(e.WithSource(data2.Baz))
			}
		},
	)

	accBoo := &fixtures2.Accumulator{}
	accBaz := &fixtures2.Accumulator{}
	xform.DispatchFor(data2.Boo, accBoo)
	xform.DispatchFor(data2.Baz, accBaz)

	xform.Start()

	xform.Handle(data2.Event1Col1AddItem1.WithSource(data2.Foo))
	xform.Handle(data2.Event1Col1AddItem1.WithSource(data2.Bar))

	g.Expect(accBoo.Events()).To(ConsistOf(
		data2.Event1Col1AddItem1.WithSource(data2.Boo),
	))
	g.Expect(accBaz.Events()).To(ConsistOf(
		data2.Event1Col1AddItem1.WithSource(data2.Baz),
	))
}

func TestTransformer_InvalidEvent(t *testing.T) {
	g := NewWithT(t)

	inputs := collection.NewSchemasBuilder().MustAdd(data2.Foo).Build()
	outputs := collection.NewSchemasBuilder().MustAdd(data2.Bar).Build()

	xform := event.NewFnTransform(
		inputs,
		outputs,
		nil,
		nil,
		func(e event.Event, h event.Handler) {
			// Simply translate events
			if e.IsSource(data2.Foo.Name()) {
				h.Handle(e.WithSource(data2.Bar))
			}
		},
	)

	acc := &fixtures2.Accumulator{}
	xform.DispatchFor(data2.Bar, acc)

	xform.Start()

	xform.Handle(data2.Event1Col1AddItem1.WithSource(data2.Bar))

	g.Expect(acc.Events()).To(BeEmpty())
}

func TestTransformer_Reset(t *testing.T) {
	g := NewWithT(t)

	inputs := collection.NewSchemasBuilder().MustAdd(data2.Foo).Build()
	outputs := collection.NewSchemasBuilder().MustAdd(data2.Bar).MustAdd(data2.Baz).Build()

	xform := event.NewFnTransform(
		inputs,
		outputs,
		nil,
		nil,
		func(e event.Event, h event.Handler) {
			// Simply translate events
			if e.IsSource(data2.Foo.Name()) {
				h.Handle(e.WithSource(data2.Bar))
			}
		},
	)

	accBar := &fixtures2.Accumulator{} // it is a trap!
	xform.DispatchFor(data2.Bar, accBar)
	accBaz := &fixtures2.Accumulator{}
	xform.DispatchFor(data2.Baz, accBaz)

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
	g := NewWithT(t)

	inputs := collection.NewSchemasBuilder().MustAdd(data2.Foo).MustAdd(data2.Bar).Build()
	outputs := collection.NewSchemasBuilder().MustAdd(data2.Boo).MustAdd(data2.Baz).Build()

	xform := event.NewFnTransform(
		inputs,
		outputs,
		nil,
		nil,
		func(e event.Event, h event.Handler) {
			// Simply translate events
			if e.IsSource(data2.Foo.Name()) {
				h.Handle(e.WithSource(data2.Boo))
			}
			if e.IsSource(data2.Bar.Name()) {
				h.Handle(e.WithSource(data2.Baz))
			}
		},
	)

	accBoo := &fixtures2.Accumulator{}
	accBaz := &fixtures2.Accumulator{}
	xform.DispatchFor(data2.Boo, accBoo)
	xform.DispatchFor(data2.Baz, accBaz)

	xform.Start()

	xform.Handle(event.FullSyncFor(data2.Foo))
	g.Expect(accBoo.Events()).To(BeEmpty())
	g.Expect(accBaz.Events()).To(BeEmpty())

	xform.Handle(event.FullSyncFor(data2.Bar))
	g.Expect(accBoo.Events()).To(ConsistOf(event.FullSyncFor(data2.Boo)))
	g.Expect(accBaz.Events()).To(ConsistOf(event.FullSyncFor(data2.Baz)))

	// redo
	accBoo.Clear()
	accBaz.Clear()
	xform.Stop()
	xform.Start()

	xform.Handle(event.FullSyncFor(data2.Bar))
	g.Expect(accBoo.Events()).To(BeEmpty())
	g.Expect(accBaz.Events()).To(BeEmpty())

	xform.Handle(event.FullSyncFor(data2.Foo))
	g.Expect(accBoo.Events()).To(ConsistOf(event.FullSyncFor(data2.Boo)))
	g.Expect(accBaz.Events()).To(ConsistOf(event.FullSyncFor(data2.Baz)))
}
