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
package dynamic

import (
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/lang/compiled"
	"istio.io/istio/mixer/pkg/protobuf/yaml"
	"istio.io/istio/mixer/pkg/runtime/lang"
)

type fakeCompiledExpr struct {
	err error
	v   interface{}
	b   bool
	s   string
	f   float64
	i   int64
}

var _ compiled.Expression = &fakeCompiledExpr{}

// Evaluate evaluates this compiled expression against the attribute bag.
func (f fakeCompiledExpr) Evaluate(attributes attribute.Bag) (interface{}, error) {
	return f.v, f.err
}

// EvaluateBoolean evaluates this compiled expression against the attribute bag and returns the result as boolean.
// panics if the expression does not return a boolean.
func (f fakeCompiledExpr) EvaluateBoolean(attributes attribute.Bag) (bool, error) {
	return f.b, f.err
}

// EvaluateString evaluates this compiled expression against the attribute bag and returns the result as string.
// panics if the expression does not return a string.
func (f fakeCompiledExpr) EvaluateString(attributes attribute.Bag) (string, error) {
	return f.s, f.err
}

// EvaluateDouble evaluates this compiled expression against the attribute bag and returns the result as float64.
// panics if the expression does not return a float64.
func (f fakeCompiledExpr) EvaluateDouble(attribute attribute.Bag) (float64, error) {
	return f.f, f.err
}

// EvaluateInteger evaluates this compiled expression against the attribute bag and returns the result as int64.
// panics if the expression does not return a int64.
func (f fakeCompiledExpr) EvaluateInteger(attribute attribute.Bag) (int64, error) {
	return f.i, f.err
}

func TestBuildEvalPrimitive(t *testing.T) {
	for _, tst := range []struct {
		desc  string
		msg   string
		vt    v1beta1.ValueType
		ftype descriptor.FieldDescriptorProto_Type
	}{{
		desc:  "unsupported type",
		msg:   "do not know how to encode",
		ftype: descriptor.FieldDescriptorProto_TYPE_GROUP,
	}, {
		desc:  "incompatible type",
		msg:   "TYPE_FLOAT does not accept BOOL",
		ftype: descriptor.FieldDescriptorProto_TYPE_FLOAT,
		vt:    v1beta1.BOOL,
	},
	} {
		t.Run(tst.desc, func(tt *testing.T) {
			fld := &descriptor.FieldDescriptorProto{Type: &tst.ftype}
			_, err := BuildPrimitiveEvalEncoder(nil, tst.vt, fld)

			if err == nil {
				tt.Fatalf("error should have occurred")
			}

			if !strings.Contains(err.Error(), tst.msg) {
				tt.Fatalf("incorrect error.\n got: %s\nwant: %s", err.Error(), tst.msg)
			}
		})
	}
}

func TestBuildPrimitive(t *testing.T) {
	for _, tst := range []struct {
		err   error
		v     interface{}
		ftype descriptor.FieldDescriptorProto_Type
	}{{
		err:   errors.New("do not know how to encode"),
		ftype: descriptor.FieldDescriptorProto_TYPE_GROUP,
	}, {
		err:   errors.New("badTypeError"),
		v:     false,
		ftype: descriptor.FieldDescriptorProto_TYPE_FLOAT,
	},
	} {
		name := fmt.Sprintf("%v-%v", tst.ftype, tst.v)
		t.Run(name, func(tt *testing.T) {
			fld := &descriptor.FieldDescriptorProto{Type: &tst.ftype}
			enc, err := BuildPrimitiveEncoder(tst.v, fld)

			wantError := tst.err != nil
			gotError := err != nil

			if wantError != gotError {
				tt.Fatalf("unexpected error. got: %v\nwant: %v", err, tst.err)
			}
			if err != nil && !strings.Contains(err.Error(), tst.err.Error()) {
				tt.Fatalf("incorrect error.\n got: %s\nwant: %s", err.Error(), tst.err.Error())
			}

			if enc == nil {
				return
			}
			_, err = enc.Encode(nil, nil)
			if err == nil {
				tt.Fatalf("error should have occurred")
			}
		})
	}
}

