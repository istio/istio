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
	c.Counter.Print(w, "test1c")
	expected := "test1c : count 0 avg NaN +/- NaN min 0 max 0 sum 0\n"
	c.Print(w, "test1h", 50.0)
	expected += "test1h : no data\n"
	c.Record(23.1)
	c.Counter.Print(w, "test2")
	expected += "test2 : count 1 avg 23.1 +/- 0 min 23.1 max 23.1 sum 23.1\n"
	c.Record(22.9)
	c.Counter.Print(w, "test3")
	expected += "test3 : count 2 avg 23 +/- 0.1 min 22.9 max 23.1 sum 46\n"
	c.Record(23.1)
	c.Record(22.9)
	c.Counter.Print(w, "test4")
	expected += "test4 : count 4 avg 23 +/- 0.1 min 22.9 max 23.1 sum 92\n"
	c.Record(1023)
	c.Record(-977)
	c.Counter.Print(w, "test5")
	// note that stddev of 577.4 below is... whatever the code said
	finalExpected := " : count 6 avg 23 +/- 577.4 min -977 max 1023 sum 138\n"
	expected += "test5" + finalExpected
	// Try the Log() function too:
	log.SetOutput(w)
	log.SetFlags(0)
	c.Counter.Log("testLog")
	expected += "I testLog" + finalExpected
	w.Flush() // nolint: errcheck
	actual := b.String()
	if actual != expected {
		t.Errorf("unexpected:\n%s\nvs:\n%s\n", actual, expected)
	}
}

func TestTransferCounter(t *testing.T) {
	var b bytes.Buffer
	w := bufio.NewWriter(&b)
	var c1 Counter
	c1.Record(10)
	c1.Record(20)
	var c2 Counter
	c2.Record(80)
	c2.Record(90)
	c1a := c1
	c2a := c2
	var c3 Counter
	c1.Print(w, "c1 before merge")
	c2.Print(w, "c2 before merge")
	c1.Transfer(&c2)
	c1.Print(w, "mergedC1C2")
	c2.Print(w, "c2 after merge")
	// reverse (exercise min if)
	c2a.Transfer(&c1a)
	c2a.Print(w, "mergedC2C1")
	// test transfer into empty - min should be set
	c3.Transfer(&c1)
	c1.Print(w, "c1 should now be empty")
	c3.Print(w, "c3 after merge - 1")
	// test empty transfer - shouldn't reset min/no-op
	c3.Transfer(&c2)
	c3.Print(w, "c3 after merge - 2")
	w.Flush() // nolint: errcheck
	actual := b.String()
	expected := `c1 before merge : count 2 avg 15 +/- 5 min 10 max 20 sum 30
c2 before merge : count 2 avg 85 +/- 5 min 80 max 90 sum 170
mergedC1C2 : count 4 avg 50 +/- 35.36 min 10 max 90 sum 200
c2 after merge : count 0 avg NaN +/- NaN min 0 max 0 sum 0
mergedC2C1 : count 4 avg 50 +/- 35.36 min 10 max 90 sum 200
c1 should now be empty : count 0 avg NaN +/- NaN min 0 max 0 sum 0
c3 after merge - 1 : count 4 avg 50 +/- 35.36 min 10 max 90 sum 200
c3 after merge - 2 : count 4 avg 50 +/- 35.36 min 10 max 90 sum 200
`
	if actual != expected {
		t.Errorf("unexpected:\n%s\tvs:\n%s", actual, expected)
	}
}

