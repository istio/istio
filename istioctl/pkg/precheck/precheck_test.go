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

package precheck

import (
	"fmt"
	"reflect"
	"testing"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/pkg/config/analysis/diag"
)

type execTestCase struct {
	args     []string
	revision string
	noIstiod bool

	// Typically use one of the three
	expectedOutput string // Expected constant output
	expectedString string // String output is expected to contain

	wantException bool
}

func Test_checkFromVersion(t *testing.T) {
	cases := []struct {
		revision       string
		version        string
		expectedOutput diag.Messages
		expectedError  error
	}{
		{
			revision:       "canary",
			version:        "1.21",
			expectedOutput: diag.Messages{
				// Add expected messages here
			},
			expectedError: nil,
		},
		// Add more test cases here
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("revision=%s, version=%s", c.revision, c.version), func(t *testing.T) {
			ctx := cli.NewFakeContext(&cli.NewFakeContextOption{
				IstioNamespace: "istio-system",
			})
			output, err := checkFromVersion(ctx, c.revision, c.version)

			if !reflect.DeepEqual(output, c.expectedOutput) {
				t.Errorf("Unexpected output for revision=%s, version=%s\n got: %v\nwant: %v", c.revision, c.version, output, c.expectedOutput)
			}

			if err != c.expectedError {
				t.Errorf("Unexpected error for revision=%s, version=%s\n got: %v\nwant: %v", c.revision, c.version, err, c.expectedError)
			}
		})
	}
}
