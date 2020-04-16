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
	"fmt"
	"testing"
	"time"

	"istio.io/istio/galley/testdatasets/conversion"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/environment"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/galley"
)

func TestConversion(t *testing.T) {
	dataset, err := conversion.Load()
	if err != nil {
		t.Fatalf("Error loading data set: %v", err)
	}

	for _, d := range dataset {
		t.Run(d.TestName(), func(t *testing.T) {
			if d.Skipped {
				t.SkipNow()
				return
			}

			// TODO: Limit to Native environment until the Kubernetes environment is supported in the Galley
			// component

			framework.NewTest(t).
				RequiresEnvironment(environment.Native).
				Run(func(ctx framework.TestContext) {

					var gal galley.Instance
					var cfg galley.Config

					for i, fset := range d.FileSets() {
						// Do init for the first set. Use Meshconfig file in this set.
						if i == 0 {
							if fset.HasMeshConfigFile() {
								mc, er := fset.LoadMeshConfigFile()
								if er != nil {
									t.Fatalf("Error loading Mesh config file: %v", er)
								}

								cfg.MeshConfig = string(mc)
							}

							gal = galley.NewOrFail(t, ctx, cfg)
						}

						t.Logf("==== Running iter: %d\n", i)
						if len(d.FileSets()) == 1 {
							runTest(t, ctx, fset, gal)
						} else {
							testName := fmt.Sprintf("%d", i)
							t.Run(testName, func(t *testing.T) {
								runTest(t, ctx, fset, gal)
							})
						}
					}
				})
		})
	}
}

func runTest(t *testing.T, ctx resource.Context, fset *conversion.FileSet, gal galley.Instance) {
	input, err := fset.LoadInputFile()
	if err != nil {
		t.Fatalf("Unable to load input test data: %v", err)
	}

	ns := namespace.NewOrFail(t, ctx, namespace.Config{
		Prefix: "conv",
		Inject: true,
	})

	expected, err := fset.LoadExpectedResources(ns.Name())
	if err != nil {
		t.Fatalf("unable to load expected resources: %v", err)
	}

	if err = gal.ClearConfig(); err != nil {
		t.Fatalf("unable to clear config from Galley: %v", err)
	}

	// TODO: This is because of subsequent events confusing the filesystem code.
	// We should do Ctrlz trigger based approach.
	time.Sleep(time.Second)

	if err = gal.ApplyConfig(ns, string(input)); err != nil {
		t.Fatalf("unable to apply config to Galley: %v", err)
	}

	for collection, e := range expected {
		validator := galley.NewGoldenSnapshotValidator(e)
		if err = gal.WaitForSnapshot(collection, validator); err != nil {
			t.Fatalf("failed waiting for %s:\n%v\n", collection, err)
		}
	}
}
