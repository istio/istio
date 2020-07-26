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
	v1 "istio.io/api/mixer/v1"
	spyadapter "istio.io/istio/mixer/test/spyAdapter"
	e2eTmpl "istio.io/istio/mixer/test/spyAdapter/template"
	checkProducerTmpl "istio.io/istio/mixer/test/spyAdapter/template/checkoutput"
)

func TestCheckOutput(t *testing.T) {
	tests := []testData{

		{
			name: "BasicCheckOutput",
			cfg: `
apiVersion: config.istio.io/v1alpha2
kind: fakehandler
metadata:
  name: fake
  namespace: istio-system
---
apiVersion: config.istio.io/v1alpha2
kind: checkproducer
metadata:
  name: instance
  namespace: istio-system
spec:
  stringPrimitive: '"test"'
---
apiVersion: config.istio.io/v1alpha2
kind: rule
metadata:
  name: example
  namespace: istio-system
spec:
  actions:
  - handler: fake.fakehandler
    instances:
    - instance.checkproducer
    name: test
  requestHeaderOperations:
  - name: x-istio
    values:
    - test.output.stringMap["key1"]
    - test.output.stringPrimitive
    operation: APPEND
---
`,
			attrs: map[string]interface{}{},
			behaviors: []spyadapter.AdapterBehavior{
				{
					Name: "fakehandler",
					Handler: spyadapter.HandlerBehavior{
						HandleCheckProducerOutput: &checkProducerTmpl.Output{
							StringPrimitive: "string0",
							StringMap: map[string]string{
								"key1": "value1",
							},
						},
					},
				},
			},
			templates: e2eTmpl.SupportedTmplInfo,
			expectAttrRefs: []expectedAttrRef{{
				name:      "destination.namespace",
				condition: v1.ABSENCE,
			}, {
				name:      "context.reporter.kind",
				condition: v1.ABSENCE,
			}},
			expectCalls: []spyadapter.CapturedCall{
				{
					Name: "HandleCheckProducer",
					Instances: []interface{}{
						&checkProducerTmpl.Instance{
							Name:            "instance.checkproducer.istio-system",
							StringPrimitive: "test",
						},
					},
				},
			},
			expectDirective: &v1.RouteDirective{
				RequestHeaderOperations: []v1.HeaderOperation{
					{
						Name:      "x-istio",
						Value:     "value1",
						Operation: v1.APPEND,
					},
					{
						Name:      "x-istio",
						Value:     "string0",
						Operation: v1.APPEND,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, v1beta1.TEMPLATE_VARIETY_CHECK, "")
		})
	}
}

func TestCheckElided(t *testing.T) {
	tests := []testData{

		{
			name: "BasicCheckOutput",
			cfg: `
apiVersion: config.istio.io/v1alpha2
kind: rule
metadata:
  name: example
  namespace: istio-system
spec:
  actions: []
  requestHeaderOperations:
  - name: x-istio
    values:
    - '"value1"'
    - '"string0"'
    operation: APPEND
---
`,
			attrs: map[string]interface{}{},
			behaviors: []spyadapter.AdapterBehavior{
				{
					Name: "fakehandler",
					Handler: spyadapter.HandlerBehavior{
						HandleCheckProducerOutput: &checkProducerTmpl.Output{
							StringPrimitive: "string0",
							StringMap: map[string]string{
								"key1": "value1",
							},
						},
					},
				},
			},
			templates: e2eTmpl.SupportedTmplInfo,
			expectAttrRefs: []expectedAttrRef{{
				name:      "destination.namespace",
				condition: v1.ABSENCE,
			}, {
				name:      "context.reporter.kind",
				condition: v1.ABSENCE,
			}},
			expectDirective: &v1.RouteDirective{
				RequestHeaderOperations: []v1.HeaderOperation{
					{
						Name:      "x-istio",
						Value:     "value1",
						Operation: v1.APPEND,
					},
					{
						Name:      "x-istio",
						Value:     "string0",
						Operation: v1.APPEND,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, v1beta1.TEMPLATE_VARIETY_CHECK, "")
		})
	}
}
