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

package maintest

import (
	"os"
	"testing"
)

const (
	// environment variable that triggers execution of the regular `main()`
	// function that was compiled into the test binary
	envRunRegularMain = "RUN_REGULAR_MAIN"
)

// TestMainOrRegularMain either runs tests or executes the regular `main()`
// function that was compiled into the test binary.
func TestMainOrRegularMain(m *testing.M, main func()) {
	if shouldRunRegularMain() {
		main() // run `main()` function that was compiled into the test binary
		os.Exit(0)
	}
	os.Exit(m.Run()) // run tests
}

// AskRegularMain changes environment for the test binary to trigger execution
// of the regular `main()` function, that was compiled into the test binary,
// instead of tests.
func AskRegularMain(env []string) []string {
	return append(env, envRunRegularMain+"=1")
}

func shouldRunRegularMain() bool {
	return os.Getenv(envRunRegularMain) == "1"
}
