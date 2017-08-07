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

func TestLogger1(t *testing.T) {
	// Setup
	var b bytes.Buffer
	w := bufio.NewWriter(&b)
	SetLogLevel(Info) // reset from other tests
	*logFileAndLine = false
	*logPrefix = ""
	log.SetOutput(w)
	log.SetFlags(0)
	// Start of the actual test
	SetLogLevel(LogLevelByName("Verbose"))
	expected := "I Log level is now 1 Verbose (was 2 Info)\n"
	i := 0
	LogVf("test Va %d", i) // Should show
	i++
	expected += "V test Va 0\n"
	Warnf("test Wa %d", i) // Should show
	i++
	expected += "W test Wa 1\n"
	prevLevel := SetLogLevel(LogLevelByName("error")) // works with lowercase too
	expected += "I Log level is now 4 Error (was 1 Verbose)\n"
	LogVf("test Vb %d", i) // Should not show
	i++
	Warnf("test Wb %d", i) // Should not show
	i++
	Errf("test E %d", i) // Should show
	i++
	expected += "E test E 4\n"
	// test the rest of the api
	Logf(LogLevelByName("Critical"), "test %d level str %s, cur %s", i, prevLevel.String(), GetLogLevel().ToString())
	expected += "C test 5 level str Verbose, cur Error\n"
	SetLogLevel(Debug) // should be fine and invisible change
	SetLogLevel(Debug - 1)
	expected += "SetLogLevel called with level -1 lower than Debug!\n"
	SetLogLevel(Fatal) // Hiding critical level is not allowed
	expected += "SetLogLevel called with level 6 higher than Critical!\n"
	SetLogLevel(Critical) // should be fine
	expected += "I Log level is now 5 Critical (was 0 Debug)\n"
	w.Flush() // nolint: errcheck
	actual := b.String()
	if actual != expected {
		t.Errorf("unexpected:\n%s\nvs:\n%s\n", actual, expected)
	}
}

func BenchmarkLogDirect1(b *testing.B) {
	level = Error
	for n := 0; n < b.N; n++ {
		Debugf("foo bar %d", n)
	}
}

func BenchmarkLogDirect2(b *testing.B) {
	level = Error
	for n := 0; n < b.N; n++ {
		Logf(Debug, "foo bar %d", n)
	}
}
