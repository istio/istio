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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	v1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"

	"istio.io/istio/operator/pkg/compare"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/operator/pkg/util/httpserver"
	"istio.io/istio/operator/pkg/util/tgz"
	tutil "istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/pkg/version"
)

const (
	istioTestVersion = "istio-1.7.0"
	testTGZFilename  = istioTestVersion + "-linux.tar.gz"
)

// chartSourceType defines where charts used in the test come from.
type chartSourceType string

var (
	operatorRootDir = filepath.Join(env.IstioSrc, "operator")

	// testDataDir contains the directory for manifest-generate test data
	testDataDir = filepath.Join(operatorRootDir, "cmd/mesh/testdata/manifest-generate")

	// Snapshot charts are in testdata/manifest-generate/data-snapshot
	snapshotCharts = chartSourceType(filepath.Join(testDataDir, "data-snapshot"))
	// Compiled in charts come from assets.gen.go
	compiledInCharts chartSourceType = "COMPILED"
	// Live charts come from manifests/
	liveCharts = chartSourceType(filepath.Join(env.IstioSrc, helm.OperatorSubdirFilePath))
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
	chartSource                 chartSourceType
}

func TestManifestGenerateComponentHubTag(t *testing.T) {
	g := NewWithT(t)

	objs, err := runManifestCommands("component_hub_tag", "", liveCharts)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		deploymentName string
		containerName  string
		want           string
	}{
		{
			deploymentName: "istio-ingressgateway",
			containerName:  "istio-proxy",
			want:           "istio-spec.hub/proxyv2:istio-spec.tag",
		},
		{
			deploymentName: "istiod",
			containerName:  "discovery",
			want:           "component.pilot.hub/pilot:2",
		},
	}

	for _, tt := range tests {
		for _, os := range objs {
			containerName := tt.deploymentName
			if tt.containerName != "" {
				containerName = tt.containerName
			}
			container := mustGetContainer(g, os, tt.deploymentName, containerName)
			g.Expect(container).Should(HavePathValueEqual(PathValue{"image", tt.want}))
		}
	}
}

func TestManifestGenerateGateways(t *testing.T) {
	g := NewWithT(t)

	flags := "-s components.ingressGateways.[0].k8s.resources.requests.memory=999Mi " +
		"-s components.ingressGateways.[name:user-ingressgateway].k8s.resources.requests.cpu=555m"

	objss, err := runManifestCommands("gateways", flags, liveCharts)
	if err != nil {
		t.Fatal(err)
	}

	for _, objs := range objss {
		g.Expect(objs.kind(name.HPAStr).size()).Should(Equal(3))
		g.Expect(objs.kind(name.PDBStr).size()).Should(Equal(3))
		g.Expect(objs.kind(name.ServiceStr).labels("istio=ingressgateway").size()).Should(Equal(3))
		g.Expect(objs.kind(name.RoleStr).nameMatches(".*gateway.*").size()).Should(Equal(3))
		g.Expect(objs.kind(name.RoleBindingStr).nameMatches(".*gateway.*").size()).Should(Equal(3))
		g.Expect(objs.kind(name.SAStr).nameMatches(".*gateway.*").size()).Should(Equal(3))

		dobj := mustGetDeployment(g, objs, "istio-ingressgateway")
		d := dobj.Unstructured()
		c := dobj.Container("istio-proxy")
		g.Expect(d).Should(HavePathValueContain(PathValue{"metadata.labels", toMap("aaa:aaa-val,bbb:bbb-val")}))
		g.Expect(c).Should(HavePathValueEqual(PathValue{"resources.requests.cpu", "111m"}))
		g.Expect(c).Should(HavePathValueEqual(PathValue{"resources.requests.memory", "999Mi"}))

		dobj = mustGetDeployment(g, objs, "user-ingressgateway")
		d = dobj.Unstructured()
		c = dobj.Container("istio-proxy")
		g.Expect(d).Should(HavePathValueContain(PathValue{"metadata.labels", toMap("ccc:ccc-val,ddd:ddd-val")}))
		g.Expect(c).Should(HavePathValueEqual(PathValue{"resources.requests.cpu", "555m"}))
		g.Expect(c).Should(HavePathValueEqual(PathValue{"resources.requests.memory", "888Mi"}))

		dobj = mustGetDeployment(g, objs, "ilb-gateway")
		d = dobj.Unstructured()
		c = dobj.Container("istio-proxy")
		s := mustGetService(g, objs, "ilb-gateway").Unstructured()
		g.Expect(d).Should(HavePathValueContain(PathValue{"metadata.labels", toMap("app:istio-ingressgateway,istio:ingressgateway,release: istio")}))
		g.Expect(c).Should(HavePathValueEqual(PathValue{"resources.requests.cpu", "333m"}))
		g.Expect(c).Should(HavePathValueEqual(PathValue{"env.[name:PILOT_CERT_PROVIDER].value", "foobar"}))
		g.Expect(s).Should(HavePathValueContain(PathValue{"metadata.annotations", toMap("cloud.google.com/load-balancer-type: internal")}))
		g.Expect(s).Should(HavePathValueContain(PathValue{"spec.ports.[0]", portVal("grpc-pilot-mtls", 15011, -1)}))
		g.Expect(s).Should(HavePathValueContain(PathValue{"spec.ports.[1]", portVal("tcp-citadel-grpc-tls", 8060, 8060)}))
		g.Expect(s).Should(HavePathValueContain(PathValue{"spec.ports.[2]", portVal("tcp-dns", 5353, -1)}))

		for _, o := range objs.kind(name.HPAStr).objSlice {
			ou := o.Unstructured()
			g.Expect(ou).Should(HavePathValueEqual(PathValue{"spec.minReplicas", int64(1)}))
			g.Expect(ou).Should(HavePathValueEqual(PathValue{"spec.maxReplicas", int64(5)}))
		}

		checkRoleBindingsReferenceRoles(g, objs)
	}
}

