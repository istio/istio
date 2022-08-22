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
	"fmt"
	"regexp"
	"strings"
	"testing"
)

func TestConfigList(t *testing.T) {
	cases := []testCase{
		{ // case 0
			args:           strings.Split("experimental config get istioNamespace", " "),
			expectedRegexp: regexp.MustCompile("Examples:\n  # list configuration parameters\n  istioctl config list"),
			wantException:  false,
		},
		{ // case 1
			args: strings.Split("experimental config list", " "),
			expectedOutput: `FLAG                    VALUE            FROM
authority                                default
cert-dir                                 default
insecure                                 default
istioNamespace          istio-system     default
plaintext                                default
prefer-experimental                      default
xds-address                              default
xds-port                15012            default
`,
			wantException: false,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyOutput(t, c)
		})
	}
}
