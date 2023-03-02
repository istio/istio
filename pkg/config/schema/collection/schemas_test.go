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
	_ "google.golang.org/protobuf/types/known/emptypb"
	_ "google.golang.org/protobuf/types/known/structpb"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/resource"
)

var (
	emptyResource = resource.Builder{
		Kind:         "Empty",
		Plural:       "empties",
		ProtoPackage: "google.golang.org/protobuf/types/known/emptypb",
		Proto:        "google.protobuf.Empty",
	}.MustBuild()

	structResource = resource.Builder{
		Kind:         "Struct",
		Plural:       "structs",
		ProtoPackage: "google.golang.org/protobuf/types/known/structpb",
		Proto:        "google.protobuf.Struct",
	}.MustBuild()
)

func TestSchemas_Basic(t *testing.T) {
	g := NewWithT(t)

	schemas := collection.SchemasFor(emptyResource)
	g.Expect(schemas.All()).To(HaveLen(1))
	g.Expect(schemas.All()[0]).To(Equal(emptyResource))
}

func TestSchemas_MustAdd(t *testing.T) {
	g := NewWithT(t)
	defer func() {
		r := recover()
		g.Expect(r).To(BeNil())
	}()
	b := collection.NewSchemasBuilder()

	b.MustAdd(emptyResource)
}

func TestSchemas_MustRegister_Panic(t *testing.T) {
	g := NewWithT(t)
	defer func() {
		r := recover()
		g.Expect(r).NotTo(BeNil())
	}()
	b := collection.NewSchemasBuilder()

	b.MustAdd(emptyResource)
	b.MustAdd(emptyResource)
}

func TestSchema_FindByGroupVersionKind(t *testing.T) {
	g := NewWithT(t)

	s := resource.Builder{
		ProtoPackage: "github.com/gogo/protobuf/types",
		Proto:        "google.protobuf.Empty",
		Group:        "mygroup",
		Kind:         "Empty",
		Plural:       "empties",
		Version:      "v1",
	}.MustBuild()

	schemas := collection.SchemasFor(s)

	s2, found := schemas.FindByGroupVersionKind(config.GroupVersionKind{
		Group:   "mygroup",
		Version: "v1",
		Kind:    "Empty",
	})
	g.Expect(found).To(BeTrue())
	g.Expect(s2).To(Equal(s))

	_, found = schemas.FindByGroupVersionKind(config.GroupVersionKind{
		Group:   "fake",
		Version: "v1",
		Kind:    "Empty",
	})
	g.Expect(found).To(BeFalse())
}

func TestSchemas_Kinds(t *testing.T) {
	g := NewWithT(t)

	s := collection.SchemasFor(emptyResource, structResource)

	actual := s.Kinds()
	expected := []string{emptyResource.Kind(), structResource.Kind()}
	g.Expect(actual).To(Equal(expected))
}

func TestSchemas_Validate(t *testing.T) {
	cases := []struct {
		name        string
		schemas     []resource.Schema
		expectError bool
	}{
		{
			name: "valid",
			schemas: []resource.Schema{
				resource.Builder{
					Kind:   "Empty1",
					Plural: "Empty1s",
					Proto:  "google.protobuf.Empty",
				}.MustBuild(),
				resource.Builder{
					Kind:   "Empty2",
					Plural: "Empty2s",
					Proto:  "google.protobuf.Empty",
				}.MustBuild(),
			},
			expectError: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := NewWithT(t)
			b := collection.NewSchemasBuilder()
			for _, s := range c.schemas {
				b.MustAdd(s)
			}
			err := b.Build().Validate()
			if c.expectError {
				g.Expect(err).ToNot(BeNil())
			} else {
				g.Expect(err).To(BeNil())
			}
		})
	}
}

func TestSchemas_Validate_Error(t *testing.T) {
	g := NewWithT(t)
	b := collection.NewSchemasBuilder()

	s1 := resource.Builder{
		Kind:         "Zoo",
		ProtoPackage: "github.com/gogo/protobuf/types",
		Proto:        "zoo",
	}.BuildNoValidate()
	b.MustAdd(s1)

	err := b.Build().Validate()
	g.Expect(err).NotTo(BeNil())
}

func TestSchemas_ForEach(t *testing.T) {
	schemas := collection.SchemasFor(emptyResource, structResource)

	cases := []struct {
		name     string
		expected []string
		actual   func() []string
	}{
		{
			name:     "all",
			expected: []string{"Empty", "Struct"},
			actual: func() []string {
				a := make([]string, 0)
				schemas.ForEach(func(s resource.Schema) bool {
					a = append(a, s.Kind())
					return false
				})
				return a
			},
		},
		{
			name:     "exit early",
			expected: []string{"Empty"},
			actual: func() []string {
				a := make([]string, 0)
				schemas.ForEach(func(s resource.Schema) bool {
					a = append(a, s.Kind())
					return true
				})
				return a
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := NewWithT(t)
			actual := c.actual()
			g.Expect(actual).To(Equal(c.expected))
		})
	}
}

func TestSchemas_Remove(t *testing.T) {
	g := NewWithT(t)

	schemas := collection.SchemasFor(emptyResource, structResource)
	g.Expect(schemas.Remove(structResource)).To(Equal(collection.SchemasFor(emptyResource)))
	g.Expect(schemas.Remove(emptyResource, structResource)).To(Equal(collection.SchemasFor()))
	g.Expect(schemas).To(Equal(collection.SchemasFor(emptyResource, structResource)))
}

func TestSchemas_Add(t *testing.T) {
	g := NewWithT(t)

	schemas := collection.SchemasFor(emptyResource)
	g.Expect(schemas.Add(structResource)).To(Equal(collection.SchemasFor(emptyResource, structResource)))
	g.Expect(schemas).To(Equal(collection.SchemasFor(emptyResource)))
}
