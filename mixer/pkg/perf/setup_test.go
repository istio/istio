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

	"istio.io/api/mixer/v1"
)

var tests = []struct {
	yaml  string
	setup Setup
}{
	{
		yaml: `
config:
  global: global
  identityAttribute: identityAttr
  identityAttributeDomain: identityAttrDomain
  rpcServer: rpcServer
load: {}
`,
		setup: Setup{
			Config: Config{
				Global:                  "global",
				Service:                 "rpcServer",
				IdentityAttribute:       "identityAttr",
				IdentityAttributeDomain: "identityAttrDomain",
			},
			Load: Load{},
		},
	},

	{
		yaml: `
config:
  global: global
  identityAttribute: identityAttr
  identityAttributeDomain: identityAttrDomain
  rpcServer: rpcServer
load:
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
		setup: Setup{
			Config: Config{
				Global:                  "global",
				Service:                 "rpcServer",
				IdentityAttribute:       "identityAttr",
				IdentityAttributeDomain: "identityAttrDomain",
			},
			Load: Load{
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
	},

	{
		yaml: `
config:
  global: global
  identityAttribute: destination.rpcServer
  identityAttributeDomain: svc.cluster.local
  rpcServer: rpcServer
load:
  iterations: 100
  requests:
  - attributes:
      target.name: somesrvcname
    type: basicReport
  - attributes:
      target.name: cvd
    type: basicReport
  - attributes:
      target.name: somesrvcname
    type: basicCheck
`,
		setup: Setup{
			Config: Config{
				Global:                  "global",
				Service:                 "rpcServer",
				IdentityAttribute:       `destination.rpcServer`,
				IdentityAttributeDomain: `svc.cluster.local`,
			},

			Load: Load{
				Multiplier: 100,
				Requests: []Request{
					BasicReport{
						Attributes: map[string]interface{}{"target.name": "somesrvcname"},
					},
					BasicReport{
						Attributes: map[string]interface{}{"target.name": "cvd"},
					},
					BasicCheck{
						Attributes: map[string]interface{}{
							"target.name": "somesrvcname",
						},
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
			actualBytes, err := marshallSetup(&test.setup)
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
			var actual Setup
			if err := unmarshallSetup([]byte(test.yaml), &actual); err != nil {
				tt.Fatalf("Unexpected error: %v", err)
			}
			actualBytes, err := marshallSetup(&actual)
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
load:
  stableOrder: AAAA
`,

		`
load:
  requests:
    - type:
      - a: b
`,

		`
load:
  requests:
    - type: boo
`,

		`
load:
  requests:
    - type: report
      attributes: 23
`,
	}

	for i, config := range configs {
		var setup Setup
		t.Run(fmt.Sprintf("%d", i), func(tt *testing.T) {
			if err := unmarshallSetup([]byte(config), &setup); err == nil {
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

func (b *BrokenRequest) createRequestProtos(c Config) []interface{} {
	return nil
}
