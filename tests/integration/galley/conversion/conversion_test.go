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

package conversion

import (
	"fmt"
	"testing"
	"time"

	"istio.io/istio/galley/pkg/testing/testdata"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
)

func TestConversion(t *testing.T) {
	dataset, err := testdata.Load()
	if err != nil {
		t.Fatalf("Error loading data set: %v", err)
	}

	for _, d := range dataset {
		t.Run(d.TestName(), func(t *testing.T) {
			if d.Skipped {
				t.SkipNow()
				return
			}

			ctx := framework.NewContext(t)
			defer ctx.Done(t)

			var gal galley.Instance
			var cfg galley.Config

			for i, fset := range d.FileSets() {
				// Do init for the first set. Use Meshconfig file in this set.
				if i == 0 {
					if fset.HasMeshConfigFile() {
						mc, err := fset.LoadMeshConfigFile()
						if err != nil {
							t.Fatalf("Error loading Mesh config file: %v", err)
						}

						cfg.MeshConfig = string(mc)
					}

					gal = galley.NewOrFail(t, ctx, cfg)
				}

				t.Logf("==== Running iter: %d\n", i)
				if len(d.FileSets()) == 1 {
					runTest(t, fset, gal)
				} else {
					testName := fmt.Sprintf("%d", i)
					t.Run(testName, func(t *testing.T) {
						runTest(t, fset, gal)
					})
				}
			}
		})
	}
}

func runTest(t *testing.T, fset *testdata.FileSet, gal galley.Instance) {
	input, err := fset.LoadInputFile()
	if err != nil {
		t.Fatalf("Unable to load input test data: %v", err)
	}

	expected, err := fset.LoadExpectedResources()
	if err != nil {
		t.Fatalf("unable to load expected resources: %v", err)
	}

	if err = gal.ClearConfig(); err != nil {
		t.Fatalf("unable to clear config from Galley: %v", err)
	}

	// TODO: This is because of subsequent events confusing the filesystem code.
	// We should do Ctrlz trigger based approach.
	time.Sleep(time.Second)

	if err = gal.ApplyConfig(nil, string(input)); err != nil {
		t.Fatalf("unable to apply config to Galley: %v", err)
	}

	for collection, e := range expected {
		validator := galley.GoldenValidatorFunc(e)

		if err = gal.WaitForSnapshot(collection, validator); err != nil {
			t.Fatalf("failed waiting for %s:\n%v\n", collection, err)
		}
	}
}

func TestMain(m *testing.M) {
	// TODO: Limit to Native environment until the Kubernetes environment is supported in the Galley
	// component

	framework.Main("galley_conversion", m, framework.RequireEnvironment(environment.Native))
}
