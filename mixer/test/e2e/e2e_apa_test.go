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
	spyadapter "istio.io/istio/mixer/test/spyAdapter"
	e2eTmpl "istio.io/istio/mixer/test/spyAdapter/template"
	apaTmpl "istio.io/istio/mixer/test/spyAdapter/template/apa"
	reportTmpl "istio.io/istio/mixer/test/spyAdapter/template/report"
)

const (
	apaGlobalCfg = `
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
      generated.string:
        value_type: STRING
      generated.int64:
        value_type: INT64
      generated.bool:
        value_type: BOOL
      generated.double:
        value_type: DOUBLE
      generated.strmap:
        value_type: STRING_MAP
---
`
	apaSvcCfg = `
apiVersion: "config.istio.io/v1alpha2"
kind: fakeHandler
metadata:
  name: fakeHandlerConfig
  namespace: istio-system

---
# YAML for apa
apiVersion: "config.istio.io/v1alpha2"
kind: instance
metadata:
  name: apaInstance
  namespace: istio-system
spec:
  compiledTemplate: sampleapa
  params:
    int64Primitive: "2"
    boolPrimitive: "true"
    doublePrimitive: "2.2"
    stringPrimitive: "\"mysrc\""
  attribute_bindings:
    generated.string: $out.stringPrimitive
    generated.bool: $out.boolPrimitive
    generated.double: $out.doublePrimitive
    generated.int64: $out.int64Primitive
    generated.strmap: $out.stringMap
---

apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rule2
  namespace: istio-system
spec:
  match: match(target.name, "*")
  actions:
  - handler: fakeHandlerConfig.fakeHandler
    instances:
    - apaInstance

---
# YAML for report that depend on APA output
apiVersion: "config.istio.io/v1alpha2"
kind: samplereport
metadata:
  name: reportInstance
  namespace: istio-system
spec:
  value: "2"
  dimensions:
    genStr: generated.string
    genBool: generated.bool
    genDouble: generated.double
    genInt64: generated.int64
    genStrMapEntry: generated.strmap["k1"]

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
    - reportInstance.samplereport

---
`
)

func TestApa(t *testing.T) {

	out := apaTmpl.NewOutput()
	out.SetStringPrimitive("gen-str")
	out.SetInt64Primitive(int64(1000))
	out.SetDoublePrimitive(float64(1000.1000))
	out.SetBoolPrimitive(true)
	out.SetStringMap(map[string]string{"k1": "v1"})

	tests := []testData{
		{
			name: "Apa",
			behaviors: []spyadapter.AdapterBehavior{
				{
					Name: "fakeHandler",
					Handler: spyadapter.HandlerBehavior{
						GenerateSampleApaOutput: out,
					},
				},
			},
			attrs: map[string]interface{}{"target.name": "somesrvcname"},
			expectCalls: []spyadapter.CapturedCall{
				{
					Name: "HandleSampleApaAttributes",
					Instances: []interface{}{
						&apaTmpl.Instance{
							Name:            "apaInstance.instance.istio-system",
							BoolPrimitive:   true,
							DoublePrimitive: float64(2.2),
							Int64Primitive:  int64(2),
							StringPrimitive: "mysrc",
						},
					},
				},

				{
					Name: "HandleSampleReport",
					Instances: []interface{}{
						&reportTmpl.Instance{
							Name:  "reportInstance.samplereport.istio-system",
							Value: int64(2),
							Dimensions: map[string]interface{}{
								"genStr":         "gen-str",
								"genBool":        true,
								"genDouble":      float64(1000.1),
								"genInt64":       int64(1000),
								"genStrMapEntry": "v1",
							},
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		// Set the defaults for the test.
		if tt.cfg == "" {
			tt.cfg = apaSvcCfg
		}

		if tt.templates == nil {
			tt.templates = e2eTmpl.SupportedTmplInfo
		}

		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, v1beta1.TEMPLATE_VARIETY_REPORT, apaGlobalCfg)
		})
	}
}
