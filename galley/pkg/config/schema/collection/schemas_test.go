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

	"istio.io/istio/galley/pkg/config/schema/resource"
)

func TestSchemas_Basic(t *testing.T) {
	g := NewGomegaWithT(t)
	b := NewSchemasBuilder()

	s := Builder{
		Name: "foo",
		Schema: resource.Builder{
			ProtoPackage: "github.com/gogo/protobuf/types",
			Proto:        "google.protobuf.Empty",
		}.Build(),
	}.MustBuild()
	err := b.Add(s)
	g.Expect(err).To(BeNil())

	schema := b.Build()
	g.Expect(schema.All()).To(HaveLen(1))
	g.Expect(schema.All()[0]).To(Equal(s))
}

func TestSchemas_MustAdd(t *testing.T) {
	g := NewGomegaWithT(t)
	defer func() {
		r := recover()
		g.Expect(r).To(BeNil())
	}()
	b := NewSchemasBuilder()

	s := Builder{
		Name: "foo",
		Schema: resource.Builder{
			ProtoPackage: "github.com/gogo/protobuf/types",
			Proto:        "google.protobuf.Empty",
		}.Build(),
	}.MustBuild()
	b.MustAdd(s)
}

func TestSchemas_MustRegister_Panic(t *testing.T) {
	g := NewGomegaWithT(t)
	defer func() {
		r := recover()
		g.Expect(r).NotTo(BeNil())
	}()
	b := NewSchemasBuilder()

	s := Builder{
		Name: "foo",
		Schema: resource.Builder{
			ProtoPackage: "github.com/gogo/protobuf/types",
			Proto:        "google.protobuf.Empty",
		}.Build(),
	}.MustBuild()
	b.MustAdd(s)
	b.MustAdd(s)
}

func TestSchemasBuilder_Remove(t *testing.T) {
	g := NewGomegaWithT(t)
	b := NewSchemasBuilder()

	s := Builder{
		Name: "foo",
		Schema: resource.Builder{
			ProtoPackage: "github.com/gogo/protobuf/types",
			Proto:        "google.protobuf.Empty",
		}.Build(),
	}.MustBuild()
	b.MustAdd(s)

	b.Remove(s.Name())

	schemas := b.Build()
	g.Expect(schemas.All()).To(HaveLen(0))
}

func TestSchemasBuilder_RemoveSpecs(t *testing.T) {
	g := NewGomegaWithT(t)

	s := Builder{
		Name: "foo",
		Schema: resource.Builder{
			ProtoPackage: "github.com/gogo/protobuf/types",
			Proto:        "google.protobuf.Empty",
		}.Build(),
	}.MustBuild()

	b1 := NewSchemasBuilder()
	b1.MustAdd(s)

	b2 := NewSchemasBuilder()
	b2.MustAdd(s)
	schemas := b2.Build()

	b1.UnregisterSchemas(schemas)
	schemas = b1.Build()
	g.Expect(schemas.All()).To(HaveLen(0))
}

func TestSchemas_Find(t *testing.T) {
	g := NewGomegaWithT(t)
	b := NewSchemasBuilder()

	s := Builder{
		Name: "foo",
		Schema: resource.Builder{
			ProtoPackage: "github.com/gogo/protobuf/types",
			Proto:        "google.protobuf.Empty",
		}.Build(),
	}.MustBuild()

	b.MustAdd(s)
	schemas := b.Build()

	s2, found := schemas.Find("foo")
	g.Expect(found).To(BeTrue())
	g.Expect(s2).To(Equal(s))

	_, found = schemas.Find("zoo")
	g.Expect(found).To(BeFalse())
}

func TestSchemas_MustFind(t *testing.T) {
	g := NewGomegaWithT(t)
	defer func() {
		r := recover()
		g.Expect(r).To(BeNil())
	}()

	b := NewSchemasBuilder()

	s := Builder{
		Name: "foo",
		Schema: resource.Builder{
			ProtoPackage: "github.com/gogo/protobuf/types",
			Proto:        "google.protobuf.Empty",
		}.Build(),
	}.MustBuild()

	b.MustAdd(s)
	schemas := b.Build()

	s2 := schemas.MustFind("foo")
	g.Expect(s2).To(Equal(s))
}

