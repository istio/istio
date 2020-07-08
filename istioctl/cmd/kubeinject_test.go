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

func TestKubeInject(t *testing.T) {
	cases := []testCase{
		{ // case 0
			args:           strings.Split("kube-inject", " "),
			expectedRegexp: regexp.MustCompile(`filename not specified \(see --filename or -f\)`),
			wantException:  true,
		},
		{ // case 1
			args:           strings.Split("kube-inject -f missing.yaml", " "),
			expectedRegexp: regexp.MustCompile(`open missing.yaml: no such file or directory`),
			wantException:  true,
		},
		{ // case 2
			args: strings.Split(
				"kube-inject --meshConfigFile testdata/mesh-config.yaml"+
					" --injectConfigFile testdata/inject-config.yaml -f testdata/deployment/hello.yaml"+
					" --valuesFile testdata/inject-values.yaml",
				" "),
			goldenFilename: "testdata/deployment/hello.yaml.injected",
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyOutput(t, c)
		})
	}
}
