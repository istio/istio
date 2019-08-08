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

package collection

import (
	"testing"

	_ "github.com/gogo/protobuf/types"
	. "github.com/onsi/gomega"
)

func TestSpecs_Basic(t *testing.T) {
	g := NewGomegaWithT(t)
	b := NewSpecsBuilder()

	s := MustNewSpec("foo", "github.com/gogo/protobuf/types", "google.protobuf.Empty")
	err := b.Add(s)
	g.Expect(err).To(BeNil())

	specs := b.Build()
	g.Expect(specs.All()).To(HaveLen(1))
	g.Expect(specs.All()[0]).To(Equal(s))
}

func TestSpecs_MustAdd(t *testing.T) {
	g := NewGomegaWithT(t)
	defer func() {
		r := recover()
		g.Expect(r).To(BeNil())
	}()
	b := NewSpecsBuilder()

	spec := MustNewSpec("foo", "github.com/gogo/protobuf/types", "google.protobuf.Empty")
	b.MustAdd(spec)
}

func TestSpecs_MustRegister_Panic(t *testing.T) {
	g := NewGomegaWithT(t)
	defer func() {
		r := recover()
		g.Expect(r).NotTo(BeNil())
	}()
	b := NewSpecsBuilder()

	spec := MustNewSpec("foo", "github.com/gogo/protobuf/types", "google.protobuf.Empty")
	b.MustAdd(spec)
	b.MustAdd(spec)
}

func TestSpecsBuilder_Remove(t *testing.T) {
	g := NewGomegaWithT(t)
	b := NewSpecsBuilder()

	spec := MustNewSpec("foo", "github.com/gogo/protobuf/types", "google.protobuf.Empty")
	b.MustAdd(spec)

	b.Remove(spec.Name)

	specs := b.Build()
	g.Expect(specs.All()).To(HaveLen(0))
}

func TestSpecsBuilder_RemoveSpecs(t *testing.T) {
	g := NewGomegaWithT(t)

	spec := MustNewSpec("foo", "github.com/gogo/protobuf/types", "google.protobuf.Empty")

	b1 := NewSpecsBuilder()
	b1.MustAdd(spec)

	b2 := NewSpecsBuilder()
	b2.MustAdd(spec)
	s := b2.Build()

	b1.UnregisterSpecs(s)
	s = b1.Build()
	g.Expect(s.All()).To(HaveLen(0))
}

func TestSpecs_Lookup(t *testing.T) {
	g := NewGomegaWithT(t)
	b := NewSpecsBuilder()

	spec := MustNewSpec("foo", "github.com/gogo/protobuf/types", "google.protobuf.Empty")

	b.MustAdd(spec)
	specs := b.Build()

	s2, found := specs.Lookup("foo")
	g.Expect(found).To(BeTrue())
	g.Expect(s2).To(Equal(spec))

	_, found = specs.Lookup("zoo")
	g.Expect(found).To(BeFalse())
}

func TestSpecs_Get(t *testing.T) {
	g := NewGomegaWithT(t)
	defer func() {
		r := recover()
		g.Expect(r).To(BeNil())
	}()

	b := NewSpecsBuilder()

	spec := MustNewSpec("foo", "github.com/gogo/protobuf/types", "google.protobuf.Empty")

	b.MustAdd(spec)
	specs := b.Build()

	s2 := specs.Get("foo")
	g.Expect(s2).To(Equal(spec))
}

func TestSpecs_Get_Panic(t *testing.T) {
	g := NewGomegaWithT(t)
	defer func() {
		r := recover()
		g.Expect(r).NotTo(BeNil())
	}()

	b := NewSpecsBuilder()

	spec := MustNewSpec("foo", "github.com/gogo/protobuf/types", "google.protobuf.Empty")

	b.MustAdd(spec)
	specs := b.Build()

	_ = specs.Get("zoo")
}

func TestSpecs_CollectionNames(t *testing.T) {
	g := NewGomegaWithT(t)
	b := NewSpecsBuilder()

	s1 := MustNewSpec("foo", "github.com/gogo/protobuf/types", "google.protobuf.Empty")
	s2 := MustNewSpec("bar", "github.com/gogo/protobuf/types", "google.protobuf.Empty")
	b.MustAdd(s1)
	b.MustAdd(s2)

	names := b.Build().CollectionNames()
	expected := []string{"bar", "foo"}
	g.Expect(names).To(Equal(expected))
}

func TestSpecs_Validate(t *testing.T) {
	g := NewGomegaWithT(t)
	b := NewSpecsBuilder()

	s1 := MustNewSpec("foo", "github.com/gogo/protobuf/types", "google.protobuf.Empty")
	s2 := MustNewSpec("bar", "github.com/gogo/protobuf/types", "google.protobuf.Empty")
	b.MustAdd(s1)
	b.MustAdd(s2)

	err := b.Build().Validate()
	g.Expect(err).To(BeNil())
}

func TestSpecs_Validate_Error(t *testing.T) {
	g := NewGomegaWithT(t)
	b := NewSpecsBuilder()

	s1 := MustNewSpec("foo", "github.com/gogo/protobuf/types", "zoo")
	b.MustAdd(s1)

	err := b.Build().Validate()
	g.Expect(err).NotTo(BeNil())
}
