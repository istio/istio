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

package testutil

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"istio.io/istio/pilot/test/util"
)

type TestCase struct {
	Args []string

	// Typically use one of the three
	ExpectedOutput string         // Expected constant output
	ExpectedRegexp *regexp.Regexp // Expected regexp output
	GoldenFilename string         // Expected output stored in golden file

	WantException bool
}

func VerifyOutput(t *testing.T, cmd *cobra.Command, c TestCase) {
	t.Helper()

	cmd.SetArgs(c.Args)

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceUsage = true

	fErr := cmd.Execute()
	output := out.String()

	if c.ExpectedOutput != "" && c.ExpectedOutput != output {
		t.Fatalf("Unexpected output for '%s %s'\n got: %q\nwant: %q", cmd.Name(),
			strings.Join(c.Args, " "), output, c.ExpectedOutput)
	}

	if c.ExpectedRegexp != nil && !c.ExpectedRegexp.MatchString(output) {
		t.Fatalf("Output didn't match for '%s %s'\n got %v\nwant: %v", cmd.Name(),
			strings.Join(c.Args, " "), output, c.ExpectedRegexp)
	}

	if c.GoldenFilename != "" {
		util.CompareContent(t, []byte(output), c.GoldenFilename)
	}

	if c.WantException {
		if fErr == nil {
			t.Fatalf("Wanted an exception for 'istioctl %s', didn't get one, output was %q",
				strings.Join(c.Args, " "), output)
		}
	} else {
		if fErr != nil {
			t.Fatalf("Unwanted exception for 'istioctl %s': %v", strings.Join(c.Args, " "), fErr)
		}
	}
}
