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

package validation

import (
	"path"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/framework/label"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
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
	by, err := Asset(path.Join("testdata", string(t)))
	if err != nil {
		return "", err
	}

	return string(by), nil
}

func loadTestData(t *testing.T) []testData {
	entries, err := AssetDir("testdata")
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
	framework.Run(t, func(ctx framework.TestContext) {

		dataset := loadTestData(t)

		denied := func(err error) bool {
			if err == nil {
				return false
			}
			return strings.Contains(err.Error(), "denied the request")
		}

		for _, d := range dataset {
			t.Run(string(d), func(t *testing.T) {
				if d.isSkipped() {
					t.SkipNow()
					return
				}

				ctx := framework.NewContext(t)
				defer ctx.Done(t)

				yml, err := d.load()
				if err != nil {
					t.Fatalf("Unable to load test data: %v", err)
				}

				env := ctx.Environment().(*kube.Environment)
				ns := namespace.NewOrFail(t, ctx, "validation", false)
				err = env.ApplyContents(ns.Name(), yml)

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

func TestMain(m *testing.M) {
	framework.
		NewSuite("galley_validation", m).
		Label(label.Presubmit).
		RequireEnvironment(environment.Kube).
		SetupOnEnv(environment.Kube, istio.Setup(nil, nil)).
		Run()
}
