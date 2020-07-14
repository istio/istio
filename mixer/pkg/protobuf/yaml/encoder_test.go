// Copyright Istio Authors.
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

// nolint
//go:generate $REPO_ROOT/bin/protoc.sh testdata/all/types.proto  --include_imports  -otestdata/all/types.descriptor -I.
//go:generate $REPO_ROOT/bin/mixer_codegen.sh  -d false -f mixer/pkg/protobuf/yaml/testdata/all/types.proto

package yaml

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"

	foo "istio.io/istio/mixer/pkg/protobuf/yaml/testdata/all"
)

var (
	simple = `
enm: TWO

r_enm:
- TWO
- THREE

r_enm_unpacked:
- ONE
- TWO
- ONE
- TWO

#### string ####
str: "mystring"
r_str:
- abcd
- "pqrs"

#### bool ####
b: true
r_b:
- true
- false
- true
r_b_unpacked:
- true
- false
- true

#### double ####
dbl: 123.456
r_dbl:
- 123.123
- 456.456
r_dbl_unpacked:
- 123.123
- 456.456

#### float ####
flt: 1.12
r_flt:
- 1.12
- 1.13
r_flt_unpacked:
- 1.1
- 1.13

#### int64 with negative val ####
i64: 123
r_i64:
- 123
r_i64_unpacked:
- -123

#### int32 with negative val ####
i32: 123
r_i32:
- -123
r_i32_unpacked:
- 123

#### uint64 ####
ui64: 123
r_ui64:
- 123
r_ui64_unpacked:
- 123

#### uint32 ####
ui32: 123
r_ui32:
- 123
r_ui32_unpacked:
- 123

#### fixed64 ####
f64: 123
r_f64:
- 123
r_f64_unpacked:
- 123

#### sfixed64 ####
sf64: 123
r_sf64:
- 123
r_sf64_unpacked:
- 123

#### fixed32 ####
f32: 123
r_f32:
- 123
r_f32_unpacked:
- 123

#### sfixed32 ####
sf32: 123
r_sf32:
- 123
r_sf32_unpacked:
- 123

#### sint32 ####
si32: -123
r_si32:
- -789
- 123
r_si32_unpacked:
- 123
- -456

#### sint64 ####
si64: -123
r_si64:
- -789
- 123
r_si64_unpacked:
- 123
- -456

## sub-message ##
oth:
  str: "mystring2"
  i64: 33333
  dbl: 333.333
  b: true
  inenum: INNERTHREE
  inmsg:
    str: "myinnerstring"
    i64: 99
    dbl: 99.99
r_oth:
  - str: "mystring2"
    i64: 33333
    dbl: 333.333
    b: true
    inenum: INNERTHREE
    inmsg:
      str: "myinnerstring"
      i64: 99
      dbl: 99.99
  - str: "mystring3"
    i64: 123
    dbl: 333.333
    b: true
    inenum: INNERTHREE
    inmsg:
      str: "myinnerstring"
      i64: 99
      dbl: 99.99
  - str: "mystring3"
    i64: 123
    dbl: 333.333
    b: true
    inenum: INNERTHREE
    inmsg:
      str: "myinnerstring"
      i64: 99123
      dbl: 99.99


#### map[string]string ####
map_str_str:
  key1: val1
  key2: val2

#### map[string]message ####
map_str_msg:
  key1:
    str: "mystring2"
    i64: 33333
    dbl: 333.333
    b: true
    inenum: INNERTHREE
    inmsg:
      str: "myinnerstring"
      i64: 99
      dbl: 99.99
  key2:
    str: "mystring2"
    i64: 33333
    dbl: 333.333
    b: true
    inenum: INNERTHREE
    inmsg:
      str: "myinnerstring"
      i64: 99
      dbl: 99.99

#### map[int32]message ####
map_i32_msg:
  "123":
    str: "mystring2"
    inmsg:
      str: "myinnerstring"
      i64: 99
      dbl: 99.99
  "456":
    str: "mystring2"

### map[int64]double ####
map_int64_double:
  123: 123.111
  456: 123.222

## other maps ##
map_str_float:
    key1: 123
map_str_uint64:
    key1: 123
map_str_uint32:
    key1: 123
map_str_fixed64:
    key1: 123
map_str_bool:
    key1: true
map_str_sfixed32:
    key1: 123
map_str_sfixed64:
    key1: 123
map_str_sint32:
    key1: 123
map_str_sint64:
    key1: 123

google_protobuf_duration: 10s
google_protobuf_timestamp: 2018-08-15T00:00:01Z
`

	simpleNoValues = `
enm:
r_enm:
r_enm_unpacked:

#### string ####
str:
r_str:

#### bool ####
b:
r_b:
r_b_unpacked:

#### double ####
dbl:
r_dbl:
r_dbl_unpacked:

#### float ####
flt:
r_flt:
r_flt_unpacked:

#### int64 with negative val ####
i64:
r_i64:
r_i64_unpacked:

#### int32 with negative val ####
i32:
r_i32:
r_i32_unpacked:

#### uint64 ####
ui64:
r_ui64:
r_ui64_unpacked:

#### uint32 ####
ui32:
r_ui32:
r_ui32_unpacked:

#### fixed64 ####
f64:
r_f64:
r_f64_unpacked:

#### sfixed64 ####
sf64:
r_sf64:
r_sf64_unpacked:

#### fixed32 ####
f32:
r_f32:
r_f32_unpacked:

#### sfixed32 ####
sf32:
r_sf32:
r_sf32_unpacked:

#### sint32 ####
si32:
r_si32:
r_si32_unpacked:

#### sint64 ####
si64:
r_si64:
r_si64_unpacked:

## sub-message ##
oth:
  str:
  i64:
  dbl:
  b:
  inenum:
  inmsg:
r_oth:

#### map[string]string ####
map_str_str:

#### map[string]message ####
map_str_msg:

#### map[int32]message ####
map_i32_msg:

### map[int64]double ####
map_int64_double:

## other maps ##
map_str_float:
map_str_uint64:
map_str_uint32:
map_str_fixed64:
map_str_bool:
map_str_sfixed32:
map_str_sfixed64:
map_str_sint32:
map_str_sint64:
`
)

