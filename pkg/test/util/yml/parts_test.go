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

package yml_test

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/yml"
)

func TestEmptyDoc(t *testing.T) {
	g := NewWithT(t)

	yaml := `
`
	parts := yml.SplitString(yaml)
	g.Expect(len(parts)).To(Equal(0))

	yaml = yml.JoinString(parts...)
	g.Expect(yaml).To(Equal(""))
}

func TestSplitWithEmptyPart(t *testing.T) {
	expected := []string{
		"a",
		"b",
	}

	cases := []struct {
		name string
		doc  string
	}{
		{
			name: "beginningNoCR",
			doc: `---
a
---
b
`,
		},
		{
			name: "beginningWithCR",
			doc: `
---
a
---
b
`,
		},
		{
			name: "middle",
			doc: `a
---
---
b
`,
		},
		{
			name: "end",
			doc: `
a
---
b
---
`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			parts := yml.SplitString(c.doc)
			assert.Equal(t, parts, expected)
		})
	}
}

func TestSplitWithNestedDocument(t *testing.T) {
	doc := `
b
    b1
    ---
    b2
`
	expected := []string{
		`b
    b1
    ---
    b2`,
	}

	g := NewWithT(t)
	parts := yml.SplitString(doc)
	g.Expect(parts).To(Equal(expected))
}

func TestJoinRemovesEmptyParts(t *testing.T) {
	parts := []string{
		`---
---
---
`,
		`
---
a
---
`,
		`
b
---
`,
	}

	expected := `a
---
b`

	g := NewWithT(t)
	doc := yml.JoinString(parts...)
	g.Expect(doc).To(Equal(expected))
}

func TestSplitWithLongPart(t *testing.T) {
	longPartA := ""
	longPartB := ""
	for range 70000 {
		longPartA += "a"
		longPartB += "b"
	}

	doc := longPartA + "\n---\n" + longPartB

	parts := yml.SplitString(doc)

	g := NewWithT(t)

	g.Expect(len(parts)).To(Equal(2))
	g.Expect(parts[0]).To(Equal(longPartA))
	g.Expect(parts[1]).To(Equal(longPartB))
}
