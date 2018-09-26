//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package mixer

import (
	"testing"
	"time"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/dependency"
)

func TestMixer_Report_Direct(t *testing.T) {
	framework.Requires(t, dependency.PolicyBackend, dependency.Mixer)

	env := framework.AcquireEnvironment(t)

	mxr := env.GetMixerOrFail(t)

	be := env.GetPolicyBackendOrFail(t)

	env.Configure(t,
		test.JoinConfigs(
			testReportConfig,
			be.CreateConfigSnippet("handler1"),
		))

	mxr.Report(t, map[string]interface{}{
		"context.protocol":    "http",
		"destination.name":    "somesrvcname",
		"response.time":       time.Now(),
		"request.time":        time.Now(),
		"destination.service": "svc.{{.TestNamespace}}",
		"destination.port":    int64(7678),
		"origin.ip":           []byte{1, 2, 3, 4},
	})

	expected := `
{
  "name": "metric1.metric.istio-system",
  "value": {
    "int64Value": "2"
  },
  "dimensions": {
    "origin_ip": {
      "ipAddressValue": {
        "value": "AQIDBA=="
      }
    },
    "target_port": {
      "int64Value": "7678"
    }
  }
}
`

	be.ExpectReportJSON(t, expected)
}

var testReportConfig = `
apiVersion: "config.istio.io/v1alpha2"
kind: metric
metadata:
  name: metric1
  namespace: istio-system
spec:
  value: "2"
  dimensions:
    target_port: destination.port | 9696
    origin_ip: origin.ip | ip("4.5.6.7")
---
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rule1
  namespace: istio-system
spec:
  actions:
  - handler: handler1.bypass
    instances:
    - metric1.metric
`
