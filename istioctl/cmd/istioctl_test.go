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
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"istio.io/istio/pilot/test/util"
)

type testCase struct {
	args []string

	// Typically use one of the three
	expectedOutput string         // Expected constant output
	expectedRegexp *regexp.Regexp // Expected regexp output
	goldenFilename string         // Expected output stored in golden file

	wantException bool
}

func TestBadParse(t *testing.T) {
	// unknown flags should be a command parse
	rootCmd := GetRootCmd([]string{"--unknown-flag"})
	fErr := rootCmd.Execute()

	switch fErr.(type) {
	case CommandParseError:
		// do nothing
	default:
		t.Errorf("Expected a CommandParseError, but got %q.", fErr)
	}

	// we should propagate to subcommands
	rootCmd = GetRootCmd([]string{"x", "analyze", "--unknown-flag"})
	fErr = rootCmd.Execute()

	switch fErr.(type) {
	case CommandParseError:
		// do nothing
	default:
		t.Errorf("Expected a CommandParseError, but got %q.", fErr)
	}

	// all of the subcommands
	rootCmd = GetRootCmd([]string{"authz", "tls-check", "--unknown-flag"})
	fErr = rootCmd.Execute()

	switch fErr.(type) {
	case CommandParseError:
		// do nothing
	default:
		t.Errorf("Expected a CommandParseError, but got %q.", fErr)
	}
}

func verifyOutput(t *testing.T, cmd *cobra.Command, c testCase) {
	t.Helper()

	cmd.SetArgs(c.args)

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceUsage = true

	fErr := cmd.Execute()
	output := out.String()

	if c.expectedOutput != "" && c.expectedOutput != output {
		t.Fatalf("Unexpected output for 'istioctl %s'\n got: %q\nwant: %q",
			strings.Join(c.args, " "), output, c.expectedOutput)
	}

	if c.expectedRegexp != nil && !c.expectedRegexp.MatchString(output) {
		t.Fatalf("Output didn't match for 'istioctl %s'\n got %v\nwant: %v",
			strings.Join(c.args, " "), output, c.expectedRegexp)
	}

	if c.goldenFilename != "" {
		util.CompareContent(t, []byte(output), c.goldenFilename)
	}

	if c.wantException {
		if fErr == nil {
			t.Fatalf("Wanted an exception for 'istioctl %s', didn't get one, output was %q",
				strings.Join(c.args, " "), output)
		}
	} else {
		if fErr != nil {
			t.Fatalf("Unwanted exception for 'istioctl %s': %v", strings.Join(c.args, " "), fErr)
		}
	}
}
