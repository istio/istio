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

package aspect

import (
	"strconv"
	"strings"
	"testing"

	dpb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/pkg/adapter"
)

func TestFromPbType(t *testing.T) {
	cases := []struct {
		in        dpb.ValueType
		out       adapter.LabelType
		errString string
	}{
		{dpb.VALUE_TYPE_UNSPECIFIED, 0, "unsupported"},
		{dpb.STRING, adapter.String, ""},
		{dpb.INT64, adapter.Int64, ""},
		{dpb.DOUBLE, adapter.Float64, ""},
		{dpb.BOOL, adapter.Bool, ""},
		{dpb.TIMESTAMP, adapter.Time, ""},
		{dpb.IP_ADDRESS, adapter.IPAddress, ""},
		{dpb.EMAIL_ADDRESS, adapter.EmailAddress, ""},
		{dpb.URI, adapter.URI, ""},
		{dpb.DNS_NAME, adapter.DNSName, ""},
		{dpb.DURATION, adapter.Duration, ""},
		{dpb.STRING_MAP, adapter.StringMap, ""},
	}
	for idx, c := range cases {
		t.Run(strconv.Itoa(idx), func(t *testing.T) {
			out, err := valueTypeToLabelType(c.in)
			errString := ""
			if err != nil {
				errString = err.Error()
			}

			if !strings.Contains(errString, c.errString) {
				t.Errorf("valueTypeToLabelType(%v) = _, %v; wanted error containing %s", c.in, err, c.errString)
			}
			if out != c.out {
				t.Errorf("valueTypeToLabelType(%v) = %v; wanted %v", c.in, out, c.out)
			}
		})
	}
}

func TestFromPbMetricKind(t *testing.T) {
	cases := []struct {
		in        dpb.MetricDescriptor_MetricKind
		out       adapter.MetricKind
		errString string
	}{
		{dpb.METRIC_KIND_UNSPECIFIED, 0, "invalid"},
		{dpb.GAUGE, adapter.Gauge, ""},
		{dpb.COUNTER, adapter.Counter, ""},
	}
	for idx, c := range cases {
		t.Run(strconv.Itoa(idx), func(t *testing.T) {
			out, err := metricKindFromProto(c.in)
			errString := ""
			if err != nil {
				errString = err.Error()
			}

			if !strings.Contains(errString, c.errString) {
				t.Errorf("metricKindFromProto(%v) = _, %v; wanted erro containing %s", c.in, err, c.errString)
			}
			if out != c.out {
				t.Errorf("metricKindFromProto(%v) = %v, nil; wanted %v", c.in, out, c.out)
			}
		})
	}
}