type testdata struct {
	n           string
	input       string
	msg         string
	skipUnknown bool
	err         string
	expected    proto.Message
}

func TestEncodeBytes(t *testing.T) {
	fds, err := GetFileDescSet("testdata/all/types.descriptor")
	if err != nil {
		t.Fatal(err)
	}

	for _, td := range []testdata{
		////////// all types valid //////////
		{
			n:           "all types",
			msg:         ".foo.Simple",
			input:       simple,
			skipUnknown: false,
			err:         "",
			expected:    &foo.Simple{},
		},
		{
			n:           "all types no values",
			msg:         ".foo.Simple",
			input:       simpleNoValues,
			skipUnknown: false,
			err:         "",
			expected:    &foo.Simple{},
		},
		{
			n:           "empty proto, empty yaml",
			msg:         ".foo.Empty",
			input:       ``,
			skipUnknown: false,
			err:         "",
			expected:    &foo.Empty{},
		},
		{
			n:           "unknown field, skip true",
			msg:         ".foo.Empty",
			input:       `fxxx: 123`,
			skipUnknown: true,
			err:         "",
			expected:    &foo.Empty{},
		},

		////////// string //////////
		{
			n:           "str type mismatch",
			msg:         ".foo.Simple",
			input:       `str: 12345`,
			skipUnknown: false,
			err:         "field 'str' is of type 'float64' instead of expected type 'string'",
		},
		{
			n:     "[]string type mismatch",
			msg:   ".foo.Simple",
			input: `r_str: ""`,
			err:   "field 'r_str' is of type 'string' instead of expected type '[]string'",
		},
		{
			n:   "[]string entry type mismatch",
			msg: ".foo.Simple",
			input: `
r_str:
- 123
`,
			err:      "field 'r_str[0]' is of type 'float64' instead of expected type 'string'",
			expected: &foo.Simple{},
		},

		////////// bool //////////
		{
			n:           "bool type mismatch",
			msg:         ".foo.Simple",
			input:       `b: ""`,
			skipUnknown: false,
			err:         "field 'b' is of type 'string' instead of expected type 'bool'",
		},
		{
			n:     "[]bool type mismatch",
			msg:   ".foo.Simple",
			input: `r_b: ""`,
			err:   "field 'r_b' is of type 'string' instead of expected type '[]bool'",
		},
		{
			n:   "[]bool entry type mismatch",
			msg: ".foo.Simple",
			input: `
r_b:
- abcd
`,
			err:      "field 'r_b[0]' is of type 'string' instead of expected type 'bool'",
			expected: &foo.Simple{},
		},
		{
			n:     "[]bool unpacked type mismatch",
			msg:   ".foo.Simple",
			input: `r_b_unpacked: ""`,
			err:   "field 'r_b_unpacked' is of type 'string' instead of expected type '[]bool'",
		},
		{
			n:   "[]bool unpacked entry type mismatch",
			msg: ".foo.Simple",
			input: `
r_b_unpacked:
- abcd
`,
			skipUnknown: false,
			err:         "field 'r_b_unpacked[0]' is of type 'string' instead of expected type 'bool'",
		},

		////////// double //////////
		{
			n:           "double type mismatch",
			msg:         ".foo.Simple",
			input:       `dbl: ""`,
			skipUnknown: false,
			err:         "field 'dbl' is of type 'string' instead of expected type 'float64'",
		},
		{
			n:     "[]double type mismatch",
			msg:   ".foo.Simple",
			input: `r_dbl: ""`,
			err:   "field 'r_dbl' is of type 'string' instead of expected type '[]float64'",
		},
		{
			n:   "[]double entry type mismatch",
			msg: ".foo.Simple",
			input: `
r_dbl:
- abcd
`,
			err:      "field 'r_dbl[0]' is of type 'string' instead of expected type 'float64'",
			expected: &foo.Simple{},
		},
		{
			n:     "[]double unpacked type mismatch",
			msg:   ".foo.Simple",
			input: `r_dbl_unpacked: ""`,
			err:   "field 'r_dbl_unpacked' is of type 'string' instead of expected type '[]float64'",
		},
		{
			n:   "[]double unpacked entry type mismatch",
			msg: ".foo.Simple",
			input: `
r_dbl_unpacked:
- abcd
`,
			skipUnknown: false,
			err:         "field 'r_dbl_unpacked[0]' is of type 'string' instead of expected type 'float64'",
		},

		////////// float //////////
		{
			n:           "float type mismatch",
			msg:         ".foo.Simple",
			input:       `flt: ""`,
			skipUnknown: false,
			err:         "field 'flt' is of type 'string' instead of expected type 'float32'",
		},
		{
			n:     "[]float type mismatch",
			msg:   ".foo.Simple",
			input: `r_flt: ""`,
			err:   "field 'r_flt' is of type 'string' instead of expected type '[]float32'",
		},
		{
			n:   "[]float entry type mismatch",
			msg: ".foo.Simple",
			input: `
r_flt:
- abcd
`,
			err:      "field 'r_flt[0]' is of type 'string' instead of expected type 'float32'",
			expected: &foo.Simple{},
		},
		{
			n:     "[]float unpacked type mismatch",
			msg:   ".foo.Simple",
			input: `r_flt_unpacked: ""`,
			err:   "field 'r_flt_unpacked' is of type 'string' instead of expected type '[]float32'",
		},
		{
			n:   "[]float unpacked entry type mismatch",
			msg: ".foo.Simple",
			input: `
r_flt_unpacked:
- abcd
`,
			skipUnknown: false,
			err:         "field 'r_flt_unpacked[0]' is of type 'string' instead of expected type 'float32'",
		},

		////////// int64/int32/uint64/uint32 //////////
		{
			n:           "int64 type mismatch",
			msg:         ".foo.Simple",
			input:       `i64: ""`,
			skipUnknown: false,
			err:         "field 'i64' is of type 'string' instead of expected type 'int'",
		},
		{
			n:     "[]int32 type mismatch",
			msg:   ".foo.Simple",
			input: `r_i32: ""`,
			err:   "field 'r_i32' is of type 'string' instead of expected type '[]int'",
		},
		{
			n:   "[]uint64 entry type mismatch",
			msg: ".foo.Simple",
			input: `
r_ui64:
- abcd
`,
			err:      "field 'r_ui64[0]' is of type 'string' instead of expected type 'int'",
			expected: &foo.Simple{},
		},
		{
			n:     "[]ui32 unpacked type mismatch",
			msg:   ".foo.Simple",
			input: `r_ui32_unpacked: ""`,
			err:   "field 'r_ui32_unpacked' is of type 'string' instead of expected type '[]int'",
		},
		{
			n:   "[]int64 unpacked entry type mismatch",
			msg: ".foo.Simple",
			input: `
r_i64_unpacked:
- abcd
`,
			skipUnknown: false,
			err:         "field 'r_i64_unpacked[0]' is of type 'string' instead of expected type 'int'",
		},

		////////// sfixed64/fixed64 //////////
		{
			n:           "sfixed64 type mismatch",
			msg:         ".foo.Simple",
			input:       `sf64: ""`,
			skipUnknown: false,
			err:         "field 'sf64' is of type 'string' instead of expected type 'int'",
		},
		{
			n:     "[]sfixed64 type mismatch",
			msg:   ".foo.Simple",
			input: `r_sf64: ""`,
			err:   "field 'r_sf64' is of type 'string' instead of expected type '[]int'",
		},
		{
			n:   "[]fixed64 entry type mismatch",
			msg: ".foo.Simple",
			input: `
r_f64:
- abcd
`,
			err:      "field 'r_f64[0]' is of type 'string' instead of expected type 'int'",
			expected: &foo.Simple{},
		},
		{
			n:     "[]fixed64 unpacked type mismatch",
			msg:   ".foo.Simple",
			input: `r_f64_unpacked: ""`,
			err:   "field 'r_f64_unpacked' is of type 'string' instead of expected type '[]int'",
		},
		{
			n:   "[]fixed64 unpacked entry type mismatch",
			msg: ".foo.Simple",
			input: `
r_f64_unpacked:
- abcd
`,
			skipUnknown: false,
			err:         "field 'r_f64_unpacked[0]' is of type 'string' instead of expected type 'int'",
		},

		////////// sfixed32/fixed32 //////////
		{
			n:           "sfixed32 type mismatch",
			msg:         ".foo.Simple",
			input:       `sf32: ""`,
			skipUnknown: false,
			err:         "field 'sf32' is of type 'string' instead of expected type 'int'",
		},
		{
			n:     "[]sfixed32 type mismatch",
			msg:   ".foo.Simple",
			input: `r_sf32: ""`,
			err:   "field 'r_sf32' is of type 'string' instead of expected type '[]int'",
		},
		{
			n:   "[]fixed32 entry type mismatch",
			msg: ".foo.Simple",
			input: `
r_f32:
- abcd
`,
			err:      "field 'r_f32[0]' is of type 'string' instead of expected type 'int'",
			expected: &foo.Simple{},
		},
		{
			n:     "[]fixed32 unpacked type mismatch",
			msg:   ".foo.Simple",
			input: `r_f32_unpacked: ""`,
			err:   "field 'r_f32_unpacked' is of type 'string' instead of expected type '[]int'",
		},
		{
			n:   "[]fixed32 unpacked entry type mismatch",
			msg: ".foo.Simple",
			input: `
r_f32_unpacked:
- abcd
`,
			skipUnknown: false,
			err:         "field 'r_f32_unpacked[0]' is of type 'string' instead of expected type 'int'",
		},

		////////// sint32 //////////
		{
			n:           "sint32 type mismatch",
			msg:         ".foo.Simple",
			input:       `si32: ""`,
			skipUnknown: false,
			err:         "field 'si32' is of type 'string' instead of expected type 'int'",
		},
		{
			n:     "[]sint32 type mismatch",
			msg:   ".foo.Simple",
			input: `r_si32: ""`,
			err:   "field 'r_si32' is of type 'string' instead of expected type '[]int'",
		},
		{
			n:   "[]fixed32 entry type mismatch",
			msg: ".foo.Simple",
			input: `
r_si32:
- abcd
`,
			err:      "field 'r_si32[0]' is of type 'string' instead of expected type 'int'",
			expected: &foo.Simple{},
		},
		{
			n:     "[]fixed32 unpacked type mismatch",
			msg:   ".foo.Simple",
			input: `r_si32_unpacked: ""`,
			err:   "field 'r_si32_unpacked' is of type 'string' instead of expected type '[]int'",
		},
		{
			n:   "[]fixed32 unpacked entry type mismatch",
			msg: ".foo.Simple",
			input: `
r_si32_unpacked:
- abcd
`,
			skipUnknown: false,
			err:         "field 'r_si32_unpacked[0]' is of type 'string' instead of expected type 'int'",
		},

		////////// sint64 //////////
		{
			n:           "sint64 type mismatch",
			msg:         ".foo.Simple",
			input:       `si64: ""`,
			skipUnknown: false,
			err:         "field 'si64' is of type 'string' instead of expected type 'int'",
		},
		{
			n:     "[]sint64 type mismatch",
			msg:   ".foo.Simple",
			input: `r_si64: ""`,
			err:   "field 'r_si64' is of type 'string' instead of expected type '[]int'",
		},
		{
			n:   "[]sint64 entry type mismatch",
			msg: ".foo.Simple",
			input: `
r_si64:
- abcd
`,
			err:      "field 'r_si64[0]' is of type 'string' instead of expected type 'int'",
			expected: &foo.Simple{},
		},
		{
			n:     "[]sint64 unpacked type mismatch",
			msg:   ".foo.Simple",
			input: `r_si64_unpacked: ""`,
			err:   "field 'r_si64_unpacked' is of type 'string' instead of expected type '[]int'",
		},
		{
			n:   "[]sint64 unpacked entry type mismatch",
			msg: ".foo.Simple",
			input: `
r_si64_unpacked:
- abcd
`,
			skipUnknown: false,
			err:         "field 'r_si64_unpacked[0]' is of type 'string' instead of expected type 'int'",
		},

		////////// Enums //////////
		{
			n:     "enum type mismatch",
			msg:   ".foo.Simple",
			input: `enm: 12345`,
			err:   "field 'enm' is of type 'float64' instead of expected type 'enum(foo.myenum)'",
		},
		{
			n:     "wrong enum value",
			msg:   ".foo.Simple",
			input: `enm: BADNAME`,
			err:   "unrecognized enum value 'BADNAME' for enum 'myenum'",
		},
		{
			n:     "[]enum type mismatch",
			msg:   ".foo.Simple",
			input: `r_enm: 123`,
			err:   "field 'r_enm' is of type 'float64' instead of expected type '[]enum(foo.myenum)'",
		},
		{
			n:   "[]enum entry type mismatch",
			msg: ".foo.Simple",
			input: `
r_enm:
- 123
`,
			err:      "field 'r_enm[0]' is of type 'float64' instead of expected type 'enum(foo.myenum)'",
			expected: &foo.Simple{},
		},
		{
			n:   "[]enum wrong enum value",
			msg: ".foo.Simple",
			input: `
r_enm:
- BADNAME
`,
			err: "unrecognized enum value 'BADNAME' for enum 'myenum'",
		},
		{
			n:     "[]enum unpacked type mismatch",
			msg:   ".foo.Simple",
			input: `r_enm_unpacked: ""`,
			err:   "field 'r_enm_unpacked' is of type 'string' instead of expected type '[]enum(foo.myenum)'",
		},
		{
			n:   "[]enum unpacked entry type mismatch",
			msg: ".foo.Simple",
			input: `
r_enm_unpacked:
- 123
`,
			skipUnknown: false,
			err:         "field 'r_enm_unpacked[0]' is of type 'float64' instead of expected type 'enum(foo.myenum)'",
		},
		{
			n:   "[]enum unpacked wrong enum value",
			msg: ".foo.Simple",
			input: `
r_enm_unpacked:
- BADNAME
`,
			err: "unrecognized enum value 'BADNAME' for enum 'myenum'",
		},

		////////// Message //////////
		{
			n:   "int64 type mismatch sub-message",
			msg: ".foo.Simple",
			input: `
oth:
  i64: ""
`,
			err: "field 'i64' is of type 'string' instead of expected type 'int'",
		},
		{
			n:     "message type mismatch",
			msg:   ".foo.Simple",
			input: `oth: "string instead of message"`,
			err:   "field 'oth' is of type 'string' instead of expected type 'foo.other'",
		},
		{
			n:     "[]message type mismatch",
			msg:   ".foo.Simple",
			input: `r_oth: ""`,
			err:   "field 'r_oth' is of type 'string' instead of expected type '[]foo.other'",
		},
		{
			n:   "[]message entry type mismatch",
			msg: ".foo.Simple",
			input: `
r_oth:
- abcd
`,
			err:      "field 'r_oth[0]' is of type 'string' instead of expected type 'foo.other'",
			expected: &foo.Simple{},
		},
		{
			n:   "[]message entry content wrong (recursion returns err)",
			msg: ".foo.Simple",
			input: `
r_oth:
- a: 1
`,
			err:      "/r_oth[0]: 'field 'a' not found in message 'other''",
			expected: &foo.Simple{},
		},
		{
			n:   "primitives only inside nested msg",
			msg: ".foo.Outer.Inner",
			input: `
str: "mystring"
i64: 56789
dbl: 123.456
b: true
`,
			skipUnknown: false,
			err:         "",
			expected:    &foo.Outer_Inner{},
		},

		////////// map //////////
		{
			n:     "map type mismatch",
			msg:   ".foo.Simple",
			input: `map_str_str: ""`,
			err:   "map_str_str' is of type 'string' instead of expected type 'map<string, string>'",
		},
		{
			n:   "map entry type mismatch",
			msg: ".foo.Simple",
			input: `
map_str_str:
  key: 1234
`,
			err: "/map_str_str[key]: 'field 'value' is of type 'float64' instead of expected type 'string''",
		},

		////////// Wrong input //////////
		{
			n:     "bad message name",
			msg:   "name not exists",
			input: ``,
			err:   "cannot resolve message 'name not exists'",
		},
		{
			n:           "unsupported type",
			msg:         ".foo.Simple",
			input:       `byts: "abcd"`,
			skipUnknown: false,
			err:         "unrecognized field type 'TYPE_BYTES'",
			expected:    &foo.Simple{},
		},
		{
			n:           "unknown field, skip false",
			msg:         ".foo.Empty",
			input:       `fxxx: 123`,
			skipUnknown: false,
			err:         "field 'fxxx' not found in message 'Empty'",
		},
	} {
		t.Run(td.n, func(tt *testing.T) {
			var in map[string]interface{}
			jsonBytes, _ := yaml.YAMLToJSON([]byte(td.input))
			if err := json.Unmarshal(jsonBytes, &in); err != nil {
				tt.Fatal("bad yaml2")
			}

			r := NewEncoder(fds)
			gotBytes, err := r.EncodeBytes(in, td.msg, td.skipUnknown)
			if td.err == "" && err != nil {
				tt.Errorf("got error '%v'; want nil", err)
			} else if td.err != "" && (err == nil || !strings.Contains(err.Error(), td.err)) {
				tt.Fatalf("got error '%v'; want '%s'", err, td.err)
				return
			}

			if td.err == "" {
				gotInstance := proto.Clone(td.expected)
				if err = proto.Unmarshal(gotBytes, gotInstance); err != nil {
					tt.Fatalf("Unable to unmarshal generated bytes back: %v", err)
				}

				wantInst := proto.Clone(td.expected)
				js, err := yaml.YAMLToJSON([]byte(td.input))
				if err != nil {
					tt.Fatalf("Cannot marshal as json %s; %v", td.input, err)
				}
				jsbp := jsonpb.Unmarshaler{AllowUnknownFields: true}
				if err = jsbp.Unmarshal(bytes.NewReader(js), wantInst); err != nil {
					tt.Fatalf("Cannot unmarshal json to proto '%v;", err)
				}

				if !reflect.DeepEqual(gotInstance, wantInst) {
					tm := proto.TextMarshaler{}

					tt.Errorf("decoded msg = '%v'; want '%v'", tm.Text(gotInstance), tm.Text(wantInst))
				}
			}
		})
	}
}

