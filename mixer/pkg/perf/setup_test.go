// Copyright 2017 Istio Authors
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

package perf

import (
	"fmt"
	"strings"
	"testing"

	istio_mixer_v1 "istio.io/api/mixer/v1"
)

var tests = []struct {
	yaml string
	load Load
}{
	{
		yaml: `{}`,
		load: Load{},
	},

	{
		yaml: `
iterations: 2
randomSeed: 123
requests:
- attributes:
   baz: 42
   foo: bar
  type: basicReport
- attributes:
   bar: baz
   foo: 23
  quotas:
   q1:
     amount: 23
     best_effort: true
   q2:
     amount: 54
  type: basicCheck
stableOrder: true
  `,
		load: Load{
			Multiplier:  2,
			StableOrder: true,
			RandomSeed:  123,
			Requests: []Request{
				BasicReport{
					Attributes: map[string]interface{}{
						"foo": "bar",
						"baz": 42,
					},
				},
				BasicCheck{
					Attributes: map[string]interface{}{
						"bar": "baz",
						"foo": 23,
					},
					Quotas: map[string]istio_mixer_v1.CheckRequest_QuotaParams{
						"q1": {
							Amount:     23,
							BestEffort: true,
						},
						"q2": {
							Amount:     54,
							BestEffort: false,
						},
					},
				},
			},
		},
	},
	{
		yaml: `

iterations: 100
requests:
- attributes:
    destination.name: somesrvcname
  type: basicReport
- attributes:
    destination.name: cvd
  type: basicReport
- attributes:
    destination.name: somesrvcname
  type: basicCheck
`,
		load: Load{
			Multiplier: 100,
			Requests: []Request{
				BasicReport{
					Attributes: map[string]interface{}{"destination.name": "somesrvcname"},
				},
				BasicReport{
					Attributes: map[string]interface{}{"destination.name": "cvd"},
				},
				BasicCheck{
					Attributes: map[string]interface{}{
						"destination.name": "somesrvcname",
					},
				},
			},
		},
	},
}

func TestSetupToYaml(t *testing.T) {
	for i, test := range tests {
		name := fmt.Sprintf("%d", i)
		t.Run(name, func(tt *testing.T) {
			actualBytes, err := marshallLoad(&test.load)
			if err != nil {
				tt.Fatalf("Unexpected error: %v", err)
			}
			actual := string(actualBytes)
			actCmp := strings.TrimSpace(strings.Replace(actual, " ", "", -1))
			expCmp := strings.TrimSpace(strings.Replace(test.yaml, " ", "", -1))
			if actCmp != expCmp {
				tt.Fatalf("mismatch: '%v' != '%v'\n", actual, test.yaml)
			}
		})
	}
}

func TestRoundtrip(t *testing.T) {
	for i, test := range tests {
		name := fmt.Sprintf("%d", i)
		t.Run(name, func(tt *testing.T) {
			var actual Load
			if err := unmarshallLoad([]byte(test.yaml), &actual); err != nil {
				tt.Fatalf("Unexpected error: %v", err)
			}
			actualBytes, err := marshallLoad(&actual)
			if err != nil {
				tt.Fatalf("unexpected marshal error: %v", err)
			}
			actualString := string(actualBytes)
			actCmp := strings.TrimSpace(strings.Replace(actualString, " ", "", -1))
			expCmp := strings.TrimSpace(strings.Replace(test.yaml, " ", "", -1))
			if actCmp != expCmp {
				tt.Fatalf("mismatch: '%v' != '%v'\n", actualString, test.yaml)
			}
		})
	}
}

func TestMarshallRequestError(t *testing.T) {
	l := Load{
		Requests: []Request{
			&BrokenRequest{},
		},
	}

	if _, err := l.MarshalJSON(); err == nil {
		t.Fatal("expected error was not thrown")
	}
}

func TestUnmarshal_Errors(t *testing.T) {
	var configs = []string{
		`
stableOrder: AAAA
`,

		`

requests:
  - type:
    - a: b
`,

		`
requests:
  - type: boo
`,

		`
requests:
  - type: report
    attributes: 23
`,
	}

	for i, config := range configs {
		var load Load
		t.Run(fmt.Sprintf("%d", i), func(tt *testing.T) {
			if err := unmarshallLoad([]byte(config), &load); err == nil {
				tt.Fatal("expected error was not thrown")
			}
		})
	}
}

func TestReportMarshal_Error(t *testing.T) {

	r := &BasicReport{
		Attributes: map[string]interface{}{
			"foo": func() {},
		},
	}

	if _, err := r.MarshalJSON(); err == nil {
		t.Fail()
	}
}

type BrokenRequest struct {
}

var _ Request = &BrokenRequest{}

func (b *BrokenRequest) MarshalJSON() ([]byte, error) {
	return nil, fmt.Errorf("marshal error")
}

func (b *BrokenRequest) createRequestProtos() []interface{} {
	return nil
}
