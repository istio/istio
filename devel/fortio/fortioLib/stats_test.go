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
	"log"
	"testing"
)

func TestCounter(t *testing.T) {
	var c Counter
	var b bytes.Buffer
	w := bufio.NewWriter(&b)
	c.Printf(w, "test1")
	expected := "test1 : count 0 avg NaN +/- NaN min 0 max 0 sum 0\n"
	c.Record(23.1)
	c.Printf(w, "test2")
	expected += "test2 : count 1 avg 23.1 +/- 0 min 23.1 max 23.1 sum 23.1\n"
	c.Record(22.9)
	c.Printf(w, "test3")
	expected += "test3 : count 2 avg 23 +/- 0.1 min 22.9 max 23.1 sum 46\n"
	c.Record(23.1)
	c.Record(22.9)
	c.Printf(w, "test4")
	expected += "test4 : count 4 avg 23 +/- 0.1 min 22.9 max 23.1 sum 92\n"
	c.Record(1023)
	c.Record(-977)
	c.Printf(w, "test5")
	// note that stddev of 577.4 below is... whatever the code said
	finalExpected := " : count 6 avg 23 +/- 577.4 min -977 max 1023 sum 138\n"
	expected += "test5" + finalExpected
	// Try the Log() function too:
	log.SetOutput(w)
	log.SetFlags(0)
	c.Log("testLog")
	expected += "testLog" + finalExpected
	w.Flush() // nolint: errcheck
	actual := string(b.Bytes())
	if actual != expected {
		t.Errorf("unexpected:\n%s\nvs:\n%s\n", actual, expected)
	}
	c.Record(1023)
	c.Record(-977)
}
