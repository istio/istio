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
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

var (
	host        string
	gitBranch   string
	gitRevision string
	user        string
	version     string

	// this is used for testing command output
	printFunc = fmt.Printf

	// Command prints version info to stdout
	Command = &cobra.Command{
		Run: func(*cobra.Command, []string) {
			// nolint: errcheck,gas
			printFunc(`Version: %v
GitRevision: %v
GitBranch: %v
User: %v@%v
Golang version: %v
`, version, gitRevision, gitBranch, user, host, runtime.Version())
		},
		Use:   "version",
		Short: "Display version information",
	}
)
