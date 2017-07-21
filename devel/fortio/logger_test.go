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
	log.SetOutput(w)
	log.SetFlags(0)
	// Start of the actual test
	SetLogLevel(LogLevelByName("Verbose"))
	expected := "I Log level is now 1 Verbose (was 2 Info)\n"
	i := 0
	LogV("test Va %d", i) // Should show
	i++
	expected += "V test Va 0\n"
	LogWarning("test Wa %d", i) // Should show
	i++
	expected += "W test Wa 1\n"
	prevLevel := SetLogLevel(LogLevelByName("Error"))
	expected += "I Log level is now 4 Error (was 1 Verbose)\n"
	LogV("test Vb %d", i) // Should not show
	i++
	LogWarning("test Wb %d", i) // Should not show
	i++
	LogError("test E %d", i) // Should show
	i++
	expected += "E test E 4\n"
	// test the rest of the api
	Log(LogLevelByName("Critical"), "test %d level str %s, %+v", i, prevLevel.String(), GetLogLevel().ToString())
	expected += "C test 5 level str Verbose, Error\n"
	SetLogLevel(D) // should be fine and invisible change
	SetLogLevel(D - 1)
	expected += "SetLogLevel called with level -1 lower than Debug!\n"
	SetLogLevel(F + 1)
	expected += "SetLogLevel called with level 7 higher than Fatal!\n"
	SetLogLevel(C) // should be fine
	expected += "I Log level is now 5 Critical (was 0 Debug)\n"
	w.Flush() // nolint: errcheck
	actual := string(b.Bytes())
	if actual != expected {
		t.Errorf("unexpected:\n%s\nvs:\n%s\n", actual, expected)
	}
}
