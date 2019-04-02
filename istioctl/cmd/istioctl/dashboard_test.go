// Copyright 2019 Istio Authors.
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

package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

type dashboardTestCase struct {
	args           []string
	expectedString string // String output is expected to contain
	wantException  bool
}

func TestDashboard(t *testing.T) {

	cases := []dashboardTestCase{
		{ // case 0
			args:           strings.Split("experimental dashboard", " "),
			expectedString: "Access to Istio web UIs",
		},
		{ // case 1
			args:           strings.Split("experimental dashboard invalid", " "),
			expectedString: `unknown dashboard "invalid"`,
			wantException:  true,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			var out bytes.Buffer
			rootCmd.SetOutput(&out)
			rootCmd.SetArgs(c.args)

			fErr := rootCmd.Execute()
			output := out.String()

			if c.expectedString != "" && !strings.Contains(output, c.expectedString) {
				t.Fatalf("Output didn't match for 'istioctl %s'\n got %v\nwant: %v", strings.Join(c.args, " "), output, c.expectedString)
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
		})
	}
}
