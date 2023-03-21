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

import "io"

// Dependencies is used as abstraction for the commands used from the operating system
type Dependencies interface {
	// RunOrFail runs a command and panics, if it fails. Stdin may be nil.
	RunOrFail(cmd string, stdin io.ReadSeeker, args ...string)
	// Run runs a command
	Run(cmd string, stdin io.ReadSeeker, args ...string) error
	// RunQuietlyAndIgnore runs a command quietly and ignores errors
	RunQuietlyAndIgnore(cmd string, stdin io.ReadSeeker, args ...string)
}
