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

package dynamic

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"

	"github.com/gogo/protobuf/types"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/lang/compiled"
)

func checkErrors(t *testing.T, gotError error, wantError error) {
	wantOk := wantError != nil
	gotOk := gotError != nil

	if wantOk != gotOk {
		t.Fatalf("got: %v, want: %v", gotError, wantError)
	}

	if !gotOk {
		return
	}

	// both are errors and they are different
	if strings.Contains(gotError.Error(), wantError.Error()) {
		return
	}

	t.Fatalf("%v should contain '%v'", gotError.Error(), wantError.Error())
}

func TestValueTypeEncoder_Errors(t *testing.T) {
	compiler := compiled.NewBuilder(StatdardVocabulary())

	for _, tst := range []struct {
		input        interface{}
		typeName     string
		builderError error
		encoderError error
	}{
		{
			input:        "badAttribute",
			builderError: errors.New("unknown attribute"),
		},
		{
			input:        "incorrectMessage",
			typeName:     ".mymessage.com",
			builderError: errors.New("cannot process message of type"),
		},
		{
			input:        "response.size",
			encoderError: errors.New("lookup failed"),
		},
		{
			input:        "test.bool",
			encoderError: errors.New("lookup failed"),
		},
		{
			input:        "test.double",
			encoderError: errors.New("lookup failed"),
		},
		{
			input:        "api.operation",
			encoderError: errors.New("lookup failed"),
		},
		{
			input:        time.Time{},
			builderError: errors.New("unsupported type"),
		},
	} {
		t.Run(fmt.Sprintf("%v", tst.input), func(t *testing.T) {
			vt := valueTypeName
			fd := &descriptor.FieldDescriptorProto{TypeName: &vt}
			if tst.typeName != "" {
				fd.TypeName = &tst.typeName
			}

			enc, err := valueTypeEncoderBuilder(nil, fd, tst.input, compiler)

			checkErrors(t, err, tst.builderError)

			if enc == nil {
				return
			}
			bag := attribute.GetMutableBagForTesting(map[string]interface{}{
				"request.reason": "TWO",
			})
			var ba []byte
			_, err = enc.Encode(bag, ba)
			checkErrors(t, err, tst.encoderError)
		})
	}
}

func TestValueTypeEncoder(t *testing.T) {
	compiler := compiled.NewBuilder(StatdardVocabulary())
	now := time.Now()
	for _, tst := range []struct {
		input    interface{}
		output   v1beta1.Value
		typeName string
		bag      map[string]interface{}
	}{
		{
			input:  1,
			output: v1beta1.Value{Value: &v1beta1.Value_Int64Value{1}},
		},
		{
			input:  "test.i64",
			output: v1beta1.Value{Value: &v1beta1.Value_Int64Value{1}},
			bag: map[string]interface{}{
				"test.i64": int64(1),
			},
		},
		{
			input:  "'astring'",
			output: v1beta1.Value{Value: &v1beta1.Value_StringValue{"astring"}},
		},
		{
			input:  "api.operation",
			output: v1beta1.Value{Value: &v1beta1.Value_StringValue{"astring"}},
			bag: map[string]interface{}{
				"api.operation": "astring",
			},
		},
		{
			input:  3.14,
			output: v1beta1.Value{Value: &v1beta1.Value_DoubleValue{3.14}},
		},
		{
			input:  "test.double",
			output: v1beta1.Value{Value: &v1beta1.Value_DoubleValue{3.14}},
			bag: map[string]interface{}{
				"test.double": 3.14,
			},
		},
		{
			input:  false,
			output: v1beta1.Value{Value: &v1beta1.Value_BoolValue{false}},
		},
		{
			input:  "test.bool",
			output: v1beta1.Value{Value: &v1beta1.Value_BoolValue{false}},
			bag: map[string]interface{}{
				"test.bool": false,
			},
		},
		{
			input:  "response.time",
			output: v1beta1.Value{Value: &v1beta1.Value_TimestampValue{&v1beta1.TimeStamp{&types.Timestamp{now.Unix(), int32(now.Nanosecond())}}}},
			bag: map[string]interface{}{
				"response.time": now,
			},
		},
	} {
		t.Run(fmt.Sprintf("%v", tst.input), func(t *testing.T) {
			vt := valueTypeName
			fd := &descriptor.FieldDescriptorProto{TypeName: &vt}
			if tst.typeName != "" {
				fd.TypeName = &tst.typeName
			}
			enc, err := valueTypeEncoderBuilder(nil, fd, tst.input, compiler)

			if err != nil {
				t.Fatalf("unexpected encoder build error: %v", err)
			}

			bag := attribute.GetMutableBagForTesting(tst.bag)
			var ba []byte
			if ba, err = enc.Encode(bag, ba); err != nil {
				t.Fatalf("unexpected encoder  error: %v", err)
			}

			bout, _ := tst.output.Marshal()
			sz, nbytes := proto.DecodeVarint(ba)

			if sz != uint64(len(bout)) || !reflect.DeepEqual(ba[nbytes:], bout) {
				t.Fatalf("encoding differs\n got: %v\nwant: %v", ba, bout)
			}
		})
	}
}
