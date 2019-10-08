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

package codegen

import (
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

func TestStaticCollections(t *testing.T) {
	var cases = []struct {
		packageName string
		collections []string
		err         string
		output      string
	}{
		{
			packageName: "pkg",
			collections: []string{"foo", "bar"},
			output: `
// GENERATED FILE -- DO NOT EDIT
//

package pkg

import (
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
)

var (

	// Bar is the name of collection bar
	Bar = collection.NewName("bar")

	// Foo is the name of collection foo
	Foo = collection.NewName("foo")

)

// CollectionNames returns the collection names declared in this package.
func CollectionNames() []collection.Name {
	return []collection.Name {
		Bar,
		Foo,
		
	}
}`,
		},
	}

	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			g := NewGomegaWithT(t)

			s, err := StaticCollections(c.packageName, c.collections)
			if c.err != "" {
				g.Expect(err).NotTo(BeNil())
				g.Expect(err.Error()).To(Equal(s))
			} else {
				g.Expect(err).To(BeNil())
				g.Expect(strings.TrimSpace(s)).To(Equal(strings.TrimSpace(c.output)))
			}
		})
	}
}