func TestEvalErrorEnum(t *testing.T) {
	ee := &eEnumEncoder{expr: &fakeCompiledExpr{err: errors.New("unknown attribute")}}
	_, err := ee.Encode(nil, nil)
	if err == nil {
		t.Fatalf("Error should have occurred")
	}

	for _, tst := range []struct {
		err error
		v   interface{}
	}{
		{v: "ABCD", err: errors.New("unknown value")},
		{v: 300, err: errors.New("unknown value")},
		{v: true, err: errors.New("unable to encode enum")},
	} {
		name := fmt.Sprintf("%v-%v", tst.err, tst.v)
		t.Run(name, func(t *testing.T) {
			_, err := EncodeEnum(tst.v, nil, nil)
			if err == nil {
				t.Fatalf("error should have occurred")
			}

			if !strings.Contains(err.Error(), tst.err.Error()) {
				t.Fatalf("incorrect error.\n got: %v\nwant: '%v'", err, tst.err)
			}

		})
	}
}

func TestEvalError(t *testing.T) {

	errExp := &fakeCompiledExpr{err: errors.New("unknown attribute")}

	for _, tst := range []struct {
		vt    v1beta1.ValueType
		ftype descriptor.FieldDescriptorProto_Type
	}{
		{v1beta1.DOUBLE, descriptor.FieldDescriptorProto_TYPE_DOUBLE},
		{v1beta1.DOUBLE, descriptor.FieldDescriptorProto_TYPE_FLOAT},
		{v1beta1.INT64, descriptor.FieldDescriptorProto_TYPE_INT64},
		{v1beta1.INT64, descriptor.FieldDescriptorProto_TYPE_UINT64},
		{v1beta1.INT64, descriptor.FieldDescriptorProto_TYPE_INT32},
		{v1beta1.INT64, descriptor.FieldDescriptorProto_TYPE_UINT32},
		{v1beta1.INT64, descriptor.FieldDescriptorProto_TYPE_FIXED32},
		{v1beta1.INT64, descriptor.FieldDescriptorProto_TYPE_FIXED64},
		{v1beta1.BOOL, descriptor.FieldDescriptorProto_TYPE_BOOL},
		{v1beta1.STRING, descriptor.FieldDescriptorProto_TYPE_STRING},
		{v1beta1.INT64, descriptor.FieldDescriptorProto_TYPE_SFIXED32},
		{v1beta1.INT64, descriptor.FieldDescriptorProto_TYPE_SFIXED64},
		{v1beta1.INT64, descriptor.FieldDescriptorProto_TYPE_SINT32},
		{v1beta1.INT64, descriptor.FieldDescriptorProto_TYPE_SINT64},
	} {
		name := fmt.Sprintf("%v--%v", tst.ftype, tst.vt)
		t.Run(name, func(tt *testing.T) {
			fld := &descriptor.FieldDescriptorProto{Type: &tst.ftype}
			enc, err := BuildPrimitiveEvalEncoder(errExp, tst.vt, fld)

			if err != nil {
				tt.Fatalf("unexpected error: %v", err)
			}

			if _, err = enc.Encode(nil, nil); err == nil {
				tt.Fatalf("error should have occurred")
			}

			if !strings.Contains(err.Error(), "unknown attribute") {
				tt.Fatalf("incorrect error.\n got: %s\nwant: 'unknown attribute'", err.Error())
			}
		})
	}
}

type encodeFn func(v interface{}, ba []byte) ([]byte, error)

