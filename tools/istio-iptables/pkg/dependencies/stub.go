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
	"io"
	"os"
	"strings"

	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

var DryRunFilePath = env.Register("DRY_RUN_FILE_PATH", "", "If provided, StdoutStubDependencies will write the input from stdin to the given file.")

// StdoutStubDependencies implementation of interface Dependencies, which is used for testing
type StdoutStubDependencies struct{}

// RunOrFail runs a command and panics, if it fails
func (s *StdoutStubDependencies) RunOrFail(cmd string, stdin io.ReadSeeker, args ...string) {
	log.Infof("%s %s", cmd, strings.Join(args, " "))

	path := DryRunFilePath.Get()
	if path != "" {
		// Print the input into the given output file.
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			panic(fmt.Errorf("unable to open dry run output file %v: %v", path, err))
		}

		defer f.Close()
		if stdin != nil {
			if _, err = io.Copy(f, stdin); err != nil {
				panic(fmt.Errorf("unable to write dry run output file: %v", err))
			}
		}
	}
}

// Run runs a command
func (s *StdoutStubDependencies) Run(cmd string, stdin io.ReadSeeker, args ...string) error {
	s.RunOrFail(cmd, stdin, args...)
	return nil
}

// RunQuietlyAndIgnore runs a command quietly and ignores errors
func (s *StdoutStubDependencies) RunQuietlyAndIgnore(cmd string, stdin io.ReadSeeker, args ...string) {
	s.RunOrFail(cmd, stdin, args...)
}
