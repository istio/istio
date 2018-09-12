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
	// TODO(ozben): Marking this local, as it doesn't work on both local and remote seamlessly with the same
	// config. Once the config problem is fixed, we should make this to not have a dependency on an environment.
	framework.Requires(t, dependency.Local, dependency.PolicyBackend, dependency.Mixer)

	env := framework.AcquireEnvironment(t)

	mxr := env.GetMixerOrFail(t)

	be := env.GetPolicyBackendOrFail(t)

	env.Configure(t,
		test.JoinConfigs(
			testReportConfig,
			attrConfig,
			be.CreateConfigSnippet("handler1"),
		))

	dstService := env.Evaluate(t, `svc.{{.TestNamespace}}`)

	mxr.Report(t, map[string]interface{}{
		"context.protocol":    "http",
		"destination.name":    "somesrvcname",
		"response.time":       time.Now(),
		"request.time":        time.Now(),
		"destination.service": dstService,
		"origin.ip":           []byte{1, 2, 3, 4},
	})

	expected := env.Evaluate(t, `
{
  "name":"metric1.metric.istio-system",
  "value":{"int64Value":"2"},
  "dimensions":{
    "source":{"stringValue":"mysrc"},
    "target_ip":{"stringValue":"somesrvcname"}
   }
}`)

	be.ExpectReportJSON(t, expected)
}

var attrConfig = `
apiVersion: "config.istio.io/v1alpha2"
kind: attributemanifest
metadata:
 name: istio-proxy
 namespace: istio-system
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
`

var testReportConfig = `
apiVersion: "config.istio.io/v1alpha2"
kind: metric
metadata:
  name: metric1
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
  actions:
  - handler: handler1.bypass
    instances:
    - metric1.metric
`
