//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package config

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/types"
)

func TestConverter(t *testing.T) {
	var cases = []struct {
		name     string
		input    string
		expected map[string]interface{}
	}{
		{
			name:     "empty",
			input:    `{}`,
			expected: map[string]interface{}{},
		},
		{
			name: "simple string",
			input: `{
"foo": "bar"
}`,
			expected: map[string]interface{}{
				"foo": "bar",
			},
		},
		{
			name: "simple bool",
			input: `{
"foo": true
}`,
			expected: map[string]interface{}{
				"foo": true,
			},
		},
		{
			name: "simple number",
			input: `{
"foo": 23
}`,
			expected: map[string]interface{}{
				"foo": float64(23),
			},
		},
		{
			name: "inline struct",
			input: `{
"foo": {
     "bar": "baz"
  }
}`,
			expected: map[string]interface{}{
				"foo": map[string]interface{}{
					"bar": "baz",
				},
			},
		},
		{
			name: "inline array",
			input: `{
"foo": [ "bar", "baz", "boo" ]
}`,
			expected: map[string]interface{}{
				"foo": []interface{}{"bar", "baz", "boo"},
			},
		},
		{
			name: "null",
			input: `{
"foo": null
}`,
			expected: map[string]interface{}{
				"foo": nil,
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pb := &types.Struct{}
			if err := jsonpb.Unmarshal(bytes.NewReader([]byte(c.input)), pb); err != nil {
				t.Fatalf("Error unmarshalling input data: %v", err)
			}

			dict, err := toDictionary(pb)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
				return
			}

			if !reflect.DeepEqual(c.expected, dict) {
				t.Fatalf("Mismatch. Got:\n%v\nWanted:\n%v\n", dict, c.expected)
			}
		})
	}
}

func TestConverter_Error(t *testing.T) {
	cases := []struct {
		name  string
		input *types.Struct
	}{
		{
			name: "top level nil field value",
			input: &types.Struct{
				Fields: map[string]*types.Value{
					"foo": nil,
				},
			},
		},
		{
			name: "inner nil field value",
			input: &types.Struct{
				Fields: map[string]*types.Value{
					"foo": {
						Kind: &types.Value_StructValue{
							StructValue: &types.Struct{
								Fields: map[string]*types.Value{
									"foo": nil,
								},
							},
						},
					},
				},
			},
		},
		{
			name: "array nil field value",
			input: &types.Struct{
				Fields: map[string]*types.Value{
					"foo": {
						Kind: &types.Value_ListValue{
							ListValue: &types.ListValue{
								Values: []*types.Value{
									nil,
								},
							},
						},
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := toDictionary(c.input)
			if err == nil {
				t.Fatal("expected error not found.")
			}
		})
	}
}