// Why is this case not included it inside the above `simple` yaml ? Because for this case jsonpb cannot unmarshal
// into proto and therefore I cannot validate the unmarshaled data of custom marshaled bits using reflect.deepequals.
// Therefore, for these two cases, we will unmarshal the encoded bytes and check for specific fields.
func TestEncodeBytesForEnumMapVals(t *testing.T) {
	fds, fdsLoadErr := GetFileDescSet("testdata/all/types.descriptor")
	if fdsLoadErr != nil {
		t.Fatal(fdsLoadErr)
	}
	mapValAsEnum := `
### map[string]enum ####
map_str_enum:
  key1:
    THREE
  key2:
    TWO
### map[fixed32]enum ####
map_fixed32_enum:
  123: THREE
  456: TWO
`
	td := testdata{
		n:        "success",
		msg:      ".foo.Simple",
		input:    mapValAsEnum,
		expected: &foo.Simple{},
	}
	var in map[string]interface{}
	jsonBytes, _ := yaml.YAMLToJSON([]byte(td.input))
	if err := json.Unmarshal(jsonBytes, &in); err != nil {
		t.Fatal("bad yaml2")
	}

	r := NewEncoder(fds)
	gotBytes, err := r.EncodeBytes(in, td.msg, td.skipUnknown)
	if td.err == "" && err != nil {
		t.Errorf("got error '%v'; want nil", err)
	} else if td.err != "" && (err == nil || !strings.Contains(err.Error(), td.err)) {
		t.Fatalf("got error '%v'; want '%s'", err, td.err)
		return
	}

	if td.err == "" {
		gotInstance := proto.Clone(td.expected)
		if err = proto.Unmarshal(gotBytes, gotInstance); err != nil {
			t.Fatal("Unable to unmarshal generated bytes back")
		}
		got := gotInstance.(*foo.Simple)
		if v, ok := got.MapFixed32Enum[123]; !ok || v != foo.THREE {
			t.Errorf("map_fixed32_enum[123]=%v; want THREE", v)
		}
		if v, ok := got.MapFixed32Enum[456]; !ok || v != foo.TWO {
			t.Errorf("map_fixed32_enum[456]=%v; want TWO", v)
		}
		if v, ok := got.MapStrEnum["key1"]; !ok || v != foo.THREE {
			t.Errorf("map_str_enum[key1]=%v; want THREE", v)
		}
		if v, ok := got.MapStrEnum["key2"]; !ok || v != foo.TWO {
			t.Errorf("map_str_enum[key2]=%v; want TWO", v)
		}
	}
}

