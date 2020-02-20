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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"istio.io/istio/operator/pkg/compare"
	"istio.io/istio/operator/pkg/util"
	"istio.io/pkg/version"
)

type testGroup []struct {
	desc       string
	flags      string
	noInput    bool
	outputDir  string
	diffSelect string
	diffIgnore string
}

func TestManifestGenerateFlags(t *testing.T) {
	flagOutputDir := createTempDirOrFail(t, "flag-output")
	flagOutputValuesDir := createTempDirOrFail(t, "flag-output-values")
	runTestGroup(t, testGroup{
		{
			desc: "all_off",
		},
		{
			desc:       "all_on",
			diffIgnore: "ConfigMap:*:istio",
		},
		{
			desc:       "prometheus",
			diffIgnore: "ConfigMap:*:istio",
		},
		{
			desc:       "gateways",
			diffIgnore: "ConfigMap:*:istio",
		},
		{
			desc:       "gateways_override_default",
			diffIgnore: "ConfigMap:*:istio",
		},
		{
			desc:       "component_hub_tag",
			diffIgnore: "ConfigMap:*:istio",
		},
		{
			desc:       "flag_set_values",
			diffIgnore: "ConfigMap:*:istio",
			flags:      "-s values.global.proxy.image=myproxy --set values.global.proxy.includeIPRanges=172.30.0.0/16,172.21.0.0/16",
			noInput:    true,
		},
		{
			desc:       "flag_values_enable_egressgateway",
			diffSelect: "Service:*:istio-egressgateway",
			flags:      "--set values.gateways.istio-egressgateway.enabled=true",
			noInput:    true,
		},
		{
			desc:  "flag_override_values",
			flags: "-s tag=my-tag",
		},
		{
			desc:      "flag_output",
			flags:     "-o " + flagOutputDir,
			outputDir: flagOutputDir,
		},
		{
			desc:       "flag_output_set_values",
			diffIgnore: "ConfigMap:*:istio",
			flags:      "-s values.global.proxy.image=mynewproxy -o " + flagOutputValuesDir,
			outputDir:  flagOutputValuesDir,
			noInput:    true,
		},
		{
			desc:       "flag_force",
			diffIgnore: "ConfigMap:*:istio",
			flags:      "--force",
		},
		{
			desc:       "flag_output_set_profile",
			diffIgnore: "ConfigMap:*:istio",
			flags:      "-s profile=minimal",
			noInput:    true,
		},
	})
	removeDirOrFail(t, flagOutputDir)
	removeDirOrFail(t, flagOutputValuesDir)
}

func TestManifestGeneratePilot(t *testing.T) {
	runTestGroup(t, testGroup{
		{
			desc: "pilot_default",
			// TODO: remove istio ConfigMap (istio/istio#16828)
			diffIgnore: "CustomResourceDefinition:*:*,ConfigMap:*:istio",
		},
		{
			desc:       "pilot_k8s_settings",
			diffIgnore: "CustomResourceDefinition:*:*,ConfigMap:*:istio",
		},
		{
			desc:       "pilot_override_values",
			diffSelect: "Deployment:*:istiod",
		},
		{
			desc:       "pilot_override_kubernetes",
			diffSelect: "Deployment:*:istiod, Service:*:istio-pilot",
		},
		{
			desc:       "pilot_merge_meshconfig",
			diffSelect: "ConfigMap:*:istio$",
		},
	})
}

func TestManifestGenerateTelemetry(t *testing.T) {
	runTestGroup(t, testGroup{
		{
			desc: "all_off",
		},
		{
			desc:       "telemetry_default",
			diffIgnore: "",
		},
		{
			desc:       "telemetry_k8s_settings",
			diffSelect: "Deployment:*:istio-telemetry, HorizontalPodAutoscaler:*:istio-telemetry",
		},
		{
			desc:       "telemetry_override_values",
			diffSelect: "handler:*:prometheus",
		},
		{
			desc:       "telemetry_override_kubernetes",
			diffSelect: "Deployment:*:istio-telemetry, handler:*:prometheus",
		},
	})
}

func TestManifestGenerateGateway(t *testing.T) {
	runTestGroup(t, testGroup{
		{
			desc:       "ingressgateway_k8s_settings",
			diffSelect: "Deployment:*:istio-ingressgateway, Service:*:istio-ingressgateway",
		},
	})
}

func TestManifestGenerateHelmValues(t *testing.T) {
	runTestGroup(t, testGroup{
		{
			desc: "helm_values_enablement",
			diffSelect: "Deployment:*:istio-egressgateway, Service:*:istio-egressgateway," +
				" Deployment:*:kiali, Service:*:kiali, Deployment:*:prometheus, Service:*:prometheus",
		},
	})
}

