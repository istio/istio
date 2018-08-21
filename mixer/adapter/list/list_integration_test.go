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

package list

import (
	"testing"

	adapter_integration "istio.io/istio/mixer/pkg/adapter/test"
)

const (
	h1OverrideSrc1Src2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: listchecker
metadata:
  name: staticversion
  namespace: istio-system
spec:
  overrides: ["src1", "src2"]
  blacklist: false
`
	i1ValSrcNameAttr = `
apiVersion: "config.istio.io/v1alpha2"
kind: listentry
metadata:
  name: appversion
  namespace: istio-system
spec:
  value: source.name | ""
`
	r1H1I1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: checkwl
  namespace: istio-system
spec:
  actions:
  - handler: staticversion.listchecker
    instances:
    - appversion.listentry
`
)

func TestReport(t *testing.T) {
	adapter_integration.RunTest(
		t,
		GetInfo,
		adapter_integration.Scenario{
			ParallelCalls: []adapter_integration.Call{
				{
					CallKind: adapter_integration.CHECK,
					Attrs:    map[string]interface{}{"source.name": "src1"},
				},
				{
					CallKind: adapter_integration.CHECK,
				},
			},
			Configs: []string{
				h1OverrideSrc1Src2,
				r1H1I1,
				i1ValSrcNameAttr,
			},
			Want: `{
            "AdapterState": null,
            "Returns": [
             {
              "Check": {
                "status": {},
                "valid_duration": 300000000000,
                "valid_use_count": 10000
              }
             },
             {
              "Check": {
               "status": {
                "code": 5,
                "message": "staticversion.listchecker.istio-system: is not whitelisted"
               },
               "valid_duration": 300000000000,
               "valid_use_count": 10000
              }
             }
            ]
            }`,
		},
	)
}
