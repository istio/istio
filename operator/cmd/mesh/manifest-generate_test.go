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
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	gm "github.com/onsi/gomega"

	"istio.io/istio/operator/pkg/compare"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/httpserver"
	"istio.io/istio/operator/pkg/util/tgz"
	"istio.io/pkg/version"
)

const (
	istioTestVersion = "istio-1.5.0"
	testTGZFilename  = istioTestVersion + "-linux.tar.gz"
)

type testGroup []struct {
	desc string
	// Small changes to the input profile produce large changes to the golden output
	// files. This makes it difficult to spot meaningful changes in pull requests.
	// By default we hide these changes to make developers life's a bit easier. However,
	// it is still useful to sometimes override this behavior and show the full diff.
	// When this flag is true, use an alternative file suffix that is not hidden by
	// default github in pull requests.
	showOutputFileInPullRequest bool
	flags                       string
	noInput                     bool
	outputDir                   string
	diffSelect                  string
	diffIgnore                  string
	useCompiledInCharts         bool
}

func TestManifestGeneratePrometheus(t *testing.T) {
	testDataDir = filepath.Join(repoRootDir, "cmd/mesh/testdata/manifest-generate")
	g := gm.NewGomegaWithT(t)
	_, objs, err := generateManifest("prometheus", "", false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"ClusterRole::prometheus-istio-system",
		"ClusterRoleBinding::prometheus-istio-system",
		"ConfigMap:istio-system:prometheus",
		"Deployment:istio-system:prometheus",
		"Service:istio-system:prometheus",
		"ServiceAccount:istio-system:prometheus",
	}
	g.Expect(objectHashesOrdered(objs)).Should(gm.ConsistOf(want))
}

func TestManifestGenerateComponentHubTag(t *testing.T) {
	testDataDir = filepath.Join(repoRootDir, "cmd/mesh/testdata/manifest-generate")
	g := gm.NewGomegaWithT(t)
	m, _, err := generateManifest("component_hub_tag", "", false)
	if err != nil {
		t.Fatal(err)
	}
	g.Expect(filteredObjects(t, m, "Deployment:*:prometheus", "")).
		Should(HavePathValueEqual(PathValue{"spec.template.spec.containers.[name:prometheus].image", "docker.io/prometheus:1.1.1"}))
	g.Expect(filteredObjects(t, m, "Deployment:*:grafana", "")).
		Should(HavePathValueEqual(PathValue{"spec.template.spec.containers.[name:grafana].image", "grafana/grafana:6.5.2"}))
	g.Expect(filteredObjects(t, m, "Deployment:*:istio-ingressgateway", "")).
		Should(HavePathValueEqual(PathValue{"spec.template.spec.containers.[name:istio-proxy].image", "istio-spec.hub/proxyv2:istio-spec.tag"}))
	g.Expect(filteredObjects(t, m, "Deployment:*:istiod", "")).
		Should(HavePathValueEqual(PathValue{"spec.template.spec.containers.[name:discovery].image", "component.pilot.hub/pilot:2"}))
	g.Expect(filteredObjects(t, m, "Deployment:*:kiali", "")).
		Should(HavePathValueEqual(PathValue{"spec.template.spec.containers.[name:kiali].image", "docker.io/testing/kiali:v1.15"}))
}

