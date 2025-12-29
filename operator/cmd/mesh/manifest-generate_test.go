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
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/yaml"

	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/render"
	operatortest "istio.io/istio/operator/pkg/test"
	uninstall2 "istio.io/istio/operator/pkg/uninstall"
	"istio.io/istio/operator/pkg/util/testhelpers"
	tutil "istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/file"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/version"
)

const (
	testIstioDiscoveryChartPath = "charts/istio-control/istio-discovery/templates"
	operatorSubdirFilePath      = "manifests"
)

// chartSourceType defines where charts used in the test come from.
type chartSourceType string

var (
	operatorRootDir = filepath.Join(env.IstioSrc, "operator")

	// testDataDir contains the directory for manifest-generate test data
	testDataDir = filepath.Join(operatorRootDir, "cmd/mesh/testdata/manifest-generate")

	// Snapshot charts are in testdata/manifest-generate/data-snapshot
	snapshotCharts = func() chartSourceType {
		d, err := os.MkdirTemp("", "data-snapshot-*")
		if err != nil {
			panic(fmt.Errorf("failed to make temp dir: %v", err))
		}
		f, err := os.Open("testdata/manifest-generate/data-snapshot.tar.gz")
		if err != nil {
			panic(fmt.Errorf("failed to read data snapshot: %v", err))
		}
		if err := extract(f, d); err != nil {
			panic(fmt.Errorf("failed to extract data snapshot: %v", err))
		}
		return chartSourceType(filepath.Join(d, "manifests"))
	}()
	// Run operator/scripts/run_update_golden_snapshots.sh to update the snapshot charts tarball.
	compiledInCharts chartSourceType = "COMPILED"
	_                                = compiledInCharts
	// Live charts come from manifests/
	liveCharts = chartSourceType(filepath.Join(env.IstioSrc, operatorSubdirFilePath))
)

type testGroup []struct {
	desc string
	// Small changes to the input profile produce large changes to the golden output
	// files. This makes it difficult to spot meaningful changes in pull requests.
	// By default we hide these changes to make developers life's a bit easier. However,
	// it is still useful to sometimes override this behavior and show the full diff.
	// When this flag is true, use an alternative file suffix that is not hidden by
	// default GitHub in pull requests.
	showOutputFileInPullRequest bool
	flags                       string
	noInput                     bool
	fileSelect                  []string
	diffSelect                  string
	diffIgnore                  string
	chartSource                 chartSourceType
}

func init() {
	kubeClientFunc = func() (kube.CLIClient, error) {
		return nil, nil
	}
}

func extract(gzipStream io.Reader, destination string) error {
	uncompressedStream, err := gzip.NewReader(gzipStream)
	if err != nil {
		return fmt.Errorf("create gzip reader: %v", err)
	}

	tarReader := tar.NewReader(uncompressedStream)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("next: %v", err)
		}

		dest := filepath.Join(destination, header.Name)
		switch header.Typeflag {
		case tar.TypeDir:
			if _, err := os.Stat(dest); err != nil {
				if err := os.Mkdir(dest, 0o755); err != nil {
					return fmt.Errorf("mkdir: %v", err)
				}
			}
		case tar.TypeReg:
			// Create containing folder if not present
			dir := path.Dir(dest)
			if _, err := os.Stat(dir); err != nil {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return err
				}
			}
			outFile, err := os.Create(dest)
			if err != nil {
				return fmt.Errorf("create: %v", err)
			}
			if _, err := io.Copy(outFile, tarReader); err != nil {
				return fmt.Errorf("copy: %v", err)
			}
			outFile.Close()
		default:
			return fmt.Errorf("unknown type: %v in %v", header.Typeflag, header.Name)
		}
	}
	return nil
}

func copyDir(src string, dest string) error {
	return filepath.Walk(src, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if strings.Contains(path, "vendor/") {
			return filepath.SkipDir
		}

		outpath := filepath.Join(dest, strings.TrimPrefix(path, src))

		if info.IsDir() {
			os.MkdirAll(outpath, info.Mode())
			return nil
		}
		cpErr := file.AtomicCopy(path, filepath.Dir(outpath), filepath.Base(outpath))
		if cpErr != nil {
			return cpErr
		}

		return nil
	})
}

func TestMain(m *testing.M) {
	code := m.Run()
	// Cleanup uncompress snapshot charts
	os.RemoveAll(string(snapshotCharts))
	os.Exit(code)
}

func TestManifestGenerateComponentHubTag(t *testing.T) {
	g := NewWithT(t)

	objs := runManifestCommands(t, "component_hub_tag", "", liveCharts,
		[]string{"templates/deployment.yaml", "templates/daemonset.yaml", "templates/zzy_descope_legacy.yaml"})

	tests := []struct {
		deploymentName string
		daemonsetName  string
		containerName  string
		want           string
	}{
		{
			deploymentName: "istio-ingressgateway",
			containerName:  "istio-proxy",
			want:           "istio-spec.hub/proxyv2:istio-spec.tag-global.variant",
		},
		{
			deploymentName: "istiod",
			containerName:  "discovery",
			want:           "component.pilot.hub/pilot:2-global.variant",
		},
		{
			daemonsetName: "istio-cni-node",
			containerName: "install-cni",
			want:          "component.cni.hub/install-cni:v3.3.3-global.variant",
		},
		{
			daemonsetName: "ztunnel",
			containerName: "istio-proxy",
			want:          "component.ztunnel.hub/ztunnel:4-global.variant",
		},
	}

	for _, tt := range tests {
		for _, os := range objs {
			var container map[string]any
			if tt.deploymentName != "" {
				container = mustGetContainer(g, os, tt.deploymentName, tt.containerName)
			} else {
				container = mustGetContainerFromDaemonset(g, os, tt.daemonsetName, tt.containerName)
			}
			g.Expect(container).Should(HavePathValueEqual(PathValue{"image", tt.want}))
		}
	}
}

