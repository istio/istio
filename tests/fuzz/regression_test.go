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

package fuzz

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"istio.io/istio/pilot/pkg/util/runtime"
)

// baseCases contains a few trivial test cases to do a very brief sanity check of a test
var baseCases = [][]byte{
	{},
	[]byte("."),
	[]byte(".............."),
}

// brokenCases contains test cases that are currently failing. These should only be added if the
// failure is publicly disclosed!
var brokenCases = map[string]string{
	"4863517148708864": "https://github.com/kubernetes/kubernetes/issues/97651",
}

func runRegressionTest(t *testing.T, name string, fuzz func(data []byte) int) {
	dir := filepath.Join("testdata", name)
	cases, err := ioutil.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	runfuzz := func(t *testing.T, name string, by []byte) {
		defer func() {
			if r := recover(); r != nil {
				if _, broken := brokenCases[name]; broken {
					t.Log("expected broken case failed")
				} else {
					runtime.LogPanic(r)
					t.Fatalf("panic encountered: %v", r)
				}
			} else {
				// Ensure we update brokenCases when they are fixed
				if _, broken := brokenCases[name]; broken {
					t.Fatalf("expected broken case passed")
				}
			}
		}()
		fuzz(by)
	}
	for i, c := range baseCases {
		t.Run(fmt.Sprintf("base case %d", i), func(t *testing.T) {
			runfuzz(t, "", c)
		})
	}
	for _, c := range cases {
		t.Run(c.Name(), func(t *testing.T) {
			by, err := ioutil.ReadFile(filepath.Join(dir, c.Name()))
			if err != nil {
				t.Fatal(err)
			}
			runfuzz(t, c.Name(), by)
		})
	}
}

func TestFuzzParseInputs(t *testing.T) {
	runRegressionTest(t, "FuzzParseInputs", FuzzParseInputs)
}

func TestFuzzParseAndBuildSchema(t *testing.T) {
	runRegressionTest(t, "FuzzParseAndBuildSchema", FuzzParseAndBuildSchema)
}
