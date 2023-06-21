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

package authz

import (
	"fmt"
	"strings"
	"testing"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/util/testutil"
)

func TestAuthz(t *testing.T) {
	cases := []testutil.TestCase{
		{
			Args:           []string{"-f fake.yaml"},
			ExpectedOutput: "Error: failed to get config dump from file  fake.yaml: open  fake.yaml: no such file or directory\n",
			WantException:  true,
		},
		{
			Args: []string{"-f", "testdata/configdump.yaml"},
			ExpectedOutput: `ACTION   AuthorizationPolicy         RULES
ALLOW    _anonymous_match_nothing_   1
ALLOW    httpbin.default             1
`,
		},
	}

	authzCmd := checkCmd(cli.NewFakeContext(&cli.NewFakeContextOption{}))
	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.Args, " ")), func(t *testing.T) {
			testutil.VerifyOutput(t, authzCmd, c)
		})
	}
}
