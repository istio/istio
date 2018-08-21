// Copyright 2017 Istio Authors.
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

package main

import (
	"io"
	"os"
	"testing"

	"istio.io/istio/pilot/test/util"
)

func TestConvertIngress(t *testing.T) {
	tt := []struct {
		in  []string
		out string
	}{
		// Verify we can convert Kubernetes Istio Ingress
		{in: []string{"myservice-ingress.yaml"},
			out: "myservice-gateway.yaml"},

		// Verify we can merge Ingresses
		{in: []string{"myservice-ingress.yaml", "another-ingress.yaml"},
			out: "merged-gateway.yaml"},
	}

	for _, tc := range tt {
		t.Run(tc.out, func(t *testing.T) {
			readers := make([]io.Reader, 0, len(tc.in))
			for _, filename := range tc.in {
				file, err := os.Open("testdata/ingress/" + filename)
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				readers = append(readers, file)
			}

			outFilename := "testdata/v1alpha3/" + tc.out
			out, err := os.Create(outFilename)
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()

			if err := convertConfigs(readers, out); err != nil {
				t.Fatalf("Unexpected error converting configs: %v", err)
			}

			util.CompareYAML(outFilename, t)
		})
	}
}
