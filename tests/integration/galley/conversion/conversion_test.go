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

	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"

	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/testing/testdata"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/util/structpath"
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

			framework.Run(t, func(ctx framework.TestContext) {

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

func runTest(t *testing.T, ctx resource.Context, fset *testdata.FileSet, gal galley.Instance) {
	input, err := fset.LoadInputFile()
	if err != nil {
		t.Fatalf("Unable to load input test data: %v", err)
	}

	ns := namespace.NewOrFail(t, ctx, "conv", true)

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
		var validator galley.SnapshotValidatorFunc

		switch collection {
		case metadata.IstioNetworkingV1alpha3SyntheticServiceentries.Collection.String():
			// The synthetic service entry includes the resource versions for service and
			// endpoints as annotations, which are volatile. This prevents us from using
			// golden files for validation. Instead, we use the structpath library to
			// validate the fields manually.
			validator = syntheticServiceEntryValidator(ns.Name())
		default:
			// All other collections use golden files.
			validator = galley.NewGoldenSnapshotValidator(ns.Name(), e)
		}

		if err = gal.WaitForSnapshot(collection, validator); err != nil {
			t.Fatalf("failed waiting for %s:\n%v\n", collection, err)
		}
	}
}

func syntheticServiceEntryValidator(ns string) galley.SnapshotValidatorFunc {
	return galley.NewSingleObjectSnapshotValidator(ns, func(ns string, actual *galley.SnapshotObject) error {
		v := structpath.ForProto(actual)
		if err := v.Equals(metadata.IstioNetworkingV1alpha3SyntheticServiceentries.TypeURL.String(), "{.TypeURL}").
			Equals(fmt.Sprintf("%s/kube-dns", ns), "{.Metadata.name}").
			Check(); err != nil {
			return err
		}

		if err := v.Select("{.Metadata.annotations}").
			Exists("{.['networking.istio.io/serviceVersion']}").
			Exists("{.['networking.istio.io/endpointsVersion']}").
			Check(); err != nil {
			return err
		}

		// Compare the body
		if err := v.Select("{.Body}").
			Equals("10.43.240.10", "{.addresses[0]}").
			Equals(fmt.Sprintf("kube-dns.%s.svc.cluster.local", ns), "{.hosts[0]}").
			Equals(1, "{.location}").
			Equals(1, "{.resolution}").
			Equals(fmt.Sprintf("spiffe://cluster.local/ns/%s/sa/kube-dns", ns), "{.subject_alt_names[0]}").
			Check(); err != nil {
			return err
		}

		// Compare Ports
		if err := v.Select("{.Body.ports[0]}").
			Equals("dns", "{.name}").
			Equals(53, "{.number}").
			Equals("UDP", "{.protocol}").
			Check(); err != nil {
			return err
		}

		if err := v.Select("{.Body.ports[1]}").
			Equals("dns-tcp", "{.name}").
			Equals(53, "{.number}").
			Equals("TCP", "{.protocol}").
			Check(); err != nil {
			return err
		}

		// Compare Endpoints
		if err := v.Select("{.Body.endpoints[0]}").
			Equals("10.40.0.5", "{.address}").
			Equals("us-central1/us-central1-a", "{.locality}").
			Equals(53, "{.ports['dns']}").
			Equals(53, "{.ports['dns-tcp']}").
			Equals("kube-dns", "{.labels['k8s-app']}").
			Equals("123", "{.labels['pod-template-hash']}").
			Check(); err != nil {
			return err
		}

		if err := v.Select("{.Body.endpoints[1]}").
			Equals("10.40.1.4", "{.address}").
			Equals("us-central1/us-central1-a", "{.locality}").
			Equals(53, "{.ports['dns']}").
			Equals(53, "{.ports['dns-tcp']}").
			Equals("kube-dns", "{.labels['k8s-app']}").
			Equals("456", "{.labels['pod-template-hash']}").
			Check(); err != nil {
			return err
		}

		return nil
	})
}

func TestMain(m *testing.M) {
	// TODO: Limit to Native environment until the Kubernetes environment is supported in the Galley
	// component
	framework.
		NewSuite("galley_conversion", m).
		Label(label.Presubmit).
		RequireEnvironment(environment.Native).
		Run()
}
