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
	"testing"

	istioctlutil "istio.io/istio/istioctl/pkg/util"
)

func TestBadParse(t *testing.T) {
	// unknown flags should be a command parse
	rootCmd := GetRootCmd([]string{"--unknown-flag"})
	fErr := rootCmd.Execute()

	switch fErr.(type) {
	case istioctlutil.CommandParseError:
		// do nothing
	default:
		t.Errorf("Expected a CommandParseError, but got %q.", fErr)
	}

	// we should propagate to subcommands
	rootCmd = GetRootCmd([]string{"x", "analyze", "--unknown-flag"})
	fErr = rootCmd.Execute()

	switch fErr.(type) {
	case istioctlutil.CommandParseError:
		// do nothing
	default:
		t.Errorf("Expected a CommandParseError, but got %q.", fErr)
	}

	// all of the subcommands
	rootCmd = GetRootCmd([]string{"authz", "tls-check", "--unknown-flag"})
	fErr = rootCmd.Execute()

	switch fErr.(type) {
	case istioctlutil.CommandParseError:
		// do nothing
	default:
		t.Errorf("Expected a CommandParseError, but got %q.", fErr)
	}
}
