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

package cmd

import (
	"strings"

	"istio.io/istio/istioctl/pkg/analyze"
	"istio.io/istio/istioctl/pkg/util"
)

// Values should try to use sendmail-style values as in <sysexits.h>
// See e.g. https://man.openbsd.org/sysexits.3
// or `less /usr/includes/sysexits.h` if you're on Linux
//
// Picking the right range is tricky--there are a lot of reserved ones (see
// https://www.tldp.org/LDP/abs/html/exitcodes.html#EXITCODESREF) and then some
// used by convention (see sysexits).
//
// The intention here is to use 64-78 in a way that matches the attempt in
// sysexits to signify some error running istioctl, and use 79-125 as custom
// error codes for other info that we'd like to use to pass info on.
const (
	ExitUnknownError   = 1 // for compatibility with existing exit code
	ExitIncorrectUsage = 64
	ExitDataError      = 65 // some format error with input data

	// below here are non-zero exit codes that don't indicate an error with istioctl itself
	ExitAnalyzerFoundIssues = 79 // istioctl analyze found issues, for CI/CD
)

func GetExitCode(e error) int {
	if strings.Contains(e.Error(), "unknown command") {
		e = util.CommandParseError{Err: e}
	}

	switch e.(type) {
	case util.CommandParseError:
		return ExitIncorrectUsage
	case analyze.FileParseError:
		return ExitDataError
	case analyze.AnalyzerFoundIssuesError:
		return ExitAnalyzerFoundIssues
	default:
		return ExitUnknownError
	}
}
