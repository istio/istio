// Copyright Istio Authors.
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

package shellescape

import (
	"os/exec"
	"strings"
	"testing"
)

func TestQuote(t *testing.T) {
	testCases := []string{
		`{"key": "value"}`,
		`it's going to need single quotes`,
		"",
		"no issues here",
	}
	for _, original := range testCases {
		out, err := exec.Command("sh", "-c", "echo "+Quote(original)).CombinedOutput()
		if err != nil {
			t.Fatal(err)
		}
		got := strings.TrimRight(string(out), "\n")
		if got != original {
			t.Errorf("got: %s; want: %s", got, original)
		}
	}
}
