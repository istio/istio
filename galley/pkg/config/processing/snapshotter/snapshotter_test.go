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

package snapshotter

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/processing/snapshotter/strategy"
	"istio.io/istio/pkg/config/event"
	basicmeta2 "istio.io/istio/pkg/config/legacy/testing/basicmeta"
	data2 "istio.io/istio/pkg/config/legacy/testing/data"
	fixtures2 "istio.io/istio/pkg/config/legacy/testing/fixtures"
	"istio.io/istio/pkg/config/schema/collection"
)

func TestSnapshotter_Basic(t *testing.T) {
	g := NewWithT(t)

	tr := fixtures2.NewTransformer(
		collection.NewSchemasBuilder().MustAdd(basicmeta2.K8SCollection1).Build(),
		collection.NewSchemasBuilder().MustAdd(basicmeta2.Collection2).Build(),
		func(tr *fixtures2.Transformer, e event.Event) {
			switch e.Kind {
			case event.Reset:
				tr.Publish(basicmeta2.Collection2.Name(), e)
			default:
				e.Source = basicmeta2.Collection2
				tr.Publish(basicmeta2.Collection2.Name(), e)
			}
		})

	d := NewInMemoryDistributor()

	options := []SnapshotOptions{
		{
			Collections: []collection.Name{basicmeta2.Collection2.Name()},
			Strategy:    strategy.NewImmediate(),
			Group:       "default",
			Distributor: d,
		},
	}

	s, err := NewSnapshotter([]event.Transformer{tr}, options)
	g.Expect(err).To(BeNil())
	s.Start()

	g.Expect(tr.Started).To(BeTrue())

	s.Stop()
	g.Expect(tr.Started).To(BeFalse())

	s.Start()

	sn := d.GetSnapshot("default")
	g.Expect(sn).To(BeNil())

	s.Handle(data2.Event1Col1AddItem1)
	s.Handle(data2.Event1Col1Synced)

	sn = d.GetSnapshot("default")
	g.Expect(sn).NotTo(BeNil())
	g.Expect(sn.Version(basicmeta2.Collection2.Name().String())).To(Equal("collection2/2"))
	g.Expect(sn.Resources(basicmeta2.Collection2.Name().String())).To(HaveLen(1))

	s.Handle(data2.Event1Col1UpdateItem1)
	s.Handle(data2.Event1Col1DeleteItem1)
	s.Handle(data2.Event1Col1Synced)

	sn = d.GetSnapshot("default")
	g.Expect(sn).NotTo(BeNil())
	g.Expect(sn.Version(basicmeta2.Collection2.Name().String())).To(Equal("collection2/4"))
	g.Expect(sn.Resources(basicmeta2.Collection2.Name().String())).To(HaveLen(0))
}

func TestSnapshotter_SnapshotMismatch(t *testing.T) {
	g := NewWithT(t)

	tr := fixtures2.NewTransformer(
		collection.NewSchemasBuilder().MustAdd(basicmeta2.K8SCollection1).Build(),
		collection.NewSchemasBuilder().MustAdd(basicmeta2.Collection2).Build(),
		func(tr *fixtures2.Transformer, e event.Event) {
			switch e.Kind {
			case event.Reset:
				tr.Publish(basicmeta2.Collection2.Name(), e)
			default:
				e.Source = basicmeta2.Collection2
				tr.Publish(basicmeta2.Collection2.Name(), e)
			}
		})

	d := NewInMemoryDistributor()

	options := []SnapshotOptions{
		{
			Collections: []collection.Name{data2.Foo.Name()},
			Strategy:    strategy.NewImmediate(),
			Group:       "default",
			Distributor: d,
		},
	}

	_, err := NewSnapshotter([]event.Transformer{tr}, options)
	g.Expect(err).NotTo(BeNil())
}

// All collections should be synced before any snapshots are made available
func TestSnapshotterWaitForAllSync(t *testing.T) {
	g := NewWithT(t)

	tr := fixtures2.NewTransformer(
		collection.NewSchemasBuilder().MustAdd(basicmeta2.K8SCollection1).MustAdd(basicmeta2.Collection2).Build(),
		collection.NewSchemasBuilder().MustAdd(basicmeta2.K8SCollection1).MustAdd(basicmeta2.Collection2).Build(),
		func(tr *fixtures2.Transformer, e event.Event) {
			tr.Publish(e.Source.Name(), e)
		})

	d := NewInMemoryDistributor()

	options := []SnapshotOptions{
		{
			Collections: []collection.Name{basicmeta2.K8SCollection1.Name(), basicmeta2.Collection2.Name()},
			Strategy:    strategy.NewImmediate(),
			Group:       "default",
			Distributor: d,
		},
	}

	s, err := NewSnapshotter([]event.Transformer{tr}, options)
	g.Expect(err).To(BeNil())
	s.Start()

	s.Handle(data2.Event1Col1Synced)

	sn := d.GetSnapshot("default")
	g.Expect(sn).To(BeNil())

	s.Handle(data2.Event1Col2Synced)

	sn = d.GetSnapshot("default")
	g.Expect(sn).NotTo(BeNil())
}
