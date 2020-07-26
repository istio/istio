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

package e2e

import (
	"testing"

	"istio.io/api/mixer/adapter/model/v1beta1"
	pb "istio.io/api/policy/v1beta1"
	spyadapter "istio.io/istio/mixer/test/spyAdapter"
	e2eTmpl "istio.io/istio/mixer/test/spyAdapter/template"
	reportTmpl "istio.io/istio/mixer/test/spyAdapter/template/report"
)

const (
	reportGlobalCfg = `
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
---
`
)

func TestReport(t *testing.T) {
	tests := []testData{

		{
			name: "Basic Report",
			cfg: `
apiVersion: "config.istio.io/v1alpha2"
kind: handler
metadata:
  name: fakeHandlerConfig
  namespace: istio-system
spec:
  compiledAdapter: fakeHandler

---

apiVersion: "config.istio.io/v1alpha2"
kind: instance
metadata:
  name: reportInstance
  namespace: istio-system
spec:
  compiledTemplate: samplereport
  params:
    value: response.count | 0
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
  - handler: fakeHandlerConfig.handler
    instances:
    - reportInstance.instance

---
`,
			attrs: map[string]interface{}{
				"target.name":    "somesrvcname",
				"response.count": int64(2),
			},

			expectSetTypes: map[string]interface{}{
				"reportInstance.instance.istio-system": &reportTmpl.Type{
					Value:      pb.INT64,
					Dimensions: map[string]pb.ValueType{"source": pb.STRING, "target_ip": pb.STRING},
				},
			},

			expectCalls: []spyadapter.CapturedCall{
				{
					Name: "HandleSampleReport",
					Instances: []interface{}{
						&reportTmpl.Instance{
							Name:       "reportInstance.instance.istio-system",
							Value:      int64(2),
							Dimensions: map[string]interface{}{"source": "mysrc", "target_ip": "somesrvcname"},
						},
					},
				},
			},
		},

		{
			name: "Multi Instance Report",
			cfg: `
apiVersion: "config.istio.io/v1alpha2"
kind: fakeHandler
metadata:
  name: fakeHandlerConfig
  namespace: istio-system

---
# Instance 1
apiVersion: "config.istio.io/v1alpha2"
kind: samplereport
metadata:
  name: reportInstance1
  namespace: istio-system
spec:
  value: "2"
  dimensions:
    source: source.name | "mysrc"
    target_ip: target.name | "mytarget"

---
# Instance 2
apiVersion: "config.istio.io/v1alpha2"
kind: samplereport
metadata:
  name: reportInstance2
  namespace: istio-system
spec:
  value: "5"
  dimensions:
    source: source.name | "yoursrc"
    target_ip: target.name | "yourtarget"

---

apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rule1
  namespace: istio-system
spec:
  match: match(target.name, "*")
  actions:
  - handler: fakeHandlerConfig.fakeHandler
    instances:
    - reportInstance1.samplereport
    - reportInstance2.samplereport

---
`,
			attrs: map[string]interface{}{
				"target.name": "somesrvcname",
			},

			expectSetTypes: map[string]interface{}{
				"reportInstance1.samplereport.istio-system": &reportTmpl.Type{
					Value:      pb.INT64,
					Dimensions: map[string]pb.ValueType{"source": pb.STRING, "target_ip": pb.STRING},
				},
				"reportInstance2.samplereport.istio-system": &reportTmpl.Type{
					Value:      pb.INT64,
					Dimensions: map[string]pb.ValueType{"source": pb.STRING, "target_ip": pb.STRING},
				},
			},

			expectCalls: []spyadapter.CapturedCall{
				{
					Name: "HandleSampleReport",
					Instances: []interface{}{
						&reportTmpl.Instance{
							Name:       "reportInstance1.samplereport.istio-system",
							Value:      int64(2),
							Dimensions: map[string]interface{}{"source": "mysrc", "target_ip": "somesrvcname"},
						},
						&reportTmpl.Instance{
							Name:       "reportInstance2.samplereport.istio-system",
							Value:      int64(5),
							Dimensions: map[string]interface{}{"source": "yoursrc", "target_ip": "somesrvcname"},
						},
					},
				},
			},
		},

		{
			name: "Conditional Report with No Success",
			cfg: `
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
    target_ip: target.name | "mytarget"

---

apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rule1
  namespace: istio-system
spec:
  match: match(target.name, "some unknown thing")
  actions:
  - handler: fakeHandlerConfig.fakeHandler
    instances:
    - reportInstance.samplereport

---
`,
			attrs: map[string]interface{}{
				"target.name": "somesrvcname",
			},

			expectCalls: nil,
		},
	}

	for _, tt := range tests {
		if tt.templates == nil {
			tt.templates = e2eTmpl.SupportedTmplInfo
		}

		if tt.behaviors == nil {
			tt.behaviors = []spyadapter.AdapterBehavior{{Name: "fakeHandler"}}
		}

		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, v1beta1.TEMPLATE_VARIETY_REPORT, reportGlobalCfg)
		})
	}
}
