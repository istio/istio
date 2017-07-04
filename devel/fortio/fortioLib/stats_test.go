// Copyright 2017 Istio Authors
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

package fortio

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"os"
	"testing"
)

func TestCounter(t *testing.T) {
	c := NewHistogram(22, 0.1)
	var b bytes.Buffer
	w := bufio.NewWriter(&b)
	c.Counter.FPrint(w, "test1c")
	expected := "test1c : count 0 avg NaN +/- NaN min 0 max 0 sum 0\n"
	c.FPrint(w, "test1h", 50.0)
	expected += "test1h : no data\n"
	c.Record(23.1)
	c.Counter.FPrint(w, "test2")
	expected += "test2 : count 1 avg 23.1 +/- 0 min 23.1 max 23.1 sum 23.1\n"
	c.Record(22.9)
	c.Counter.FPrint(w, "test3")
	expected += "test3 : count 2 avg 23 +/- 0.1 min 22.9 max 23.1 sum 46\n"
	c.Record(23.1)
	c.Record(22.9)
	c.Counter.FPrint(w, "test4")
	expected += "test4 : count 4 avg 23 +/- 0.1 min 22.9 max 23.1 sum 92\n"
	c.Record(1023)
	c.Record(-977)
	c.Counter.FPrint(w, "test5")
	// note that stddev of 577.4 below is... whatever the code said
	finalExpected := " : count 6 avg 23 +/- 577.4 min -977 max 1023 sum 138\n"
	expected += "test5" + finalExpected
	// Try the Log() function too:
	log.SetOutput(w)
	log.SetFlags(0)
	c.Counter.Log("testLog")
	expected += "testLog" + finalExpected
	w.Flush() // nolint: errcheck
	actual := string(b.Bytes())
	if actual != expected {
		t.Errorf("unexpected:\n%s\nvs:\n%s\n", actual, expected)
	}
}

func TestHistogram(t *testing.T) {
	h := NewHistogram(0, 10)
	h.Record(1)
	h.Record(251)
	h.Record(501)
	h.Record(751)
	h.Record(1001)
	h.FPrint(os.Stdout, "testHistogram1", 50)
	for i := 25; i <= 100; i += 25 {
		fmt.Printf("%d%% at %g\n", i, h.CalcPercentile(float64(i)))
	}
	var tests = []struct {
		actual   float64
		expected float64
		msg      string
	}{
		{h.Avg(), 501, "avg"},
		{h.CalcPercentile(-1), 1, "p-1"}, // not valid but should return min
		{h.CalcPercentile(0), 1, "p0"},
		{h.CalcPercentile(0.1), 1.045, "p0.1"},
		{h.CalcPercentile(1), 1.45, "p1"},
		{h.CalcPercentile(20), 10, "p20"},         // 20% = first point, 1st bucket is 10
		{h.CalcPercentile(20.1), 250.25, "p20.1"}, // near beginning of bucket of 2nd pt
		{h.CalcPercentile(50), 550, "p50"},
		{h.CalcPercentile(75), 775, "p75"},
		{h.CalcPercentile(90), 1000.5, "p90"},
		{h.CalcPercentile(99), 1000.95, "p99"},
		{h.CalcPercentile(99.9), 1000.995, "p99.9"},
		{h.CalcPercentile(100), 1001, "p100"},
		{h.CalcPercentile(101), 1001, "p101"},
	}
	for _, tst := range tests {
		if tst.actual != tst.expected {
			t.Errorf("%s: got %g, not as expected %g", tst.msg, tst.actual, tst.expected)
		}
	}
}