func TestSetGatewayCustomTagsAndNoLabels(t *testing.T) {
	g := NewWithT(t)

	objss := runManifestCommands(t, "gateways-with-custom-tags-and-no-labels", "", liveCharts, nil)

	for _, objs := range objss {
		{
			dobj := mustGetDeployment(g, objs, "istio-ingressgateway")
			c := getContainer(dobj, "istio-proxy")
			g.Expect(c).Should(HavePathValueMatchRegex(PathValue{"image", "^.*:special-tag$"}))
		}
		{
			dobj := mustGetDeployment(g, objs, "istio-egressgateway")
			c := getContainer(dobj, "istio-proxy")
			g.Expect(c).Should(HavePathValueMatchRegex(PathValue{"image", "^.*:special-tag2$"}))
		}
	}
}

func TestManifestGenerateGateways(t *testing.T) {
	g := NewWithT(t)

	flags := "-s components.ingressGateways.[0].k8s.resources.requests.memory=999Mi " +
		"-s components.ingressGateways.[name:user-ingressgateway].k8s.resources.requests.cpu=555m " +
		"-s values.gateways.istio-ingressgateway.autoscaleMin=2"
	objss := runManifestCommands(t, "gateways", flags, liveCharts, nil)

	for _, objs := range objss {
		g.Expect(objs.kind(manifest.HorizontalPodAutoscaler).size()).Should(Equal(3))
		g.Expect(objs.kind(manifest.PodDisruptionBudget).size()).Should(Equal(3))
		g.Expect(objs.kind(gvk.Service.Kind).labels("istio=ingressgateway").size()).Should(Equal(3))
		g.Expect(objs.kind(manifest.Role).nameMatches(".*gateway.*").size()).Should(Equal(3))
		g.Expect(objs.kind(manifest.RoleBinding).nameMatches(".*gateway.*").size()).Should(Equal(3))
		g.Expect(objs.kind(gvk.ServiceAccount.Kind).nameMatches(".*gateway.*").size()).Should(Equal(3))

		dobj := mustGetDeployment(g, objs, "istio-ingressgateway")
		d := dobj.Unstructured.Object
		c := getContainer(dobj, "istio-proxy")
		g.Expect(d).Should(HavePathValueContain(PathValue{"spec.template.metadata.labels", toMap("service.istio.io/canonical-revision:21")}))
		g.Expect(d).Should(HavePathValueContain(PathValue{"metadata.labels", toMap("aaa:aaa-val,bbb:bbb-val")}))
		g.Expect(c).Should(HavePathValueEqual(PathValue{"resources.requests.cpu", "111m"}))
		g.Expect(c).Should(HavePathValueEqual(PathValue{"resources.requests.memory", "999Mi"}))

		dobj = mustGetDeployment(g, objs, "user-ingressgateway")
		d = dobj.Unstructured.Object
		c = getContainer(dobj, "istio-proxy")
		g.Expect(d).Should(HavePathValueContain(PathValue{"metadata.labels", toMap("ccc:ccc-val,ddd:ddd-val")}))
		g.Expect(c).Should(HavePathValueEqual(PathValue{"resources.requests.cpu", "555m"}))
		g.Expect(c).Should(HavePathValueEqual(PathValue{"resources.requests.memory", "888Mi"}))

		dobj = mustGetDeployment(g, objs, "ilb-gateway")
		d = dobj.Unstructured.Object
		c = getContainer(dobj, "istio-proxy")
		s := mustGetService(g, objs, "ilb-gateway").Unstructured.Object
		g.Expect(d).Should(HavePathValueContain(PathValue{"metadata.labels", toMap("app:istio-ingressgateway,istio:ingressgateway,release: istio")}))
		g.Expect(c).Should(HavePathValueEqual(PathValue{"resources.requests.cpu", "333m"}))
		g.Expect(c).Should(HavePathValueEqual(PathValue{"env.[name:PILOT_CERT_PROVIDER].value", "foobar"}))
		g.Expect(s).Should(HavePathValueContain(PathValue{"metadata.annotations", toMap("cloud.google.com/load-balancer-type: internal")}))
		g.Expect(s).Should(HavePathValueContain(PathValue{"spec.ports.[0]", portVal("grpc-pilot-mtls", 15011, -1)}))
		g.Expect(s).Should(HavePathValueContain(PathValue{"spec.ports.[1]", portVal("tcp-citadel-grpc-tls", 8060, 8060)}))
		g.Expect(s).Should(HavePathValueContain(PathValue{"spec.ports.[2]", portVal("tcp-dns", 5353, -1)}))

		for _, o := range objs.kind(manifest.HorizontalPodAutoscaler).objSlice {
			ou := o.Unstructured.Object
			g.Expect(ou).Should(HavePathValueEqual(PathValue{"spec.minReplicas", int64(2)}))
			g.Expect(ou).Should(HavePathValueEqual(PathValue{"spec.maxReplicas", int64(5)}))
		}

		checkRoleBindingsReferenceRoles(g, objs)
	}

	runTestGroup(t, testGroup{
		{
			desc:       "ingressgateway_k8s_settings",
			diffSelect: "Deployment:*:istio-ingressgateway, Service:*:istio-ingressgateway",
		},
	})
}

