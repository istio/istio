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

package exec

import (
	"log"
	"path/filepath"
)

// dispatchScriptPath is the path of the script that can dispatch
// bash functions to `asm-lib.sh`
const dispatchScriptPath = "prow/asm/tester/scripts/dispatcher.sh"

// Dispatch calls a single bash function from a bash library. We pass the arguments to the function
// as arguments to the Dispatch command.
func Dispatch(repoRoot, bashFunc string, args []string, option ...Option) error {
	log.Printf("🔥 dispatching bash function: %s", bashFunc)
	// order matters here, make sure command name is the first arg into the dispatcher
	// script and that command's arguments come after
	option = append([]Option{
		WithAdditionalArgs([]string{bashFunc}),
		WithAdditionalArgs(args),
		WithWorkingDir(repoRoot),
	}, option...)
	return Run(filepath.Join(repoRoot, dispatchScriptPath), option...)
}

func DispatchWithOutput(repoRoot, bashFunc string, args []string, option ...Option) (string, error) {
	log.Printf("🔥 dispatching bash function: %s", bashFunc)
	option = append([]Option{
		WithAdditionalArgs([]string{bashFunc}),
		WithAdditionalArgs(args),
		WithWorkingDir(repoRoot),
	}, option...)
	return RunWithOutput(filepath.Join(repoRoot, dispatchScriptPath), option...)
}
