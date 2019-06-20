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

package constraint

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestParse(t *testing.T) {
	var cases = []struct {
		Input    string
		Expected *Constraints
	}{
		{
			Input:    ``,
			Expected: &Constraints{},
		},
		{
			Input: `
constraints:
`,
			Expected: &Constraints{},
		},

		{
			Input: `
constraints:
  - collection: foo
`,
			Expected: &Constraints{
				Constraints: []*Collection{
					{
						Name: "foo",
					},
				},
			},
		},

		{
			Input: `
constraints:
  - collection: foo
    check:
`,
			Expected: &Constraints{
				Constraints: []*Collection{
					{
						Name: "foo",
					},
				},
			},
		},

		{
			Input: `
constraints:
  - collection: foo
    check:
    - exactlyOne:
`,
			Expected: &Constraints{
				Constraints: []*Collection{
					{
						Name: "foo",
						Check: []Range{
							&ExactlyOne{},
						},
					},
				},
			},
		},

		{
			Input: `
constraints:
  - collection: foo
    check:
    - any:
`,
			Expected: &Constraints{
				Constraints: []*Collection{
					{
						Name: "foo",
						Check: []Range{
							&Any{},
						},
					},
				},
			},
		},

		{
			Input: `
constraints:
  - collection: foo
    check:
    - exactlyOne:
      - select: foo
        exists: true
`,
			Expected: &Constraints{
				Constraints: []*Collection{
					{
						Name: "foo",
						Check: []Range{
							&ExactlyOne{
								Constraints: []Check{
									&Select{
										Expression: "foo",
										Op:         SelectExists,
									},
								},
							},
						},
					},
				},
			},
		},

		{
			Input: `
constraints:
  - collection: foo
    check:
    - exactlyOne:
      - select: foo
        exists: false
`,
			Expected: &Constraints{
				Constraints: []*Collection{
					{
						Name: "foo",
						Check: []Range{
							&ExactlyOne{
								Constraints: []Check{
									&Select{
										Expression: "foo",
										Op:         SelectNotExists,
									},
								},
							},
						},
					},
				},
			},
		},

		{
			Input: `
constraints:
  - collection: foo
    check:
    - exactlyOne:
      - select: foo
        equals:
          boo: bar
`,
			Expected: &Constraints{
				Constraints: []*Collection{
					{
						Name: "foo",
						Check: []Range{
							&ExactlyOne{
								Constraints: []Check{
									&Select{
										Expression: "foo",
										Op:         SelectEquals,
										Arg: map[string]interface{}{
											"boo": "bar",
										},
									},
								},
							},
						},
					},
				},
			},
		},

		{
			Input: `
constraints:
  - collection: foo
    check:
    - exactlyOne:
      - select: foo
        equals:
          - boo: bar
`,
			Expected: &Constraints{
				Constraints: []*Collection{
					{
						Name: "foo",
						Check: []Range{
							&ExactlyOne{
								Constraints: []Check{
									&Select{
										Expression: "foo",
										Op:         SelectEquals,
										Arg: []interface{}{
											map[string]interface{}{
												"boo": "bar",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},

		{
			Input: `
constraints:
  - collection: foo
    check:
    - any:
      - select: foo
        then:
        - select: bar
          exists: true
`,
			Expected: &Constraints{
				Constraints: []*Collection{
					{
						Name: "foo",
						Check: []Range{
							&Any{
								Constraints: []Check{
									&Select{
										Expression: "foo",
										Op:         SelectGroup,
										Children: []*Select{
											{
												Expression: "bar",
												Op:         SelectExists,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			g := NewGomegaWithT(t)
			cons, err := Parse([]byte(c.Input))
			g.Expect(err).To(BeNil())

			// Clear parentage to simplify testing
			for _, c := range cons.Constraints {
				for _, d := range c.Check {
					var constr []Check
					switch v := d.(type) {
					case *ExactlyOne:
						constr = v.Constraints
					case *Any:
						constr = v.Constraints
					}

					for _, e := range constr {
						s, ok := e.(*Select)
						if ok {
							s.Parent = nil
						}

						for _, c := range s.Children {
							c.Parent = nil
						}
					}
				}
			}
			g.Expect(cons).To(Equal(c.Expected))
		})
	}
}