func TestManifestGenerateIstiodRemote(t *testing.T) {
	g := NewWithT(t)

	objss, err := runManifestCommands("istiod_remote", "", liveCharts)
	if err != nil {
		t.Fatal(err)
	}

	for _, objs := range objss {
		// check core CRDs exists
		g.Expect(objs.kind(name.CRDStr).nameEquals("destinationrules.networking.istio.io")).Should(Not(BeNil()))
		g.Expect(objs.kind(name.CRDStr).nameEquals("gateways.networking.istio.io")).Should(Not(BeNil()))
		g.Expect(objs.kind(name.CRDStr).nameEquals("sidecars.networking.istio.io")).Should(Not(BeNil()))
		g.Expect(objs.kind(name.CRDStr).nameEquals("virtualservices.networking.istio.io")).Should(Not(BeNil()))
		g.Expect(objs.kind(name.CRDStr).nameEquals("adapters.config.istio.io")).Should(BeNil())
		g.Expect(objs.kind(name.CRDStr).nameEquals("authorizationpolicies.security.istio.io")).Should(Not(BeNil()))

		g.Expect(objs.kind(name.ClusterRoleStr).nameEquals("istiod-istio-system")).Should(Not(BeNil()))
		g.Expect(objs.kind(name.ClusterRoleStr).nameEquals("istio-reader-istio-system")).Should(Not(BeNil()))
		g.Expect(objs.kind(name.ClusterRoleBindingStr).nameEquals("istiod-istio-system")).Should(Not(BeNil()))
		g.Expect(objs.kind(name.ClusterRoleBindingStr).nameEquals("istio-reader-istio-system")).Should(Not(BeNil()))
		g.Expect(objs.kind(name.CMStr).nameEquals("istio-sidecar-injector")).Should(Not(BeNil()))
		g.Expect(objs.kind(name.ServiceStr).nameEquals("istiod")).Should(Not(BeNil()))
		g.Expect(objs.kind(name.SAStr).nameEquals("istio-reader-service-account")).Should(Not(BeNil()))
		g.Expect(objs.kind(name.SAStr).nameEquals("istiod-service-account")).Should(Not(BeNil()))

		mwc := mustGetMutatingWebhookConfiguration(g, objs, "istio-sidecar-injector").Unstructured()
		g.Expect(mwc).Should(HavePathValueEqual(PathValue{"webhooks.[0].clientConfig.url", "https://xxx:15017/inject"}))

		vwc := mustGetValidatingWebhookConfiguration(g, objs, "istiod-istio-system").Unstructured()
		g.Expect(vwc).Should(HavePathValueEqual(PathValue{"webhooks.[0].clientConfig.url", "https://xxx:15017/validate"}))

		ep := mustGetEndpoint(g, objs, "istiod").Unstructured()
		g.Expect(ep).Should(HavePathValueEqual(PathValue{"subsets.[0].addresses.[0]", endpointSubsetAddressVal("", "169.10.112.88", "")}))
		g.Expect(ep).Should(HavePathValueContain(PathValue{"subsets.[0].ports.[0]", portVal("tcp-istiod", 15012, -1)}))

		checkClusterRoleBindingsReferenceRoles(g, objs)
	}
}