func TestManifestGenerateWithDuplicateMutatingWebhookConfig(t *testing.T) {
	testResourceFile := "duplicate_mwc"

	tmpDir := t.TempDir()
	tmpCharts := chartSourceType(filepath.Join(tmpDir, operatorSubdirFilePath))
	err := copyDir(string(liveCharts), string(tmpCharts))
	if err != nil {
		t.Fatal(err)
	}

	rs, err := readFile(filepath.Join(testDataDir, "input-extra-resources", testResourceFile+".yaml"))
	if err != nil {
		t.Fatal(err)
	}

	err = writeFile(filepath.Join(tmpDir, operatorSubdirFilePath+"/"+testIstioDiscoveryChartPath+"/"+testResourceFile+".yaml"), []byte(rs))
	if err != nil {
		t.Fatal(err)
	}

	c := SetupFakeClient()
	mwh := clienttest.NewWriter[*v1.MutatingWebhookConfiguration](t, c)

	// First attempt: success
	objs, err := fakeControllerReconcileInternal(c, testResourceFile, tmpCharts)
	assert.NoError(t, err)
	assert.Equal(t, objs.kind(gvk.MutatingWebhookConfiguration.Kind).size(), 3)
	// Install writes to dynamic client, but we read from the typed one. Copy things over.
	for _, o := range objs.kind(gvk.MutatingWebhookConfiguration.Kind).objSlice {
		wh := &v1.MutatingWebhookConfiguration{}
		assert.NoError(t, yaml.Unmarshal([]byte(o.Content), wh))
		mwh.CreateOrUpdate(wh)
	}

	// Second attempt: fails
	_, err = fakeControllerReconcileInternal(c, testResourceFile, tmpCharts)
	assert.Error(t, err)
	assert.Equal(t, strings.Contains(err.Error(), "Webhook overlaps with others"), true)
}

func TestManifestGenerateDefaultWithRevisionedWebhook(t *testing.T) {
	runRevisionedWebhookTest(t, "minimal-revisioned", "default_tag")
}

func TestManifestGenerateFailedDefaultInstallation(t *testing.T) {
	runRevisionedWebhookTest(t, "minimal", "default_installation_failed")
}

func runRevisionedWebhookTest(t *testing.T, testResourceFile, whSource string) {
	t.Helper()
	recreateSimpleTestEnv()
	tmpDir := t.TempDir()
	tmpCharts := chartSourceType(filepath.Join(tmpDir, operatorSubdirFilePath))
	err := copyDir(string(liveCharts), string(tmpCharts))
	if err != nil {
		t.Fatal(err)
	}

	// Add a default tag which is the webhook that will be processed post-install
	rs, err := readFile(filepath.Join(testDataDir, "input-extra-resources", whSource+".yaml"))
	if err != nil {
		t.Fatal(err)
	}

	err = writeFile(filepath.Join(tmpDir, operatorSubdirFilePath+"/"+testIstioDiscoveryChartPath+"/"+testResourceFile+".yaml"), []byte(rs))
	if err != nil {
		t.Fatal(err)
	}
	_ = fakeControllerReconcile(t, testResourceFile, tmpCharts)

	// Install a default revision should not cause any error
	minimal := "minimal"
	_ = fakeControllerReconcile(t, minimal, tmpCharts)
}

func TestManifestGenerateIstiodRemote(t *testing.T) {
	g := NewWithT(t)

	const primarySVCName = "istiod"
	objss := runManifestCommands(t, "istiod_remote", "", liveCharts, nil)

	for _, objs := range objss {
		// check core CRDs exists
		g.Expect(objs.kind(gvk.CustomResourceDefinition.Kind).nameEquals("destinationrules.networking.istio.io")).Should(Not(BeNil()))
		g.Expect(objs.kind(gvk.CustomResourceDefinition.Kind).nameEquals("gateways.networking.istio.io")).Should(Not(BeNil()))
		g.Expect(objs.kind(gvk.CustomResourceDefinition.Kind).nameEquals("sidecars.networking.istio.io")).Should(Not(BeNil()))
		g.Expect(objs.kind(gvk.CustomResourceDefinition.Kind).nameEquals("virtualservices.networking.istio.io")).Should(Not(BeNil()))
		g.Expect(objs.kind(gvk.CustomResourceDefinition.Kind).nameEquals("adapters.config.istio.io")).Should(BeNil())
		g.Expect(objs.kind(gvk.CustomResourceDefinition.Kind).nameEquals("authorizationpolicies.security.istio.io")).Should(Not(BeNil()))

		g.Expect(objs.kind(gvk.ConfigMap.Kind).nameEquals("istio-sidecar-injector")).Should(Not(BeNil()))
		g.Expect(objs.kind(gvk.Service.Kind).nameEquals(primarySVCName)).Should(Not(BeNil()))
		g.Expect(objs.kind(gvk.ServiceAccount.Kind).nameEquals("istio-reader-service-account")).Should(Not(BeNil()))

		mwc := mustGetMutatingWebhookConfiguration(g, objs, "istio-sidecar-injector").Unstructured.Object
		g.Expect(mwc).Should(HavePathValueEqual(PathValue{"webhooks.[0].clientConfig.url", "https://xxx:15017/inject"}))

		ep := mustGetEndpointSlice(g, objs, primarySVCName).Unstructured.Object
		g.Expect(ep).Should(HavePathValueEqual(PathValue{"endpoints.[0].addresses.[0]", "169.10.112.88"}))
		g.Expect(ep).Should(HavePathValueContain(PathValue{"ports.[0]", portVal("tcp-istiod", 15012, -1)}))

		checkClusterRoleBindingsReferenceRoles(g, objs)
	}
}