func TestManifestGenerateOrdered(t *testing.T) {
	testDataDir = filepath.Join(repoRootDir, "cmd/mesh/testdata/manifest-generate")
	// Since this is testing the special case of stable YAML output order, it
	// does not use the established test group pattern
	t.Run("stable_manifest", func(t *testing.T) {
		inPath := filepath.Join(testDataDir, "input/all_on.yaml")
		got1, err := runManifestGenerate([]string{inPath}, "")
		if err != nil {
			t.Fatal(err)
		}
		got2, err := runManifestGenerate([]string{inPath}, "")
		if err != nil {
			t.Fatal(err)
		}

		if got1 != got2 {
			fmt.Printf("%s", util.YAMLDiff(got1, got2))
			t.Errorf("stable_manifest: Manifest generation is not producing stable text output.")
		}
	})
}

func TestMultiICPSFiles(t *testing.T) {
	testDataDir = filepath.Join(repoRootDir, "cmd/mesh/testdata/manifest-generate")
	t.Run("multi-ICPS files", func(t *testing.T) {
		inPathBase := filepath.Join(testDataDir, "input/all_off.yaml")
		inPathOverride := filepath.Join(testDataDir, "input/telemetry_override_only.yaml")
		got, err := runManifestGenerate([]string{inPathBase, inPathOverride}, "")
		if err != nil {
			t.Fatal(err)
		}
		outPath := filepath.Join(testDataDir, "output/telemetry_override_values.yaml")

		want, err := readFile(outPath)
		if err != nil {
			t.Fatal(err)
		}
		diffSelect := "handler:*:prometheus"
		got, err = compare.SelectAndIgnoreFromOutput(got, diffSelect, "")
		if err != nil {
			t.Errorf("error selecting from output manifest: %v", err)
		}
		diff := compare.YAMLCmp(got, want)
		if diff != "" {
			t.Errorf("`manifest generate` diff = %s", diff)
		}
	})
}

// TestLDFlags checks whether building mesh command with
// -ldflags "-X istio.io/pkg/version.buildHub=myhub -X istio.io/pkg/version.buildVersion=mytag"
// results in these values showing up in a generated manifest.
func TestLDFlags(t *testing.T) {
	tmpHub, tmpTag := version.DockerInfo.Hub, version.DockerInfo.Tag
	defer func() {
		version.DockerInfo.Hub, version.DockerInfo.Tag = tmpHub, tmpTag
	}()
	version.DockerInfo.Hub = "testHub"
	version.DockerInfo.Tag = "testTag"
	l := NewLogger(true, os.Stdout, os.Stderr)
	_, iops, err := GenerateConfig(nil, "", true, nil, l)
	if err != nil {
		t.Fatal(err)
	}
	if iops.Hub != version.DockerInfo.Hub || iops.Tag != version.DockerInfo.Tag {
		t.Fatalf("DockerInfoHub, DockerInfoTag got: %s,%s, want: %s, %s", iops.Hub, iops.Tag, version.DockerInfo.Hub, version.DockerInfo.Tag)
	}
}

func runTestGroup(t *testing.T, tests testGroup) {
	testDataDir = filepath.Join(repoRootDir, "cmd/mesh/testdata/manifest-generate")
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			inPath := filepath.Join(testDataDir, "input", tt.desc+".yaml")
			outPath := filepath.Join(testDataDir, "output", tt.desc+".yaml")

			var filenames []string
			if !tt.noInput {
				filenames = []string{inPath}
			}

			got, err := runManifestGenerate(filenames, tt.flags)
			if err != nil {
				t.Fatal(err)
			}

			diffSelect := "*:*:*"
			if tt.diffSelect != "" {
				diffSelect = tt.diffSelect
				got, err = compare.SelectAndIgnoreFromOutput(got, diffSelect, "")
				if err != nil {
					t.Errorf("error selecting from output manifest: %v", err)
				}
			}

			if tt.outputDir != "" {
				got, err = util.ReadFilesWithFilter(tt.outputDir, func(fileName string) bool {
					return strings.HasSuffix(fileName, ".yaml")
				})
				if err != nil {
					t.Fatal(err)
				}
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

			for _, v := range []bool{true, false} {
				diff, err := compare.ManifestDiffWithRenameSelectIgnore(got, want,
					"", diffSelect, tt.diffIgnore, v)
				if err != nil {
					t.Fatal(err)
				}
				if diff != "" {
					t.Errorf("%s: got:\n%s\nwant:\n%s\n(-got, +want)\n%s\n", tt.desc, "", "", diff)
				}
			}

		})
	}
}

// runManifestGenerate runs the manifest generate command. If filenames is set, passes the given filenames as -f flag,
// flags is passed to the command verbatim. If you set both flags and path, make sure to not use -f in flags.
func runManifestGenerate(filenames []string, flags string) (string, error) {
	args := "manifest generate"
	for _, f := range filenames {
		args += " -f " + f
	}
	if flags != "" {
		args += " " + flags
	}
	return runCommand(args)
}

func createTempDirOrFail(t *testing.T, prefix string) string {
	dir, err := ioutil.TempDir("", prefix)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func removeDirOrFail(t *testing.T, path string) {
	err := os.RemoveAll(path)
	if err != nil {
		t.Fatal(err)
	}
}
