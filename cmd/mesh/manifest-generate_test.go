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

package mesh

import (
	"path/filepath"
	"testing"

	"istio.io/operator/pkg/object"
)

func TestManifestGenerate(t *testing.T) {
	testDataDir = filepath.Join(repoRootDir, "cmd/mesh/testdata/manifest-generate")
	tests := []struct {
		desc       string
		diffSelect string
		diffIgnore string
	}{
		{
			desc: "all_off",
		},
		{
			desc: "pilot_default",
			// TODO: remove istio-control
			diffIgnore: "CustomResourceDefinition:*:*,ConfigMap:*:istio",
		},
		{
			desc:       "pilot_k8s_settings",
			diffIgnore: "CustomResourceDefinition:*:*,ConfigMap:*:istio",
		},
		{
			desc:       "pilot_override_values",
			diffSelect: "Deployment:*:istio-pilot",
		},
		{
			desc:       "pilot_override_kubernetes",
			diffSelect: "Deployment:*:istio-pilot, Service:*:istio-pilot",
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			inPath := filepath.Join(testDataDir, "input", tt.desc+".yaml")
			outPath := filepath.Join(testDataDir, "output", tt.desc+".yaml")

			got, err := runManifestGenerate(inPath)
			if err != nil {
				t.Fatal(err)
			}

			want, err := readFile(outPath)
			if err != nil {
				t.Fatal(err)
			}

			diffSelect := "*:*:*"
			if tt.diffSelect != "" {
				diffSelect = tt.diffSelect
			}
			diff, err := object.ManifestDiffWithSelectAndIgnore(got, want, diffSelect, tt.diffIgnore)
			if err != nil {
				t.Fatal(err)
			}
			if diff != "" {
				t.Errorf("%s: got:\n%s\nwant:\n%s\n(-got, +want)\n%s\n", tt.desc, "", "", diff)
			}

		})
	}
}

func runManifestGenerate(path string) (string, error) {
	return runCommand("manifest generate -f " + path)
}