func TestManifestGenerateIstiodRemoteConfigCluster(t *testing.T) {
	g := NewWithT(t)

	const primarySVCName = "istiod"
	objss := runManifestCommands(t, "istiod_remote_config", "", liveCharts, nil)

	for _, objs := range objss {
		// check core CRDs exists
		g.Expect(objs.kind(gvk.CustomResourceDefinition.Kind).nameEquals("destinationrules.networking.istio.io")).Should(Not(BeNil()))
		g.Expect(objs.kind(gvk.CustomResourceDefinition.Kind).nameEquals("gateways.networking.istio.io")).Should(Not(BeNil()))
		g.Expect(objs.kind(gvk.CustomResourceDefinition.Kind).nameEquals("sidecars.networking.istio.io")).Should(Not(BeNil()))
		g.Expect(objs.kind(gvk.CustomResourceDefinition.Kind).nameEquals("virtualservices.networking.istio.io")).Should(Not(BeNil()))
		g.Expect(objs.kind(gvk.CustomResourceDefinition.Kind).nameEquals("adapters.config.istio.io")).Should(BeNil())
		g.Expect(objs.kind(gvk.CustomResourceDefinition.Kind).nameEquals("authorizationpolicies.security.istio.io")).Should(Not(BeNil()))

		g.Expect(objs.kind(gvk.ConfigMap.Kind).nameEquals("istio-sidecar-injector")).Should(Not(BeNil()))
		g.Expect(objs.kind(gvk.Service.Kind).nameEquals(primarySVCName)).Should(Not(BeNil()))
		g.Expect(objs.kind(gvk.ServiceAccount.Kind).nameEquals("istio-reader-service-account")).Should(Not(BeNil()))

		mwc := mustGetMutatingWebhookConfiguration(g, objs, "istio-sidecar-injector").Unstructured.Object
		g.Expect(mwc).Should(HavePathValueEqual(PathValue{"webhooks.[0].clientConfig.service.name", primarySVCName}))

		g.Expect(objs.kind(gvk.Service.Kind).nameEquals(primarySVCName)).Should(Not(BeNil()))
		ep := mustGetEndpointSlice(g, objs, primarySVCName).Unstructured.Object

		g.Expect(ep).Should(HavePathValueEqual(PathValue{"endpoints.[0].addresses.[0]", "169.10.112.88"}))
		g.Expect(ep).Should(HavePathValueContain(PathValue{"ports.[0]", portVal("tcp-istiod", 15012, -1)}))

		// validation webhook
		vwc := mustGetValidatingWebhookConfiguration(g, objs, "istio-validator-istio-system").Unstructured.Object
		g.Expect(vwc).Should(HavePathValueEqual(PathValue{"webhooks.[0].clientConfig.url", "https://xxx:15017/validate"}))

		checkClusterRoleBindingsReferenceRoles(g, objs)
	}
}