func TestBadEncodes(t *testing.T) {
	for _, tst := range []struct {
		enc encodeFn
		v   interface{}
	}{
		{enc: EncodeDouble, v: false},
		{enc: EncodeFloat, v: false},
		{enc: EncodeInt, v: false},
		{enc: EncodeSInt64, v: false},
		{enc: EncodeSInt32, v: false},
		{enc: EncodeFixed64, v: false},
		{enc: EncodeFixed32, v: false},
		{enc: EncodeBool, v: -2000.2},
		{enc: EncodeString, v: -2000.2},
	} {
		p := reflect.ValueOf(tst.enc).Pointer()
		rf := runtime.FuncForPC(p)
		comps := strings.Split(rf.Name(), ".")
		t.Run(fmt.Sprintf("%v", comps[len(comps)-1]), func(t *testing.T) {
			_, err := tst.enc(tst.v, nil)
			if err == nil {
				t.Fatalf("error should have occurred")
			}

			if !strings.Contains(err.Error(), "badTypeError") {
				t.Fatalf("unexpected error. got: %v, want: badTypeError", err)
			}
		})
	}
}

type fakeres struct {
	resolveMessage func(name string) *descriptor.DescriptorProto
	resolveEnum    func(name string) *descriptor.EnumDescriptorProto
}

func (f fakeres) ResolveMessage(name string) *descriptor.DescriptorProto {
	if f.resolveMessage != nil {
		return f.resolveMessage(name)
	}
	return nil
}

func (f fakeres) ResolveEnum(name string) *descriptor.EnumDescriptorProto {
	if f.resolveEnum != nil {
		return f.resolveEnum(name)
	}
	return nil
}

func (f fakeres) ResolveService(namePrefix string) (svc *descriptor.ServiceDescriptorProto, pkg string) {
	return nil, ""
}

func TestBuilder_Build(t *testing.T) {
	b := NewEncoderBuilder(&fakeres{}, nil, false)
	_, err := b.Build("", nil)
	if err == nil {
		t.Fatalf("want error, got nothing")
	}

	if !strings.Contains(err.Error(), "cannot resolve message") {
		t.Fatalf("unexpected error. got %v, want 'cannot resolve message'", err)
	}
}