func TestManifestGenerateFlags(t *testing.T) {
	flagOutputDir := createTempDirOrFail(t, "flag-output")
	flagOutputValuesDir := createTempDirOrFail(t, "flag-output-values")
	runTestGroup(t, testGroup{
		{
			desc: "all_off",
		},
		{
			desc:                        "all_on",
			diffIgnore:                  "ConfigMap:*:istio",
			showOutputFileInPullRequest: true,
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
			desc:       "flag_set_values",
			diffSelect: "Deployment:*:istio-ingressgateway,ConfigMap:*:istio-sidecar-injector",
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
			desc:       "flag_override_values",
			diffSelect: "Deployment:*:istiod",
			flags:      "-s tag=my-tag",
		},
		{
			desc:       "flag_output",
			flags:      "-o " + flagOutputDir,
			diffSelect: "Deployment:*:istiod",
			outputDir:  flagOutputDir,
		},
		{
			desc:       "flag_output_set_values",
			diffSelect: "Deployment:*:istio-ingressgateway",
			flags:      "-s values.global.proxy.image=mynewproxy -o " + flagOutputValuesDir,
			outputDir:  flagOutputValuesDir,
			noInput:    true,
		},
		{
			desc:       "flag_force",
			diffSelect: "no:resources:selected",
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
			desc:       "pilot_default",
			diffIgnore: "CustomResourceDefinition:*:*,ConfigMap:*:istio",
		},
		{
			desc:       "pilot_k8s_settings",
			diffSelect: "Deployment:*:istiod,HorizontalPodAutoscaler:*:istiod",
		},
		{
			desc:       "pilot_override_values",
			diffSelect: "Deployment:*:istiod",
		},
		{
			desc:       "pilot_override_kubernetes",
			diffSelect: "Deployment:*:istiod, Service:*:istiod",
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

func TestManifestGenerateAddonK8SOverride(t *testing.T) {
	runTestGroup(t, testGroup{
		{
			desc:       "addon_k8s_override",
			diffSelect: "Service:*:prometheus, Deployment:*:prometheus, Service:*:kiali",
		},
	})
}

// TestManifestGenerateHelmValues tests whether enabling components through the values passthrough interface works as
// expected i.e. without requiring enablement also in IstioOperator API.
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
	inPath := filepath.Join(testDataDir, "input/all_on.yaml")
	got1, err := runManifestGenerate([]string{inPath}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	got2, err := runManifestGenerate([]string{inPath}, "", false)
	if err != nil {
		t.Fatal(err)
	}

	if got1 != got2 {
		fmt.Printf("%s", util.YAMLDiff(got1, got2))
		t.Errorf("stable_manifest: Manifest generation is not producing stable text output.")
	}
}

func TestMultiICPSFiles(t *testing.T) {
	testDataDir = filepath.Join(repoRootDir, "cmd/mesh/testdata/manifest-generate")
	inPathBase := filepath.Join(testDataDir, "input/all_off.yaml")
	inPathOverride := filepath.Join(testDataDir, "input/telemetry_override_only.yaml")
	got, err := runManifestGenerate([]string{inPathBase, inPathOverride}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(testDataDir, "output/telemetry_override_values"+goldenFileSuffixHideChangesInReview)

	want, err := readFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	diffSelect := "handler:*:prometheus"
	got, err = compare.FilterManifest(got, diffSelect, "")
	if err != nil {
		t.Errorf("error selecting from output manifest: %v", err)
	}
	diff := compare.YAMLCmp(got, want)
	if diff != "" {
		t.Errorf("`manifest generate` diff = %s", diff)
	}
}

func TestBareSpec(t *testing.T) {
	testDataDir = filepath.Join(repoRootDir, "cmd/mesh/testdata/manifest-generate")
	inPathBase := filepath.Join(testDataDir, "input/bare_spec.yaml")
	_, err := runManifestGenerate([]string{inPathBase}, "", false)
	if err != nil {
		t.Fatal(err)
	}
}

func TestInstallPackagePath(t *testing.T) {
	testDataDir = filepath.Join(repoRootDir, "cmd/mesh/testdata/manifest-generate")
	releaseDir, err := createLocalReleaseCharts()
	defer os.RemoveAll(releaseDir)
	if err != nil {
		t.Fatal(err)
	}
	operatorArtifactDir := filepath.Join(releaseDir, istioTestVersion, helm.OperatorSubdirFilePath)
	serverDir, err := ioutil.TempDir(os.TempDir(), "istio-test-server-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(serverDir)
	if err := tgz.Create(releaseDir, filepath.Join(serverDir, testTGZFilename)); err != nil {
		t.Fatal(err)
	}
	srv := httpserver.NewServer(serverDir)
	runTestGroup(t, testGroup{
		{
			// Use some arbitrary small test input (pilot only) since we are testing the local filesystem code here, not
			// manifest generation.
			desc:       "install_package_path",
			diffSelect: "Deployment:*:istiod",
			flags:      "--set installPackagePath=" + operatorArtifactDir,
		},
		{
			// Specify both charts and profile from local filesystem.
			desc:       "install_package_path",
			diffSelect: "Deployment:*:istiod",
			flags:      fmt.Sprintf("--set installPackagePath=%s --set profile=%s/profiles/default.yaml", operatorArtifactDir, operatorArtifactDir),
		},
		{
			// --force is needed for version mismatch.
			desc:       "install_package_path",
			diffSelect: "Deployment:*:istiod",
			flags:      "--force --set installPackagePath=" + srv.URL() + "/" + testTGZFilename,
		},
	})

}

// This test enforces that objects that reference other objects do so properly, such as Service selecting deployment
func TestConfigSelectors(t *testing.T) {
	got, err := runManifestGenerate([]string{}, "", true)
	if err != nil {
		t.Fatal(err)
	}
	objs, err := object.ParseK8sObjectsFromYAMLManifest(got)
	if err != nil {
		t.Fatal(err)
	}
	gotRev, e := runManifestGenerate([]string{}, "--set revision=canary", true)
	if e != nil {
		t.Fatal(e)
	}
	objsRev, err := object.ParseK8sObjectsFromYAMLManifest(gotRev)
	if err != nil {
		t.Fatal(err)
	}

	// First we fetch all the objects for our default install
	name := "istiod"
	deployment := mustFindObject(t, objs, name, "Deployment")
	service := mustFindObject(t, objs, name, "Service")
	pdb := mustFindObject(t, objs, name, "PodDisruptionBudget")
	hpa := mustFindObject(t, objs, name, "HorizontalPodAutoscaler")
	podLabels := mustGetLabels(t, deployment, "spec.template.metadata.labels")
	// Check all selectors align
	mustSelect(t, mustGetLabels(t, pdb, "spec.selector.matchLabels"), podLabels)
	mustSelect(t, mustGetLabels(t, service, "spec.selector"), podLabels)
	mustSelect(t, mustGetLabels(t, deployment, "spec.selector.matchLabels"), podLabels)
	if hpaName := mustGetPath(t, hpa, "spec.scaleTargetRef.name"); name != hpaName {
		t.Fatalf("HPA does not match deployment: %v != %v", name, hpaName)
	}

	// Next we fetch all the objects for a revision install
	nameRev := "istiod-canary"
	deploymentRev := mustFindObject(t, objsRev, nameRev, "Deployment")
	serviceRev := mustFindObject(t, objsRev, nameRev, "Service")
	pdbRev := mustFindObject(t, objsRev, nameRev, "PodDisruptionBudget")
	hpaRev := mustFindObject(t, objsRev, nameRev, "HorizontalPodAutoscaler")
	podLabelsRev := mustGetLabels(t, deploymentRev, "spec.template.metadata.labels")
	// Check all selectors align for revision
	mustSelect(t, mustGetLabels(t, pdbRev, "spec.selector.matchLabels"), podLabelsRev)
	mustSelect(t, mustGetLabels(t, serviceRev, "spec.selector"), podLabelsRev)
	mustSelect(t, mustGetLabels(t, deploymentRev, "spec.selector.matchLabels"), podLabelsRev)
	if hpaName := mustGetPath(t, hpaRev, "spec.scaleTargetRef.name"); nameRev != hpaName {
		t.Fatalf("HPA does not match deployment: %v != %v", nameRev, hpaName)
	}

	// Make sure default and revisions do not cross
	mustNotSelect(t, mustGetLabels(t, serviceRev, "spec.selector"), podLabels)
	mustNotSelect(t, mustGetLabels(t, service, "spec.selector"), podLabelsRev)
	mustNotSelect(t, mustGetLabels(t, pdbRev, "spec.selector.matchLabels"), podLabels)
	mustNotSelect(t, mustGetLabels(t, pdb, "spec.selector.matchLabels"), podLabelsRev)

	// Check selection of previous versions . This only matters for in place upgrade (non revision)
	podLabels15 := map[string]string{
		"app":   "istiod",
		"istio": "pilot",
	}
	mustSelect(t, mustGetLabels(t, service, "spec.selector"), podLabels15)
	mustNotSelect(t, mustGetLabels(t, serviceRev, "spec.selector"), podLabels15)
	mustSelect(t, mustGetLabels(t, pdb, "spec.selector.matchLabels"), podLabels15)
	mustNotSelect(t, mustGetLabels(t, pdbRev, "spec.selector.matchLabels"), podLabels15)

	// Check we aren't changing immutable fields. This only matters for in place upgrade (non revision)
	// This one is not a selector, it must be an exact match
	deploymentSelector15 := map[string]string{
		"istio": "pilot",
	}
	if sel := mustGetLabels(t, deployment, "spec.selector.matchLabels"); !reflect.DeepEqual(deploymentSelector15, sel) {
		t.Fatalf("Depployment selectors are immutable, but changed since 1.5. Was %v, now is %v", deploymentSelector15, sel)
	}
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

func generateManifest(inFile, flags string, useCompiledInCharts bool) (string, object.K8sObjects, error) {
	inPath := filepath.Join(repoRootDir, "cmd/mesh/testdata/manifest-generate/input", inFile+".yaml")
	manifest, err := runManifestGenerate([]string{inPath}, flags, useCompiledInCharts)
	if err != nil {
		return "", nil, fmt.Errorf("error %s: %s", err, manifest)
	}
	objs, err := object.ParseK8sObjectsFromYAMLManifest(manifest)
	return manifest, objs, err
}

func runTestGroup(t *testing.T, tests testGroup) {
	testDataDir = filepath.Join(repoRootDir, "cmd/mesh/testdata/manifest-generate")
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			inPath := filepath.Join(testDataDir, "input", tt.desc+".yaml")
			outputSuffix := goldenFileSuffixHideChangesInReview
			if tt.showOutputFileInPullRequest {
				outputSuffix = goldenFileSuffixShowChangesInReview
			}
			outPath := filepath.Join(testDataDir, "output", tt.desc+outputSuffix)

			var filenames []string
			if !tt.noInput {
				filenames = []string{inPath}
			}

			got, err := runManifestGenerate(filenames, tt.flags, tt.useCompiledInCharts)
			if err != nil {
				t.Fatal(err)
			}

			if tt.outputDir != "" {
				got, err = util.ReadFilesWithFilter(tt.outputDir, func(fileName string) bool {
					return strings.HasSuffix(fileName, ".yaml")
				})
				if err != nil {
					t.Fatal(err)
				}
			}

			diffSelect := "*:*:*"
			if tt.diffSelect != "" {
				diffSelect = tt.diffSelect
				got, err = compare.FilterManifest(got, diffSelect, "")
				if err != nil {
					t.Errorf("error selecting from output manifest: %v", err)
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
func runManifestGenerate(filenames []string, flags string, useCompiledInCharts bool) (string, error) {
	args := "manifest generate"
	for _, f := range filenames {
		args += " -f " + f
	}
	if flags != "" {
		args += " " + flags
	}
	if !useCompiledInCharts {
		args += " --set installPackagePath=" + filepath.Join(testDataDir, "data-snapshot")
	}
	return runCommand(args)
}

func createLocalReleaseCharts() (string, error) {
	releaseDir, err := ioutil.TempDir(os.TempDir(), "istio-test-release-*")
	if err != nil {
		return "", err
	}
	releaseSubDir := filepath.Join(releaseDir, istioTestVersion, helm.OperatorSubdirFilePath)
	cmd := exec.Command("../../release/create_release_charts.sh", "-o", releaseSubDir)
	if stdo, err := cmd.Output(); err != nil {
		return "", fmt.Errorf("%s: \n%s", err, string(stdo))
	}
	return releaseDir, nil
}
