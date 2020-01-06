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
	"testing"

	. "github.com/onsi/gomega"
)

func TestCommentBlock(t *testing.T) {
	cases := []struct {
		name       string
		input      []string
		indentTabs int
		expected   string
	}{
		{
			name: "single line",
			input: []string{
				"single line comment",
			},
			indentTabs: 1,
			expected:   "// single line comment",
		},
		{
			name: "single line",
			input: []string{
				"first line no indent",
				"second line has indent",
			},
			indentTabs: 3,
			expected: "// first line no indent\n" +
				"\t\t\t// second line has indent",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := NewGomegaWithT(t)
			output := commentBlock(c.input, c.indentTabs)
			g.Expect(output).To(Equal(c.expected))
		})
	}
}

func TestWordWrap(t *testing.T) {
	cases := []struct {
		name          string
		input         string
		maxLineLength int
		expected      []string
	}{
		{
			name:          "no wrap",
			input:         "no wrap is required",
			maxLineLength: 100,
			expected: []string{
				"no wrap is required",
			},
		},
		{
			name:          "wrap after word",
			input:         "wrap after word",
			maxLineLength: 11,
			expected: []string{
				"wrap after",
				"word",
			},
		},
		{
			name:          "wrap mid word",
			input:         "wrap mid-word",
			maxLineLength: 10,
			expected: []string{
				"wrap",
				"mid-word",
			},
		},
		{
			name:          "user carriage return",
			input:         "user carriage\nreturn",
			maxLineLength: 100,
			expected: []string{
				"user carriage",
				"return",
			},
		},
		{
			name: "multiple lines",
			input: "This is a long-winded example.\nIt shows:\n  -user-defined carriage returns\n  " +
				"-wrapping at the max line length\n  -removal of extra whitespace around words",
			maxLineLength: 22,
			expected: []string{
				"This is a long-winded",
				"example.",
				"It shows:",
				"-user-defined carriage",
				"returns",
				"-wrapping at the max",
				"line length",
				"-removal of extra",
				"whitespace around words",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := NewGomegaWithT(t)
			output := wordWrap(c.input, c.maxLineLength)
			g.Expect(output).To(Equal(c.expected))
		})
	}
}
