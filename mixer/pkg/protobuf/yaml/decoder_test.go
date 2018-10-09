// Copyright 2018 Istio Authors.
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

package yaml

import (
	"reflect"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/jsonpb"

	"istio.io/istio/mixer/pkg/protobuf/yaml/testdata/all"
)

func TestDecoder(t *testing.T) {
	fds, err := GetFileDescSet("testdata/all/types.descriptor")
	if err != nil {
		t.Fatal(err)
	}

	for _, td := range []struct {
		name   string
		data   string
		fields map[string]bool
		want   map[string]interface{}
		err    bool
	}{
		{
			name: "bool",
			data: `b: true`,
			want: map[string]interface{}{
				"b": true,
			},
		},
		{
			name: "repeated bool",
			data: "r_b: [true, false, true]",
			want: map[string]interface{}{"r_b": []interface{}{true, false, true}},
		},
		{
			name: "unpacked repeated bool",
			data: "r_b_unpacked: [true, false, true]",
			want: map[string]interface{}{"r_b_unpacked": []interface{}{true, false, true}},
		},
		{
			name: "integers",
			data: `
i32: 12
i64: 123
ui32: 1234
ui64: 12345
si32: -123456
si64: -1234567`,
			want: map[string]interface{}{
				"i32":  int64(12),
				"i64":  int64(123),
				"ui32": int64(1234),
				"ui64": int64(12345),
				"si32": int64(-123456),
				"si64": int64(-1234567),
			},
		},
		{
			name: "repeated integers",
			data: `
r_i32:
- 123
r_i64:
- -123
r_ui32:
- 123
r_ui64:
- 123
r_si32:
- 123
- -456
r_si64:
- 123
- -456`,
			want: map[string]interface{}{
				"r_i32":  []interface{}{int64(123)},
				"r_i64":  []interface{}{int64(-123)},
				"r_ui32": []interface{}{int64(123)},
				"r_ui64": []interface{}{int64(123)},
				"r_si32": []interface{}{int64(123), int64(-456)},
				"r_si64": []interface{}{int64(123), int64(-456)},
			},
		},
		{
			name: "unpacked repeated integers",
			data: `
r_i32_unpacked:
- 123
r_i64_unpacked:
- -123
r_ui32_unpacked:
- 123
r_ui64_unpacked:
- 123
r_si32_unpacked:
- 123
- -456
r_si64_unpacked:
- 123
- -456`,
			want: map[string]interface{}{
				"r_i32_unpacked":  []interface{}{int64(123)},
				"r_i64_unpacked":  []interface{}{int64(-123)},
				"r_ui32_unpacked": []interface{}{int64(123)},
				"r_ui64_unpacked": []interface{}{int64(123)},
				"r_si32_unpacked": []interface{}{int64(123), int64(-456)},
				"r_si64_unpacked": []interface{}{int64(123), int64(-456)},
			},
		},
		{
			name: "negative integers",
			data: `
i32: -12
i64: -123`,
			want: map[string]interface{}{
				"i32": int64(-12),
				"i64": int64(-123),
			},
		},
		{
			name: "fixed integers",
			data: `
f32: 12
f64: 123
sf32: 1234
sf64: 12345`,
			want: map[string]interface{}{
				"f32":  int64(12),
				"f64":  int64(123),
				"sf32": int64(1234),
				"sf64": int64(12345),
			},
		},
		{
			name: "floats",
			data: `
flt: 1.12
dbl: 123.456`,
			want: map[string]interface{}{"flt": float32(1.12), "dbl": 123.456},
		},
		{
			name: "repeated fixed encoding",
			data: `
r_f32:
- 123
r_sf32:
- 1234
r_f64:
- 12345
r_sf64:
- 123456
r_dbl:
- 123.123
- 456.456
r_flt:
- 1.1
- 1.13`,
			want: map[string]interface{}{
				"r_f32":  []interface{}{int64(123)},
				"r_sf32": []interface{}{int64(1234)},
				"r_f64":  []interface{}{int64(12345)},
				"r_sf64": []interface{}{int64(123456)},
				"r_dbl":  []interface{}{123.123, 456.456},
				"r_flt":  []interface{}{float32(1.1), float32(1.13)},
			},
		},
		{
			name: "unpacked repeated fixed encoding",
			data: `
r_f32_unpacked:
- 123
r_sf32_unpacked:
- 1234
r_f64_unpacked:
- 12345
r_sf64_unpacked:
- 123456
r_dbl_unpacked:
- 123.123
- 456.456
r_flt_unpacked:
- 1.1
- 1.13`,
			want: map[string]interface{}{
				"r_f32_unpacked":  []interface{}{int64(123)},
				"r_sf32_unpacked": []interface{}{int64(1234)},
				"r_f64_unpacked":  []interface{}{int64(12345)},
				"r_sf64_unpacked": []interface{}{int64(123456)},
				"r_dbl_unpacked":  []interface{}{123.123, 456.456},
				"r_flt_unpacked":  []interface{}{float32(1.1), float32(1.13)},
			},
		},
		{
			name: "string",
			data: `str: "mystring"`,
			want: map[string]interface{}{"str": "mystring"},
		},
		{
			name: "repeated string",
			data: `r_str: ["a", "b"]`,
			want: map[string]interface{}{"r_str": []interface{}{"a", "b"}},
		},
		{
			name: "bytes",
			data: `byts: [24, 32]`,
			want: map[string]interface{}{"byts": []byte{24, 32}},
		},
		{
			name: "enum",
			data: `enm: TWO`,
			want: map[string]interface{}{"enm": int64(foo.TWO)},
		},
		{
			name: "repeated enum",
			data: `r_enm: [ONE, TWO]`,
			want: map[string]interface{}{"r_enm": []interface{}{int64(foo.ONE), int64(foo.TWO)}},
		},
		{
			name: "unpacked repeated enum",
			data: `r_enm_unpacked: [ONE, THREE]`,
			want: map[string]interface{}{"r_enm_unpacked": []interface{}{int64(foo.ONE), int64(foo.THREE)}},
		},
		{
			name: "field mask",
			data: `
str: "a"
i32: 12
i64: 123
ui32: 1234
ui64: 12345
si32: -123456
si64: -1234567
f32: 12
f64: 123
sf32: 1234
sf64: 12345
r_dbl:
- 123.123
- 456.456
r_flt_unpacked:
- 1.1
- 1.13`,
			fields: map[string]bool{"r_dbl": true},
			want: map[string]interface{}{
				"r_dbl": []interface{}{123.123, 456.456},
			},
		},
		{
			name: "no values",
			data: simpleNoValues,
			want: map[string]interface{}{},
		},
		{
			name: "string map",
			data: `
map_str_str:
  key1: val1
  key2: val2`,
			fields: map[string]bool{"map_str_str": true},
			want: map[string]interface{}{
				"map_str_str": map[string]string{
					"key1": "val1",
					"key2": "val2",
				},
			},
		},
		{
			name: "unsupported map",
			data: `
map_str_bool:
    key1: true`,
			err:  true,
			want: map[string]interface{}{},
		},
	} {
		t.Run(td.name, func(tt *testing.T) {
			jsonBytes, _ := yaml.YAMLToJSON([]byte(td.data))
			instance := &foo.Simple{}
			if err = jsonpb.UnmarshalString(string(jsonBytes), instance); err != nil {
				tt.Fatal(err)
			}
			bytes, err := instance.Marshal()
			if err != nil {
				tt.Fatal(err)
			}

			decoder := NewDecoder(NewResolver(fds), ".foo.Simple", td.fields)
			got, err := decoder.Decode(bytes)

			if td.err {
				if err == nil {
					t.Errorf("yaml.Decode(%q) => got no error, expect an error", td.name)
				}
			} else {
				if err != nil {
					t.Errorf("yaml.Decode(%q) => got an error %q, expect no errors", td.name, err)
				}
			}

			if !reflect.DeepEqual(got, td.want) {
				tt.Errorf("yaml.Decode(%q) => got %#v, want %#v", td.name, got, td.want)
			}
		})
	}
}
