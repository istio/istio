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
	"strings"
)

// StdoutStubDependencies implementation of interface Dependencies, which is used for testing
type StdoutStubDependencies struct {
}

// RunOrFail runs a command and panics, if it fails
func (s *StdoutStubDependencies) RunOrFail(cmd string, args ...string) {
	fmt.Printf("%s %s\n", cmd, strings.Join(args, " "))
}

// Run runs a command
func (s *StdoutStubDependencies) Run(cmd string, args ...string) error {
	fmt.Printf("%s %s\n", cmd, strings.Join(args, " "))
	return nil
}

// RunQuietlyAndIgnore runs a command quietly and ignores errors
func (s *StdoutStubDependencies) RunQuietlyAndIgnore(cmd string, args ...string) {
	fmt.Printf("%s %s\n", cmd, strings.Join(args, " "))
}
