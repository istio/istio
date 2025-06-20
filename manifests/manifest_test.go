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

package manifests

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/test/helm"
	"istio.io/istio/pkg/test/util/assert"
)

func fromYAML(in string) *unstructured.Unstructured {
	var un unstructured.Unstructured
	if err := yaml.Unmarshal([]byte(in), &un); err != nil {
		panic(err)
	}
	return &un
}

func TestHelmTemplate(t *testing.T) {
	cases := []struct {
		name         string
		chart        string
		template     string
		values       map[string]string
		expectations map[string]string
	}{
		{
			name:     "pod disruption budget default min available 1",
			chart:    "istio-control/istio-discovery",
			template: "poddisruptionbudget.yaml",
			values:   map[string]string{},
			expectations: map[string]string{
				"spec.minAvailable": "1",
			},
		},
		{
			name:     "pod disruption budget max unavailable 1",
			chart:    "istio-control/istio-discovery",
			template: "poddisruptionbudget.yaml",
			values: map[string]string{
				"maxUnavailable": "1",
			},
			expectations: map[string]string{
				"spec.maxUnavailable": "1",
				"spec.minAvailable":   "",
			},
		},
		{
			name:     "pod disruption budget max unavailable if both",
			chart:    "istio-control/istio-discovery",
			template: "poddisruptionbudget.yaml",
			values: map[string]string{
				"maxUnavailable": "1",
				"minAvailable":   "1",
			},
			expectations: map[string]string{
				"spec.maxUnavailable": "1",
				"spec.minAvailable":   "",
			},
		},
	}

	for _, c := range cases {
		helm := helm.New("kubeconfig")
		t.Run(c.name, func(t *testing.T) {
			var overrides []string
			templateArgs := ""
			for key, value := range c.values {
				overrides = append(overrides, fmt.Sprintf("%s=%s", key, value))
			}
			if len(overrides) > 0 {
				templateArgs = fmt.Sprintf("--set %s", strings.Join(overrides, ","))
			}
			template, err := helm.Template(
				"istiod",
				fmt.Sprintf("charts/%s", c.chart), "istio-system",
				fmt.Sprintf("templates/%s", c.template),
				1*time.Second,
				templateArgs,
			)
			if err != nil {
				t.Errorf("failure to render template: %v", err)
			}

			un := fromYAML(template)
			for assertKey, assertValue := range c.expectations {
				val, found, err := unstructured.NestedFieldNoCopy(un.Object, strings.Split(assertKey, ".")...)
				if err != nil {
					t.Errorf("failure to find spec expectation: %v", err)
				}
				if assertValue == "" {
					assert.Equal(t, found, false)
					continue
				}
				assert.Equal(t, found, true)
				switch s := val.(type) {
				case string:
					assert.Equal(t, assertValue, s)
				case int64:
					assert.Equal(t, assertValue, strconv.FormatInt(s, 10))
				default:
					t.Errorf("failure to handle expected value in yaml %v", val)
					t.Fail()
				}
			}
		})
	}
}
