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

package dashboard

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/util/testutil"
)

func TestDashboard(t *testing.T) {
	cases := []testutil.TestCase{
		{ // case 0
			Args:           strings.Split("--browser=false", " "),
			ExpectedRegexp: regexp.MustCompile("Access to Istio web UIs"),
		},
		{ // case 1
			Args:           strings.Split("invalid --browser=false", " "),
			ExpectedRegexp: regexp.MustCompile(`unknown dashboard "invalid"`),
			WantException:  true,
		},
		{ // case 2
			Args:           strings.Split("controlz --browser=false", " "),
			ExpectedRegexp: regexp.MustCompile(".*Error: specify a pod, --selector, or --revision"),
			WantException:  true,
		},
		{ // case 3
			Args:           strings.Split("controlz --browser=false pod-123456-7890", " "),
			ExpectedRegexp: regexp.MustCompile(".*http://localhost:3456"),
			WantException:  false,
		},
		{ // case 4
			Args:           strings.Split("envoy --browser=false", " "),
			ExpectedRegexp: regexp.MustCompile(".*Error: specify a pod or --selector"),
			WantException:  true,
		},
		{ // case 5
			Args:           strings.Split("envoy --browser=false pod-123456-7890", " "),
			ExpectedRegexp: regexp.MustCompile("http://localhost:3456"),
			WantException:  false,
		},
		{ // case 6
			Args:           strings.Split("grafana --browser=false", " "),
			ExpectedOutput: "Error: no pods found with selector app.kubernetes.io/name=grafana\n",
			WantException:  true,
		},
		{ // case 7
			Args:           strings.Split("jaeger --browser=false", " "),
			ExpectedOutput: "Error: no pods found with selector app=jaeger\n",
			WantException:  true,
		},
		{ // case 8
			Args:           strings.Split("kiali --browser=false", " "),
			ExpectedOutput: "Error: no pods found with selector app=kiali\n",
			WantException:  true,
		},
		{ // case 9
			Args:           strings.Split("prometheus --browser=false", " "),
			ExpectedOutput: "Error: no pods found with selector app.kubernetes.io/name=prometheus\n",
			WantException:  true,
		},
		{ // case 10
			Args:           strings.Split("zipkin --browser=false", " "),
			ExpectedOutput: "Error: no pods found with selector app=zipkin\n",
			WantException:  true,
		},
		{ // case 11
			Args:           strings.Split("envoy --selector app=example --browser=false", " "),
			ExpectedRegexp: regexp.MustCompile(".*no pods found"),
			WantException:  true,
		},
		{ // case 12
			Args:           strings.Split("envoy --browser=false --selector app=example pod-123456-7890", " "),
			ExpectedRegexp: regexp.MustCompile(".*Error: name cannot be provided when a selector is specified"),
			WantException:  true,
		},
		{ // case 13
			Args:           strings.Split("--browser=false controlz --selector app=example", " "),
			ExpectedRegexp: regexp.MustCompile(".*no pods found"),
			WantException:  true,
		},
		{ // case 14
			Args:           strings.Split("--browser=false controlz --selector app=example pod-123456-7890", " "),
			ExpectedRegexp: regexp.MustCompile(".*Error: only one of name, --selector, or --revision can be specified"),
			WantException:  true,
		},
		{ // case 16
			Args:           strings.Split("controlz --browser=false pod-123456-7890", " "),
			ExpectedRegexp: regexp.MustCompile(".*http://localhost:3456"),
			WantException:  false,
		},
		{ // case 17
			Args:           strings.Split("envoy --browser=false pod-123456-7890", " "),
			ExpectedRegexp: regexp.MustCompile("http://localhost:3456"),
			WantException:  false,
		},
	}

	dbCmd := Dashboard(cli.NewFakeContext(&cli.NewFakeContextOption{
		Namespace: "istio-system",
	}))
	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.Args, " ")), func(t *testing.T) {
			testutil.VerifyOutput(t, dbCmd, c)
			cleanupTestCase()
		})
	}
}

func cleanupTestCase() {
	labelSelector = ""
}
