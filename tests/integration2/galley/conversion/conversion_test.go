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
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/api/components"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/ids"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/tests/integration2/galley/conversion/testdata"
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

			// Call Requires to explicitly initialize dependencies that the test needs.
			ctx := framework.GetContext(t)

			// TODO: Limit to Native environment until the Kubernetes environment is supported in the Galley
			// component
			ctx.Require(lifecycle.Suite, &descriptors.NativeEnvironment)

			ctx.Require(lifecycle.Test, &ids.Galley)

			gal := components.GetGalley(ctx, t)

			input, err := d.LoadInputFile()
			if err != nil {
				t.Fatalf("Unable to load input test data: %v", err)
			}

			if d.HasMeshConfigFile() {
				mc, err := d.LoadMeshConfigFile()
				if err != nil {
					t.Fatalf("Error loading Mesh config file: %v", err)
				}
				if err = gal.SetMeshConfig(string(mc)); err != nil {
					t.Fatalf("Error setting Mesh config file: %v", err)
				}
			}

			//expectedJSON, err := d.LoadExpectedFile()
			//if err != nil {
			//	t.Fatalf("unable to load expectedTypes test data: %v", err)
			//}
			//expected, err := createTypeURLMap(expectedJSON)
			//if err != nil {
			//	t.Fatal(err)
			//}

			expected, err := d.LoadExpectedResources()
			if err != nil {
				t.Fatalf("unable to load expected resources: %v", err)
			}

			if err = gal.ApplyConfig(string(input)); err != nil {
				t.Fatalf("unable to apply config to Galley: %v", err)
			}

			for typeURL, e := range expected {
				if err = gal.WaitForSnapshot(typeURL, e...); err != nil {
					t.Errorf("Error waiting for %s:\n%v\n", typeURL, err)
				}
			}
		})
	}
}
//
//// create a mapping from type URLs to the array of corresponding resources.
//func createTypeURLMap(js []byte) (map[string][]map[string]interface{}, error) {
//	var expectedArray []interface{}
//
//	if err := json.Unmarshal(js, &expectedArray); err != nil {
//		return nil, fmt.Errorf("error parsing expectedTypes JSON: %v", err)
//	}
//
//	m := make(map[string][]map[string]interface{})
//	for _, e := range expectedArray {
//		exp := e.(map[string]interface{})
//
//		typeURL := exp["TypeURL"].(string)
//		arr := m[typeURL]
//		arr = append(arr, exp)
//		m[typeURL] = arr
//	}
//
//	return m, nil
//}
