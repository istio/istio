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
	testKube "istio.io/istio/pkg/test/kube"
)

func TestDashboard(t *testing.T) {
	kubeClientWithRevision = mockExecClientDashboard
	kubeClient = mockEnvoyClientDashboard

	cases := []testCase{
		{ // case 0
			args:           strings.Split("dashboard", " "),
			expectedRegexp: regexp.MustCompile("Access to Istio web UIs"),
		},
		{ // case 1
			args:           strings.Split("dashboard invalid", " "),
			expectedRegexp: regexp.MustCompile(`unknown dashboard "invalid"`),
			wantException:  true,
		},
		{ // case 2
			args:           strings.Split("dashboard controlz", " "),
			expectedRegexp: regexp.MustCompile(".*Error: specify a pod or --selector"),
			wantException:  true,
		},
		{ // case 3
			args:           strings.Split("dashboard controlz pod-123456-7890", " "),
			expectedRegexp: regexp.MustCompile(".*MockClient doesn't implement port forwarding"),
			wantException:  true,
		},
		{ // case 4
			args:           strings.Split("dashboard envoy", " "),
			expectedRegexp: regexp.MustCompile(".*Error: specify a pod or --selector"),
			wantException:  true,
		},
		{ // case 5
			args:           strings.Split("dashboard envoy pod-123456-7890", " "),
			expectedRegexp: regexp.MustCompile(".*MockClient doesn't implement port forwarding"),
			wantException:  true,
		},
		{ // case 6
			args:           strings.Split("dashboard grafana", " "),
			expectedOutput: "Error: no Grafana pods found\n",
			wantException:  true,
		},
		{ // case 7
			args:           strings.Split("dashboard jaeger", " "),
			expectedOutput: "Error: no Jaeger pods found\n",
			wantException:  true,
		},
		{ // case 8
			args:           strings.Split("dashboard kiali", " "),
			expectedOutput: "Error: no Kiali pods found\n",
			wantException:  true,
		},
		{ // case 9
			args:           strings.Split("dashboard prometheus", " "),
			expectedOutput: "Error: no Prometheus pods found\n",
			wantException:  true,
		},
		{ // case 10
			args:           strings.Split("dashboard zipkin", " "),
			expectedOutput: "Error: no Zipkin pods found\n",
			wantException:  true,
		},
		{ // case 11
			args:           strings.Split("dashboard envoy --selector app=example", " "),
			expectedRegexp: regexp.MustCompile(".*no pods found"),
			wantException:  true,
		},
		{ // case 12
			args:           strings.Split("dashboard envoy --selector app=example pod-123456-7890", " "),
			expectedRegexp: regexp.MustCompile(".*Error: name cannot be provided when a selector is specified"),
			wantException:  true,
		},
		{ // case 13
			args:           strings.Split("dashboard controlz --selector app=example", " "),
			expectedRegexp: regexp.MustCompile(".*no pods found"),
			wantException:  true,
		},
		{ // case 14
			args:           strings.Split("dashboard controlz --selector app=example pod-123456-7890", " "),
			expectedRegexp: regexp.MustCompile(".*Error: name cannot be provided when a selector is specified"),
			wantException:  true,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyOutput(t, c)
		})
	}
}

func mockExecClientDashboard(_, _, _ string) (kube.ExtendedClient, error) {
	return testKube.MockClient{}, nil
}

func mockEnvoyClientDashboard(_, _ string) (kube.ExtendedClient, error) {
	return testKube.MockClient{}, nil
}
