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

package codegen

import (
	"strings"
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/config/schema/ast"
)

func TestStaticInit(t *testing.T) {
	var cases = []struct {
		packageName string
		packages    []string
		err         string
		output      string
	}{
		{
			packageName: "pkg",
			packages:    []string{"foo", "bar"},
			output: `
// GENERATED FILE -- DO NOT EDIT
//

package pkg

import (
	// Pull in all the known proto types to ensure we get their types registered.


	// Register protos in "bar"
	_ "bar"

	// Register protos in "foo"
	_ "foo"

)`,
		},
	}

	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			g := NewGomegaWithT(t)

			m := ast.Metadata{}
			for _, p := range c.packages {
				m.Resources = append(m.Resources, &ast.Resource{ProtoPackage: p})
			}
			s, err := StaticInit(c.packageName, &m)
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

func TestApplyTemplate_Error(t *testing.T) {
	g := NewGomegaWithT(t)

	_, err := applyTemplate(staticCollectionsTemplate, struct{}{})
	g.Expect(err).ToNot(BeNil())
}
