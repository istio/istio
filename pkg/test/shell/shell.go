//  Copyright 2018 Istio Authors
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
	"os"
	"os/exec"
	"sort"
	"strings"

	"istio.io/istio/pkg/log"
)

var scope = log.RegisterScope("shell", "Shell execution scope", 0)

// Execute the given command.
func Execute(format string, args ...interface{}) (string, error) {
	s := fmt.Sprintf(format, args...)
	// TODO: escape handling
	parts := strings.Split(s, " ")

	for i := 0; i < len(parts); i++ {
		if parts[i] == "" {
			parts = append(parts[:i], parts[i+1:]...)
		}
	}

	return executeArgs(nil, parts[0], parts[1:]...)
}

// ExecuteEnv executes the given command, with the specified environment value overrides.
func ExecuteEnv(env map[string]string, format string, args ...interface{}) (string, error) {
	s := fmt.Sprintf(format, args...)
	// TODO: escape handling
	parts := strings.Split(s, " ")

	return executeArgs(toEnvironmentList(env), parts[0], parts[1:]...)
}

func executeArgs(env []string, name string, args ...string) (string, error) {

	if scope.DebugEnabled() {
		cmd := strings.Join(args, " ")
		cmd = name + " " + cmd
		scope.Debugf("Executing command: %s", cmd)
	}

	c := exec.Command(name, args...)
	c.Env = env
	b, err := c.CombinedOutput()

	if c.ProcessState.Success() {
		scope.Debugf("Command[%s] => %s", name, string(b))
	} else {
		scope.Debugf("Command[%s] => (FAILED) %s", name, string(b))
	}

	return string(b), err
}

func toEnvironmentList(m map[string]string) []string {
	result := os.Environ() // Start with the current environment values and let the caller override.

	for k, v := range m {
		result = append(result, fmt.Sprintf("%s=%s", k, v))
	}

	sort.Strings(result)
	return result
}
