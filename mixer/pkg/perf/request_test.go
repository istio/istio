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

package perf

import (
	"testing"

	istio_mixer_v1 "istio.io/api/mixer/v1"
)

func TestBasicReportRequest(t *testing.T) {
	report := BuildBasicReport(map[string]interface{}{
		"foo": "bar",
		"baz": int64(42),
	})

	proto := report.getRequestProto()
	if proto == nil {
		t.Fatalf("should have created 1 proto")
	}

	actual, ok := proto.(*istio_mixer_v1.ReportRequest)
	if !ok {
		t.Fatalf("should have created a ReportRequest proto")
	}

	if len(actual.Attributes) != 1 {
		t.Fatalf("should have 1 set of attributes")
	}
	if len(actual.Attributes[0].Words) != 3 {
		t.Fatalf("got %v, should have 3 words", actual.Attributes[0].Words)
	}
	if len(actual.Attributes[0].Strings) != 1 {
		t.Fatalf("got %v, should have one string", actual.Attributes[0].Strings)
	}
	if len(actual.Attributes[0].Int64S) != 1 {
		t.Fatalf("should have 1 integers")
	}
	for _, v := range actual.Attributes[0].Int64S {
		if v != int64(42) {
			t.Fatal("The single int64 attribute should have been 42")
		}
	}
	actualMap := make(map[string]string)
	for k, v := range actual.Attributes[0].Strings {
		key := actual.Attributes[0].Words[(k*-1)-1]
		value := actual.Attributes[0].Words[(v*-1)-1]
		actualMap[key] = value
	}

	if actualMap["foo"] != "bar" {
		t.Fail()
	}
}

func TestBasicCheckRequest(t *testing.T) {
	report := BuildBasicCheck(
		map[string]interface{}{
			"foo": "bar",
		},
		map[string]istio_mixer_v1.CheckRequest_QuotaParams{
			"zoo": {
				BestEffort: true,
				Amount:     43,
			},
			"far": {
				BestEffort: false,
				Amount:     23,
			},
		})

	proto := report.getRequestProto()
	if proto == nil {
		t.Fatalf("should have created 1 proto")
	}

	actual, ok := proto.(*istio_mixer_v1.CheckRequest)
	if !ok {
		t.Fatalf("should have created a CheckRequest proto")
	}

	if len(actual.Attributes.Words) != 2 {
		t.Fatalf("should have 2 words")
	}
	if len(actual.Attributes.Strings) != 1 {
		t.Fatalf("should have one string")
	}
	actualMap := make(map[string]string)
	for k, v := range actual.Attributes.Strings {
		key := actual.Attributes.Words[(k*-1)-1]
		value := actual.Attributes.Words[(v*-1)-1]
		actualMap[key] = value
	}

	if actualMap["foo"] != "bar" {
		t.Fail()
	}

	if len(actual.Quotas) != 2 {
		t.Fatalf("should have 2 quota params")
	}

	q1, ok := actual.Quotas["zoo"]
	if !ok {
		t.Fatalf("should have found zoo")
	}
	if q1.Amount != 43 || !q1.BestEffort {
		t.Fail()
	}

	q2, ok := actual.Quotas["far"]
	if !ok {
		t.Fatalf("should have found far")
	}
	if q2.Amount != 23 || q2.BestEffort {
		t.Fail()
	}
}
