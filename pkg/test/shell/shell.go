//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package shell

import (
	"fmt"
	"os/exec"
	"strings"

	"istio.io/pkg/log"
)

var scope = log.RegisterScope("shell", "Shell execution scope", 0)

// Execute the given command.
func Execute(combinedOutput bool, format string, args ...interface{}) (string, error) {
	s := fmt.Sprintf(format, args...)
	// TODO: escape handling
	parts := strings.Split(s, " ")

	var p []string
	for i := 0; i < len(parts); i++ {
		if parts[i] != "" {
			p = append(p, parts[i])
		}
	}

	var argStrings []string
	if len(p) > 0 {
		argStrings = p[1:]
	}
	return ExecuteArgs(nil, combinedOutput, parts[0], argStrings...)
}

func ExecuteArgs(env []string, combinedOutput bool, name string, args ...string) (string, error) {
	if scope.DebugEnabled() {
		cmd := strings.Join(args, " ")
		cmd = name + " " + cmd
		scope.Debugf("Executing command: %s", cmd)
	}

	c := exec.Command(name, args...)
	c.Env = env

	var b []byte
	var err error
	if combinedOutput {
		// Combine stderr and stdout in b.
		b, err = c.CombinedOutput()
	} else {
		// Just return stdout in b.
		b, err = c.Output()
	}

	if err != nil || !c.ProcessState.Success() {
		scope.Debugf("Command[%s] => (FAILED) %s", name, string(b))
	} else {
		scope.Debugf("Command[%s] => %s", name, string(b))
	}

	return string(b), err
}
