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

	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/resource"
)

func TestSchema_NewSchema(t *testing.T) {
	g := NewGomegaWithT(t)

	s, err := collection.Builder{
		Name:     "foo",
		Resource: emptyResource,
	}.Build()
	g.Expect(err).To(BeNil())
	g.Expect(s.Name()).To(Equal(collection.NewName("foo")))
	g.Expect(s.Resource().ProtoPackage()).To(Equal("github.com/gogo/protobuf/types"))
	g.Expect(s.Resource().Proto()).To(Equal("google.protobuf.Empty"))
}

func TestSchema_NewSchema_Error(t *testing.T) {
	g := NewGomegaWithT(t)

	_, err := collection.Builder{
		Name:     "$",
		Resource: emptyResource,
	}.Build()
	g.Expect(err).NotTo(BeNil())
}

func TestSchema_MustNewSchema(t *testing.T) {
	g := NewGomegaWithT(t)
	defer func() {
		r := recover()
		g.Expect(r).To(BeNil())
	}()

	s := collection.Builder{
		Name:     "foo",
		Resource: emptyResource,
	}.MustBuild()
	g.Expect(s.Name()).To(Equal(collection.NewName("foo")))
	g.Expect(s.Resource().ProtoPackage()).To(Equal("github.com/gogo/protobuf/types"))
	g.Expect(s.Resource().Proto()).To(Equal("google.protobuf.Empty"))
}

func TestSchema_MustNewSchema_Error(t *testing.T) {
	g := NewGomegaWithT(t)
	defer func() {
		r := recover()
		g.Expect(r).NotTo(BeNil())
	}()

	collection.Builder{
		Name: "$",
		Resource: resource.Builder{
			ProtoPackage: "github.com/gogo/protobuf/types",
			Proto:        "google.protobuf.Empty",
		}.MustBuild(),
	}.MustBuild()
}

func TestSchema_String(t *testing.T) {
	g := NewGomegaWithT(t)

	s := collection.Builder{
		Name: "foo",
		Resource: resource.Builder{
			Kind:         "Empty",
			Plural:       "empties",
			ProtoPackage: "github.com/gogo/protobuf/types",
			Proto:        "google.protobuf.Empty",
		}.MustBuild(),
	}.MustBuild()

	g.Expect(s.String()).To(Equal(`[Schema](foo, "github.com/gogo/protobuf/types", google.protobuf.Empty)`))
}
