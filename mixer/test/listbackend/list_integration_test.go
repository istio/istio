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

package listbackend

import (
	"fmt"
	"io/ioutil"
	"testing"

	adapter_integration "istio.io/istio/mixer/pkg/adapter/test"
)

const (
	h1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: handler
metadata:
  name: h1
  namespace: istio-system
spec:
  adapter: listbackend-nosession
  params:
    overrides: ["mysource", "mysource2"]  # overrides provide a static list
    blacklist: false
  connection:
    address: "%s"
---
`
	i1list = `
apiVersion: "config.istio.io/v1alpha2"
kind: instance
metadata:
  name: i1list
  namespace: istio-system
spec:
  template: listentry
  params:
    value: source.name | "defaultstr"
---
`

	r1H1i1list = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r1
  namespace: istio-system
spec:
  actions:
  - handler: h1.istio-system
    instances:
    - i1list
---
`
)

func TestNoSessionBackend(t *testing.T) {

	testdata := []struct {
		name  string
		calls []adapter_integration.Call
		want  string
	}{
		{
			name: "single report call with attributes",
			calls: []adapter_integration.Call{
				{
					CallKind: adapter_integration.CHECK,
					Attrs:    map[string]interface{}{"source.name": "mysource"},
				},
			},
			want: `
    		{
    		 "AdapterState": null,
    		 "Returns": [
    		  {
    		   "Check": {
    		    "Status": {},
    		    "ValidDuration": 60000000000,
    		    "ValidUseCount": 10000
    		   },
    		   "Quota": null,
    		   "Error": null
    		  }
    		 ]
    		}
`,
		},
	}

	adptCfgBytes, err := ioutil.ReadFile("nosession.yaml")
	if err != nil {
		t.Fatalf("cannot open file: %v", err)
	}

	for _, td := range testdata {
		t.Run(td.name, func(tt *testing.T) {
			adapter_integration.RunTest(
				tt,
				nil,
				adapter_integration.Scenario{
					Setup: func() (interface{}, error) {
						var s Server
						var err error
						if s, err = NewNoSessionServer(""); err != nil {
							return nil, err
						}
						s.Run()
						return s, nil
					},
					Teardown: func(ctx interface{}) {
						_ = ctx.(Server).Close()
					},
					GetState: func(ctx interface{}) (interface{}, error) {
						return nil, nil
					},
					ParallelCalls: td.calls,
					GetConfig: func(ctx interface{}) ([]string, error) {
						s := ctx.(Server)
						return []string{
							// CRs for built-in templates are automatically added by the integration test framework.
							string(adptCfgBytes),
							fmt.Sprintf(h1, s.Addr().String()),
							i1list,
							r1H1i1list,
						}, nil
					},
					Want: td.want,
				},
			)
		})
	}
}
