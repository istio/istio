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

package version

import (
	"bytes"
	"fmt"
	"runtime"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	host = "test-host"
	gitBranch = "test-git-branch"
	gitRevision = "test-git-revision"
	user = "test-user"
	version = "test-version"

	expectedOutput := fmt.Sprintf(
		"Version: %v\nGitRevision: %v\nGitBranch: %v\nUser: %v@%v\nGolang version: %v\n",
		version, gitRevision, gitBranch, user, host, runtime.Version())

	var buffer bytes.Buffer
	printFunc = func(format string, a ...interface{}) (int, error) {
		buffer.WriteString(fmt.Sprintf(format, a...))
		return 0, nil
	}
	Command.Run(nil, nil)
	actualOutput := buffer.String()

	if expectedOutput != actualOutput {
		t.Errorf("Unexpected output: wanted %v but got %v", expectedOutput, actualOutput)
	}
}
