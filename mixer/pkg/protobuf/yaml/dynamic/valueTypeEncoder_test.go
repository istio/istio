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
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
	"github.com/gogo/protobuf/types"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/lang/compiled"
	"istio.io/pkg/attribute"
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
	compiler := compiled.NewBuilder(StandardVocabulary())

	for _, tst := range []struct {
		input        interface{}
		typeName     string
		builderError error
		encoderError error
		bag          map[string]interface{}
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
		{
			input:        "source.ip",
			encoderError: errors.New("incorrect type for IP_ADDRESS"),
			bag: map[string]interface{}{
				"source.ip": "this should be a byte array",
			},
		},
		{
			input:        "request.path",
			builderError: errors.New("incorrect type for .istio.policy.v1beta1.IPAddress"),
			bag: map[string]interface{}{
				"request.path": "this should be a byte array",
			},
			typeName: ".istio.policy.v1beta1.IPAddress",
		},
		{
			input:        "context.timestamp",
			encoderError: errors.New("incorrect type for TIMESTAMP"),
			bag: map[string]interface{}{
				"context.timestamp": []byte{1, 2, 4, 8},
			},
		},
		{
			input:        "context.timestamp",
			encoderError: errors.New("invalid timestamp"),
			bag: map[string]interface{}{
				"context.timestamp": time.Date(20000, 1, 1, 0, 0, 0, 0, time.UTC).UTC(),
			},
		},
		{
			input:        "response.duration",
			encoderError: errors.New("error converting value"),
			bag: map[string]interface{}{
				"response.duration": "invalid",
			},
		},
		{
			input:        "request.headers",
			builderError: errors.New("unsupported type: STRING_MAP"),
			bag: map[string]interface{}{
				"request.headers": map[string]string{
					"user": "me",
				},
			},
		},
		{
			input:        "test.uri",
			encoderError: errors.New("error converting value"),
			bag: map[string]interface{}{
				"test.uri": int64(5),
			},
		},
		{
			input:        "test.dns_name",
			encoderError: errors.New("error converting value"),
			bag: map[string]interface{}{
				"test.dns_name": int64(5),
			},
		},
		{
			input:        "test.email_address",
			encoderError: errors.New("error converting value"),
			bag: map[string]interface{}{
				"test.email_address": int64(5),
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

			checkErrors(t, err, tst.builderError)

			if enc == nil {
				return
			}
			bag := attribute.GetMutableBagForTesting(tst.bag)
			var ba []byte
			_, err = enc.Encode(bag, ba)
			checkErrors(t, err, tst.encoderError)
		})
	}
}

func TestValueTypeEncoder(t *testing.T) {
	compiler := compiled.NewBuilder(StandardVocabulary())
	now := time.Now()
	ts, err := types.TimestampProto(now)
	if err != nil {
		t.Fatalf("invalid time: %v", err)
	}
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
			input: "response.time",
			output: v1beta1.Value{Value: &v1beta1.Value_TimestampValue{
				TimestampValue: &v1beta1.TimeStamp{ts}}},
			bag: map[string]interface{}{
				"response.time": now,
			},
		},
		{
			input: "source.ip",
			output: v1beta1.Value{Value: &v1beta1.Value_IpAddressValue{
				IpAddressValue: &v1beta1.IPAddress{Value: []byte{1, 2, 4, 8}}}},
			bag: map[string]interface{}{
				"source.ip": []byte{1, 2, 4, 8},
			},
		},
		{
			input: "response.duration",
			output: v1beta1.Value{&v1beta1.Value_DurationValue{
				DurationValue: &v1beta1.Duration{Value: types.DurationProto(time.Minute)}}},
			bag: map[string]interface{}{
				"response.duration": time.Minute,
			},
		},
		{
			input:  "test.uri",
			output: v1beta1.Value{&v1beta1.Value_UriValue{UriValue: &v1beta1.Uri{Value: "/health"}}},
			bag: map[string]interface{}{
				"test.uri": "/health",
			},
		},
		{
			input:  "test.dns_name",
			output: v1beta1.Value{&v1beta1.Value_DnsNameValue{DnsNameValue: &v1beta1.DNSName{Value: "a.b.c.d"}}},
			bag: map[string]interface{}{
				"test.dns_name": "a.b.c.d",
			},
		},
		{
			input: "test.email_address",
			output: v1beta1.Value{&v1beta1.Value_EmailAddressValue{
				EmailAddressValue: &v1beta1.EmailAddress{Value: "user@google.com"}}},
			bag: map[string]interface{}{
				"test.email_address": "user@google.com",
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
				t.Fatalf("unexpected encoder error: %v", err)
			}

			bout, _ := tst.output.Marshal()
			sz, nbytes := proto.DecodeVarint(ba)

			if sz != uint64(len(bout)) || !reflect.DeepEqual(ba[nbytes:], bout) {
				t.Fatalf("encoding differs\n got: %v\nwant: %v", ba, bout)
			}
		})
	}
}
