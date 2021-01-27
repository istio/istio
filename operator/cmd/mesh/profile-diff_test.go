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

package mesh

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"istio.io/istio/operator/pkg/util/clog"
)

type profileDiffTestcase struct {
	args           string
	shouldFail     bool
	expectedString string // String output is expected to contain
	notExpected    string // String the output must NOT contain
}

func TestProfileDiff(t *testing.T) {
	cases := []profileDiffTestcase{
		{
			args:       "profile diff demo default --unknown-flag",
			shouldFail: true,
		},
		{
			args:       "profile diff demo",
			shouldFail: true,
		},
		{
			args:       "profile diff",
			shouldFail: true,
		},
		{
			args:       fmt.Sprintf("profile diff default unknown-profile --manifests %s", snapshotCharts),
			shouldFail: true,
		},
		{
			args:           fmt.Sprintf("profile diff default default --manifests %s", snapshotCharts),
			expectedString: "Profiles are identical",
		},
		{
			args:           fmt.Sprintf("profile diff demo demo --manifests %s", snapshotCharts),
			expectedString: "Profiles are identical",
		},
		{
			args:           fmt.Sprintf("profile diff openshift openshift --manifests %s", snapshotCharts),
			expectedString: "Profiles are identical",
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %q", i, c.args), func(t *testing.T) {
			output, fErr := runCommand(c.args)
			verifyProfileDiffCommandCaseOutput(t, c, output, fErr)
		})
	}
}

func TestProfileDiffDirect(t *testing.T) {
	cases := []profileDiffTestcase{
		{
			// We won't be parsing with Cobra, but this is the command equivalent
			args: "profile diff default openshift",
			// This is just one of the many differences
			expectedString: "+      cniBinDir: /var/lib/cni/bin",
			// The profile doesn't change istiocoredns, so we shouldn't see this in the diff
			notExpected: "-    istiocoredns:",
			// 'profile diff' "fails" so that the error level `$?` is 1, not 0, if there is a diff
			shouldFail: false,
		},
	}

	l := clog.NewConsoleLogger(os.Stdout, os.Stderr, nil)
	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %q", i, c.args), func(t *testing.T) {
			args := strings.Split(c.args, " ")
			setFlag := fmt.Sprintf("installPackagePath=%s", snapshotCharts)
			var by bytes.Buffer
			_, err := profileDiffInternal(args[len(args)-2], args[len(args)-1], []string{setFlag}, bufio.NewWriter(&by), l)
			verifyProfileDiffCommandCaseOutput(t, c, by.String(), err)
		})
	}
}

func verifyProfileDiffCommandCaseOutput(t *testing.T, c profileDiffTestcase, output string, fErr error) {
	t.Helper()

	if c.expectedString != "" && !strings.Contains(output, c.expectedString) {
		t.Fatalf("Output didn't match for 'istioctl %s'\n got %v\nwant: %v", c.args, output, c.expectedString)
	}

	if c.notExpected != "" && strings.Contains(output, c.notExpected) {
		t.Fatalf("Output didn't match for 'istioctl %s'\n got %v\nDON'T want: %v", c.args, output, c.expectedString)
	}

	if c.shouldFail {
		if fErr == nil {
			t.Fatalf("Command should have failed for 'istioctl %s', didn't get one, output was %q",
				c.args, output)
		}
	} else {
		if fErr != nil {
			t.Fatalf("Command should not have failed for 'istioctl %s': %v", c.args, fErr)
		}
	}
}
