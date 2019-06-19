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

package snapshotter

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/processing/snapshotter/strategy"
	"istio.io/istio/galley/pkg/config/testing/data"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
)

func TestSnapshotter_Basic(t *testing.T) {
	g := NewGomegaWithT(t)

	tr := fixtures.NewTransformer(
		[]collection.Name{data.Collection1},
		[]collection.Name{data.Collection2},
		func(tr *fixtures.Transformer, e event.Event) {
			switch e.Kind {
			case event.Reset:
				tr.Publish(data.Collection2, e)
			default:
				e.Source = data.Collection2
				tr.Publish(data.Collection2, e)
			}
		})

	d := NewInMemoryDistributor()

	options := []SnapshotOption{
		{
			Collections: []collection.Name{data.Collection2},
			Strategy:    strategy.NewImmediate(&NoopReporter{}),
			Group:       "default",
			Distributor: d,
		},
	}

	s := NewSnapshotter([]event.Transformer{tr}, options, &NoopReporter{})
	s.Start(struct{}{})

	g.Expect(tr.Started).To(BeTrue())
	// Options should be plumbed-in.
	g.Expect(tr.Options).To(Equal(struct{}{}))

	s.Stop()
	g.Expect(tr.Started).To(BeFalse())

	s.Start(struct{}{})

	sn := d.GetSnapshot("default")
	g.Expect(sn).To(BeNil())

	s.Handle(data.Event1Col1AddItem1)
	s.Handle(data.Event1Col1Synced)

	sn = d.GetSnapshot("default")
	g.Expect(sn).NotTo(BeNil())
	g.Expect(sn.Version(data.Collection2.String())).To(Equal("collection2/2"))
	g.Expect(sn.Resources(data.Collection2.String())).To(HaveLen(1))

	s.Handle(data.Event1Col1UpdateItem1)
	s.Handle(data.Event1Col1DeleteItem1)
	s.Handle(data.Event1Col1Synced)

	sn = d.GetSnapshot("default")
	g.Expect(sn).NotTo(BeNil())
	g.Expect(sn.Version(data.Collection2.String())).To(Equal("collection2/4"))
	g.Expect(sn.Resources(data.Collection2.String())).To(HaveLen(0))

	// Erroneous event
	e := data.Event1Col1DeleteItem1
	e.Kind = event.None
	s.Handle(e)

	sn = d.GetSnapshot("default")
	g.Expect(sn).NotTo(BeNil())
	g.Expect(sn.Version(data.Collection2.String())).To(Equal("collection2/4"))
	g.Expect(sn.Resources(data.Collection2.String())).To(HaveLen(0))
}

func TestSnapshotter_SnapshotMismatch(t *testing.T) {
	g := NewGomegaWithT(t)

	tr := fixtures.NewTransformer(
		[]collection.Name{data.Collection1},
		[]collection.Name{data.Collection2},
		func(tr *fixtures.Transformer, e event.Event) {
			switch e.Kind {
			case event.Reset:
				tr.Publish(data.Collection2, e)
			default:
				e.Source = data.Collection2
				tr.Publish(data.Collection2, e)
			}
		})

	d := NewInMemoryDistributor()

	options := []SnapshotOption{
		{
			Collections: []collection.Name{data.Collection3},
			Strategy:    strategy.NewImmediate(&NoopReporter{}),
			Group:       "default",
			Distributor: d,
		},
	}

	s := NewSnapshotter([]event.Transformer{tr}, options, &NoopReporter{})
	s.Start(nil)

	s.Handle(data.Event1Col1AddItem1)
	s.Handle(data.Event1Col1Synced)

	sn := d.GetSnapshot("default")
	g.Expect(sn).To(BeNil())
}


