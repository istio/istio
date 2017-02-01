// Copyright 2017 Google Inc.
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

package expr

import (
	"testing"

	"github.com/golang/protobuf/ptypes"
	ts "github.com/golang/protobuf/ptypes/timestamp"

	"bytes"
	"time"

	"strings"

	"fmt"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/pkg/attribute"
)

type dictionary map[int32]string

var (
	t1     = time.Date(2001, 1, 1, 1, 1, 1, 9, time.UTC)
	ts1, _ = ptypes.TimestampProto(t1)
)

func TestEval(t *testing.T) {
	cases := []struct {
		attrs *mixerpb.Attributes
		in    string
		out   interface{}
		err   bool
	}{
		// We cannot test ByteAttributes here because you cannot compare []byte using '==' or '!='
		{attrs: &mixerpb.Attributes{Dictionary: dictionary{1: "str"}, StringAttributes: map[int32]string{1: "foo"}}, in: "str", out: "foo", err: false},
		{attrs: &mixerpb.Attributes{Dictionary: dictionary{4: "i64"}, Int64Attributes: map[int32]int64{4: 37}}, in: "i64", out: int64(37), err: false},
		{attrs: &mixerpb.Attributes{Dictionary: dictionary{17: "dbl"}, DoubleAttributes: map[int32]float64{17: 5.9}}, in: "dbl", out: 5.9, err: false},
		{attrs: &mixerpb.Attributes{Dictionary: dictionary{0: "bool"}, BoolAttributes: map[int32]bool{0: true}}, in: "bool", out: true, err: false},
		{attrs: &mixerpb.Attributes{Dictionary: dictionary{5: "time"}, TimestampAttributes: map[int32]*ts.Timestamp{5: ts1}}, in: "time", out: t1, err: false},
		{attrs: &mixerpb.Attributes{Dictionary: dictionary{2: "a"}, StringAttributes: map[int32]string{2: "foo"}}, in: "b", out: nil, err: true},
	}
	for _, c := range cases {
		bag, err := attribute.NewManager().NewTracker().StartRequest(c.attrs)
		if err != nil {
			t.Errorf("Failed to create bag for case: %v; err: %v", c, err)
		}
		id := NewIdentityEvaluator()
		val, err := id.Eval(c.in, bag)
		if c.err && err == nil {
			t.Errorf("Expected err on case '%v' but got none.", c)
		}
		if !c.err && err != nil {
			t.Errorf("Failed to eval mapExpression '%s' in bag: %v; with err: %v", c.in, bag, err)
		}
		if val != c.out {
			t.Errorf("Expected val '%v' for key '%s', actual '%v'", c.out, c.in, val)
		}
	}
}

func TestEval_Bytes(t *testing.T) {
	// You can't compare two []byte using the == operator, so we have to special case this test.
	cases := []struct {
		attrs *mixerpb.Attributes
		in    string
		out   []byte
		err   bool
	}{
		{attrs: &mixerpb.Attributes{Dictionary: dictionary{3: "bytes"}, BytesAttributes: map[int32][]byte{3: {11}}}, in: "bytes", out: []byte{11}, err: false},
	}
	for _, c := range cases {
		if len(c.attrs.BytesAttributes) == 0 {
			t.Error("TestEval_Bytes expects only ByteAttributes in the Attributes proto.")
		}

		bag, err := attribute.NewManager().NewTracker().StartRequest(c.attrs)
		if err != nil {
			t.Errorf("Failed to create bag for case: %v; err: %v", c, err)
		}
		id := NewIdentityEvaluator()
		val, err := id.Eval(c.in, bag)
		if c.err && err == nil {
			t.Errorf("Expected err on case '%v' but got none.", c)
		}
		if !c.err && err != nil {
			t.Errorf("Failed to eval mapExpression '%s' in bag: %v; with err: %v", c.in, bag, err)
		}
		if valBytes, ok := val.([]byte); !c.err && (!ok || !bytes.Equal(valBytes, c.out)) {
			t.Errorf("Expected val '%v' for key '%s', actual '%v'", c.out, c.in, val)
		}
	}
}

