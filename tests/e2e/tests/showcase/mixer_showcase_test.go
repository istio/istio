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

package showcase

import (
	"testing"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/dependency"
	"istio.io/istio/pkg/test/label"
)

func TestMixer_Report_Direct(t *testing.T) {
	test.Tag(t, label.Label("QQQ"))
	test.Requires(t, dependency.PolicyBackend, dependency.Mixer)

	env := test.AcquireEnvironment(t)

	m := env.GetMixerOrFail(t)

	be := env.GetPolicyBackendOrFail(t)

	env.Configure(t,
		test.JoinConfigs(
			testConfig,
			attrConfig,
			be.CreateConfigSnippet("handler1"),
		))

	m.Report(t, map[string]interface{}{
		"target.name":         "somesrvcname",
		"destination.service": "svc.cluster.local",
	})

	be.ExpectReportJSON(t,
		`
{
  "name":"metric1.metric.istio-system",
  "value":{"int64Value":"2"},
  "dimensions":{
    "source":{"stringValue":"mysrc"},
    "target_ip":{"stringValue":"somesrvcname"}
   }
}`)

}

//
//func TestMixer_Report(t *testing.T) {
//	test.Requires(t, dependency.PolicyBackend)
//
//	env := test.AcquireEnvironment(t)
//	env.Configure(t, testConfig)
//
//	be := env.GetPolicyBackendOrFail(t)
//	// TODO: Define how backend should behave when Mixer dispatches the request
//	// be.SetBehavior()
//	_ = be
//
//	appa := env.GetAppOrFail("a", t)
//	appb := env.GetAppOrFail("b", t)
//	u := appb.EndpointsForProtocol(model.ProtocolHTTP)[0].MakeURL()
//	result := appa.CallOrFail(u, 1, nil, t)
//
//	// assert call result
//	if !result.IsSuccess() {
//		t.Fatalf("Call should have succeeded")
//	}
//
//	// TODO: Define how we can query the mock backend.
//	be.ExpectReport(t, `
//Name: reportInstance.samplereport.istio-system,
//Value: 2,
//Dimensions:
//	- source: mysrc
//	- target_ip: somesrvcname
//    - request_id: ...
//`)
//	//be.ExpectReport(t).With("request_id: ,....")
//}

var attrConfig = `
apiVersion: "config.istio.io/v1alpha2"
kind: attributemanifest
metadata:
  name: istio-proxy
  namespace: default
spec:
    attributes:
      source.name:
        value_type: STRING
      target.name:
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

var testConfig = `
apiVersion: "config.istio.io/v1alpha2"
kind: metric
metadata:
 name: metric1
 namespace: istio-system
spec:
 value: "2"
 dimensions:
   source: source.name | "mysrc"
   target_ip: target.name | "mytarget"

---
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
 name: rule1
 namespace: istio-system
spec:
 match: match(target.name, "*")
 actions:
 - handler: handler1.bypass
   instances:
   - metric1.metric

`
