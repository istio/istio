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

package list

import (
	"net"
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
	h1BlacklistOverrideSrc1Src2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: listchecker
metadata:
  name: staticversion
  namespace: istio-system
spec:
  overrides: ["src1", "src2"]
  blacklist: true
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
	h1OverrideIP = `
apiVersion: "config.istio.io/v1alpha2"
kind: listchecker
metadata:
  name: ipoveride
  namespace: istio-system
spec:
  overrides: ["10.57.0.0/16"]
  blacklist: false
  entryType: IP_ADDRESSES
`
	i1ValIPAttr = `
apiVersion: "config.istio.io/v1alpha2"
kind: listentry
metadata:
  name: ipwhitelist
  namespace: istio-system
spec:
  value: source.ip
`
	r1IPI1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: checkip
  namespace: istio-system
spec:
  actions:
  - handler: ipoveride.listchecker
    instances:
    - ipwhitelist.listentry
`
	i1ValIPAsStringAttr = `
apiVersion: "config.istio.io/v1alpha2"
kind: listentry
metadata:
  name: ipwhitelist
  namespace: istio-system
spec:
  value: request.headers["x-forwarded-for"]
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
                "Status": {},
                "ValidDuration": 300000000000,
                "ValidUseCount": 10000
              }
             },
             {
              "Check": {
               "Status": {
                "code": 7,
                "message": "staticversion.listchecker.istio-system: is not whitelisted"
               },
               "ValidDuration": 300000000000,
               "ValidUseCount": 10000
              }
             }
            ]
            }`,
		},
	)
}

func TestReportBlacklist(t *testing.T) {
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
				h1BlacklistOverrideSrc1Src2,
				r1H1I1,
				i1ValSrcNameAttr,
			},
			Want: `{
            "AdapterState": null,
            "Returns": [
             {
              "Check": {
                "Status": {
                "code": 7,
                "message": "staticversion.listchecker.istio-system:src1 is blacklisted"
               },
               "ValidDuration": 300000000000,
               "ValidUseCount": 10000
              }
             },
             {
              "Check": {
                "Status": {},
                "ValidDuration": 300000000000,
                "ValidUseCount": 10000
              }
             }
            ]
            }`,
		},
	)
}

func TestReportIpWhitelist(t *testing.T) {
	adapter_integration.RunTest(
		t,
		GetInfo,
		adapter_integration.Scenario{
			ParallelCalls: []adapter_integration.Call{
				{
					CallKind: adapter_integration.CHECK,
					Attrs: map[string]interface{}{"source.ip": []byte(net.ParseIP("10.56.0.1")),
						"request.headers": map[string]string{
							"x-forwarded-for": "10.56.0.1",
						},
					},
				},
				{
					CallKind: adapter_integration.CHECK,
					Attrs: map[string]interface{}{"source.ip": []byte(net.ParseIP("10.57.0.1")),
						"request.headers": map[string]string{
							"x-forwarded-for": "10.57.0.1",
						},
					},
				},
			},
			Configs: []string{
				h1OverrideIP,
				i1ValIPAttr,
				r1IPI1,
			},
			Want: `{
            "AdapterState": null,
            "Returns": [
             {
              "Check": {
                "Status": {
                "code": 7,
                "message": "ipoveride.listchecker.istio-system:10.56.0.1 is not whitelisted"
               },
               "ValidDuration": 300000000000,
               "ValidUseCount": 10000
              }
             },
             {
              "Check": {
                "Status": {},
                "ValidDuration": 300000000000,
                "ValidUseCount": 10000
              }
             }
            ]
            }`,
		},
	)
}

func TestReportIpAsStringWhitelist(t *testing.T) {
	adapter_integration.RunTest(
		t,
		GetInfo,
		adapter_integration.Scenario{
			ParallelCalls: []adapter_integration.Call{
				{
					CallKind: adapter_integration.CHECK,
					Attrs: map[string]interface{}{"source.ip": []byte(net.ParseIP("10.56.0.1")),
						"request.headers": map[string]string{
							"x-forwarded-for": "10.56.0.1",
						},
					},
				},
				{
					CallKind: adapter_integration.CHECK,
					Attrs: map[string]interface{}{"source.ip": []byte(net.ParseIP("10.57.0.1")),
						"request.headers": map[string]string{
							"x-forwarded-for": "10.57.0.1",
						},
					},
				},
			},
			Configs: []string{
				h1OverrideIP,
				i1ValIPAsStringAttr,
				r1IPI1,
			},
			Want: `{
            "AdapterState": null,
            "Returns": [
             {
              "Check": {
                "Status": {
                "code": 7,
                "message": "ipoveride.listchecker.istio-system:10.56.0.1 is not whitelisted"
               },
               "ValidDuration": 300000000000,
               "ValidUseCount": 10000
              }
             },
             {
              "Check": {
                "Status": {},
                "ValidDuration": 300000000000,
                "ValidUseCount": 10000
              }
             }
            ]
            }`,
		},
	)
}
