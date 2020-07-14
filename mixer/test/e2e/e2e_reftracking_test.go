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
	istio_mixer_v1 "istio.io/api/mixer/v1"
	spyadapter "istio.io/istio/mixer/test/spyAdapter"
	e2eTmpl "istio.io/istio/mixer/test/spyAdapter/template"
	"istio.io/pkg/attribute"
)

const (
	refTrackingGlobalCfg = `
apiVersion: "config.istio.io/v1alpha2"
kind: attributemanifest
metadata:
  name: istio-proxy
  namespace: default
spec:
    attributes:
      destination.service:
        value_type: STRING
      source.service:
        value_type: STRING
      request.headers:
        value_type: STRING_MAP
---
`
	refTrackingSvcCfg = `
apiVersion: "config.istio.io/v1alpha2"
kind: fakeHandler
metadata:
  name: fakeHandlerConfig
  namespace: istio-system

---

apiVersion: "config.istio.io/v1alpha2"
kind: samplecheck
metadata:
  name: checkInstance
  namespace: istio-system
spec:
  stringPrimitive: "destination.service"
---

apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: rule1
  namespace: istio-system
spec:
  match: destination.service == "echosrv1.istio.svc.cluster.local" && request.headers["x-request-id"] == "foo"
  actions:
  - handler: fakeHandlerConfig.fakeHandler
    instances:
    - checkInstance.samplecheck

---
`
)

// TestRefTracking is an end-to-end test for ensuring that we do appropriate short-circuiting and attribute
// reference tracking on certain circumstances.
// When an expression with a logical operator is evaluated, the current semantics of our expression language
// short-circuits the right-hand-side. This means, any attributes that are appearing on the RHS should *not*
// be referenced. This test captures the behavior.
func TestRefTracking(t *testing.T) {
	tests := []testData{
		{
			name: "RefTracking",
			attrs: map[string]interface{}{
				"destination.service":   "echosrv2.istio.svc.cluster.local",
				"destination.namespace": "istio",
				"source.service":        "foo",
				"request.headers": attribute.WrapStringMap(map[string]string{
					"x-request-id": "foo",
				}),
			},

			expectAttrRefs: []expectedAttrRef{
				{
					name:      "context.reporter.kind",
					condition: istio_mixer_v1.ABSENCE,
				},
				{
					name:      "destination.service",
					condition: istio_mixer_v1.EXACT,
				},
				{
					name:      "destination.namespace",
					condition: istio_mixer_v1.EXACT,
				},
			},
		},
	}
	for _, tt := range tests {
		// Set the defaults for the test.
		if tt.cfg == "" {
			tt.cfg = refTrackingSvcCfg
		}

		if tt.templates == nil {
			tt.templates = e2eTmpl.SupportedTmplInfo
		}

		if tt.behaviors == nil {
			tt.behaviors = []spyadapter.AdapterBehavior{{Name: "fakeHandler"}}
		}

		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, v1beta1.TEMPLATE_VARIETY_CHECK, refTrackingGlobalCfg)
		})
	}
}