func TestSchemas_MustFind_Panic(t *testing.T) {
	g := NewGomegaWithT(t)
	defer func() {
		r := recover()
		g.Expect(r).NotTo(BeNil())
	}()

	b := NewSchemasBuilder()

	s := Builder{
		Name: "foo",
		Schema: resource.Builder{
			ProtoPackage: "github.com/gogo/protobuf/types",
			Proto:        "google.protobuf.Empty",
		}.Build(),
	}.MustBuild()

	b.MustAdd(s)
	schemas := b.Build()

	_ = schemas.MustFind("zoo")
}

func TestSchema_FindByGroupAndKind(t *testing.T) {
	g := NewGomegaWithT(t)
	b := NewSchemasBuilder()

	s := Builder{
		Name: "foo",
		Schema: resource.Builder{
			ProtoPackage: "github.com/gogo/protobuf/types",
			Proto:        "google.protobuf.Empty",
			Group:        "mygroup",
			Kind:         "Empty",
		}.Build(),
	}.MustBuild()

	b.MustAdd(s)
	schemas := b.Build()

	s2, found := schemas.FindByGroupAndKind("mygroup", "Empty")
	g.Expect(found).To(BeTrue())
	g.Expect(s2).To(Equal(s))

	_, found = schemas.FindByGroupAndKind("bad", "bad")
	g.Expect(found).To(BeFalse())
}

func TestSchema_MustFind(t *testing.T) {
	g := NewGomegaWithT(t)
	b := NewSchemasBuilder()

	s := Builder{
		Name: "foo",
		Schema: resource.Builder{
			ProtoPackage: "github.com/gogo/protobuf/types",
			Proto:        "google.protobuf.Empty",
			Group:        "mygroup",
			Kind:         "Empty",
		}.Build(),
	}.MustBuild()

	b.MustAdd(s)
	schemas := b.Build()

	s2 := schemas.MustFindByGroupAndKind("mygroup", "Empty")
	g.Expect(s2).To(Equal(s))
}

func TestSchema_MustFind_Panic(t *testing.T) {
	g := NewGomegaWithT(t)

	defer func() {
		r := recover()
		g.Expect(r).NotTo(BeNil())
	}()

	schemas := NewSchemasBuilder().Build()
	_ = schemas.MustFindByGroupAndKind("mygroup", "Empty")
}

func TestSchemas_CollectionNames(t *testing.T) {
	g := NewGomegaWithT(t)
	b := NewSchemasBuilder()

	s1 := Builder{
		Name: "foo",
		Schema: resource.Builder{
			ProtoPackage: "github.com/gogo/protobuf/types",
			Proto:        "google.protobuf.Empty",
		}.Build(),
	}.MustBuild()
	s2 := Builder{
		Name: "bar",
		Schema: resource.Builder{
			ProtoPackage: "github.com/gogo/protobuf/types",
			Proto:        "google.protobuf.Empty",
		}.Build(),
	}.MustBuild()
	b.MustAdd(s1)
	b.MustAdd(s2)

	names := b.Build().CollectionNames()
	expected := Names{NewName("bar"), NewName("foo")}
	g.Expect(names).To(Equal(expected))
}

func TestSchemas_Validate(t *testing.T) {
	g := NewGomegaWithT(t)
	b := NewSchemasBuilder()

	s1 := Builder{
		Name: "foo",
		Schema: resource.Builder{
			ProtoPackage: "github.com/gogo/protobuf/types",
			Proto:        "google.protobuf.Empty",
		}.Build(),
	}.MustBuild()
	s2 := Builder{
		Name: "bar",
		Schema: resource.Builder{
			ProtoPackage: "github.com/gogo/protobuf/types",
			Proto:        "google.protobuf.Empty",
		}.Build(),
	}.MustBuild()
	b.MustAdd(s1)
	b.MustAdd(s2)

	err := b.Build().Validate()
	g.Expect(err).To(BeNil())
}

func TestSchemas_Validate_Error(t *testing.T) {
	g := NewGomegaWithT(t)
	b := NewSchemasBuilder()

	s1 := Builder{
		Name: "foo",
		Schema: resource.Builder{
			ProtoPackage: "github.com/gogo/protobuf/types",
			Proto:        "zoo",
		}.Build(),
	}.MustBuild()
	b.MustAdd(s1)

	err := b.Build().Validate()
	g.Expect(err).NotTo(BeNil())
}