func TestEvalString(t *testing.T) {
	cases := []struct {
		attrs *mixerpb.Attributes
		in    string
		out   interface{}
		err   bool
	}{
		// We cannot test ByteAttributes here because you cannot compare []byte using '==' or '!='
		{attrs: &mixerpb.Attributes{
			Dictionary:       dictionary{1: "foo", 2: "bar"},
			StringAttributes: map[int32]string{1: "foo", 2: "bar"},
		}, in: "bar", out: "bar", err: false},
		{attrs: &mixerpb.Attributes{Dictionary: dictionary{4: "i64"}, Int64Attributes: map[int32]int64{4: 37}}, in: "i64", out: int64(0), err: true},
		{attrs: &mixerpb.Attributes{Dictionary: dictionary{17: "dbl"}, DoubleAttributes: map[int32]float64{17: 5.9}}, in: "dbl", out: 0, err: true},
		{attrs: &mixerpb.Attributes{Dictionary: dictionary{0: "bool"}, BoolAttributes: map[int32]bool{0: true}}, in: "bool", out: true, err: true},
		{attrs: &mixerpb.Attributes{Dictionary: dictionary{5: "time"}, TimestampAttributes: map[int32]*ts.Timestamp{5: ts1}}, in: "time", out: nil, err: true},
		{attrs: &mixerpb.Attributes{Dictionary: dictionary{2: "a"}, StringAttributes: map[int32]string{2: "foo"}}, in: "b", out: nil, err: true},
	}
	for _, c := range cases {
		bag, err := attribute.NewManager().NewTracker().StartRequest(c.attrs)
		if err != nil {
			t.Errorf("Failed to create bag for case: %v; err: %v", c, err)
		}
		id := NewIdentityEvaluator()
		val, err := id.EvalString(c.in, bag)
		if c.err && err == nil {
			t.Errorf("Expected err on case '%v' but got none.", c)
		}
		if !c.err && err != nil {
			t.Errorf("Failed to eval mapExpression '%s' in bag: %v; with err: %v", c.in, bag, err)
		}
		if !c.err && val != c.out {
			t.Errorf("Expected val '%v' for key '%s', actual '%v'", c.out, c.in, val)
		}
	}
}

func TestEvalPredicate(t *testing.T) {
	cases := []struct {
		attrs *mixerpb.Attributes
		in    string
		out   interface{}
		err   string
	}{
		// We cannot test ByteAttributes here because you cannot compare []byte using '==' or '!='
		{attrs: &mixerpb.Attributes{
			Dictionary:       dictionary{1: "foo", 2: "bar"},
			StringAttributes: map[int32]string{1: "foo", 2: "bar"},
		}, in: "badExpr1", out: "bar", err: "invalid expression"},
		{attrs: &mixerpb.Attributes{
			Dictionary:       dictionary{1: "foo", 2: "bar"},
			StringAttributes: map[int32]string{1: "foo", 2: "bar"},
		}, in: "badExpr2==", out: false, err: ""},
		{attrs: &mixerpb.Attributes{
			Dictionary:       dictionary{1: "foo", 2: "bar"},
			StringAttributes: map[int32]string{1: "foo", 2: "bar"},
		}, in: "$badExpr3==", out: "bar", err: "unresolved attribute"},
		{attrs: &mixerpb.Attributes{
			Dictionary:       dictionary{1: "foo", 2: "bar"},
			StringAttributes: map[int32]string{1: "foo", 2: "bar"},
		}, in: "==$badExpr3", out: "bar", err: "unresolved attribute"},
		{attrs: &mixerpb.Attributes{
			Dictionary:       dictionary{1: "foo", 2: "bar"},
			StringAttributes: map[int32]string{1: "foo", 2: "bar"},
		}, in: "==$", out: "bar", err: "empty attribute name"},
		{attrs: &mixerpb.Attributes{
			Dictionary:       dictionary{1: "foo", 2: "bar"},
			StringAttributes: map[int32]string{1: "foo", 2: "bar"},
		}, in: "$bar==bar", out: true, err: ""},
		{attrs: &mixerpb.Attributes{
			Dictionary:       dictionary{1: "foo", 2: "bar"},
			StringAttributes: map[int32]string{1: "foo", 2: "bar"},
		}, in: "$bar==foo", out: false, err: ""},
		{attrs: &mixerpb.Attributes{
			Dictionary:       dictionary{1: "foo", 2: "bar"},
			StringAttributes: map[int32]string{1: "foo", 2: "bar"},
		}, in: "$bar==foo", out: false, err: ""},
		{attrs: &mixerpb.Attributes{
			Dictionary:       dictionary{1: "foo", 2: "bar"},
			StringAttributes: map[int32]string{1: "foo", 2: "bar"},
		}, in: "$bar == $foo", out: false, err: ""},
		{attrs: &mixerpb.Attributes{
			Dictionary:       dictionary{1: "foo", 2: "bar"},
			StringAttributes: map[int32]string{1: "foo", 2: "foo"},
		}, in: "$bar == $foo", out: true, err: ""},
	}
	for idx, c := range cases {
		bag, err := attribute.NewManager().NewTracker().StartRequest(c.attrs)
		if err != nil {
			t.Errorf("Failed to create bag for case: %v; err: %v", c, err)
		}
		id := NewIdentityEvaluator()
		if err = id.Validate(c.in); err != nil {
			t.Errorf("[%d] Unexpected error %s", idx, err.Error())
		}
		val, err := id.EvalPredicate(c.in, bag)
		if c.err != "" { // error is expected
			errStr := fmt.Sprintf("%s", err)
			if !strings.Contains(errStr, c.err) {
				t.Errorf("[%d] Got :%s\nWant: %s", idx, errStr, c.err)
			}
		} else { // no error expected
			if err == nil {
				if val != c.out {
					t.Errorf("[%d] Expected val '%v' for key '%s', actual '%v'", idx, c.out, c.in, val)
				}
			} else {
				t.Errorf("[%d] Unexpected error %s evaluating mapExpression '%s'", idx, c.in, err)
			}
		}
	}
}