func TestMapDeterministicEncoding(t *testing.T) {
	fds, fdsLoadErr := GetFileDescSet("testdata/all/types.descriptor")
	if fdsLoadErr != nil {
		t.Fatal(fdsLoadErr)
	}
	strMaps := []string{
		`
map_str_str:
  key1: one
  key2: two
  key3: three
  key4: four
  key5: five
  key6: six
  key7: seven
  key8: eight
  key9: nine
  key10: ten
`,
		`
map_str_str:
  key2: two
  key1: one
  key9: nine
  key3: three
  key4: four
  key7: seven
  key5: five
  key6: six
  key8: eight
  key10: ten
`,
		`
map_str_str:
  key2: two
  key7: seven
  key1: one
  key4: four
  key9: nine
  key3: three
  key5: five
  key10: ten
  key6: six
  key8: eight
`,
	}

	var ba []byte
	str := ""
	for _, s := range strMaps {
		jsonBytes, _ := yaml.YAMLToJSON([]byte(s))
		var in map[string]interface{}
		if err := json.Unmarshal(jsonBytes, &in); err != nil {
			t.Fatal("bad yaml")
		}
		r := NewEncoder(fds)
		gotBytes, err := r.EncodeBytes(in, ".foo.Simple", false)
		if err != nil {
			t.Errorf("got error '%v'", err)
		}
		if ba == nil {
			ba = gotBytes
			str = s
		} else if !bytes.Equal(ba, gotBytes) {
			t.Fatalf("map encoding is not deterministic: %v and %v encode differently, want %v, got %v", s, str, ba, gotBytes)
		}
	}
}
