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

package perf

import (
	"fmt"
	"reflect"
	"testing"

	istio_mixer_v1 "istio.io/api/mixer/v1"
)

var tests = []struct {
	load Load
}{
	{
		load: Load{
			Requests: []Request{},
		},
	},
	{
		Load{
			Multiplier:  2,
			StableOrder: true,
			RandomSeed:  123,
			Requests: []Request{
				BuildBasicReport(
					map[string]interface{}{
						"foo": "bar",
						"baz": int64(42),
					}),
				BuildBasicCheck(
					map[string]interface{}{
						"bar": "baz",
						"foo": int64(23),
					},
					map[string]istio_mixer_v1.CheckRequest_QuotaParams{
						"q1": {
							Amount:     23,
							BestEffort: true,
						},
						"q2": {
							Amount:     54,
							BestEffort: false,
						},
					}),
			},
		},
	},
	{
		load: Load{
			Multiplier: 100,
			Requests: []Request{
				BuildBasicReport(map[string]interface{}{"destination.name": "somesrvcname"}),
				BuildBasicReport(map[string]interface{}{"destination.name": "cvd"}),
				BuildBasicCheck(map[string]interface{}{"destination.name": "somesrvcname"}, nil),
			},
		},
	},
}

func TestRoundtrip(t *testing.T) {
	for i, test := range tests {
		name := fmt.Sprintf("%d", i)
		t.Run(name, func(tt *testing.T) {
			loadBytes, err := marshalLoad(&test.load)
			if err != nil {
				tt.Fatalf("Unexpected error: %v", err)
			}

			var loadCmp Load
			if err := unmarshalLoad(loadBytes, &loadCmp); err != nil {
				tt.Fatalf("Unexpected error: %v", err)
			}

			if !reflect.DeepEqual(test.load, loadCmp) {
				tt.Fatalf("mismatch: '%v' != '%v'\n", test.load, loadCmp)
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
			if err := unmarshalLoad([]byte(config), &load); err == nil {
				tt.Fatal("expected error was not thrown")
			}
		})
	}
}

func TestReportMarshal_Error(t *testing.T) {

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Marshal logic did not panic")
		}
	}()

	r := BuildBasicReport(map[string]interface{}{
		"foo": func() {},
	})
	r.MarshalJSON()
}

type BrokenRequest struct {
}

var _ Request = &BrokenRequest{}

func (b *BrokenRequest) MarshalJSON() ([]byte, error) {
	return nil, fmt.Errorf("marshal error")
}

func (b *BrokenRequest) getRequestProto() interface{} {
	return nil
}
