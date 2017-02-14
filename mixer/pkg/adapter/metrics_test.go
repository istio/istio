// Copyright 2017 The Istio Authors.
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

package adapter

import (
	"strconv"
	"strings"
	"testing"

	dpb "istio.io/api/mixer/v1/config/descriptor"
)

func TestString(t *testing.T) {
	val := Value{MetricValue: 1}
	if _, err := val.String(); err == nil {
		t.Error("val.String() = _, nil; wanted err")
	}
	mv := "foo"
	val.MetricValue = mv
	s, err := val.String()
	if err != nil {
		t.Errorf("val.String() = _, %s; wanted no err", err)
	}
	if s != mv {
		t.Errorf("val.String() = %s; wanted '%v'", s, mv)
	}
}

func TestBool(t *testing.T) {
	val := Value{MetricValue: "foo"}
	if _, err := val.Bool(); err == nil {
		t.Error("val.Bool() = _, nil; wanted err")
	}
	mv := true
	val.MetricValue = mv
	s, err := val.Bool()
	if err != nil {
		t.Errorf("val.Bool() = _, %s; wanted no err", err)
	}
	if s != mv {
		t.Errorf("val.Bool() = %t; wanted '%v'", s, mv)
	}
}

func TestInt64(t *testing.T) {
	val := Value{MetricValue: false}
	if _, err := val.Int64(); err == nil {
		t.Error("val.Int64() = _, nil; wanted err")
	}
	mv := int64(1)
	val.MetricValue = mv
	s, err := val.Int64()
	if err != nil {
		t.Errorf("val.Int64() = _, %s; wanted no err", err)
	}
	if s != mv {
		t.Errorf("val.Int64() = %d; wanted '%v'", s, mv)
	}
}

func TestFloat64(t *testing.T) {
	val := Value{MetricValue: int64(1)}
	if _, err := val.Float64(); err == nil {
		t.Error("val.Float64() = _, nil; wanted err")
	}
	mv := 37.0
	val.MetricValue = mv
	s, err := val.Float64()
	if err != nil {
		t.Errorf("val.Float64() = _, %s; wanted no err", err)
	}
	if s != mv {
		t.Errorf("val.Float64() = %f; wanted '%v'", s, mv)
	}
}

func TestFromPbType(t *testing.T) {
	cases := []struct {
		in        dpb.ValueType
		out       LabelType
		errString string
	}{
		{dpb.VALUE_TYPE_UNSPECIFIED, 0, "invalid"},
		{dpb.STRING, String, ""},
		{dpb.INT64, Int64, ""},
		{dpb.DOUBLE, Float64, ""},
		{dpb.BOOL, Bool, ""},
		{dpb.TIMESTAMP, Time, ""},
		{dpb.IP_ADDRESS, IPAddress, ""},
		{dpb.EMAIL_ADDRESS, EmailAddress, ""},
		{dpb.URI, URI, ""},
		{dpb.DNS_NAME, DNSName, ""},
		{dpb.DURATION, Duration, ""},
	}
	for idx, c := range cases {
		t.Run(strconv.Itoa(idx), func(t *testing.T) {
			out, err := LabelTypeFromProto(c.in)
			errString := ""
			if err != nil {
				errString = err.Error()
			}

			if !strings.Contains(errString, c.errString) {
				t.Errorf("LabelTypeFromProto(%v) = _, %v; wanted erro containing %s", c.in, err, c.errString)
			}
			if out != c.out {
				t.Errorf("LabelTypeFromProto(%v) = %v; wanted %v", c.in, out, c.out)
			}
		})
	}
}

func TestFromPbMetricKind(t *testing.T) {
	cases := []struct {
		in        dpb.MetricDescriptor_MetricKind
		out       MetricKind
		errString string
	}{
		{dpb.METRIC_KIND_UNSPECIFIED, 0, "invalid"},
		{dpb.GAUGE, Gauge, ""},
		{dpb.COUNTER, Counter, ""},
	}
	for idx, c := range cases {
		t.Run(strconv.Itoa(idx), func(t *testing.T) {
			out, err := MetricKindFromProto(c.in)
			errString := ""
			if err != nil {
				errString = err.Error()
			}

			if !strings.Contains(errString, c.errString) {
				t.Errorf("MetricKindFromProto(%v) = _, %v; wanted erro containing %s", c.in, err, c.errString)
			}
			if out != c.out {
				t.Errorf("MetricKindFromProto(%v) = %v, nil; wanted %v", c.in, out, c.out)
			}
		})
	}
}
