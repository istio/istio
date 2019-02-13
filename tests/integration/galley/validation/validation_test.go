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

	"istio.io/istio/pkg/test/framework2"
	"istio.io/istio/pkg/test/framework2/components/environment"
	"istio.io/istio/pkg/test/framework2/components/environment/kube"
	"istio.io/istio/pkg/test/framework2/components/istio"
	"istio.io/istio/pkg/test/framework2/runtime"
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
	framework2.Run(t, func(s *runtime.TestContext) {

		s.RequireEnvironmentOrSkip(environment.NameKube)

		dataset := loadTestData(t)

		denied := func(err error) bool {
			if err == nil {
				return false
			}
			return strings.Contains(err.Error(), "denied the request")
		}

		for _, d := range dataset {
			s.Run(string(d), func(s *runtime.TestContext) {
				if d.isSkipped() {
					s.T().SkipNow()
					return
				}

				yml, err := d.load()
				if err != nil {
					s.T().Fatalf("Unable to load test data: %v", err)
				}

				kubeEnv := s.Environment().(*kube.Environment)
				ns := kubeEnv.NewNamespaceOrFail(s, "validation", false)
				err = kubeEnv.ApplyContents(ns, yml)

				switch {
				case err != nil && d.isValid():
					if denied(err) {
						s.T().Fatalf("got unexpected for valid config: %v", err)
					} else {
						s.T().Fatalf("got unexpected unknown error for valid config: %v", err)
					}
				case err == nil && !d.isValid():
					s.T().Fatalf("got unexpected success for invalid config")
				case err != nil && !d.isValid():
					if !denied(err) {
						s.T().Fatalf("config request denied for wrong reason: %v", err)
					}
				}
			})
		}
	})
}


func TestMain(m *testing.M) {
	framework2.RunSuite("galley_validation", m, setup)
}

func setup(s *runtime.SuiteContext) error {
	return istio.Deploy(s)
}