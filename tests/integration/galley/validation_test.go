// Copyright 2019 Istio Authors
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

package galley

import (
	"path"
	"strings"
	"testing"

	"gopkg.in/square/go-jose.v2/json"
	"sigs.k8s.io/yaml"

	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/testdata/validation"
	"istio.io/istio/pkg/test/util/yml"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

type testData string

func (t testData) isValid() bool {
	return !strings.HasSuffix(string(t), "-invalid.yaml")
}

func (t testData) isSkipped() bool {
	return strings.HasSuffix(string(t), "-skipped.yaml")
}

func (t testData) load() (string, error) {
	by, err := validation.Asset(path.Join("dataset", string(t)))
	if err != nil {
		return "", err
	}

	return string(by), nil
}

func loadTestData(t *testing.T) []testData {
	entries, err := validation.AssetDir("dataset")
	if err != nil {
		t.Fatalf("Error loading test data: %v", err)
	}

	var result []testData
	for _, e := range entries {
		result = append(result, testData(e))
		t.Logf("Found test data: %v", e)
	}

	return result
}

func TestValidation(t *testing.T) {
	framework.NewTest(t).
		// Limit to Kube environment as we're testing integration of webhook with K8s.
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {

			dataset := loadTestData(t)

			denied := func(err error) bool {
				if err == nil {
					return false
				}
				// We are only checking the string literals of the rejection reasons
				// from the webhook and the k8s api server as the returned errors are not
				// k8s typed errors.
				return strings.Contains(err.Error(), "denied the request") ||
					strings.Contains(err.Error(), "error validating data") ||
					strings.Contains(err.Error(), "Invalid value") ||
					strings.Contains(err.Error(), "is invalid")
			}

			for _, d := range dataset {
				t.Run(string(d), func(t *testing.T) {
					if d.isSkipped() {
						t.SkipNow()
						return
					}

					fctx := framework.NewContext(t)
					defer fctx.Done()

					ym, err := d.load()
					if err != nil {
						t.Fatalf("Unable to load test data: %v", err)
					}

					env := fctx.Environment().(*kube.Environment)
					ns := namespace.NewOrFail(t, fctx, namespace.Config{
						Prefix: "validation",
					})
					err = env.ApplyContents(ns.Name(), ym)
					defer func() { _ = env.DeleteContents(ns.Name(), ym) }()

					switch {
					case err != nil && d.isValid():
						if denied(err) {
							t.Fatalf("got unexpected for valid config: %v", err)
						} else {
							t.Fatalf("got unexpected unknown error for valid config: %v", err)
						}
					case err == nil && !d.isValid():
						t.Fatalf("got unexpected success for invalid config")
					case err != nil && !d.isValid():
						if !denied(err) {
							t.Fatalf("config request denied for wrong reason: %v", err)
						}
					}
				})
			}
		})
}

var ignoredCRDs = []string{
	// We don't validate K8s resources
	"/v1/Namespace",
	"/v1/Node",
	"/v1/Pod",
	"/v1/Endpoints",
	"/v1/Service",
	"extensions/v1beta1/Ingress",
	"apps/v1/Deployment",
	"networking.istio.io/v1alpha3/SyntheticServiceEntry",

	// Legacy Mixer CRDs are ignored
	"config.istio.io/v1alpha2/cloudwatch",
	"config.istio.io/v1alpha2/statsd",
	"config.istio.io/v1alpha2/stdio",
	"config.istio.io/v1alpha2/listentry",
	"config.istio.io/v1alpha2/metric",
	"config.istio.io/v1alpha2/stackdriver",
	"config.istio.io/v1alpha2/kubernetes",
	"config.istio.io/v1alpha2/quota",
	"config.istio.io/v1alpha2/zipkin",
	"config.istio.io/v1alpha2/prometheus",
	"config.istio.io/v1alpha2/redisquota",
	"config.istio.io/v1alpha2/reportnothing",
	"config.istio.io/v1alpha2/edge",
	"config.istio.io/v1alpha2/noop",
	"config.istio.io/v1alpha2/signalfx",
	"config.istio.io/v1alpha2/solarwinds",
	"config.istio.io/v1alpha2/apikey",
	"config.istio.io/v1alpha2/bypass",
	"config.istio.io/v1alpha2/dogstatsd",
	"config.istio.io/v1alpha2/kubernetesenv",
	"config.istio.io/v1alpha2/listchecker",
	"config.istio.io/v1alpha2/tracespan",
	"config.istio.io/v1alpha2/authorization",
	"config.istio.io/v1alpha2/fluentd",
	"config.istio.io/v1alpha2/memquota",
	"config.istio.io/v1alpha2/opa",
	"config.istio.io/v1alpha2/checknothing",
	"config.istio.io/v1alpha2/circonus",
	"config.istio.io/v1alpha2/denier",
	"config.istio.io/v1alpha2/logentry",
	"config.istio.io/v1alpha2/rbac",
}

func TestEnsureNoMissingCRDs(t *testing.T) {
	// This test ensures that we have necessary tests for all known CRDs. If you're breaking this test, it is likely
	// that you need to update validation tests by either adding new/missing test cases, or removing test cases for
	// types that are no longer supported.
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {

			ignored := make(map[string]struct{})
			for _, ig := range ignoredCRDs {
				ignored[ig] = struct{}{}
			}

			recognized := make(map[string]struct{})

			for _, r := range metadata.MustGet().KubeSource().Resources() {
				s := strings.Join([]string{r.Group, r.Version, r.Kind}, "/")
				recognized[s] = struct{}{}
			}

			testedValid := make(map[string]struct{})
			testedInvalid := make(map[string]struct{})
			for _, te := range loadTestData(t) {
				yamlBatch, err := te.load()
				yamlParts := yml.SplitString(yamlBatch)
				for _, yamlPart := range yamlParts {
					if err != nil {
						ctx.Fatalf("error loading test data: %v", err)
					}

					m := make(map[string]interface{})
					by, er := yaml.YAMLToJSON([]byte(yamlPart))
					if er != nil {
						ctx.Fatalf("error loading test data: %v", er)
					}
					if err = json.Unmarshal(by, &m); err != nil {
						ctx.Fatalf("error parsing JSON: %v", err)
					}

					apiVersion := m["apiVersion"].(string)
					kind := m["kind"].(string)

					key := strings.Join([]string{apiVersion, kind}, "/")
					if te.isValid() {
						testedValid[key] = struct{}{}
					} else {
						testedInvalid[key] = struct{}{}
					}
				}
			}

			for rec := range recognized {
				if _, found := ignored[rec]; found {
					continue
				}

				if _, found := testedValid[rec]; !found {
					ctx.Errorf("CRD does not have a positive validation test: %v", rec)
				}
				if _, found := testedInvalid[rec]; !found {
					ctx.Errorf("CRD does not have a negative validation test: %v", rec)
				}
			}

			for tst := range testedValid {
				if _, found := recognized[tst]; !found {
					ctx.Errorf("Unrecognized positive validation test data found: %v", tst)
				}
			}
			for tst := range testedInvalid {
				if _, found := recognized[tst]; !found {
					ctx.Errorf("Unrecognized negative validation test data found: %v", tst)
				}
			}
		})
}
