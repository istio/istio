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

package dependencies

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// RealDependencies implementation of interface Dependencies, which is used in production
type RealDependencies struct {
}

func (r *RealDependencies) execute(cmd string, redirectStdout bool, args ...string) error {
	fmt.Printf("%s %s\n", cmd, strings.Join(args, " "))
	externalCommand := exec.Command(cmd, args...)
	externalCommand.Stdout = os.Stdout
	//TODO Check naming and redirection logic
	if !redirectStdout {
		externalCommand.Stderr = os.Stderr
	}
	return externalCommand.Run()
}

// RunOrFail runs a command and panics, if it fails
func (r *RealDependencies) RunOrFail(cmd string, args ...string) {
	err := r.execute(cmd, false, args...)

	if err != nil {
		panic(err)
	}
}

// Run runs a command
func (r *RealDependencies) Run(cmd string, args ...string) error {
	return r.execute(cmd, false, args...)
}

// RunQuietlyAndIgnore runs a command quietly and ignores errors
func (r *RealDependencies) RunQuietlyAndIgnore(cmd string, args ...string) {
	_ = r.execute(cmd, true, args...)
}
