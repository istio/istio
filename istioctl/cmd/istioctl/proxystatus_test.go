// Copyright 2017 Istio Authors
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
	"fmt"
	"strings"
	"testing"

	"istio.io/istio/pilot/test/util"
)

func TestProxyStatus(t *testing.T) {
	cannedConfig := map[string][]byte{
		"details-v1-5b7f94f9bc-wp5tb": util.ReadFile("../../pkg/writer/compare/testdata/envoyconfigdump.json", t),
	}
	cases := []execTestCase{
		{ // case 0
			args:           strings.Split("proxy-status", " "),
			expectedString: "NAME     CDS     LDS     EDS     RDS     PILOT",
		},
		{ // case 1 short name "ps"
			args:           strings.Split("ps", " "),
			expectedString: "NAME     CDS     LDS     EDS     RDS     PILOT",
		},
		{ // case 2  "proxy-status podName.namespace"
			execClientConfig: cannedConfig,
			args:             strings.Split("proxy-status details-v1-5b7f94f9bc-wp5tb.default", " "),
			expectedOutput: `Clusters Match
Listeners Match
Routes Match
`,
		},
		{ // case 3  "proxy-status podName -n namespace"
			execClientConfig: cannedConfig,
			args:             strings.Split("proxy-status details-v1-5b7f94f9bc-wp5tb -n default", " "),
			expectedOutput: `Clusters Match
Listeners Match
Routes Match
`,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyExecTestOutput(t, c)
		})
	}
}