func TestManifestGenerateIstiodRemoteLocalInjection(t *testing.T) {
	g := NewWithT(t)

	const istiodPrimaryXDSServiceName = "istiod-remote"
	const istiodInjectionServiceName = "istiod"
	objss := runManifestCommands(t, "istiod_remote_local", "", liveCharts, nil)

	for _, objs := range objss {
		// check core CRDs exists
		g.Expect(objs.kind(gvk.CustomResourceDefinition.Kind).nameEquals("destinationrules.networking.istio.io")).Should(Not(BeNil()))
		g.Expect(objs.kind(gvk.CustomResourceDefinition.Kind).nameEquals("gateways.networking.istio.io")).Should(Not(BeNil()))
		g.Expect(objs.kind(gvk.CustomResourceDefinition.Kind).nameEquals("sidecars.networking.istio.io")).Should(Not(BeNil()))
		g.Expect(objs.kind(gvk.CustomResourceDefinition.Kind).nameEquals("virtualservices.networking.istio.io")).Should(Not(BeNil()))
		g.Expect(objs.kind(gvk.CustomResourceDefinition.Kind).nameEquals("adapters.config.istio.io")).Should(BeNil())
		g.Expect(objs.kind(gvk.CustomResourceDefinition.Kind).nameEquals("authorizationpolicies.security.istio.io")).Should(Not(BeNil()))

		g.Expect(objs.kind(gvk.ConfigMap.Kind).nameEquals("istio-sidecar-injector")).Should(Not(BeNil()))
		g.Expect(objs.kind(gvk.Service.Kind).nameEquals(istiodInjectionServiceName)).Should(Not(BeNil()))
		// injection istiod service & deployment
		g.Expect(objs.kind(gvk.Service.Kind).nameEquals(istiodInjectionServiceName)).Should(Not(BeNil()))
		g.Expect(objs.kind(gvk.Deployment.Kind).nameEquals(istiodInjectionServiceName)).Should(Not(BeNil()))

		g.Expect(objs.kind(gvk.ServiceAccount.Kind).nameEquals("istio-reader-service-account")).Should(Not(BeNil()))

		mwc := mustGetMutatingWebhookConfiguration(g, objs, "istio-sidecar-injector").Unstructured.Object
		g.Expect(mwc).Should(HavePathValueEqual(PathValue{"webhooks.[0].clientConfig.service.name", istiodInjectionServiceName}))

		ep := mustGetEndpointSlice(g, objs, istiodPrimaryXDSServiceName).Unstructured.Object
		g.Expect(objs.kind(gvk.Service.Kind).nameEquals(istiodPrimaryXDSServiceName)).Should(Not(BeNil()))
		g.Expect(ep).Should(HavePathValueEqual(PathValue{"endpoints.[0].addresses.[0]", "169.10.112.88"}))
		g.Expect(ep).Should(HavePathValueContain(PathValue{"ports.[0]", portVal("tcp-istiod", 15012, -1)}))

		// validation webhook
		vwc := mustGetValidatingWebhookConfiguration(g, objs, "istio-validator-istio-system").Unstructured.Object
		g.Expect(vwc).Should(HavePathValueEqual(PathValue{"webhooks.[0].clientConfig.service.name", istiodInjectionServiceName}))

		checkClusterRoleBindingsReferenceRoles(g, objs)
	}
}

func TestPrune(t *testing.T) {
	recreateSimpleTestEnv()
	tmpDir := t.TempDir()
	tmpCharts := chartSourceType(filepath.Join(tmpDir, operatorSubdirFilePath))
	err := copyDir(string(liveCharts), string(tmpCharts))
	if err != nil {
		t.Fatal(err)
	}

	rs, err := readFile(filepath.Join(testDataDir, "input-extra-resources", "envoyfilter"+".yaml"))
	if err != nil {
		t.Fatal(err)
	}

	err = writeFile(filepath.Join(tmpDir, operatorSubdirFilePath+"/"+testIstioDiscoveryChartPath+"/"+"default.yaml"), []byte(rs))
	if err != nil {
		t.Fatal(err)
	}
	_ = fakeControllerReconcile(t, "default", tmpCharts)

	// Install a default revision should not cause any error
	objs := fakeControllerReconcile(t, "empty", tmpCharts)

	for _, s := range uninstall2.PrunedResourcesSchemas() {
		remainedObjs := objs.kind(s.Kind)
		if remainedObjs.size() == 0 {
			continue
		}
		for _, v := range remainedObjs.objMap {
			// exclude operator objects, which will not be pruned
			if strings.Contains(v.GetName(), "istio-operator") {
				continue
			}
			t.Fatalf("obj %s/%s is not pruned", v.GetNamespace(), v.GetName())
		}
	}
}

func TestManifestGenerateAllOff(t *testing.T) {
	g := NewWithT(t)
	m := generateManifest(t, "all_off", "", liveCharts, nil)
	objs := parseObjectSetFromManifest(t, m)
	g.Expect(objs.size()).Should(Equal(0))
}

func TestManifestGenerateFlagsMinimalProfile(t *testing.T) {
	g := NewWithT(t)
	// Change profile from empty to minimal using flag.
	m := generateManifest(t, "empty", "-s profile=minimal", liveCharts, []string{"templates/deployment.yaml"})
	objs := parseObjectSetFromManifest(t, m)
	// minimal profile always has istiod, empty does not.
	mustGetDeployment(g, objs, "istiod")
}

func TestManifestGenerateFlagsSetHubTag(t *testing.T) {
	g := NewWithT(t)
	m := generateManifest(t, "minimal", "-s hub=foo -s tag=bar", liveCharts, []string{"templates/deployment.yaml"})
	objs := parseObjectSetFromManifest(t, m)

	dobj := mustGetDeployment(g, objs, "istiod")

	c := getContainer(dobj, "discovery")
	g.Expect(c).Should(HavePathValueEqual(PathValue{"image", "foo/pilot:bar"}))
}

func TestManifestGenerateFlagsSetValues(t *testing.T) {
	g := NewWithT(t)
	m := generateManifest(t, "default", "-s values.global.proxy.image=myproxy -s values.global.proxy.includeIPRanges=172.30.0.0/16,172.21.0.0/16", liveCharts,
		[]string{"templates/deployment.yaml", "templates/istiod-injector-configmap.yaml"})
	objs := parseObjectSetFromManifest(t, m)
	dobj := mustGetDeployment(g, objs, "istio-ingressgateway")

	c := getContainer(dobj, "istio-proxy")
	g.Expect(c).Should(HavePathValueEqual(PathValue{"image", "gcr.io/istio-testing/myproxy:latest"}))

	cm := objs.kind("ConfigMap").nameEquals("istio-sidecar-injector").Unstructured.Object
	// TODO: change values to some nicer format rather than text block.
	g.Expect(cm).Should(HavePathValueMatchRegex(PathValue{"data.values", `.*"includeIPRanges"\: "172\.30\.0\.0/16,172\.21\.0\.0/16".*`}))
}