//
// import (
// 	"testing"
//
// 	. "github.com/onsi/gomega"
// 	"github.com/pkg/errors"
//
// 	"istio.io/pkg/log"
//
// 	collection2 "istio.io/istio/galley/pkg/config/collection"
// 	"istio.io/istio/galley/pkg/config/event"
// 	eventtesting2 "istio.io/istio/galley/pkg/config/event/eventtesting"
// 	"istio.io/istio/galley/pkg/config/processor/state"
// 	"istio.io/istio/galley/pkg/config/resource"
// 	"istio.io/istio/galley/pkg/config/schema/collection"
// 	"istio.io/istio/pkg/mcp/snapshot"
// )
//
// func TestNewSnapshotter(t *testing.T) {
// 	enableDebugLogging()
// 	defer disableDebugLogging()
//
// 	g := NewGomegaWithT(t)
//
// 	sources := event.NewSources()
// 	st := state.New(sources)
// 	idx := collection.NewNameIndex(eventtesting2.Collection1)
// 	st.CollectionIndices.MustAdd(idx.Collection(), idx)
//
// 	changed := false
// 	notifier := SnapshotChangedHandlerFromFn(func() {
// 		changed = true
// 	})
//
// 	s, err := NewSnapshotter(eventtesting2.Collection1, st, notifier)
// 	g.Expect(err).To(BeNil())
//
// 	s.Handle(eventtesting2.Event1Col1AddItem1)
//
// 	g.Expect(changed).To(BeTrue())
// }
//
// func TestNewSnapshotter_Error(t *testing.T) {
// 	enableDebugLogging()
// 	defer disableDebugLogging()
//
// 	g := NewGomegaWithT(t)
//
// 	sources := event.NewSources()
// 	st := state.New(sources)
// 	src := &eventtesting2.Dispatcher{CollectionName: eventtesting2.Collection1, Err: errors.New("register error")}
// 	sources.MustAdd(src)
//
// 	notifier := SnapshotChangedHandlerFromFn(func() {})
// 	_, err := NewSnapshotter(eventtesting2.Collection1, st, notifier)
// 	g.Expect(err).NotTo(BeNil())
// }
//
// func TestNewSnapshotter_NoSource_Error(t *testing.T) {
// 	enableDebugLogging()
// 	defer disableDebugLogging()
//
// 	g := NewGomegaWithT(t)
//
// 	sources := event.NewSources()
// 	st := state.New(sources)
// 	src := &eventtesting2.Dispatcher{CollectionName: eventtesting2.Collection1}
// 	sources.MustAdd(src)
//
// 	notifier := SnapshotChangedHandlerFromFn(func() {})
// 	_, err := NewSnapshotter(eventtesting2.Collection2, st, notifier)
// 	g.Expect(err).NotTo(BeNil())
// }
//
// func TestNewSnapshotters(t *testing.T) {
// 	enableDebugLogging()
// 	defer disableDebugLogging()
//
// 	g := NewGomegaWithT(t)
//
// 	st := state.New(event.NewSources())
// 	idx1 := collection.NewNameIndex(eventtesting2.Collection1)
// 	st.CollectionIndices.MustAdd(idx1.Collection(), idx1)
//
// 	idx2 := collection.NewNameIndex(eventtesting2.Collection2)
// 	st.CollectionIndices.MustAdd(idx2.Collection(), idx2)
//
// 	notifier := SnapshotChangedHandlerFromFn(func() {
// 	})
//
// 	collections := []collection2.Name{eventtesting2.Collection1, eventtesting2.Collection2}
// 	snapshotters, err := NewSnapshotters(collections, st, notifier)
// 	g.Expect(err).To(BeNil())
// 	g.Expect(snapshotters).To(HaveLen(2))
// }
//
// func TestNewSnapshotters_Error(t *testing.T) {
// 	enableDebugLogging()
// 	defer disableDebugLogging()
//
// 	g := NewGomegaWithT(t)
//
// 	notifier := SnapshotChangedHandlerFromFn(func() {
// 	})
//
// 	st := state.New(event.NewSources())
//
// 	idx := collection.NewNameIndex(eventtesting2.Collection1)
// 	st.CollectionIndices.MustAdd(idx.Collection(), idx)
//
// 	collections := []collection2.Name{eventtesting2.Collection1, eventtesting2.Collection2}
// 	_, err := NewSnapshotters(collections, st, notifier)
// 	g.Expect(err).NotTo(BeNil())
// }
//
// func TestSnapshotter_Add(t *testing.T) {
// 	enableDebugLogging()
// 	defer disableDebugLogging()
//
// 	g := NewGomegaWithT(t)
//
// 	st := state.New(event.NewSources())
// 	idx := collection.NewNameIndex(eventtesting2.Collection1)
// 	st.CollectionIndices.MustAdd(idx.Collection(), idx)
//
// 	changed := false
// 	notifier := SnapshotChangedHandlerFromFn(func() {
// 		changed = true
// 	})
//
// 	s, err := NewSnapshotter(eventtesting2.Collection1, st, notifier)
// 	g.Expect(err).To(BeNil())
//
// 	s.Handle(eventtesting2.Event1Col1AddItem1)
// 	g.Expect(changed).To(BeTrue())
//
// 	bu := snapshot.NewInMemoryBuilder()
// 	s.Snapshot(bu)
// 	sn := bu.Build()
//
// 	g.Expect(sn.Resources(eventtesting2.Collection1.String())).To(HaveLen(1))
//
// 	r1 := sn.Resources(eventtesting2.Collection1.String())[0]
// 	e, err := resource.Deserialize(r1)
// 	g.Expect(err).To(BeNil())
// 	g.Expect(e).To(Equal(eventtesting2.Event1Col1AddItem1.Entry))
// }
//
// func TestSnapshotter_Add_Error(t *testing.T) {
// 	enableDebugLogging()
// 	defer disableDebugLogging()
//
// 	g := NewGomegaWithT(t)
//
// 	st := state.New(event.NewSources())
// 	idx := collection.NewNameIndex(eventtesting2.Collection1)
// 	st.CollectionIndices.MustAdd(idx.Collection(), idx)
//
// 	changed := false
// 	notifier := SnapshotChangedHandlerFromFn(func() {
// 		changed = true
// 	})
//
// 	s, err := NewSnapshotter(eventtesting2.Collection1, st, notifier)
// 	g.Expect(err).To(BeNil())
//
// 	s.Handle(eventtesting2.Event1Col1AddItem1Broken)
// 	g.Expect(changed).To(BeFalse())
//
// 	bu := snapshot.NewInMemoryBuilder()
// 	s.Snapshot(bu)
// 	sn := bu.Build()
//
// 	g.Expect(sn.Resources(eventtesting2.Collection1.String())).To(HaveLen(0))
// }
//
// func TestSnapshotter_Update(t *testing.T) {
// 	enableDebugLogging()
// 	defer disableDebugLogging()
//
// 	g := NewGomegaWithT(t)
//
// 	st := state.New(event.NewSources())
// 	idx := collection.NewNameIndex(eventtesting2.Collection1)
// 	st.CollectionIndices.MustAdd(idx.Collection(), idx)
//
// 	changed := false
// 	notifier := SnapshotChangedHandlerFromFn(func() {
// 		changed = true
// 	})
//
// 	s, err := NewSnapshotter(eventtesting2.Collection1, st, notifier)
// 	g.Expect(err).To(BeNil())
//
// 	s.Handle(eventtesting2.Event1Col1AddItem1)
// 	g.Expect(changed).To(BeTrue())
//
// 	changed = false
// 	s.Handle(eventtesting2.Event1Col1UpdateItem1)
// 	g.Expect(changed).To(BeTrue())
//
// 	bu := snapshot.NewInMemoryBuilder()
// 	s.Snapshot(bu)
// 	sn := bu.Build()
//
// 	g.Expect(sn.Resources(eventtesting2.Collection1.String())).To(HaveLen(1))
//
// 	r1 := sn.Resources(eventtesting2.Collection1.String())[0]
// 	e, err := resource.Deserialize(r1)
// 	g.Expect(err).To(BeNil())
// 	g.Expect(e).To(Equal(eventtesting2.Event1Col1UpdateItem1.Entry))
// }
//
// func TestSnapshotter_Delete(t *testing.T) {
// 	enableDebugLogging()
// 	defer disableDebugLogging()
//
// 	g := NewGomegaWithT(t)
//
// 	st := state.New(event.NewSources())
// 	idx := collection.NewNameIndex(eventtesting2.Collection1)
// 	st.CollectionIndices.MustAdd(idx.Collection(), idx)
//
// 	changed := false
// 	notifier := SnapshotChangedHandlerFromFn(func() {
// 		changed = true
// 	})
//
// 	s, err := NewSnapshotter(eventtesting2.Collection1, st, notifier)
// 	g.Expect(err).To(BeNil())
//
// 	s.Handle(eventtesting2.Event1Col1AddItem1)
// 	g.Expect(changed).To(BeTrue())
//
// 	changed = false
// 	s.Handle(eventtesting2.Event1Col1DeleteItem1)
// 	g.Expect(changed).To(BeTrue())
//
// 	bu := snapshot.NewInMemoryBuilder()
// 	s.Snapshot(bu)
// 	sn := bu.Build()
//
// 	g.Expect(sn.Resources(eventtesting2.Collection1.String())).To(HaveLen(0))
// }
//
// func TestSnapshotter_Synced(t *testing.T) {
// 	enableDebugLogging()
// 	defer disableDebugLogging()
//
// 	g := NewGomegaWithT(t)
//
// 	st := state.New(event.NewSources())
// 	idx := collection.NewNameIndex(eventtesting2.Collection1)
// 	st.CollectionIndices.MustAdd(idx.Collection(), idx)
//
// 	changed := false
// 	notifier := SnapshotChangedHandlerFromFn(func() {
// 		changed = true
// 	})
//
// 	s, err := NewSnapshotter(eventtesting2.Collection1, st, notifier)
// 	g.Expect(err).To(BeNil())
//
// 	g.Expect(s.Synced()).To(BeFalse())
//
// 	s.Handle(eventtesting2.Event1Col1Synced)
// 	g.Expect(changed).To(BeTrue())
// 	g.Expect(s.Synced()).To(BeTrue())
// }
//
// func enableDebugLogging() {
// 	o := log.DefaultOptions()
// 	o.SetOutputLevel("processing", log.DebugLevel)
// 	if err := log.Configure(o); err != nil {
// 		panic(err)
// 	}
// }
//
// func disableDebugLogging() {
// 	o := log.DefaultOptions()
// 	if err := log.Configure(o); err != nil {
// 		panic(err)
// 	}
// }
