// Copyright 2018 Istio Authors
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

package schema_test

import (
	"testing"

	"github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/source/kube/schema"
)

func TestSchemaBuilder(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	spec := schema.ResourceSpec{
		Kind:     "kind",
		Version:  "version",
		ListKind: "listkind",
		Plural:   "plural",
		Singular: "singular",
		Group:    "groupd",
	}

	s := schema.NewBuilder().Add(spec).Build()
	actual := s.All()
	expected := []schema.ResourceSpec{spec}

	g.Expect(actual).To(gomega.Equal(expected))
}

func TestGet(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	spec := schema.ResourceSpec{
		Kind:     "kind",
		Version:  "version",
		ListKind: "listkind",
		Plural:   "plural",
		Singular: "singular",
		Group:    "groupd",
	}
	s := schema.New(spec)

	actual := s.Get("kind")
	g.Expect(actual).ToNot(gomega.BeNil())
	g.Expect(*actual).To(gomega.Equal(spec))
}

func TestGetNotFound(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	spec := schema.ResourceSpec{
		Kind:     "kind",
		Version:  "version",
		ListKind: "listkind",
		Plural:   "plural",
		Singular: "singular",
		Group:    "groupd",
	}
	s := schema.New(spec)

	actual := s.Get("someotherkind")
	g.Expect(actual).To(gomega.BeNil())
}