func TestManifestGenerateFlags(t *testing.T) {
	runTestGroup(t, testGroup{
		{
			desc:                        "all_on",
			diffIgnore:                  "ConfigMap:*:istio",
			showOutputFileInPullRequest: true,
		},
		{
			desc:       "flag_values_enable_egressgateway",
			diffSelect: "Service:*:istio-egressgateway",
			fileSelect: []string{"templates/service.yaml"},
			flags:      "--set values.gateways.istio-egressgateway.enabled=true",
			noInput:    true,
		},
		{
			desc:       "flag_force",
			diffSelect: "no:resources:selected",
			fileSelect: []string{""},
			flags:      "--force",
		},
	})
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
			fileSelect: []string{"templates/deployment.yaml", "templates/autoscale.yaml"},
		},
		{
			desc:       "pilot_override_values",
			diffSelect: "Deployment:*:istiod,HorizontalPodAutoscaler:*:istiod",
			fileSelect: []string{"templates/deployment.yaml", "templates/autoscale.yaml"},
		},
		{
			desc: "pilot_override_kubernetes",
			// nolint: lll
			diffSelect: "Deployment:*:istiod, Service:*:istiod,PodDisruptionBudget:*:istiod,MutatingWebhookConfiguration:*:istio-sidecar-injector,ServiceAccount:*:istio-reader-service-account",
			fileSelect: []string{
				"templates/deployment.yaml", "templates/mutatingwebhook.yaml",
				"templates/service.yaml", "templates/reader-serviceaccount.yaml",
			},
		},
		// TODO https://github.com/istio/istio/issues/22347 this is broken for overriding things to default value
		// This can be seen from REGISTRY_ONLY not applying
		{
			desc:       "pilot_merge_meshconfig",
			diffSelect: "ConfigMap:*:istio$",
			fileSelect: []string{"templates/configmap.yaml", "templates/_helpers.tpl"},
		},
		{
			desc:       "pilot_disable_tracing",
			diffSelect: "ConfigMap:*:istio$",
		},
		{
			desc:       "autoscaling_ingress_v2",
			diffSelect: "HorizontalPodAutoscaler:*:istiod,HorizontalPodAutoscaler:*:istio-ingressgateway",
			fileSelect: []string{"templates/autoscale.yaml"},
		},
		{
			desc:       "autoscaling_v2",
			diffSelect: "HorizontalPodAutoscaler:*:istiod,HorizontalPodAutoscaler:*:istio-ingressgateway",
			fileSelect: []string{"templates/autoscale.yaml"},
		},
		{
			desc:        "pilot_env_var_from",
			diffSelect:  "Deployment:*:istiod",
			fileSelect:  []string{"templates/deployment.yaml"},
			chartSource: liveCharts,
		},
		{
			desc:        "networkpolicy_enabled",
			diffSelect:  "NetworkPolicy:*:istiod",
			fileSelect:  []string{"templates/networkpolicy.yaml"},
			chartSource: liveCharts,
		},
	})
}

func TestManifestGenerateCni(t *testing.T) {
	runTestGroup(t, testGroup{
		{
			desc:       "istio-cni",
			diffSelect: "DaemonSet:*:istio-cni",
		},
		{
			desc:       "istio-cni_tolerations",
			diffSelect: "DaemonSet:*:istio-cni",
		},
	})
}

