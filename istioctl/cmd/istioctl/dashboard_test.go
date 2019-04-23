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
	"fmt"
	"regexp"
	"strings"
	"testing"

	"istio.io/istio/istioctl/pkg/kubernetes"
)

func TestDashboard(t *testing.T) {
	clientExecFactory = mockExecClientDashboard

	cases := []testCase{
		{ // case 0
			args:           strings.Split("experimental dashboard", " "),
			expectedRegexp: regexp.MustCompile("Access to Istio web UIs"),
		},
		{ // case 1
			args:           strings.Split("experimental dashboard invalid", " "),
			expectedRegexp: regexp.MustCompile(`unknown dashboard "invalid"`),
			wantException:  true,
		},
		{ // case 2
			args:           strings.Split("experimental dashboard controlz", " "),
			expectedRegexp: regexp.MustCompile(".*Error: specify a pod"),
			wantException:  true,
		},
		{ // case 3
			args:           strings.Split("experimental dashboard controlz pod-123456-7890", " "),
			expectedRegexp: regexp.MustCompile(".*mock k8s does not forward"),
			wantException:  true,
		},
		{ // case 4
			args:           strings.Split("experimental dashboard envoy", " "),
			expectedRegexp: regexp.MustCompile(".*Error: specify a pod"),
			wantException:  true,
		},
		{ // case 5
			args:           strings.Split("experimental dashboard envoy pod-123456-7890", " "),
			expectedRegexp: regexp.MustCompile(".*mock k8s does not forward"),
			wantException:  true,
		},
		{ // case 6
			args:           strings.Split("experimental dashboard grafana", " "),
			expectedOutput: "Error: no Grafana pods found\n",
			wantException:  true,
		},
		{ // case 7
			args:           strings.Split("experimental dashboard jaeger", " "),
			expectedOutput: "Error: no Jaeger pods found\n",
			wantException:  true,
		},
		{ // case 8
			args:           strings.Split("experimental dashboard kiali", " "),
			expectedOutput: "Error: no Kiali pods found\n",
			wantException:  true,
		},
		{ // case 9
			args:           strings.Split("experimental dashboard prometheus", " "),
			expectedOutput: "Error: no Prometheus pods found\n",
			wantException:  true,
		},
		{ // case 10
			args:           strings.Split("experimental dashboard zipkin", " "),
			expectedOutput: "Error: no Zipkin pods found\n",
			wantException:  true,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyOutput(t, c)
		})
	}
}

func mockExecClientDashboard(_, _ string) (kubernetes.ExecClient, error) {
	return &mockExecConfig{}, nil
}
