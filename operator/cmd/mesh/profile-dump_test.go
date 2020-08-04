// Copyright Istio Authors
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
	"regexp"
	"testing"

	"github.com/kylelemons/godebug/diff"

	"istio.io/istio/operator/pkg/util"
)

func TestProfileDump(t *testing.T) {
	testDataDir = filepath.Join(operatorRootDir, "cmd/mesh/testdata/profile-dump")
	tests := []struct {
		desc       string
		configPath string
	}{
		{
			desc: "all_off",
		},
		{
			desc:       "config_path",
			configPath: "components",
		},
	}
	installPackagePathRegex := regexp.MustCompile("  installPackagePath: .*")
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			inPath := filepath.Join(testDataDir, "input", tt.desc+".yaml")
			outPath := filepath.Join(testDataDir, "output", tt.desc+".yaml")

			got, err := runProfileDump(inPath, tt.configPath, snapshotCharts, "")
			if err != nil {
				t.Fatal(err)
			}
			// installPackagePath may change, we will remove it for consistent output
			got = installPackagePathRegex.ReplaceAllString(got, "")

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
				t.Errorf("profile-dump command(%s): got:\n%s\n\nwant:\n%s\nDiff:\n%s\n", tt.desc, got, want, util.YAMLDiff(got, want))
			}
		})
	}
}

func runProfileDump(profilePath, configPath string, chartSource chartSourceType, outfmt string) (string, error) {
	cmd := "profile dump -f " + profilePath
	if configPath != "" {
		cmd += " --config-path " + configPath
	}
	if len(chartSource) > 0 {
		cmd += " --manifests=" + string(chartSource)
	}
	if outfmt != "" {
		cmd += " --output=" + outfmt
	}
	return runCommand(cmd)
}

func TestProfileDumpFlags(t *testing.T) {
	testDataDir = filepath.Join(operatorRootDir, "cmd/mesh/testdata/profile-dump")
	tests := []struct {
		desc       string
		configPath string
	}{
		{
			desc: "all_off",
		},
		{
			desc:       "config_path",
			configPath: "components",
		},
	}
	installPackagePathRegex := regexp.MustCompile("(?m)^installPackagePath=\".*\"\n")
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			inPath := filepath.Join(testDataDir, "input", tt.desc+".yaml")
			outPath := filepath.Join(testDataDir, "output", tt.desc+".txt")

			got, err := runProfileDump(inPath, tt.configPath, snapshotCharts, "flags")
			if err != nil {
				t.Fatal(err)
			}
			// installPackagePath may change, we will remove it for consistent output
			got = installPackagePathRegex.ReplaceAllString(got, "")

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
			if got != want {
				t.Errorf("profile-dump command(%s): got:\n%s\n\nwant:\n%s\nDiff:\n%s\n", tt.desc, got, want, diff.Diff(got, want))
			}
		})
	}
}
