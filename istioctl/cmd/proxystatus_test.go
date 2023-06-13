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
	"strings"
	"testing"

	"istio.io/istio/istioctl/pkg/cli"
)

func TestProxyStatus(t *testing.T) {
	cases := []execTestCase{
		{ // case 0
			args:           []string{},
			expectedString: "NAME     CLUSTER     CDS     LDS     EDS     RDS     ECDS     ISTIOD",
		},
		{ // case 2: supplying nonexistent pod name should result in error with flag
			args:          strings.Split("deployment/random-gibberish", " "),
			wantException: true,
		},
		{ // case 3: supplying nonexistent deployment name
			args:          strings.Split("deployment/random-gibberish.default", " "),
			wantException: true,
		},
		{ // case 4: supplying nonexistent deployment name in nonexistent namespace
			args:          strings.Split("deployment/random-gibberish.bogus", " "),
			wantException: true,
		},
		{ // case 5: supplying nonexistent pod name should result in error
			args:          strings.Split("random-gibberish-podname-61789237418234", " "),
			wantException: true,
		},
		{ // case 6: new --revision argument
			args:           strings.Split("--revision canary", " "),
			expectedString: "NAME     CLUSTER     CDS     LDS     EDS     RDS     ECDS     ISTIOD",
		},
		{ // case 7: supplying type that doesn't select pods should fail
			args:          strings.Split("serviceaccount/sleep", " "),
			wantException: true,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyExecTestOutput(t, statusCommand(cli.NewFakeContext(nil)), c)
		})
	}
}
