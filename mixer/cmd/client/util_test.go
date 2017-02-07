// Copyright 2016 Google Inc.
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

package main

import (
	"reflect"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/duration"
	"github.com/golang/protobuf/ptypes/timestamp"

	mixerpb "istio.io/api/mixer/v1"
)

func TestAttributeHandling(t *testing.T) {
	type testCase struct {
		rootArgs rootArgs
		attrs    mixerpb.Attributes
		result   bool
	}

	ts, _ := ptypes.TimestampProto(time.Date(2006, 1, 2, 15, 4, 5, 0, time.UTC))
	d := ptypes.DurationProto(time.Duration(42) * time.Second)

	cases := []testCase{
		{
			rootArgs: rootArgs{
				stringAttributes:    "a=X,b=Y,ccc=XYZ,d=X Z,e=X",
				int64Attributes:     "f=1,g=2,hhh=345",
				doubleAttributes:    "i=1,j=2,kkk=345.678",
				boolAttributes:      "l=true,m=false,nnn=true",
				timestampAttributes: "o=2006-01-02T15:04:05Z",
				durationAttributes:  "p=42s",
				bytesAttributes:     "q=1,r=34:56",
				attributes:          "s=XYZ,t=2,u=3.0,v=true,w=2006-01-02T15:04:05Z,x=98:76,y=42s",
			},
			attrs: mixerpb.Attributes{
				Dictionary: map[int32]string{
					0: "a", 1: "b", 2: "ccc", 3: "d", 4: "e", 5: "f", 6: "g",
					7: "hhh", 8: "i", 9: "j", 10: "kkk", 11: "l", 12: "m", 13: "nnn", 14: "o",
					15: "p", 16: "q", 17: "r", 18: "s", 19: "t", 20: "u", 21: "v", 22: "w", 23: "x", 24: "y"},
				StringAttributes:    map[int32]string{0: "X", 1: "Y", 2: "XYZ", 3: "X Z", 4: "X", 18: "XYZ"},
				Int64Attributes:     map[int32]int64{5: 1, 6: 2, 7: 345, 19: 2},
				DoubleAttributes:    map[int32]float64{8: 1, 9: 2, 10: 345.678, 20: 3.0},
				BoolAttributes:      map[int32]bool{11: true, 12: false, 13: true, 21: true},
				TimestampAttributes: map[int32]*timestamp.Timestamp{14: ts, 22: ts},
				DurationAttributes:  map[int32]*duration.Duration{15: d, 24: d},
				BytesAttributes:     map[int32][]uint8{16: {1}, 17: {0x34, 0x56}, 23: {0x98, 0x76}},
			},
			result: true,
		},

		{rootArgs: rootArgs{stringAttributes: "a,b=Y,ccc=XYZ,d=X Z,e=X"}},
		{rootArgs: rootArgs{stringAttributes: "=,b=Y,ccc=XYZ,d=X Z,e=X"}},
		{rootArgs: rootArgs{stringAttributes: "=X,b=Y,ccc=XYZ,d=X Z,e=X"}},

		{rootArgs: rootArgs{int64Attributes: "f,g=2,hhh=345"}},
		{rootArgs: rootArgs{int64Attributes: "f=,g=2,hhh=345"}},
		{rootArgs: rootArgs{int64Attributes: "=,g=2,hhh=345"}},
		{rootArgs: rootArgs{int64Attributes: "=1,g=2,hhh=345"}},
		{rootArgs: rootArgs{int64Attributes: "f=XY,g=2,hhh=345"}},

		{rootArgs: rootArgs{doubleAttributes: "i,j=2,kkk=345.678"}},
		{rootArgs: rootArgs{doubleAttributes: "i=,j=2,kkk=345.678"}},
		{rootArgs: rootArgs{doubleAttributes: "=,j=2,kkk=345.678"}},
		{rootArgs: rootArgs{doubleAttributes: "=1,j=2,kkk=345.678"}},
		{rootArgs: rootArgs{doubleAttributes: "i=XY,j=2,kkk=345.678"}},

		{rootArgs: rootArgs{boolAttributes: "l,m=false,nnn=true"}},
		{rootArgs: rootArgs{boolAttributes: "l=,m=false,nnn=true"}},
		{rootArgs: rootArgs{boolAttributes: "=,m=false,nnn=true"}},
		{rootArgs: rootArgs{boolAttributes: "=true,m=false,nnn=true"}},
		{rootArgs: rootArgs{boolAttributes: "l=EURT,m=false,nnn=true"}},

		{rootArgs: rootArgs{timestampAttributes: "o"}},
		{rootArgs: rootArgs{timestampAttributes: "o="}},
		{rootArgs: rootArgs{timestampAttributes: "="}},
		{rootArgs: rootArgs{timestampAttributes: "=2006-01-02T15:04:05Z"}},
		{rootArgs: rootArgs{timestampAttributes: "o=XYZ"}},

		{rootArgs: rootArgs{durationAttributes: "x"}},
		{rootArgs: rootArgs{durationAttributes: "x="}},
		{rootArgs: rootArgs{durationAttributes: "="}},
		{rootArgs: rootArgs{durationAttributes: "=1.2"}},
		{rootArgs: rootArgs{durationAttributes: "x=XYZ"}},

		{rootArgs: rootArgs{bytesAttributes: "p,q=34:56"}},
		{rootArgs: rootArgs{bytesAttributes: "p=,q=34:56"}},
		{rootArgs: rootArgs{bytesAttributes: "=,q=34:56"}},
		{rootArgs: rootArgs{bytesAttributes: "=1,q=34:56"}},
		{rootArgs: rootArgs{bytesAttributes: "p=XY,q=34:56"}},
		{rootArgs: rootArgs{bytesAttributes: "p=123,q=34:56"}},
	}

	for i, c := range cases {
		if a, err := parseAttributes(&c.rootArgs); err == nil {
			if !c.result {
				t.Errorf("Expected failure for test case %v, got success", i)
			}

			// technically, this is enforcing the dictionary mappings, which is not guaranteed by the
			// API contract, but that's OK...
			if !reflect.DeepEqual(a, &c.attrs) {
				t.Errorf("Mismatched results for test %v\nGot:\n%#v\nExpected\n%#v\n", i, a, c.attrs)
			}
		} else {
			if c.result {
				t.Errorf("Expected success for test case %v, got failure %v", i, err)
			}

			if a != nil {
				t.Errorf("Expecting nil attributes, got some instead for test case %v", i)
			}
		}
	}
}