func TestManifestGenerateAllOff(t *testing.T) {
	g := NewWithT(t)
	m, _, err := generateManifest("all_off", "", liveCharts)
	if err != nil {
		t.Fatal(err)
	}
	objs, err := parseObjectSetFromManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	g.Expect(objs.size()).Should(Equal(0))
}

func TestManifestGenerateFlagsMinimalProfile(t *testing.T) {
	g := NewWithT(t)
	// Change profile from empty to minimal using flag.
	m, _, err := generateManifest("empty", "-s profile=minimal", liveCharts)
	if err != nil {
		t.Fatal(err)
	}
	objs, err := parseObjectSetFromManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	// minimal profile always has istiod, empty does not.
	mustGetDeployment(g, objs, "istiod")
}

func TestManifestGenerateFlagsSetHubTag(t *testing.T) {
	g := NewWithT(t)
	m, _, err := generateManifest("minimal", "-s hub=foo -s tag=bar", liveCharts)
	if err != nil {
		t.Fatal(err)
	}
	objs, err := parseObjectSetFromManifest(m)
	if err != nil {
		t.Fatal(err)
	}

	dobj := mustGetDeployment(g, objs, "istiod")

	c := dobj.Container("discovery")
	g.Expect(c).Should(HavePathValueEqual(PathValue{"image", "foo/pilot:bar"}))
}

func TestManifestGenerateFlagsSetValues(t *testing.T) {
	g := NewWithT(t)
	m, _, err := generateManifest("default", "-s values.global.proxy.image=myproxy -s values.global.proxy.includeIPRanges=172.30.0.0/16,172.21.0.0/16", liveCharts)
	if err != nil {
		t.Fatal(err)
	}
	objs, err := parseObjectSetFromManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	dobj := mustGetDeployment(g, objs, "istio-ingressgateway")

	c := dobj.Container("istio-proxy")
	g.Expect(c).Should(HavePathValueEqual(PathValue{"image", "gcr.io/istio-testing/myproxy:latest"}))

	cm := objs.kind("ConfigMap").nameEquals("istio-sidecar-injector").Unstructured()
	// TODO: change values to some nicer format rather than text block.
	g.Expect(cm).Should(HavePathValueMatchRegex(PathValue{"data.values", `.*"includeIPRanges"\: "172\.30\.0\.0/16,172\.21\.0\.0/16".*`}))
}

