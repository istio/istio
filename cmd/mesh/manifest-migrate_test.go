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
	"io/ioutil"
	"path/filepath"
	"testing"

	"istio.io/operator/pkg/util"
)

func TestManifestMigrate(t *testing.T) {
	testDataDir = filepath.Join(repoRootDir, "cmd/mesh/testdata/manifest-migrate")
	tests := []struct {
		desc string
	}{
		{
			desc: "default_values",
		},
		{
			desc: "overlay_values",
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			inPath := filepath.Join(testDataDir, "input", tt.desc+".yaml")
			outPath := filepath.Join(testDataDir, "output", tt.desc+".yaml")

			got, err := runManifestMigrate(inPath)
			if err != nil {
				t.Fatal(err)
			}

			if refreshGoldenFiles() {
				t.Logf("Refreshing golden file for %s", outPath)
				if err := ioutil.WriteFile(outPath, []byte(got), 0644); err != nil {
					t.Error(err)
				}
			}

			want, err := readFile(outPath)
			if err != nil {
				t.Fatal(err)
			}
			if !util.IsYAMLEqual(got, want) {
				t.Errorf("manifest-migrate command(%s): got:\n%s\n\nwant:\n%s\nDiff:\n%s\n", tt.desc, got, want, util.YAMLDiff(got, want))
			}
		})
	}
}

func runManifestMigrate(path string) (string, error) {
	return runCommand("manifest migrate --logtostderr " + path)
}
