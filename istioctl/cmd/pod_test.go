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

package cmd

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"istio.io/istio/istioctl/pkg/kubernetes"
)

func TestPod(t *testing.T) {
	clientExecFactory = mockExecClientDashboard

	cases := []testCase{
		{ // case 0
			args:           strings.Split("experimental pod", " "),
			expectedRegexp: regexp.MustCompile(".*Error: specify a pod"),
			wantException:  true,
		},
		{ // case 1
			args:           strings.Split("experimental pod pod-123456-7890", " "),
			expectedRegexp: regexp.MustCompile(".*mock k8s does not forward"),
			wantException:  true,
		},
		{ // case 2
			args:           strings.Split("experimental pod pod-123456-7890 --log_level=debug", " "),
			expectedRegexp: regexp.MustCompile(".*mock k8s does not forward"),
			wantException:  true,
		},
		{ // case 3
			args:           strings.Split("experimental pod pod-123456-7890 --log_level=invalid", " "),
			expectedRegexp: regexp.MustCompile(".*Error: unknown log level"),
			wantException:  true,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyOutput(t, c)
		})
	}
}

func mockExecClientPod(_, _ string) (kubernetes.ExecClient, error) {
	return &mockExecConfig{}, nil
}