func TestBuilderErrors(t *testing.T) {
	fds, err := yaml.GetFileDescSet("../testdata/all/types.descriptor")
	if err != nil {
		t.Fatal(err)
	}
	res := yaml.NewResolver(fds)
	compiler := compiled.NewBuilder(StandardVocabulary())

	for _, td := range []struct {
		desc        string
		input       map[string]interface{}
		msg         string
		compiler    lang.Compiler
		skipUnknown bool
		err         error
		res         yaml.Resolver
	}{
		{
			desc:        "unknown field, skipUnknown",
			msg:         ".foo.Simple",
			input:       map[string]interface{}{"unknownField": "abc"},
			skipUnknown: true,
		},
		{
			desc:        "unknown field, no-skipUnknown",
			msg:         ".foo.Simple",
			input:       map[string]interface{}{"unknownField": "abc"},
			skipUnknown: false,
			err:         errors.New("not found in message"),
		},
		{
			desc: "repeated string problem",
			msg:  ".foo.Simple",
			input: map[string]interface{}{"r_str": map[string]interface{}{
				"bad": "type",
			}},
			skipUnknown: false,
			err:         errors.New("want: []interface{}"),
		},
		{
			desc: "unpacked primitive",
			msg:  ".foo.Simple",
			input: map[string]interface{}{"r_i64_unpacked": []interface{}{
				1, 2, 3}},
			skipUnknown: false,
			err:         errors.New("unpacked primitives not supported"),
		},
		{
			desc: "unable to resolve message",
			msg:  ".foo.Simple",
			input: map[string]interface{}{"oth": map[string]interface{}{
				"inenum": "'INNERTHREE'",
			}},
			skipUnknown: false,
			err:         errors.New("unable to resolve message"),
			res: &fakeres{
				resolveMessage: func(name string) *descriptor.DescriptorProto {
					if name == ".foo.Simple" {
						return res.ResolveMessage(name)
					}
					return nil
				},
			},
		},
		{
			desc: "incorrect map type",
			msg:  ".foo.Simple",
			input: map[string]interface{}{"mapStrStr": map[interface{}]interface{}{
				"inenum": "'INNERTHREE'",
			}},
			skipUnknown: false,
			err:         errors.New("incorrect map type"),
		},
		{
			desc: "incorrect repeated message",
			msg:  ".foo.Simple",
			input: map[string]interface{}{"r_oth": map[string]interface{}{
				"inenum": "'INNERTHREE'",
			}},
			skipUnknown: false,
			err:         errors.New("want: []interface"),
		},
		{
			desc: "field type not supported",
			msg:  ".foo.Simple",
			input: map[string]interface{}{"r_si32_unpacked": []interface{}{
				"inenum", "'INNERTHREE'",
			}},
			skipUnknown: false,
			err:         errors.New("field type not supported"),
			res: &fakeres{
				resolveMessage: func(name string) *descriptor.DescriptorProto {
					if name == ".foo.Simple" {
						m := res.ResolveMessage(name)
						fd := yaml.FindFieldByName(m, "r_si32_unpacked")
						*fd.Type = descriptor.FieldDescriptorProto_TYPE_GROUP
						return m
					}
					return nil
				},
			},
		},
		{
			desc:        "compile error",
			msg:         ".foo.Simple",
			input:       map[string]interface{}{"str": "ab("},
			skipUnknown: false,
			err:         errors.New("unable to parse expression"),
			compiler:    compiler,
		},
		{
			desc:        "compile error - incorrect attribute type",
			msg:         ".foo.Simple",
			input:       map[string]interface{}{"enm": "origin.ip"},
			skipUnknown: false,
			err:         errors.New("want INT64 or STRING"),
			compiler:    compiler,
		},
		{
			desc:        "bad message input",
			msg:         ".foo.Simple",
			input:       map[string]interface{}{"oth": "origin.ip"},
			skipUnknown: false,
			err:         errors.New("want: map[string]interface{}"),
			compiler:    compiler,
		},
		{
			desc: "bad message input",
			msg:  ".foo.Simple",
			input: map[string]interface{}{"oth": map[string]interface{}{
				"ienum": false,
			}},
			skipUnknown: false,
			err:         errors.New("unable to build message field"),
			compiler:    compiler,
		},
		{
			desc:        "enum not found",
			msg:         ".foo.Simple",
			input:       map[string]interface{}{"enm": "origin.ip"},
			skipUnknown: false,
			err:         errors.New("unable to resolve enum"),
			compiler:    compiler,
			res: &fakeres{
				resolveMessage: res.ResolveMessage,
			},
		},
	} {
		t.Run(td.desc, func(t *testing.T) {
			resolver := res
			if td.res != nil {
				resolver = td.res
			}
			db := NewEncoderBuilder(resolver, td.compiler, td.skipUnknown)
			_, err := db.Build(td.msg, td.input)
			gotErr := err != nil
			wantErr := td.err != nil

			if gotErr != wantErr {
				t.Fatalf("unable to build: got %v, want %v", err, td.err)
			}

			if err == nil {
				return
			}

			if !strings.Contains(err.Error(), td.err.Error()) {
				t.Fatalf("unable to build: got %v, want %v", err, td.err)
			}
		})
	}
}

type fakeEncoder struct {
	err error
}

func (f fakeEncoder) Encode(_ attribute.Bag, _ []byte) ([]byte, error) {
	return nil, f.err
}

func TestMessageEncoder_EncodeErrors(t *testing.T) {
	err := errors.New("this always fails")
	me := messageEncoder{
		fields: []*fieldEncoder{
			{
				encoder: []Encoder{
					&fakeEncoder{err: err},
				},
			},
		},
	}

	_, gotErr := me.Encode(nil, nil)

	if gotErr == nil {
		t.Fatalf("got <nil> want %v", err)
	}

	if !strings.Contains(gotErr.Error(), err.Error()) {
		t.Fatalf("got %v, want %v", gotErr, err)
	}
}
