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

package direct

import (
	"testing"

	. "github.com/onsi/gomega"
	"istio.io/istio/pkg/config/legacy/processing"
	basicmeta2 "istio.io/istio/pkg/config/legacy/testing/basicmeta"
	data2 "istio.io/istio/pkg/config/legacy/testing/data"
	fixtures2 "istio.io/istio/pkg/config/legacy/testing/fixtures"

	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/schema/collection"
)

func TestDirect_Input_Output(t *testing.T) {
	g := NewWithT(t)

	xform, _, _ := setup(g)

	fixtures2.ExpectEqual(t, xform.Inputs(), collection.NewSchemasBuilder().MustAdd(basicmeta2.K8SCollection1).Build())
	fixtures2.ExpectEqual(t, xform.Outputs(), collection.NewSchemasBuilder().MustAdd(basicmeta2.Collection2).Build())
}

func TestDirect_AddSync(t *testing.T) {
	g := NewWithT(t)

	xform, src, acc := setup(g)

	xform.Start()
	defer xform.Stop()

	src.Handlers.Handle(event.AddFor(basicmeta2.K8SCollection1, data2.EntryN1I1V1))
	src.Handlers.Handle(event.FullSyncFor(basicmeta2.K8SCollection1))

	fixtures2.ExpectEventsEventually(t, acc,
		event.AddFor(basicmeta2.Collection2, data2.EntryN1I1V1), // XForm to Collection2
		event.FullSyncFor(basicmeta2.Collection2))
}

func TestDirect_SyncAdd(t *testing.T) {
	g := NewWithT(t)

	xform, src, acc := setup(g)

	xform.Start()
	defer xform.Stop()

	src.Handlers.Handle(event.FullSyncFor(basicmeta2.K8SCollection1))
	src.Handlers.Handle(event.AddFor(basicmeta2.K8SCollection1, data2.EntryN1I1V1))

	fixtures2.ExpectEventsEventually(t, acc,
		event.FullSyncFor(basicmeta2.Collection2),
		event.AddFor(basicmeta2.Collection2, data2.EntryN1I1V1)) // XForm to Collection2
}

func TestDirect_AddUpdateDelete(t *testing.T) {
	g := NewWithT(t)

	xform, src, acc := setup(g)

	xform.Start()
	defer xform.Stop()

	src.Handlers.Handle(event.FullSyncFor(basicmeta2.K8SCollection1))
	src.Handlers.Handle(event.AddFor(basicmeta2.K8SCollection1, data2.EntryN1I1V1))
	src.Handlers.Handle(event.UpdateFor(basicmeta2.K8SCollection1, data2.EntryN1I1V2))
	src.Handlers.Handle(event.DeleteForResource(basicmeta2.K8SCollection1, data2.EntryN1I1V2))

	fixtures2.ExpectEventsEventually(t, acc,
		event.FullSyncFor(basicmeta2.Collection2),
		event.AddFor(basicmeta2.Collection2, data2.EntryN1I1V1),
		event.UpdateFor(basicmeta2.Collection2, data2.EntryN1I1V2),
		event.DeleteForResource(basicmeta2.Collection2, data2.EntryN1I1V2),
	)
}

func TestDirect_SyncReset(t *testing.T) {
	g := NewWithT(t)

	xform, src, acc := setup(g)

	xform.Start()
	defer xform.Stop()

	src.Handlers.Handle(event.FullSyncFor(basicmeta2.K8SCollection1))
	src.Handlers.Handle(event.Event{Kind: event.Reset})

	fixtures2.ExpectEventsEventually(t, acc,
		event.FullSyncFor(basicmeta2.Collection2),
		event.Event{Kind: event.Reset},
	)
}

func TestDirect_InvalidEventKind(t *testing.T) {
	g := NewWithT(t)

	xform, src, acc := setup(g)

	xform.Start()
	defer xform.Stop()

	src.Handlers.Handle(event.FullSyncFor(basicmeta2.K8SCollection1))
	src.Handlers.Handle(event.Event{Kind: 55})

	fixtures2.ExpectEventsEventually(t, acc,
		event.FullSyncFor(basicmeta2.Collection2),
	)
}

