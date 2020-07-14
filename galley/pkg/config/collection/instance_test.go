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

package collection_test

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/testing/basicmeta"
	"istio.io/istio/galley/pkg/config/testing/data"
	"istio.io/istio/pkg/config/resource"
)

func TestInstance_Basics(t *testing.T) {
	g := NewGomegaWithT(t)

	inst := collection.New(basicmeta.K8SCollection1)

	g.Expect(inst.Size()).To(Equal(0))

	var fe []*resource.Instance
	inst.ForEach(func(r *resource.Instance) bool {
		fe = append(fe, r)
		return true
	})
	g.Expect(fe).To(HaveLen(0))

	g.Expect(inst.Generation()).To(Equal(int64(0)))

	inst.Set(data.EntryN1I1V2)
	inst.Set(data.EntryN2I2V2)

	g.Expect(inst.Size()).To(Equal(2))

	fe = nil
	inst.ForEach(func(r *resource.Instance) bool {
		fe = append(fe, r)
		return true
	})
	g.Expect(fe).To(HaveLen(2))

	g.Expect(inst.Generation()).To(Equal(int64(2)))

	inst.Remove(data.EntryN1I1V1.Metadata.FullName)

	g.Expect(inst.Size()).To(Equal(1))

	fe = nil
	inst.ForEach(func(r *resource.Instance) bool {
		fe = append(fe, r)
		return true
	})
	g.Expect(fe).To(HaveLen(1))

	g.Expect(inst.Generation()).To(Equal(int64(3)))

	inst.Clear()

	fe = nil
	inst.ForEach(func(r *resource.Instance) bool {
		fe = append(fe, r)
		return true
	})
	g.Expect(fe).To(HaveLen(0))

	g.Expect(inst.Generation()).To(Equal(int64(4)))
	g.Expect(inst.Size()).To(Equal(0))

}

func TestInstance_Clone(t *testing.T) {
	g := NewGomegaWithT(t)

	inst := collection.New(basicmeta.K8SCollection1)
	inst.Set(data.EntryN1I1V1)
	inst.Set(data.EntryN2I2V2)

	inst2 := inst.Clone()

	g.Expect(inst2.Size()).To(Equal(2))
	g.Expect(inst2.Generation()).To(Equal(int64(2)))

	var fe []*resource.Instance
	inst2.ForEach(func(r *resource.Instance) bool {
		fe = append(fe, r)
		return true
	})
	g.Expect(fe).To(HaveLen(2))

	inst.Remove(data.EntryN1I1V1.Metadata.FullName)

	g.Expect(inst2.Size()).To(Equal(2))
	g.Expect(inst2.Generation()).To(Equal(int64(2)))

	fe = nil
	inst2.ForEach(func(r *resource.Instance) bool {
		fe = append(fe, r)
		return true
	})

	g.Expect(fe).To(HaveLen(2))
}

func TestInstance_ForEach_False(t *testing.T) {
	g := NewGomegaWithT(t)

	inst := collection.New(basicmeta.K8SCollection1)
	inst.Set(data.EntryN1I1V2)
	inst.Set(data.EntryN2I2V2)
	inst.Set(data.EntryN3I3V1)

	var fe []*resource.Instance
	inst.ForEach(func(r *resource.Instance) bool {
		fe = append(fe, r)
		return false
	})
	g.Expect(fe).To(HaveLen(1))

	fe = nil
	inst.ForEach(func(r *resource.Instance) bool {
		fe = append(fe, r)
		return len(fe) < 2
	})
	g.Expect(fe).To(HaveLen(2))
}

func TestInstance_Get(t *testing.T) {
	g := NewGomegaWithT(t)

	inst := collection.New(basicmeta.K8SCollection1)
	inst.Set(data.EntryN1I1V1)
	inst.Set(data.EntryN3I3V1)

	e := inst.Get(data.EntryN1I1V1.Metadata.FullName)
	g.Expect(e).To(Equal(data.EntryN1I1V1))

	e = inst.Get(data.EntryN3I3V1.Metadata.FullName)
	g.Expect(e).To(Equal(data.EntryN3I3V1))

	e = inst.Get(data.EntryN2I2V2.Metadata.FullName)
	g.Expect(e).To(BeNil())
}
