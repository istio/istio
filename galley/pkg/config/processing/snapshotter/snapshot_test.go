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

	coll "istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/testing/basicmeta"
	"istio.io/istio/galley/pkg/config/testing/data"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
)

func TestSnapshot_Basics(t *testing.T) {
	g := NewGomegaWithT(t)

	set := coll.NewSet(collection.NewSchemasBuilder().MustAdd(basicmeta.K8SCollection1).Build())
	set.Collection(basicmeta.K8SCollection1.Name()).Set(data.EntryN1I1V1)
	sn := &Snapshot{set: set}

	resources := sn.Resources(basicmeta.K8SCollection1.Name().String())
	g.Expect(resources).To(HaveLen(1))

	r, err := resource.Deserialize(resources[0], basicmeta.K8SCollection1.Resource())
	g.Expect(err).To(BeNil())
	fixtures.ExpectEqual(t, r, data.EntryN1I1V1)

	v := sn.Version(basicmeta.K8SCollection1.Name().String())
	g.Expect(v).To(Equal(basicmeta.K8SCollection1.Name().String() + "/1"))

	expected := `[0] k8s/collection1 (@k8s/collection1/1)
  [0] n1/i1
`
	g.Expect(sn.String()).To(Equal(expected))
}

func TestSnapshot_SerializeError(t *testing.T) {
	g := NewGomegaWithT(t)

	set := coll.NewSet(collection.NewSchemasBuilder().MustAdd(basicmeta.K8SCollection1).Build())
	e := data.Event1Col1AddItem1.Resource.Clone()
	e.Message = nil
	set.Collection(basicmeta.K8SCollection1.Name()).Set(e)
	sn := &Snapshot{set: set}

	resources := sn.Resources(basicmeta.K8SCollection1.Name().String())
	g.Expect(resources).To(HaveLen(0))
}

func TestSnapshot_WrongCollection(t *testing.T) {
	g := NewGomegaWithT(t)

	set := coll.NewSet(collection.NewSchemasBuilder().MustAdd(basicmeta.K8SCollection1).Build())
	set.Collection(basicmeta.K8SCollection1.Name()).Set(data.Event1Col1AddItem1.Resource)
	sn := &Snapshot{set: set}

	g.Expect(sn.Version("foo")).To(Equal(""))
	g.Expect(sn.Resources("foo")).To(BeEmpty())
}
