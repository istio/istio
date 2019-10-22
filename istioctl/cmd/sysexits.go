// Copyright 2019 Istio Authors
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

package cmd

import "strings"

// Values should try to use sendmail-style values as in <sysexits.h>
// See e.g. https://man.openbsd.org/sysexits.3
// or `less /usr/includes/sysexits.h` if you're on Linux
const (
	ExitUnknownError   = 1 // for compatibility with existing exit code
	ExitIncorrectUsage = 64
	// below here are non-zero exit codes that don't indicate an error with istioctl itself
	ExitAnalyzeFoundIssues = 79 // istioctl analyze found issues, for CI/CD
)

func GetExitCode(e error) int {
	if strings.Contains(e.Error(), "unknown command") {
		e = CommandParseError{e}
	}

	switch e.(type) {
	case CommandParseError:
		return ExitIncorrectUsage
	case FoundAnalyzeIssuesError:
		return ExitAnalyzeFoundIssues
	default:
		return ExitUnknownError
	}
}
