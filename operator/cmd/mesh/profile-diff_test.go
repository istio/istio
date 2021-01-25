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
	"fmt"
	"strings"
	"testing"
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
		{
			args: fmt.Sprintf("profile diff default openshift --manifests %s", snapshotCharts),
			// This is just one of the many differences
			expectedString: "+      cniBinDir: /var/lib/cni/bin",
			// The profile doesn't change istiocoredns, so we shouldn't see this in the diff
			notExpected: "-    istiocoredns:",
			// 'profile diff' "fails" so that the error level `$?` is 1, not 0, if there is a diff
			shouldFail: true,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %q", i, c.args), func(t *testing.T) {
			verifyProfileDiffCommandCaseOutput(t, c)
		})
	}
}

func verifyProfileDiffCommandCaseOutput(t *testing.T, c profileDiffTestcase) {
	t.Helper()

	output, fErr := runCommand(c.args)
	// Note that 'output' doesn't capture stderr

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
