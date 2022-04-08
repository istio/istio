//go:build integ
// +build integ

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

package pilot

import (
	"path"
	"strings"
	"testing"

	"gopkg.in/square/go-jose.v2/json"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/test/datasets/validation"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/yml"
	"istio.io/istio/pkg/util/sets"
)

type testData string

func (t testData) isValid() bool {
	return !strings.HasSuffix(string(t), "-invalid.yaml")
}

func (t testData) isSkipped() bool {
	return strings.HasSuffix(string(t), "-skipped.yaml")
}

func (t testData) load() (string, error) {
	by, err := validation.FS.ReadFile(path.Join("dataset", string(t)))
	if err != nil {
		return "", err
	}

	return string(by), nil
}

func loadTestData(t framework.TestContext) []testData {
	entries, err := validation.FS.ReadDir("dataset")
	if err != nil {
		t.Fatalf("Error loading test data: %v", err)
	}

	var result []testData
	for _, e := range entries {
		result = append(result, testData(e.Name()))
	}

	return result
}

func TestValidation(t *testing.T) {
	framework.NewTest(t).
		// Limit to Kube environment as we're testing integration of webhook with K8s.

		RunParallel(func(t framework.TestContext) {
			dataset := loadTestData(t)

			denied := func(err error) bool {
				if err == nil {
					return false
				}
				// We are only checking the string literals of the rejection reasons
				// from the webhook and the k8s api server as the returned errors are not
				// k8s typed errors.
				// Note: this explicitly does NOT catch OpenAPI schema rejections - only validating webhook rejections.
				return strings.Contains(err.Error(), "denied the request")
			}

			for _, cluster := range t.Clusters().Configs() {
				for i := range dataset {
					d := dataset[i]
					t.NewSubTest(string(d)).RunParallel(func(t framework.TestContext) {
						if d.isSkipped() {
							t.SkipNow()
							return
						}

						ym, err := d.load()
						if err != nil {
							t.Fatalf("Unable to load test data: %v", err)
						}

						ns := namespace.NewOrFail(t, t, namespace.Config{
							Prefix: "validation",
						})

						applyFiles := t.WriteYAMLOrFail(t, "apply", ym)
						dryRunErr := cluster.ApplyYAMLFilesDryRun(ns.Name(), applyFiles...)

						switch {
						case dryRunErr != nil && d.isValid():
							if denied(dryRunErr) {
								t.Fatalf("got unexpected for valid config: %v", dryRunErr)
							} else {
								t.Fatalf("got unexpected unknown error for valid config: %v", dryRunErr)
							}
						case dryRunErr == nil && !d.isValid():
							t.Fatalf("got unexpected success for invalid config")
						case dryRunErr != nil && !d.isValid():
							if !denied(dryRunErr) {
								t.Fatalf("config request denied for wrong reason: %v", dryRunErr)
							}
						}

						wetRunErr := cluster.ApplyYAMLFiles(ns.Name(), applyFiles...)
						t.ConditionalCleanup(func() {
							cluster.DeleteYAMLFiles(ns.Name(), applyFiles...)
						})

						if dryRunErr != nil && wetRunErr == nil {
							t.Fatalf("dry run returned error, but wet run returned none: %v", dryRunErr)
						}
						if dryRunErr == nil && wetRunErr != nil {
							t.Fatalf("wet run returned errors, but dry run returned none: %v", wetRunErr)
						}
					})
				}
			}
		})
}

var ignoredCRDs = []string{
	// We don't validate K8s resources
	"/v1/Endpoints",
	"/v1/Namespace",
	"/v1/Node",
	"/v1/Pod",
	"/v1/Secret",
	"/v1/Service",
	"/v1/ConfigMap",
	"apiextensions.k8s.io/v1/CustomResourceDefinition",
	"admissionregistration.k8s.io/v1/MutatingWebhookConfiguration",
	"apps/v1/Deployment",
	"extensions/v1beta1/Ingress",
}

func TestEnsureNoMissingCRDs(t *testing.T) {
	// This test ensures that we have necessary tests for all known CRDs. If you're breaking this test, it is likely
	// that you need to update validation tests by either adding new/missing test cases, or removing test cases for
	// types that are no longer supported.
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			ignored := sets.New(ignoredCRDs...)
			recognized := sets.New()

			// TODO(jasonwzm) remove this after multi-version APIs are supported.
			for _, r := range collections.Pilot.All() {
				s := strings.Join([]string{r.Resource().Group(), r.Resource().Version(), r.Resource().Kind()}, "/")
				recognized.Insert(s)
			}
			for _, gvk := range []string{
				"networking.istio.io/v1beta1/Gateway",
				"networking.istio.io/v1beta1/DestinationRule",
				"networking.istio.io/v1beta1/VirtualService",
				"networking.istio.io/v1beta1/WorkloadEntry",
				"networking.istio.io/v1beta1/Sidecar",
			} {
				recognized.Insert(gvk)
			}
			// These CRDs are validated outside of Istio
			for _, gvk := range []string{
				"gateway.networking.k8s.io/v1alpha2/Gateway",
				"gateway.networking.k8s.io/v1alpha2/GatewayClass",
				"gateway.networking.k8s.io/v1alpha2/HTTPRoute",
				"gateway.networking.k8s.io/v1alpha2/TCPRoute",
				"gateway.networking.k8s.io/v1alpha2/TLSRoute",
				"gateway.networking.k8s.io/v1alpha2/ReferencePolicy",
			} {
				recognized.Delete(gvk)
			}

			testedValid := sets.New()
			testedInvalid := sets.New()
			for _, te := range loadTestData(t) {
				yamlBatch, err := te.load()
				yamlParts := yml.SplitString(yamlBatch)
				for _, yamlPart := range yamlParts {
					if err != nil {
						t.Fatalf("error loading test data: %v", err)
					}

					m := make(map[string]interface{})
					by, er := yaml.YAMLToJSON([]byte(yamlPart))
					if er != nil {
						t.Fatalf("error loading test data: %v", er)
					}
					if err = json.Unmarshal(by, &m); err != nil {
						t.Fatalf("error parsing JSON: %v", err)
					}

					apiVersion := m["apiVersion"].(string)
					kind := m["kind"].(string)

					key := strings.Join([]string{apiVersion, kind}, "/")
					if te.isValid() {
						testedValid.Insert(key)
					} else {
						testedInvalid.Insert(key)
					}
				}
			}

			for rec := range recognized {
				if ignored.Contains(rec) {
					continue
				}

				if !testedValid.Contains(rec) {
					t.Errorf("CRD does not have a positive validation test: %v", rec)
				}
				if !testedInvalid.Contains(rec) {
					t.Errorf("CRD does not have a negative validation test: %v", rec)
				}
			}

			for tst := range testedValid {
				if _, found := recognized[tst]; !found {
					t.Errorf("Unrecognized positive validation test data found: %v", tst)
				}
			}
			for tst := range testedInvalid {
				if _, found := recognized[tst]; !found {
					t.Errorf("Unrecognized negative validation test data found: %v", tst)
				}
			}
		})
}
