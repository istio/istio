// Copyright Istio Authors.
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

	"istio.io/istio/pkg/kube"
)

func TestDashboard(t *testing.T) {
	kubeClientWithRevision = mockExecClientDashboard
	kubeClient = mockEnvoyClientDashboard

	cases := []testCase{
		{ // case 0
			args:           strings.Split("dashboard --browser=false", " "),
			expectedRegexp: regexp.MustCompile("Access to Istio web UIs"),
		},
		{ // case 1
			args:           strings.Split("dashboard invalid --browser=false", " "),
			expectedRegexp: regexp.MustCompile(`unknown dashboard "invalid"`),
			wantException:  true,
		},
		{ // case 2
			args:           strings.Split("dashboard controlz --browser=false", " "),
			expectedRegexp: regexp.MustCompile(".*Error: specify a pod or --selector"),
			wantException:  true,
		},
		{ // case 3
			args:           strings.Split("dashboard controlz --browser=false pod-123456-7890", " "),
			expectedRegexp: regexp.MustCompile(".*http://localhost:3456"),
			wantException:  false,
		},
		{ // case 4
			args:           strings.Split("dashboard envoy --browser=false", " "),
			expectedRegexp: regexp.MustCompile(".*Error: specify a pod or --selector"),
			wantException:  true,
		},
		{ // case 5
			args:           strings.Split("dashboard envoy --browser=false pod-123456-7890", " "),
			expectedRegexp: regexp.MustCompile("http://localhost:3456"),
			wantException:  false,
		},
		{ // case 6
			args:           strings.Split("dashboard grafana --browser=false", " "),
			expectedOutput: "Error: no Grafana pods found\n",
			wantException:  true,
		},
		{ // case 7
			args:           strings.Split("dashboard jaeger --browser=false", " "),
			expectedOutput: "Error: no Jaeger pods found\n",
			wantException:  true,
		},
		{ // case 8
			args:           strings.Split("dashboard kiali --browser=false", " "),
			expectedOutput: "Error: no Kiali pods found\n",
			wantException:  true,
		},
		{ // case 9
			args:           strings.Split("dashboard prometheus --browser=false", " "),
			expectedOutput: "Error: no Prometheus pods found\n",
			wantException:  true,
		},
		{ // case 10
			args:           strings.Split("dashboard zipkin --browser=false", " "),
			expectedOutput: "Error: no Zipkin pods found\n",
			wantException:  true,
		},
		{ // case 11
			args:           strings.Split("dashboard envoy --selector app=example --browser=false", " "),
			expectedRegexp: regexp.MustCompile(".*no pods found"),
			wantException:  true,
		},
		{ // case 12
			args:           strings.Split("dashboard envoy --browser=false --selector app=example pod-123456-7890", " "),
			expectedRegexp: regexp.MustCompile(".*Error: name cannot be provided when a selector is specified"),
			wantException:  true,
		},
		{ // case 13
			args:           strings.Split("dashboard --browser=false controlz --selector app=example", " "),
			expectedRegexp: regexp.MustCompile(".*no pods found"),
			wantException:  true,
		},
		{ // case 14
			args:           strings.Split("dashboard --browser=false controlz --selector app=example pod-123456-7890", " "),
			expectedRegexp: regexp.MustCompile(".*Error: name cannot be provided when a selector is specified"),
			wantException:  true,
		},
		{ // case 15
			args:           strings.Split("-n test dashboard", " "),
			expectedRegexp: regexp.MustCompile("Access to Istio web UIs"),
		},
		{ // case 16
			args:           strings.Split("dashboard controlz --browser=false pod-123456-7890 -n istio-system", " "),
			expectedRegexp: regexp.MustCompile(".*http://localhost:3456"),
			wantException:  false,
		},
		{ // case 17
			args:           strings.Split("dashboard envoy --browser=false pod-123456-7890 -n istio-system", " "),
			expectedRegexp: regexp.MustCompile("http://localhost:3456"),
			wantException:  false,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyOutput(t, c)
		})
	}
}

func mockExecClientDashboard(_, _, _ string) (kube.CLIClient, error) {
	return kube.MockClient{}, nil
}

func mockEnvoyClientDashboard(_, _ string) (kube.CLIClient, error) {
	return kube.MockClient{}, nil
}