func TestManifestGenerateFlags(t *testing.T) {
	flagOutputDir := createTempDirOrFail(t, "flag-output")
	flagOutputValuesDir := createTempDirOrFail(t, "flag-output-values")
	runTestGroup(t, testGroup{
		{
			desc:                        "all_on",
			diffIgnore:                  "ConfigMap:*:istio",
			showOutputFileInPullRequest: true,
		},
		{
			desc:       "flag_values_enable_egressgateway",
			diffSelect: "Service:*:istio-egressgateway",
			flags:      "--set values.gateways.istio-egressgateway.enabled=true",
			noInput:    true,
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
			diffSelect: "Deployment:*:istiod,HorizontalPodAutoscaler:*:istiod",
		},
		{
			desc:       "pilot_override_kubernetes",
			diffSelect: "Deployment:*:istiod, Service:*:istiod,MutatingWebhookConfiguration:*:istio-sidecar-injector,ClusterRoleBinding::istio-reader-istio-system",
		},
		// TODO https://github.com/istio/istio/issues/22347 this is broken for overriding things to default value
		// This can be seen from REGISTRY_ONLY not applying
		{
			desc:       "pilot_merge_meshconfig",
			diffSelect: "ConfigMap:*:istio$",
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

// TestManifestGenerateHelmValues tests whether enabling components through the values passthrough interface works as
// expected i.e. without requiring enablement also in IstioOperator API.
func TestManifestGenerateHelmValues(t *testing.T) {
	runTestGroup(t, testGroup{
		{
			desc:       "helm_values_enablement",
			diffSelect: "Deployment:*:istio-egressgateway, Service:*:istio-egressgateway",
		},
	})
}

func TestManifestGenerateOrdered(t *testing.T) {
	// Since this is testing the special case of stable YAML output order, it
	// does not use the established test group pattern
	inPath := filepath.Join(testDataDir, "input/all_on.yaml")
	got1, err := runManifestGenerate([]string{inPath}, "", snapshotCharts)
	if err != nil {
		t.Fatal(err)
	}
	got2, err := runManifestGenerate([]string{inPath}, "", snapshotCharts)
	if err != nil {
		t.Fatal(err)
	}

	if got1 != got2 {
		fmt.Printf("%s", util.YAMLDiff(got1, got2))
		t.Errorf("stable_manifest: Manifest generation is not producing stable text output.")
	}
}
func TestManifestGenerateFlagAliases(t *testing.T) {
	inPath := filepath.Join(testDataDir, "input/all_on.yaml")
	gotSet, err := runManifestGenerate([]string{inPath}, "--set revision=foo", snapshotCharts)
	if err != nil {
		t.Fatal(err)
	}
	gotAlias, err := runManifestGenerate([]string{inPath}, "--revision=foo --manifests="+filepath.Join(testDataDir, "data-snapshot"), compiledInCharts)
	if err != nil {
		t.Fatal(err)
	}

	if gotAlias != gotSet {
		t.Errorf("Flag aliases not producing same output: with --set: \n\n%s\n\nWith alias:\n\n%s\nDiff:\n\n%s\n",
			gotSet, gotAlias, util.YAMLDiff(gotSet, gotAlias))
	}
}

func TestMultiICPSFiles(t *testing.T) {
	inPathBase := filepath.Join(testDataDir, "input/all_off.yaml")
	inPathOverride := filepath.Join(testDataDir, "input/helm_values_enablement.yaml")
	got, err := runManifestGenerate([]string{inPathBase, inPathOverride}, "", snapshotCharts)
	if err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(testDataDir, "output/helm_values_enablement"+goldenFileSuffixHideChangesInReview)

	want, err := readFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	diffSelect := "Deployment:*:istio-egressgateway, Service:*:istio-egressgateway"
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
	inPathBase := filepath.Join(testDataDir, "input/bare_spec.yaml")
	_, err := runManifestGenerate([]string{inPathBase}, "", liveCharts)
	if err != nil {
		t.Fatal(err)
	}
}

func TestBareValues(t *testing.T) {
	inPathBase := filepath.Join(testDataDir, "input/bare_values.yaml")
	// As long as the generate doesn't panic, we pass it.  bare_values.yaml doesn't
	// overlay well because JSON doesn't handle null values, and our charts
	// don't expect values to be blown away.
	_, _ = runManifestGenerate([]string{inPathBase}, "", liveCharts)
}

func TestBogusControlPlaneSec(t *testing.T) {
	inPathBase := filepath.Join(testDataDir, "input/bogus_cps.yaml")
	_, err := runManifestGenerate([]string{inPathBase}, "", liveCharts)
	if err != nil {
		t.Fatal(err)
	}
}

func TestInstallPackagePath(t *testing.T) {
	serverDir, err := ioutil.TempDir(os.TempDir(), "istio-test-server-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(serverDir)
	if err := tgz.Create(string(liveCharts), filepath.Join(serverDir, testTGZFilename)); err != nil {
		t.Fatal(err)
	}
	srv := httpserver.NewServer(serverDir)
	runTestGroup(t, testGroup{
		{
			// Use some arbitrary small test input (pilot only) since we are testing the local filesystem code here, not
			// manifest generation.
			desc:       "install_package_path",
			diffSelect: "Deployment:*:istiod",
			flags:      "--set installPackagePath=" + string(liveCharts),
		},
		{
			// Specify both charts and profile from local filesystem.
			desc:       "install_package_path",
			diffSelect: "Deployment:*:istiod",
			flags:      fmt.Sprintf("--set installPackagePath=%s --set profile=%s/profiles/default.yaml", string(liveCharts), string(liveCharts)),
		},
		{
			// --force is needed for version mismatch.
			desc:       "install_package_path",
			diffSelect: "Deployment:*:istiod",
			flags:      "--force --set installPackagePath=" + srv.URL() + "/" + testTGZFilename,
		},
	})

}

// TestTrailingWhitespace ensures there are no trailing spaces in the manifests
// This is important because `kubectl edit` and other commands will get escaped if they are present
// making it hard to read/edit
func TestTrailingWhitespace(t *testing.T) {
	got, err := runManifestGenerate([]string{}, "--set values.gateways.istio-egressgateway.enabled=true", liveCharts)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(got, "\n")
	for i, l := range lines {
		if strings.HasSuffix(l, " ") {
			t.Errorf("Line %v has a trailing space: [%v]. Context: %v", i, l, strings.Join(lines[i-5:i+5], "\n"))
		}
	}
}

func validateReferentialIntegrity(t *testing.T, objs object.K8sObjects, cname string, deploymentSelector map[string]string) {
	t.Run(cname, func(t *testing.T) {
		deployment := mustFindObject(t, objs, cname, name.DeploymentStr)
		service := mustFindObject(t, objs, cname, name.ServiceStr)
		pdb := mustFindObject(t, objs, cname, name.PDBStr)
		hpa := mustFindObject(t, objs, cname, name.HPAStr)
		podLabels := mustGetLabels(t, deployment, "spec.template.metadata.labels")
		// Check all selectors align
		mustSelect(t, mustGetLabels(t, pdb, "spec.selector.matchLabels"), podLabels)
		mustSelect(t, mustGetLabels(t, service, "spec.selector"), podLabels)
		mustSelect(t, mustGetLabels(t, deployment, "spec.selector.matchLabels"), podLabels)
		if hpaName := mustGetPath(t, hpa, "spec.scaleTargetRef.name"); cname != hpaName {
			t.Fatalf("HPA does not match deployment: %v != %v", cname, hpaName)
		}

		serviceAccountName := mustGetPath(t, deployment, "spec.template.spec.serviceAccountName").(string)
		mustFindObject(t, objs, serviceAccountName, name.SAStr)

		// Check we aren't changing immutable fields. This only matters for in place upgrade (non revision)
		// This one is not a selector, it must be an exact match
		if sel := mustGetLabels(t, deployment, "spec.selector.matchLabels"); !reflect.DeepEqual(deploymentSelector, sel) {
			t.Fatalf("Depployment selectors are immutable, but changed since 1.5. Was %v, now is %v", deploymentSelector, sel)
		}
	})
}

// This test enforces that objects that reference other objects do so properly, such as Service selecting deployment
func TestConfigSelectors(t *testing.T) {
	got, err := runManifestGenerate([]string{}, "--set values.gateways.istio-egressgateway.enabled=true", liveCharts)
	if err != nil {
		t.Fatal(err)
	}
	objs, err := object.ParseK8sObjectsFromYAMLManifest(got)
	if err != nil {
		t.Fatal(err)
	}
	gotRev, e := runManifestGenerate([]string{}, "--set revision=canary", liveCharts)
	if e != nil {
		t.Fatal(e)
	}
	objsRev, err := object.ParseK8sObjectsFromYAMLManifest(gotRev)
	if err != nil {
		t.Fatal(err)
	}

	istiod15Selector := map[string]string{
		"istio": "pilot",
	}
	istiodCanary16Selector := map[string]string{
		"app":          "istiod",
		"istio.io/rev": "canary",
	}
	ingress15Selector := map[string]string{
		"app":   "istio-ingressgateway",
		"istio": "ingressgateway",
	}
	egress15Selector := map[string]string{
		"app":   "istio-egressgateway",
		"istio": "egressgateway",
	}

	// Validate references within the same deployment
	validateReferentialIntegrity(t, objs, "istiod", istiod15Selector)
	validateReferentialIntegrity(t, objs, "istio-ingressgateway", ingress15Selector)
	validateReferentialIntegrity(t, objs, "istio-egressgateway", egress15Selector)
	validateReferentialIntegrity(t, objsRev, "istiod-canary", istiodCanary16Selector)

	t.Run("cross revision", func(t *testing.T) {
		// Istiod revisions have complicated cross revision implications. We should assert these are correct
		// First we fetch all the objects for our default install
		cname := "istiod"
		deployment := mustFindObject(t, objs, cname, name.DeploymentStr)
		service := mustFindObject(t, objs, cname, name.ServiceStr)
		pdb := mustFindObject(t, objs, cname, name.PDBStr)
		podLabels := mustGetLabels(t, deployment, "spec.template.metadata.labels")

		// Next we fetch all the objects for a revision install
		nameRev := "istiod-canary"
		deploymentRev := mustFindObject(t, objsRev, nameRev, name.DeploymentStr)
		hpaRev := mustFindObject(t, objsRev, nameRev, name.HPAStr)
		serviceRev := mustFindObject(t, objsRev, nameRev, name.ServiceStr)
		pdbRev := mustFindObject(t, objsRev, nameRev, name.PDBStr)
		podLabelsRev := mustGetLabels(t, deploymentRev, "spec.template.metadata.labels")

		// Make sure default and revisions do not cross
		mustNotSelect(t, mustGetLabels(t, serviceRev, "spec.selector"), podLabels)
		mustNotSelect(t, mustGetLabels(t, service, "spec.selector"), podLabelsRev)
		mustNotSelect(t, mustGetLabels(t, pdbRev, "spec.selector.matchLabels"), podLabels)
		mustNotSelect(t, mustGetLabels(t, pdb, "spec.selector.matchLabels"), podLabelsRev)

		// Make sure the scaleTargetRef points to the correct Deployment
		if hpaName := mustGetPath(t, hpaRev, "spec.scaleTargetRef.name"); nameRev != hpaName {
			t.Fatalf("HPA does not match deployment: %v != %v", nameRev, hpaName)
		}

		// Check selection of previous versions . This only matters for in place upgrade (non revision)
		podLabels15 := map[string]string{
			"app":   "istiod",
			"istio": "pilot",
		}
		mustSelect(t, mustGetLabels(t, service, "spec.selector"), podLabels15)
		mustNotSelect(t, mustGetLabels(t, serviceRev, "spec.selector"), podLabels15)
		mustSelect(t, mustGetLabels(t, pdb, "spec.selector.matchLabels"), podLabels15)
		mustNotSelect(t, mustGetLabels(t, pdbRev, "spec.selector.matchLabels"), podLabels15)
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
	l := clog.NewConsoleLogger(os.Stdout, os.Stderr, installerScope)
	_, iop, err := manifest.GenerateConfig(nil, []string{"installPackagePath=" + string(liveCharts)}, true, nil, l)
	if err != nil {
		t.Fatal(err)
	}
	if iop.Spec.Hub != version.DockerInfo.Hub || iop.Spec.Tag != version.DockerInfo.Tag {
		t.Fatalf("DockerInfoHub, DockerInfoTag got: %s,%s, want: %s, %s", iop.Spec.Hub, iop.Spec.Tag, version.DockerInfo.Hub, version.DockerInfo.Tag)
	}
}

func runTestGroup(t *testing.T, tests testGroup) {
	for _, tt := range tests {
		tt := tt
		t.Run(tt.desc, func(t *testing.T) {
			t.Parallel()
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

			csource := snapshotCharts
			if tt.chartSource != "" {
				csource = tt.chartSource
			}
			got, err := runManifestGenerate(filenames, tt.flags, csource)
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

			tutil.RefreshGoldenFile([]byte(got), outPath, t)

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

// nolint: unparam
func generateManifest(inFile, flags string, chartSource chartSourceType) (string, object.K8sObjects, error) {
	inPath := filepath.Join(testDataDir, "input", inFile+".yaml")
	manifest, err := runManifestGenerate([]string{inPath}, flags, chartSource)
	if err != nil {
		return "", nil, fmt.Errorf("error %s: %s", err, manifest)
	}
	objs, err := object.ParseK8sObjectsFromYAMLManifest(manifest)
	return manifest, objs, err
}

// runManifestGenerate runs the manifest generate command. If filenames is set, passes the given filenames as -f flag,
// flags is passed to the command verbatim. If you set both flags and path, make sure to not use -f in flags.
func runManifestGenerate(filenames []string, flags string, chartSource chartSourceType) (string, error) {
	return runManifestCommand("generate", filenames, flags, chartSource)
}

func mustGetWebhook(t test.Failer, obj object.K8sObject) []v1.MutatingWebhook {
	t.Helper()
	path := mustGetPath(t, obj, "webhooks")
	by, err := json.Marshal(path)
	if err != nil {
		t.Fatal(err)
	}
	var mwh []v1.MutatingWebhook
	if err := json.Unmarshal(by, &mwh); err != nil {
		t.Fatal(err)
	}
	return mwh
}

func getWebhooks(t *testing.T, setFlags string, webhookName string) []v1.MutatingWebhook {
	t.Helper()
	got, err := runManifestGenerate([]string{}, setFlags, liveCharts)
	if err != nil {
		t.Fatal(err)
	}
	objs, err := object.ParseK8sObjectsFromYAMLManifest(got)
	if err != nil {
		t.Fatal(err)
	}
	return mustGetWebhook(t, mustFindObject(t, objs, webhookName, name.MutatingWebhookConfigurationStr))
}

type LabelSet struct {
	namespace, pod klabels.Set
}

func mergeWebhooks(whs ...[]v1.MutatingWebhook) []v1.MutatingWebhook {
	res := []v1.MutatingWebhook{}
	for _, wh := range whs {
		res = append(res, wh...)
	}
	return res
}

// This test checks the mutating webhook selectors behavior, especially with interaction with revisions
func TestWebhookSelector(t *testing.T) {
	// Setup various labels to be tested
	empty := klabels.Set{}
	revLabel := klabels.Set{"istio.io/rev": "canary"}
	legacyAndRevLabel := klabels.Set{"istio-injection": "enabled", "istio.io/rev": "canary"}
	legacyDisabledAndRevLabel := klabels.Set{"istio-injection": "disabled", "istio.io/rev": "canary"}
	legacyLabel := klabels.Set{"istio-injection": "enabled"}
	legacyLabelDisabled := klabels.Set{"istio-injection": "disabled"}

	objEnabled := klabels.Set{"sidecar.istio.io/inject": "true"}
	objDisable := klabels.Set{"sidecar.istio.io/inject": "false"}
	objEnabledAndRev := klabels.Set{"sidecar.istio.io/inject": "true", "istio.io/rev": "canary"}
	objDisableAndRev := klabels.Set{"sidecar.istio.io/inject": "false", "istio.io/rev": "canary"}

	defaultWebhook := getWebhooks(t, "--set values.sidecarInjectorWebhook.useLegacySelectors=false", "istio-sidecar-injector")
	revWebhook := getWebhooks(t, "--set values.sidecarInjectorWebhook.useLegacySelectors=false --set revision=canary", "istio-sidecar-injector-canary")
	autoWebhook := getWebhooks(t,
		"--set values.sidecarInjectorWebhook.useLegacySelectors=false --set values.sidecarInjectorWebhook.enableNamespacesByDefault=true",
		"istio-sidecar-injector")
	legacyWebhook := getWebhooks(t, "--set values.sidecarInjectorWebhook.useLegacySelectors=true", "istio-sidecar-injector")
	legacyRevWebhook := getWebhooks(t, "--set values.sidecarInjectorWebhook.useLegacySelectors=true --set revision=canary", "istio-sidecar-injector-canary")

	// predicate is used to filter out "obvious" test cases, to avoid enumerating all cases
	// nolint: unparam
	predicate := func(ls LabelSet) (string, bool) {
		if ls.namespace.Get("istio-injection") == "disabled" {
			return "", true
		}
		if ls.pod.Get("sidecar.istio.io/inject") == "false" {
			return "", true
		}
		return "", false
	}

	// We test the cross product namespace and pod labels:
	// 1. revision label (istio.io/rev)
	// 2. inject label true (istio-injection on namespace, sidecar.istio.io/inject on pod)
	// 3. inject label false
	// 4. inject label true and revision label
	// 5. inject label false and revision label
	// 6. no label
	// However, we filter out all the disable cases, leaving us with a reasonable number of cases
	testLabels := []LabelSet{}
	for _, namespaceLabel := range []klabels.Set{empty, revLabel, legacyLabel, legacyLabelDisabled, legacyAndRevLabel, legacyDisabledAndRevLabel} {
		for _, podLabel := range []klabels.Set{empty, revLabel, objEnabled, objDisable, objEnabledAndRev, objDisableAndRev} {
			testLabels = append(testLabels, LabelSet{namespaceLabel, podLabel})
		}
	}
	type assertion struct {
		namespaceLabel klabels.Set
		objectLabel    klabels.Set
		match          string
	}
	baseAssertions := []assertion{
		{empty, empty, ""},
		{empty, revLabel, "istiod-canary"},
		{empty, objEnabled, "istiod"},
		{empty, objEnabledAndRev, "istiod-canary"},
		{revLabel, empty, "istiod-canary"},
		{revLabel, revLabel, "istiod-canary"},
		{revLabel, objEnabled, "istiod-canary"},
		{revLabel, objEnabledAndRev, "istiod-canary"},
		{legacyLabel, empty, "istiod"},
		{legacyLabel, objEnabled, "istiod"},
		{legacyAndRevLabel, empty, "istiod"},
		{legacyAndRevLabel, objEnabled, "istiod"},

		// The behavior of these is a bit odd; they are explicitly selecting a revision but getting
		// the default Unfortunately, the legacy webhook selectors would select these, cause
		// duplicate injection, so we defer to the namespace label.
		{legacyLabel, revLabel, "istiod"},
		{legacyAndRevLabel, revLabel, "istiod"},
		{legacyLabel, objEnabledAndRev, "istiod"},
		{legacyAndRevLabel, objEnabledAndRev, "istiod"},
	}
	cases := []struct {
		name     string
		webhooks []v1.MutatingWebhook
		checks   []assertion
	}{
		{
			name:     "base",
			webhooks: mergeWebhooks(defaultWebhook, revWebhook),
			checks:   baseAssertions,
		},
		{
			// This is exactly the same as above, but empty/empty matches
			name:     "auto injection",
			webhooks: mergeWebhooks(autoWebhook, revWebhook),
			checks:   append([]assertion{{empty, empty, "istiod"}}, baseAssertions...),
		},
		{
			// Upgrade from a legacy webhook to a new revision based
			// Note: we don't need non revision legacy -> non revision, since it will overwrite the webhook
			name:     "revision upgrade",
			webhooks: mergeWebhooks(legacyWebhook, revWebhook),
			checks: append([]assertion{
				{empty, objEnabled, ""}, // Legacy one requires namespace label
			}, baseAssertions...),
		},
		{
			// Use new default webhook, while we still have a legacy revision one around.
			name:     "inplace upgrade",
			webhooks: mergeWebhooks(defaultWebhook, legacyRevWebhook),
			checks: append([]assertion{
				{empty, revLabel, ""},         // Legacy one requires namespace label
				{empty, objEnabledAndRev, ""}, // Legacy one requires namespace label
			}, baseAssertions...),
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			whs := tt.webhooks
			for _, s := range testLabels {
				t.Run(fmt.Sprintf("ns:%v pod:%v", s.namespace, s.pod), func(t *testing.T) {
					found := ""
					match := 0
					for i, wh := range whs {
						sn := wh.ClientConfig.Service.Name
						matches := selectorMatches(t, wh.NamespaceSelector, s.namespace) && selectorMatches(t, wh.ObjectSelector, s.pod)
						if matches && found != "" {
							// There must be exactly one match, or we will double inject.
							t.Fatalf("matched multiple webhooks. Had %v, matched %v", found, sn)
						}
						if matches {
							found = sn
							match = i
						}
					}
					// If our predicate can tell us the expected match, use that
					if want, ok := predicate(s); ok {
						if want != found {
							t.Fatalf("expected webhook to go to service %q, found %q", want, found)
						}
						return
					}
					// Otherwise, look through our assertions for a matching one, and check that
					for _, w := range tt.checks {
						if w.namespaceLabel.String() == s.namespace.String() && w.objectLabel.String() == s.pod.String() {
							if found != w.match {
								if found != "" {
									t.Fatalf("expected webhook to go to service %q, found %q (from match %d)\nNamespace selector: %v\nObject selector: %v)",
										w.match, found, match, whs[match].NamespaceSelector.MatchExpressions, whs[match].ObjectSelector.MatchExpressions)
								} else {
									t.Fatalf("expected webhook to go to service %q, found %q", w.match, found)
								}
							}
							return
						}
					}
					// If none match, a test case is missing for the label set.
					t.Fatalf("no assertion for namespace=%v pod=%v", s.namespace, s.pod)
				})
			}
		})
	}
}

func selectorMatches(t *testing.T, selector *metav1.LabelSelector, labels klabels.Set) bool {
	t.Helper()
	// From webhook spec: "Default to the empty LabelSelector, which matches everything."
	if selector == nil {
		return true
	}
	s, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		t.Fatal(err)
	}
	return s.Matches(labels)
}

func TestSidecarTemplate(t *testing.T) {
	runTestGroup(t, testGroup{
		{
			desc:       "sidecar_template",
			diffSelect: "ConfigMap:*:istio-sidecar-injector",
		},
	})
}
