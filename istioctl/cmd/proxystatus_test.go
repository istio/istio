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
)

func TestProxyStatus(t *testing.T) {
	cases := []execTestCase{
		{ // case 0
			args:           strings.Split("proxy-status", " "),
			expectedString: "NAME     CDS     LDS     EDS     RDS     ISTIOD",
		},
		{ // case 1 short name "ps"
			args:           strings.Split("ps", " "),
			expectedString: "NAME     CDS     LDS     EDS     RDS     ISTIOD",
		},
		{ // case 5: supplying nonexistent pod name should result in error with --sds flag
			args:          strings.Split("proxy-status random-gibberish-podname-61789237418234", " "),
			wantException: true,
		},
		{ // case 6: new --revision argument
			args:           strings.Split("proxy-status --revision canary", " "),
			expectedString: "NAME     CDS     LDS     EDS     RDS     ISTIOD",
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyExecTestOutput(t, c)
		})
	}
}
