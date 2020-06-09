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

package perf

import (
	istio_mixer_v1 "istio.io/api/mixer/v1"
)

// This file contains sample data sets to simplify the tests.

// MinimalConfig is a very basic configuration, mainly useful for testing the perf infrastructure itself,
var MinimalConfig = Config{
	Service: minimalSvcCfg,
	Global:  minimalGlobalCfg,
}

// MinimalSetup is a very basic setup, mainly useful for testing the perf infrastructure itself.
var MinimalSetup = Setup{
	Config: MinimalConfig,
	Loads: []Load{{
		Multiplier:  100,
		StableOrder: false,
		Requests: []Request{

			BuildBasicReport(map[string]interface{}{
				"foo": "bar",
				"baz": int64(42),
			}),

			BuildBasicCheck(
				map[string]interface{}{
					"bar": "baz",
					"foo": int64(23),
				},
				map[string]istio_mixer_v1.CheckRequest_QuotaParams{
					"q1": {
						Amount:     23,
						BestEffort: true,
					},
					"q2": {
						Amount:     54,
						BestEffort: false,
					},
				}),
		},
	}},
}

const (
	minimalSvcCfg = `
apiVersion: "config.istio.io/v1alpha2"
kind: attributemanifest
metadata:
  name: istio-proxy
  namespace: default
spec:
    attributes:
      source.name:
        value_type: STRING
      destination.name:
        value_type: STRING
      response.count:
        value_type: INT64
      attr.bool:
        value_type: BOOL
      attr.string:
        value_type: STRING
      attr.double:
        value_type: DOUBLE
      attr.int64:
        value_type: INT64
---
`

	minimalGlobalCfg = `
apiVersion: "config.istio.io/v1alpha2"
kind: fakeHandler
metadata:
  name: fakeHandlerConfig
  namespace: istio-system

---

apiVersion: "config.istio.io/v1alpha2"
kind: samplereport
metadata:
  name: reportInstance
  namespace: istio-system
spec:
  value: "2"
  dimensions:
    source: source.name | "mysrc"
    target_ip: destination.name | "mytarget"

---

apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rule1
  namespace: istio-system
spec:
  selector: match(destination.name, "*")
  actions:
  - handler: fakeHandlerConfig.fakeHandler
    instances:
    - reportInstance.samplereport

---
`
)