func TestHistogram(t *testing.T) {
	h := NewHistogram(0, 10)
	h.Record(1)
	h.Record(251)
	h.Record(501)
	h.Record(751)
	h.Record(1001)
	h.Print(os.Stdout, "testHistogram1", 50)
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

func TestHistogramLastBucket(t *testing.T) {
	// Use -1 offset so first bucket is negative values
	h := NewHistogram( /* offset */ -1 /*scale */, 1)
	h.Record(-1)
	h.Record(0)
	h.Record(1)
	h.Record(3)
	h.Record(10)
	h.Record(99998)
	h.Record(99999) // first value of last bucket 100k-offset
	h.Record(200000)
	var b bytes.Buffer
	w := bufio.NewWriter(&b)
	h.Print(w, "testLastBucket", 90)
	w.Flush() // nolint: errcheck
	actual := b.String()
	// stdev part is not verified/could be brittle
	expected := `testLastBucket : count 8 avg 50001.25 +/- 7.071e+04 min -1 max 200000 sum 400010
# range, mid point, percentile, count
< 0 , 0 , 12.50, 1
>= 0 < 1 , 0.5 , 25.00, 1
>= 1 < 2 , 1.5 , 37.50, 1
>= 3 < 4 , 3.5 , 50.00, 1
>= 10 < 11 , 10.5 , 62.50, 1
>= 74999 < 99999 , 87499 , 75.00, 1
>= 99999 , 99999 , 100.00, 2
# target 90% 160000
`
	if actual != expected {
		t.Errorf("unexpected:\n%s\tvs:\n%s", actual, expected)
	}
}

func TestHistogramNegativeNumbers(t *testing.T) {
	h := NewHistogram( /* offset */ -10 /*scale */, 1)
	h.Record(-10)
	h.Record(10)
	var b bytes.Buffer
	w := bufio.NewWriter(&b)
	// TODO: fix the p51 (and p1...), should be 0 not 10
	h.Print(w, "testHistogramWithNegativeNumbers", 51)
	w.Flush() // nolint: errcheck
	actual := b.String()
	// stdev part is not verified/could be brittle
	expected := `testHistogramWithNegativeNumbers : count 2 avg 0 +/- 10 min -10 max 10 sum 0
# range, mid point, percentile, count
< -9 , -9 , 50.00, 1
>= 10 < 15 , 12.5 , 100.00, 1
# target 51% 10
`
	if actual != expected {
		t.Errorf("unexpected:\n%s\tvs:\n%s", actual, expected)
	}
}

func TestTransferHistogram(t *testing.T) {
	tP := 100. // TODO: use 75 and fix bug
	var b bytes.Buffer
	w := bufio.NewWriter(&b)
	h1 := NewHistogram(0, 10)
	h1.Record(10)
	h1.Record(20)
	h2 := NewHistogram(0, 10)
	h2.Record(80)
	h2.Record(90)
	h1a := h1.Clone()
	h1a.Record(50) // add extra pt to make sure h1a and h1 are distinct
	h2a := h2.Clone()
	h3 := NewHistogram(0, 10)
	h1.Print(w, "h1 before merge", tP)
	h2.Print(w, "h2 before merge", tP)
	h1.Transfer(h2)
	h1.Print(w, "merged h2 -> h1", tP)
	h2.Print(w, "h2 after merge", tP)
	// reverse (exercise min if)
	h2a.Transfer(h1a)
	h2a.Print(w, "merged h1a -> h2a", tP)
	// test transfer into empty - min should be set
	h3.Transfer(h1)
	h1.Print(w, "h1 should now be empty", tP)
	h3.Print(w, "h3 after merge - 1", tP)
	// test empty transfer - shouldn't reset min/no-op
	h3.Transfer(h2)
	h3.Print(w, "h3 after merge - 2", tP)
	w.Flush() // nolint: errcheck
	actual := b.String()
	expected := `h1 before merge : count 2 avg 15 +/- 5 min 10 max 20 sum 30
# range, mid point, percentile, count
>= 10 < 20 , 15 , 50.00, 1
>= 20 < 30 , 25 , 100.00, 1
# target 100% 20
h2 before merge : count 2 avg 85 +/- 5 min 80 max 90 sum 170
# range, mid point, percentile, count
>= 80 < 90 , 85 , 50.00, 1
>= 90 < 100 , 95 , 100.00, 1
# target 100% 90
merged h2 -> h1 : count 4 avg 50 +/- 35.36 min 10 max 90 sum 200
# range, mid point, percentile, count
>= 10 < 20 , 15 , 25.00, 1
>= 20 < 30 , 25 , 50.00, 1
>= 80 < 90 , 85 , 75.00, 1
>= 90 < 100 , 95 , 100.00, 1
# target 100% 90
h2 after merge : no data
merged h1a -> h2a : count 5 avg 50 +/- 31.62 min 10 max 90 sum 250
# range, mid point, percentile, count
>= 10 < 20 , 15 , 20.00, 1
>= 20 < 30 , 25 , 40.00, 1
>= 50 < 60 , 55 , 60.00, 1
>= 80 < 90 , 85 , 80.00, 1
>= 90 < 100 , 95 , 100.00, 1
# target 100% 90
h1 should now be empty : no data
h3 after merge - 1 : count 4 avg 50 +/- 35.36 min 10 max 90 sum 200
# range, mid point, percentile, count
>= 10 < 20 , 15 , 25.00, 1
>= 20 < 30 , 25 , 50.00, 1
>= 80 < 90 , 85 , 75.00, 1
>= 90 < 100 , 95 , 100.00, 1
# target 100% 90
h3 after merge - 2 : count 4 avg 50 +/- 35.36 min 10 max 90 sum 200
# range, mid point, percentile, count
>= 10 < 20 , 15 , 25.00, 1
>= 20 < 30 , 25 , 50.00, 1
>= 80 < 90 , 85 , 75.00, 1
>= 90 < 100 , 95 , 100.00, 1
# target 100% 90
`
	if actual != expected {
		t.Errorf("unexpected:\n%s\tvs:\n%s", actual, expected)
	}
}
