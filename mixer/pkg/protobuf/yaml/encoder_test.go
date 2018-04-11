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

// nolint
//go:generate protoc testdata/all/types.proto -otestdata/all/types.descriptor -I$GOPATH/src/istio.io/istio/vendor/istio.io/api -I.
//go:generate $GOPATH/src/istio.io/istio/bin/mixer_codegen.sh -f mixer/pkg/protobuf/yaml/testdata/all/types.proto

package yaml

import (
	"bytes"
	"io/ioutil"
	"reflect"
	"strings"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
	yaml2 "gopkg.in/yaml.v2"

	"istio.io/istio/mixer/pkg/protobuf/yaml/testdata/all"
)

var (
	empty = ``

	unknownField = `
fxxx: 123
`
	simple = `
str: "mystring"
i64: 56789
dbl: 123.456
b: true
enm: TWO
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
`
	inner = `
str: "mystring"
i64: 56789
dbl: 123.456
b: true
`

	badStr = `
str: 12345
`
	badBool = `
b: ""
`
	badInt64 = `
i64: ""
`

	badInt64SubMsg = `
oth:
  i64: ""
`

	badDouble = `
dbl: ""
`
	badEnumType = `
enm: 12345
`
	badEnumValue = `
enm: BADNAME
`
	badMessage = `
oth: "string instead of message"
`
	// Not supported yet; will be added soon.
	unsupportedType = `
byts: "abcd"
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
	fds, err := getFileDescSet("testdata/all/types.descriptor")
	if err != nil {
		t.Fatal(err)
	}

	for _, td := range []testdata{
		{
			n:           "success",
			msg:         ".foo.Simple",
			input:       simple,
			skipUnknown: false,
			err:         "",
			expected:    &foo.Simple{},
		},
		{
			n:           "empty proto, empty yaml2",
			msg:         ".foo.Empty",
			input:       empty,
			skipUnknown: false,
			err:         "",
			expected:    &foo.Empty{},
		},
		{
			n:           "unknown field, skip false",
			msg:         ".foo.Empty",
			input:       unknownField,
			skipUnknown: false,
			err:         "field 'fxxx' not found in message 'Empty'",
			expected:    &foo.Empty{},
		},
		{
			n:           "unknown field, skip true",
			msg:         ".foo.Empty",
			input:       unknownField,
			skipUnknown: true,
			err:         "",
			expected:    &foo.Empty{},
		},
		{
			n:           "primitives only inside nested msg",
			msg:         ".foo.Outer.Inner",
			input:       inner,
			skipUnknown: false,
			err:         "",
			expected:    &foo.Outer_Inner{},
		},
		{
			n:           "str type mismatch",
			msg:         ".foo.Simple",
			input:       badStr,
			skipUnknown: false,
			err:         "field 'str' is of type 'int' instead of expected type 'string'",
			expected:    &foo.Simple{},
		},
		{
			n:           "bool type mismatch",
			msg:         ".foo.Simple",
			input:       badBool,
			skipUnknown: false,
			err:         "field 'b' is of type 'string' instead of expected type 'bool'",
			expected:    &foo.Simple{},
		},
		{
			n:           "double type mismatch",
			msg:         ".foo.Simple",
			input:       badDouble,
			skipUnknown: false,
			err:         "field 'dbl' is of type 'string' instead of expected type 'float64'",
			expected:    &foo.Simple{},
		},
		{
			n:           "int64 type mismatch",
			msg:         ".foo.Simple",
			input:       badInt64,
			skipUnknown: false,
			err:         "field 'i64' is of type 'string' instead of expected type 'int'",
			expected:    &foo.Simple{},
		},
		{
			n:           "enum type mismatch",
			msg:         ".foo.Simple",
			input:       badEnumType,
			skipUnknown: false,
			err:         "field 'enm' is of type 'int' instead of expected type 'enum(foo.myenum)'",
			expected:    &foo.Simple{},
		},
		{
			n:           "wrong enum value",
			msg:         ".foo.Simple",
			input:       badEnumValue,
			skipUnknown: false,
			err:         "unrecognized enum value 'BADNAME' for enum 'myenum'",
			expected:    &foo.Simple{},
		},
		{
			n:           "int64 type mismatch sub-message",
			msg:         ".foo.Simple",
			input:       badInt64SubMsg,
			skipUnknown: false,
			err:         "field 'i64' is of type 'string' instead of expected type 'int'",
			expected:    &foo.Simple{},
		},
		{
			n:           "message type mismatch",
			msg:         ".foo.Simple",
			input:       badMessage,
			skipUnknown: false,
			err:         "field 'oth' is of type 'string' instead of expected type 'foo.other'",
			expected:    &foo.Simple{},
		},
		{
			n:           "bad message name",
			msg:         "name not exists",
			input:       ``,
			skipUnknown: false,
			err:         "cannot resolve message 'name not exists'",
			expected:    &foo.Simple{},
		},
		{
			n:           "type not supported yet",
			msg:         ".foo.Simple",
			input:       unsupportedType,
			skipUnknown: false,
			err:         "unrecognized field type 'TYPE_BYTES'",
			expected:    &foo.Simple{},
		},
	} {
		t.Run(td.n, func(tt *testing.T) {
			in := make(map[interface{}]interface{})
			if err = yaml2.Unmarshal([]byte(td.input), &in); err != nil {
				tt.Fatal("bad yaml2")
			}

			r := NewEncoder(fds)
			gotBytes, err := r.EncodeBytes(in, td.msg, td.skipUnknown)
			if td.err == "" && err != nil {
				tt.Errorf("got error '%v'; want nil", err)
			} else if td.err != "" && (err == nil || !strings.Contains(err.Error(), td.err)) {
				tt.Errorf("got error '%v'; want '%s'", err, td.err)
			}

			if td.err == "" {
				wantInst := proto.Clone(td.expected)
				js, err := yaml.YAMLToJSON([]byte(td.input))
				if err != nil {
					tt.Fatalf("Cannot marshal as json %s; %v", td.input, err)
				}
				jsbp := jsonpb.Unmarshaler{AllowUnknownFields: true}
				if err = jsbp.Unmarshal(bytes.NewReader(js), wantInst); err != nil {
					tt.Fatalf("Cannot unmarshal json to proto '%v;", err)
				}

				defaultInst := proto.Clone(td.expected)
				if err = proto.Unmarshal(gotBytes, defaultInst); err != nil {
					tt.Errorf("Unable to unmarshal generated bytes back")
				}
				if !reflect.DeepEqual(defaultInst, wantInst) {
					tt.Errorf("decoded msg = '%v'; want '%v'", spew.Sdump(defaultInst), spew.Sdump(wantInst))
				}
			}
		})
	}
}

func getFileDescSet(path string) (*descriptor.FileDescriptorSet, error) {
	byts, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	fds := &descriptor.FileDescriptorSet{}
	if err = proto.Unmarshal(byts, fds); err != nil {
		return nil, err
	}

	return fds, nil
}
