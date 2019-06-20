// Copyright 2018 Istio Authors
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

	"istio.io/istio/pilot/pkg/model"
)

func TestVersion(t *testing.T) {
	cases := []testCase{
		{ // case 0 client-side only, normal output
			configs: []model.Config{},
			args:    strings.Split("version --remote=false --short=false", " "),
			// ignore the output, all output checks are now in istio/pkg
		},
		{ // case 1 bogus arg
			configs:        []model.Config{},
			args:           strings.Split("version --typo", " "),
			expectedOutput: "Error: unknown flag: --typo\n",
			wantException:  true,
		},
		{ // case 2 bogus output arg
			configs:        []model.Config{},
			args:           strings.Split("version --output xyz", " "),
			expectedOutput: "Error: --output must be 'yaml' or 'json'\n",
			wantException:  true,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyOutput(t, c)
		})
	}
}
