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

package cmd

import (
	"errors"
	"testing"
)

var KnownSubstrings = []string{"unknown command"}

func TestKnownExitStrings(t *testing.T) {
	for _, s := range KnownSubstrings {
		if GetExitCode(errors.New(s)) == ExitUnknownError {
			t.Errorf("Expected %q to have a known exit code.", s)
		}
	}
}