func TestManifestGenerateZtunnel(t *testing.T) {
	runTestGroup(t, testGroup{
		{
			desc:       "ztunnel",
			diffSelect: "DaemonSet:*:ztunnel",
		},
		{
			desc:       "ztunnel_tolerations",
			diffSelect: "DaemonSet:*:ztunnel",
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
	got1, err := runManifestGenerate([]string{inPath}, "", snapshotCharts, nil)
	if err != nil {
		t.Fatal(err)
	}
	got2, err := runManifestGenerate([]string{inPath}, "", snapshotCharts, nil)
	if err != nil {
		t.Fatal(err)
	}

	if got1 != got2 {
		fmt.Printf("%s", testhelpers.YAMLDiff(got1, got2))
		t.Errorf("stable_manifest: Manifest generation is not producing stable text output.")
	}
}

func TestManifestGenerateFlagAliases(t *testing.T) {
	inPath := filepath.Join(testDataDir, "input/all_on.yaml")
	gotSet, err := runManifestGenerate([]string{inPath}, "--set revision=foo", snapshotCharts, []string{"templates/deployment.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	gotAlias, err := runManifestGenerate([]string{inPath}, "--revision=foo", snapshotCharts, []string{"templates/deployment.yaml"})
	if err != nil {
		t.Fatal(err)
	}

	if gotAlias != gotSet {
		t.Errorf("Flag aliases not producing same output: with --set: \n\n%s\n\nWith alias:\n\n%s\nDiff:\n\n%s\n",
			gotSet, gotAlias, testhelpers.YAMLDiff(gotSet, gotAlias))
	}
}

func TestMultiICPSFiles(t *testing.T) {
	inPathBase := filepath.Join(testDataDir, "input/all_off.yaml")
	inPathOverride := filepath.Join(testDataDir, "input/helm_values_enablement.yaml")
	got, err := runManifestGenerate([]string{inPathBase, inPathOverride}, "", snapshotCharts, []string{"templates/deployment.yaml", "templates/service.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(testDataDir, "output/helm_values_enablement"+goldenFileSuffixHideChangesInReview)

	want, err := readFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	diffSelect := "Deployment:*:istio-egressgateway, Service:*:istio-egressgateway"
	got = operatortest.FilterManifest(t, got, diffSelect)
	assert.Equal(t, got, want)
}

func TestBareSpec(t *testing.T) {
	inPathBase := filepath.Join(testDataDir, "input/bare_spec.yaml")
	_, err := runManifestGenerate([]string{inPathBase}, "", liveCharts, []string{"templates/deployment.yaml"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMultipleSpecOneFile(t *testing.T) {
	inPathBase := filepath.Join(testDataDir, "input/multiple_iops.yaml")
	_, err := runManifestGenerate([]string{inPathBase}, "", liveCharts, []string{"templates/deployment.yaml"})
	if !strings.Contains(err.Error(), "contains multiple IstioOperator CRs, only one per file is supported") {
		t.Fatalf("got %v, expected error for file with multiple IOPs", err)
	}
}

func TestBareValues(t *testing.T) {
	inPathBase := filepath.Join(testDataDir, "input/bare_values.yaml")
	// As long as the generate doesn't panic, we pass it.  bare_values.yaml doesn't
	// overlay well because JSON doesn't handle null values, and our charts
	// don't expect values to be blown away.
	_, _ = runManifestGenerate([]string{inPathBase}, "", liveCharts, []string{"templates/deployment.yaml"})
}

func TestBogusControlPlaneSec(t *testing.T) {
	inPathBase := filepath.Join(testDataDir, "input/bogus_cps.yaml")
	_, err := runManifestGenerate([]string{inPathBase}, "", liveCharts, []string{"templates/deployment.yaml"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestInstallPackagePath(t *testing.T) {
	runTestGroup(t, testGroup{
		{
			// Use some arbitrary small test input (pilot only) since we are testing the local filesystem code here, not
			// manifest generation.
			desc:       "install_package_path",
			diffSelect: "Deployment:*:istiod",
			flags:      "--set installPackagePath=" + string(liveCharts),
		},
	})
}

// TestTrailingWhitespace ensures there are no trailing spaces in the manifests
// This is important because `kubectl edit` and other commands will get escaped if they are present
// making it hard to read/edit
func TestTrailingWhitespace(t *testing.T) {
	got, err := runManifestGenerate([]string{}, "--set values.gateways.istio-egressgateway.enabled=true", liveCharts, nil)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(got, "\n")
	for i, l := range lines {
		if strings.HasSuffix(l, " ") {
			t.Errorf("Line %v has a trailing space: [%v]. Context: %v", i, l, strings.Join(lines[i-25:i], "\n"))
		}
	}
}

func validateReferentialIntegrity(t *testing.T, objs []manifest.Manifest, cname string, deploymentSelector map[string]string) {
	t.Run(cname, func(t *testing.T) {
		deployment := mustFindObject(t, objs, cname, gvk.Deployment.Kind)
		service := mustFindObject(t, objs, cname, gvk.Service.Kind)
		pdb := mustFindObject(t, objs, cname, manifest.PodDisruptionBudget)
		hpa := mustFindObject(t, objs, cname, manifest.HorizontalPodAutoscaler)
		podLabels := mustGetLabels(t, deployment, "spec.template.metadata.labels")
		// Check all selectors align
		mustSelect(t, mustGetLabels(t, pdb, "spec.selector.matchLabels"), podLabels)
		mustSelect(t, mustGetLabels(t, service, "spec.selector"), podLabels)
		mustSelect(t, mustGetLabels(t, deployment, "spec.selector.matchLabels"), podLabels)
		if hpaName := mustGetPath(t, hpa, "spec.scaleTargetRef.name"); cname != hpaName {
			t.Fatalf("HPA does not match deployment: %v != %v", cname, hpaName)
		}

		serviceAccountName := mustGetPath(t, deployment, "spec.template.spec.serviceAccountName").(string)
		mustFindObject(t, objs, serviceAccountName, gvk.ServiceAccount.Kind)

		// Check we aren't changing immutable fields. This only matters for in place upgrade (non revision)
		// This one is not a selector, it must be an exact match
		if sel := mustGetLabels(t, deployment, "spec.selector.matchLabels"); !reflect.DeepEqual(deploymentSelector, sel) {
			t.Fatalf("Depployment selectors are immutable, but changed since 1.5. Was %v, now is %v", deploymentSelector, sel)
		}
	})
}

// This test enforces that objects that reference other objects do so properly, such as Service selecting deployment
func TestConfigSelectors(t *testing.T) {
	selectors := []string{
		"templates/deployment.yaml",
		"templates/service.yaml",
		"templates/poddisruptionbudget.yaml",
		"templates/autoscale.yaml",
		"templates/serviceaccount.yaml",
	}
	flags := "--set values.gateways.istio-egressgateway.enabled=true " +
		"--set values.gateways.istio-ingressgateway.autoscaleMin=2 " +
		"--set values.gateways.istio-egressgateway.autoscaleMin=2 " +
		"--set values.pilot.autoscaleMin=2"
	got, err := runManifestGenerate([]string{}, flags, liveCharts, selectors)
	if err != nil {
		t.Fatal(err)
	}
	objs := parseObjectSetFromManifest(t, got).objSlice
	gotRev, e := runManifestGenerate([]string{}, fmt.Sprintf("%s %s", flags, "--set revision=canary"), liveCharts, selectors)
	if e != nil {
		t.Fatal(e)
	}
	objsRev := parseObjectSetFromManifest(t, gotRev).objSlice

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
		deployment := mustFindObject(t, objs, cname, gvk.Deployment.Kind)
		service := mustFindObject(t, objs, cname, gvk.Service.Kind)
		pdb := mustFindObject(t, objs, cname, manifest.PodDisruptionBudget)
		podLabels := mustGetLabels(t, deployment, "spec.template.metadata.labels")

		// Next we fetch all the objects for a revision install
		nameRev := "istiod-canary"
		deploymentRev := mustFindObject(t, objsRev, nameRev, gvk.Deployment.Kind)
		hpaRev := mustFindObject(t, objsRev, nameRev, manifest.HorizontalPodAutoscaler)
		serviceRev := mustFindObject(t, objsRev, nameRev, gvk.Service.Kind)
		pdbRev := mustFindObject(t, objsRev, nameRev, manifest.PodDisruptionBudget)
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
// -ldflags "-X istio.io/istio/pkg/version.buildHub=myhub -X istio.io/istio/pkg/version.buildVersion=mytag"
// results in these values showing up in a generated manifest.
func TestLDFlags(t *testing.T) {
	tmpHub, tmpTag := version.DockerInfo.Hub, version.DockerInfo.Tag
	defer func() {
		version.DockerInfo.Hub, version.DockerInfo.Tag = tmpHub, tmpTag
	}()
	version.DockerInfo.Hub = "testHub"
	version.DockerInfo.Tag = "testTag"
	_, vals, err := render.GenerateManifest(nil, []string{"installPackagePath=" + string(liveCharts)}, true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, vals.GetPathString("spec.hub"), version.DockerInfo.Hub)
	assert.Equal(t, vals.GetPathString("spec.tag"), version.DockerInfo.Tag)
}

// TestManifestGenerateStructure makes some basic assertions about the structure of GeneratedManifests output.
// This is to ensure that we only generate a single ManifestSet per component-type (in this case ingress gateways).
// prevent an `istioctl install` regression of https://github.com/istio/istio/issues/53875
func TestManifestGenerateStructure(t *testing.T) {
	multiGatewayFile := filepath.Join(testDataDir, "input/gateways.yaml")
	sets, _, err := render.GenerateManifest([]string{multiGatewayFile}, []string{}, false, nil, nil)
	assert.NoError(t, err)
	assert.Equal(t, len(sets), 1) // if this produces more than 1 ManifestSet it will cause a deadlock during install
	gateways := sets[0].Manifests
	assert.Equal(t, len(gateways), 18) // 6 kube resources * 3 gateways
}

func runTestGroup(t *testing.T, tests testGroup) {
	for _, tt := range tests {
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
			got, err := runManifestGenerate(filenames, tt.flags, csource, tt.fileSelect)
			if err != nil {
				t.Fatal(err)
			}

			if tt.diffSelect != "" {
				got = operatortest.FilterManifest(t, got, tt.diffSelect)
			}

			tutil.RefreshGoldenFile(t, []byte(got), outPath)

			want := string(tutil.ReadFile(t, outPath))
			if got != want {
				t.Fatal(cmp.Diff(got, want))
			}
		})
	}
}

func generateManifest(t test.Failer, inFile, flags string, chartSource chartSourceType, fileSelect []string) string {
	inPath := filepath.Join(testDataDir, "input", inFile+".yaml")
	manifest, err := runManifestGenerate([]string{inPath}, flags, chartSource, fileSelect)
	if err != nil {
		t.Fatalf("error %s: %s", err, manifest)
	}
	return manifest
}

// runManifestGenerate runs the manifest generate command. If filenames is set, passes the given filenames as -f flag,
// flags is passed to the command verbatim. If you set both flags and path, make sure to not use -f in flags.
func runManifestGenerate(filenames []string, flags string, chartSource chartSourceType, fileSelect []string) (string, error) {
	return runManifestCommand("generate", filenames, flags, chartSource, fileSelect)
}

func mustGetWebhook(t test.Failer, obj manifest.Manifest) []v1.MutatingWebhook {
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
	got, err := runManifestGenerate([]string{}, setFlags, liveCharts, []string{"templates/mutatingwebhook.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	objs := parseObjectSetFromManifest(t, got).objSlice
	return mustGetWebhook(t, mustFindObject(t, objs, webhookName, gvk.MutatingWebhookConfiguration.Kind))
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

	defaultWebhook := getWebhooks(t, "", "istio-sidecar-injector")
	revWebhook := getWebhooks(t, "--set revision=canary", "istio-sidecar-injector-canary")
	autoWebhook := getWebhooks(t, "--set values.sidecarInjectorWebhook.enableNamespacesByDefault=true", "istio-sidecar-injector")

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
