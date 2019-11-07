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

	coll "istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/testing/data"
)

func TestSnapshot_Basics(t *testing.T) {
	g := NewGomegaWithT(t)

	set := coll.NewSet([]collection.Name{data.Collection1})
	set.Collection(data.Collection1).Set(data.EntryN1I1V1)
	sn := &Snapshot{set: set}

	resources := sn.Resources(data.Collection1.String())
	g.Expect(resources).To(HaveLen(1))

	r, err := resource.Deserialize(resources[0])
	g.Expect(err).To(BeNil())
	g.Expect(r).To(Equal(data.EntryN1I1V1))

	v := sn.Version(data.Collection1.String())
	g.Expect(v).To(Equal("collection1/1"))

	expected := `[0] collection1 (@collection1/1)
  [0] n1/i1
`
	g.Expect(sn.String()).To(Equal(expected))
}

func TestSnapshot_SerializeError(t *testing.T) {
	g := NewGomegaWithT(t)

	set := coll.NewSet([]collection.Name{data.Collection1})
	e := data.Event1Col1AddItem1.Entry.Clone()
	e.Item = nil
	set.Collection(data.Collection1).Set(e)
	sn := &Snapshot{set: set}

	resources := sn.Resources(data.Collection1.String())
	g.Expect(resources).To(HaveLen(0))
}

func TestSnapshot_WrongCollection(t *testing.T) {
	g := NewGomegaWithT(t)

	set := coll.NewSet([]collection.Name{data.Collection1})
	set.Collection(data.Collection1).Set(data.Event1Col1AddItem1.Entry)
	sn := &Snapshot{set: set}

	g.Expect(sn.Version("foo")).To(Equal(""))
	g.Expect(sn.Resources("foo")).To(BeEmpty())
}
