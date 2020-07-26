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

package cmd

import (
	"strconv"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"

	attr "istio.io/istio/mixer/pkg/attribute"
	"istio.io/pkg/attribute"
)

func TestAttributeHandling(t *testing.T) {
	ra := rootArgs{
		stringAttributes:    "a=X,b=Y,ccc=XYZ,d=X Z,e=X",
		int64Attributes:     "f=1,g=2,hhh=345",
		doubleAttributes:    "i=1,j=2,kkk=345.678",
		boolAttributes:      "l=true,m=false,nnn=true",
		timestampAttributes: "o=2006-01-02T15:04:05Z",
		durationAttributes:  "p=42s",
		bytesAttributes:     "q=1,r=34:56",
		stringMapAttributes: "s=k1:v1;k2:v2",
		attributes:          "t=XYZ,u=2,v=3.0,w=true,x=2006-01-02T15:04:05Z,y=42s,z=98:76,zz=k3:v3",
	}

	a, dw, err := parseAttributes(&ra)
	if err != nil {
		t.Errorf("Expected to parse attributes, got failure %v", err)
	}

	var b attribute.Bag
	if b, err = attr.GetBagFromProto(a, dw); err != nil {
		t.Errorf("Expected to get proto bag, got failure %v", err)
	}

	results := []struct {
		name  string
		value interface{}
	}{
		{"a", "X"},
		{"b", "Y"},
		{"ccc", "XYZ"},
		{"d", "X Z"},
		{"e", "X"},
		{"f", int64(1)},
		{"g", int64(2)},
		{"hhh", int64(345)},
		{"i", 1.0},
		{"j", 2.0},
		{"kkk", 345.678},
		{"l", true},
		{"m", false},
		{"nnn", true},
		{"o", time.Date(2006, 1, 2, 15, 4, 5, 0, time.UTC)},
		{"p", 42 * time.Second},
		{"q", []byte{1}},
		{"r", []byte{0x34, 0x56}},
		{"s", attribute.WrapStringMap(map[string]string{"k1": "v1", "k2": "v2"})},
		{"t", "XYZ"},
		{"u", int64(2)},
		{"v", 3.0},
		{"w", true},
		{"x", time.Date(2006, 1, 2, 15, 4, 5, 0, time.UTC)},
		{"y", 42 * time.Second},
		{"z", []byte{0x98, 0x76}},
		{"zz", attribute.WrapStringMap(map[string]string{"k3": "v3"})},
	}

	for _, r := range results {
		t.Run(r.name, func(t *testing.T) {
			v, found := b.Get(r.name)
			if !found {
				t.Error("Got false, expecting true")
			}

			if !attribute.Equal(v, r.value) {
				t.Errorf("Got %v, expected %v", v, r.value)
			}

			found = false
			for _, v := range dw {
				if v == r.name {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Got false, want true")
			}
		})
	}
}

func TestAttributeErrorHandling(t *testing.T) {
	cases := []rootArgs{
		{stringAttributes: "a,b=Y,ccc=XYZ,d=X Z,e=X"},
		{stringAttributes: "=,b=Y,ccc=XYZ,d=X Z,e=X"},
		{stringAttributes: "=X,b=Y,ccc=XYZ,d=X Z,e=X"},

		{int64Attributes: "f,g=2,hhh=345"},
		{int64Attributes: "f=,g=2,hhh=345"},
		{int64Attributes: "=,g=2,hhh=345"},
		{int64Attributes: "=1,g=2,hhh=345"},
		{int64Attributes: "f=XY,g=2,hhh=345"},

		{doubleAttributes: "i,j=2,kkk=345.678"},
		{doubleAttributes: "i=,j=2,kkk=345.678"},
		{doubleAttributes: "=,j=2,kkk=345.678"},
		{doubleAttributes: "=1,j=2,kkk=345.678"},
		{doubleAttributes: "i=XY,j=2,kkk=345.678"},

		{boolAttributes: "l,m=false,nnn=true"},
		{boolAttributes: "l=,m=false,nnn=true"},
		{boolAttributes: "=,m=false,nnn=true"},
		{boolAttributes: "=true,m=false,nnn=true"},
		{boolAttributes: "l=EURT,m=false,nnn=true"},

		{timestampAttributes: "o"},
		{timestampAttributes: "o="},
		{timestampAttributes: "="},
		{timestampAttributes: "=2006-01-02T15:04:05Z"},
		{timestampAttributes: "o=XYZ"},

		{durationAttributes: "x"},
		{durationAttributes: "x="},
		{durationAttributes: "="},
		{durationAttributes: "=1.2"},
		{durationAttributes: "x=XYZ"},

		{bytesAttributes: "p,q=34:56"},
		{bytesAttributes: "p=,q=34:56"},
		{bytesAttributes: "=,q=34:56"},
		{bytesAttributes: "=1,q=34:56"},
		{bytesAttributes: "p=XY,q=34:56"},
		{bytesAttributes: "p=123,q=34:56"},

		{stringMapAttributes: "p,q=34:56"},
		{stringMapAttributes: "p=,q=34:56"},
		{stringMapAttributes: "=,q=34:56"},
		{stringMapAttributes: "=1,q=34:56"},
		{stringMapAttributes: "p=XY,q=34:56"},
		{stringMapAttributes: "p=123,q=34:56"},
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			a, _, err := parseAttributes(&c)
			if a != nil {
				t.Error("Got a valid struct, expected nil")
			}
			if err == nil {
				t.Error("Got success, expected failure")
			}
		})
	}
}

func TestDecodeStatus(t *testing.T) {
	// just making sure all paths work properly
	cases := []rpc.Status{
		{Code: int32(rpc.ALREADY_EXISTS)},
		{Code: 123456},

		{Code: int32(rpc.ALREADY_EXISTS), Message: "FOO"},
		{Code: 123456, Message: "FOO"},
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			s := decodeStatus(c)
			if s == "" {
				t.Error("Got '', expecting a valid string")
			}
		})
	}
}

func TestDecodeError(t *testing.T) {
	cases := []error{
		status.Errorf(codes.AlreadyExists, ""),
		status.Errorf(codes.Code(123456), ""),
		status.Errorf(codes.AlreadyExists, "FOO"),
		status.Errorf(codes.Code(123456), "FOO"),
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			s := decodeError(c)
			if s == "" {
				t.Error("Got '', expecting a valid string")
			}
		})
	}
}
