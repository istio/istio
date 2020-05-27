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
	"reflect"
	"strings"
	"testing"

	"istio.io/istio/operator/pkg/object"

	"istio.io/istio/operator/pkg/compare"
	"istio.io/istio/operator/pkg/util"
	"istio.io/pkg/version"

	. "github.com/onsi/gomega"
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

func TestManifestGenerateGateways(t *testing.T) {
	testDataDir = filepath.Join(repoRootDir, "cmd/mesh/testdata/manifest-generate")
	g := NewGomegaWithT(t)
	m, _, err := generateManifest("gateways", "")
	if err != nil {
		t.Fatal(err)
	}
	objs := parseObjectSetFromManifest(t, m)
	g.Expect(objs.kind(hpaStr).size()).Should(Equal(3))
	g.Expect(objs.kind(pdbStr).size()).Should(Equal(3))
	g.Expect(objs.kind(serviceStr).size()).Should(Equal(3))
	g.Expect(objs.kind(roleStr).size()).Should(Equal(3))
	g.Expect(objs.kind(roleBindingStr).size()).Should(Equal(3))
	g.Expect(objs.kind(saStr).size()).Should(Equal(3))

	dobj := mustGetDeployment(g, objs, "istio-ingressgateway")
	d := dobj.Unstructured()
	c := dobj.Container("istio-proxy")
	g.Expect(d).Should(HavePathValueContain(PathValue{"metadata.labels", toMap("aaa:aaa-val,bbb:bbb-val")}))
	g.Expect(c).Should(HavePathValueEqual(PathValue{"resources.requests.cpu", "111m"}))

	dobj = mustGetDeployment(g, objs, "user-ingressgateway")
	d = dobj.Unstructured()
	c = dobj.Container("istio-proxy")
	g.Expect(d).Should(HavePathValueContain(PathValue{"metadata.labels", toMap("ccc:ccc-val,ddd:ddd-val")}))
	g.Expect(c).Should(HavePathValueEqual(PathValue{"resources.requests.cpu", "222m"}))

	dobj = mustGetDeployment(g, objs, "ilb-gateway")
	d = dobj.Unstructured()
	c = dobj.Container("istio-proxy")
	s := mustGetService(g, objs, "ilb-gateway").Unstructured()
	g.Expect(d).Should(HavePathValueEqual(PathValue{"metadata.labels", toMap("app:istio-ingressgateway,istio:ingressgateway,release: istio")}))
	g.Expect(c).Should(HavePathValueEqual(PathValue{"resources.requests.cpu", "333m"}))
	g.Expect(c).Should(HavePathValueEqual(PathValue{"volumeMounts.[name:ilbgateway-certs].name", "ilbgateway-certs"}))
	g.Expect(s).Should(HavePathValueEqual(PathValue{"metadata.annotations", toMap("cloud.google.com/load-balancer-type: internal")}))
	g.Expect(s).Should(HavePathValueEqual(PathValue{"spec.ports.[0]", portVal("grpc-pilot-mtls", 15011, -1)}))
	g.Expect(s).Should(HavePathValueEqual(PathValue{"spec.ports.[1]", portVal("tcp-citadel-grpc-tls", 8060, 8060)}))
	g.Expect(s).Should(HavePathValueEqual(PathValue{"spec.ports.[2]", portVal("tcp-dns", 5353, -1)}))

	for _, o := range objs.kind(hpaStr).objSlice {
		ou := o.Unstructured()
		g.Expect(ou).Should(HavePathValueEqual(PathValue{"spec.minReplicas", int64(1)}))
		g.Expect(ou).Should(HavePathValueEqual(PathValue{"spec.maxReplicas", int64(5)}))
	}

	checkRoleBindingsReferenceRoles(g, objs)
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
			diffSelect: "Deployment:*:*",
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
}

func TestBareSpec(t *testing.T) {
	testDataDir = filepath.Join(repoRootDir, "cmd/mesh/testdata/manifest-generate")
	inPathBase := filepath.Join(testDataDir, "input/bare_spec.yaml")
	_, err := runManifestGenerate([]string{inPathBase}, "", false)
	if err != nil {
		t.Fatal(err)
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

	// First we fetch all the objects for our default install
	name := "istiod"
	deployment := mustFindObject(t, objs, name, deploymentStr)
	service := mustFindObject(t, objs, name, serviceStr)
	pdb := mustFindObject(t, objs, name, pdbStr)
	hpa := mustFindObject(t, objs, name, hpaStr)
	podLabels := mustGetLabels(t, deployment, "spec.template.metadata.labels")

	// Check all selectors align
	mustSelect(t, mustGetLabels(t, pdb, "spec.selector.matchLabels"), podLabels)
	mustSelect(t, mustGetLabels(t, service, "spec.selector"), podLabels)
	mustSelect(t, mustGetLabels(t, deployment, "spec.selector.matchLabels"), podLabels)
	if hpaName := mustGetPath(t, hpa, "spec.scaleTargetRef.name"); name != hpaName {
		t.Fatalf("HPA does not match deployment: %v != %v", name, hpaName)
	}

	// Check selection of previous versions . This only matters for in place upgrade (non revision)
	podLabels15 := map[string]string{
		"app":   "istiod",
		"istio": "pilot",
	}
	mustSelect(t, mustGetLabels(t, service, "spec.selector"), podLabels15)
	mustSelect(t, mustGetLabels(t, pdb, "spec.selector.matchLabels"), podLabels15)

	// Check we aren't changing immutable fields. This one is not a selector, it must be an exact match
	deploymentSelector14 := map[string]string{
		"istio": "pilot",
	}
	if sel := mustGetLabels(t, deployment, "spec.selector.matchLabels"); !reflect.DeepEqual(deploymentSelector14, sel) {
		t.Fatalf("Depployment selectors are immutable, but changed since 1.4. Was %v, now is %v", deploymentSelector14, sel)
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
				got, err = compare.SelectAndIgnoreFromOutput(got, diffSelect, "")
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

func generateManifest(inFile, flags string) (string, object.K8sObjects, error) {
	inPath := filepath.Join(repoRootDir, "cmd/mesh/testdata/manifest-generate/input", inFile+".yaml")
	manifest, err := runManifestGenerate([]string{inPath}, flags, true)
	if err != nil {
		return "", nil, fmt.Errorf("error %s: %s", err, manifest)
	}
	objs, err := object.ParseK8sObjectsFromYAMLManifest(manifest)
	return manifest, objs, err
}
