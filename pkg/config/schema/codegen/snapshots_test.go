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

func TestStaticSnapshots(t *testing.T) {
	var cases = []struct {
		packageName string
		snapshots   []*ast.Snapshot
		err         string
		output      string
	}{
		{
			packageName: "pkg",
			snapshots: []*ast.Snapshot{
				{
					Name:         "foo",
					VariableName: "Foo",
					Description:  "this is a really cool foo snapshot",
				},
				{
					Name:         "bar",
					VariableName: "Bar",
					Description:  "this is a really cool bar snapshot",
				},
			},
			output: `
// GENERATED FILE -- DO NOT EDIT
//

package pkg

var (

	// Bar this is a really cool bar snapshot
	Bar = "bar"

	// Foo this is a really cool foo snapshot
	Foo = "foo"

)

// SnapshotNames returns the snapshot names declared in this package.
func SnapshotNames() []string {
	return []string {
		Bar,
		Foo,
		
	}
}`,
		},
	}

	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			g := NewGomegaWithT(t)

			s, err := StaticSnapshots(c.packageName, &ast.Metadata{
				Snapshots: c.snapshots,
			})
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