func TestDirect_NoListeners(t *testing.T) {
	g := NewWithT(t)

	xforms := GetProviders(basicmeta2.MustGet()).Create(util.ProcessorOptions{})
	g.Expect(xforms).To(HaveLen(1))

	src := &fixtures2.Source{}
	xform := xforms[0]
	src.Dispatch(xform)

	xform.Start()
	defer xform.Stop()

	src.Handlers.Handle(event.FullSyncFor(basicmeta2.K8SCollection1))
	src.Handlers.Handle(event.Event{Kind: event.Reset})
	src.Handlers.Handle(event.AddFor(basicmeta2.K8SCollection1, data2.EntryN1I1V1))

	// No crash
}

func TestDirect_DoubleStart(t *testing.T) {
	g := NewWithT(t)

	xform, src, acc := setup(g)

	xform.Start()
	xform.Start()
	defer xform.Stop()

	src.Handlers.Handle(event.FullSyncFor(basicmeta2.K8SCollection1))
	src.Handlers.Handle(event.AddFor(basicmeta2.K8SCollection1, data2.EntryN1I1V1))

	fixtures2.ExpectEventsEventually(t, acc,
		event.FullSyncFor(basicmeta2.Collection2),
		event.AddFor(basicmeta2.Collection2, data2.EntryN1I1V1), // XForm to Collection2
	)
}

func TestDirect_DoubleStop(t *testing.T) {
	g := NewWithT(t)

	xform, src, acc := setup(g)

	xform.Start()

	src.Handlers.Handle(event.FullSyncFor(basicmeta2.K8SCollection1))
	src.Handlers.Handle(event.AddFor(basicmeta2.K8SCollection1, data2.EntryN1I1V1))

	fixtures2.ExpectEventsEventually(t, acc,
		event.FullSyncFor(basicmeta2.Collection2),
		event.AddFor(basicmeta2.Collection2, data2.EntryN1I1V1), // XForm to Collection2
	)

	acc.Clear()

	xform.Stop()
	xform.Stop()

	g.Consistently(acc.Events).Should(BeEmpty())
}

func TestDirect_StartStopStartStop(t *testing.T) {
	g := NewWithT(t)

	xform, src, acc := setup(g)

	xform.Start()
	defer xform.Stop()

	src.Handlers.Handle(event.FullSyncFor(basicmeta2.K8SCollection1))
	src.Handlers.Handle(event.AddFor(basicmeta2.K8SCollection1, data2.EntryN1I1V1))

	fixtures2.ExpectEventsEventually(t, acc,
		event.FullSyncFor(basicmeta2.Collection2),
		event.AddFor(basicmeta2.Collection2, data2.EntryN1I1V1), // XForm to Collection2
	)

	acc.Clear()
	xform.Stop()
	g.Consistently(acc.Events).Should(BeEmpty())

	xform.Start()
	src.Handlers.Handle(event.FullSyncFor(basicmeta2.K8SCollection1))
	src.Handlers.Handle(event.AddFor(basicmeta2.K8SCollection1, data2.EntryN1I1V1))

	fixtures2.ExpectEventsEventually(t, acc,
		event.FullSyncFor(basicmeta2.Collection2),
		event.AddFor(basicmeta2.Collection2, data2.EntryN1I1V1), // XForm to Collection2
	)

	acc.Clear()
	xform.Stop()
	g.Consistently(acc.Events).Should(BeEmpty())
}

func TestDirect_InvalidEvent(t *testing.T) {
	g := NewWithT(t)

	xform, src, acc := setup(g)

	xform.Start()
	defer xform.Stop()

	src.Handlers.Handle(event.FullSyncFor(basicmeta2.Collection2)) // Collection2
	src.Handlers.Handle(event.AddFor(basicmeta2.Collection2, data2.EntryN1I1V1))

	g.Consistently(acc.Events).Should(BeEmpty())
}

func setup(g *GomegaWithT) (event.Transformer, *fixtures2.Source, *fixtures2.Accumulator) {
	xforms := GetProviders(basicmeta2.MustGet()).Create(util.ProcessorOptions{})
	g.Expect(xforms).To(HaveLen(1))

	src := &fixtures2.Source{}
	acc := &fixtures2.Accumulator{}
	xform := xforms[0]
	src.Dispatch(xform)
	xform.DispatchFor(xform.Outputs().All()[0], acc)

	return xform, src, acc
}
