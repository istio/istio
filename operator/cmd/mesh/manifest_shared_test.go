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
	"bytes"
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/cache"
	"istio.io/istio/operator/pkg/controller/istiocontrolplane"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/test/env"
)

// cmdType is one of the commands used to generate and optionally apply a manifest.
type cmdType int

const (
	// istioctl manifest generate
	cmdGenerate cmdType = iota
	// istioctl manifest apply or istioctl install
	cmdApply
	// in-cluster controller
	cmdController
)

// chartSourceType defines where charts used in the test come from.
type chartSourceType int

const (
	// Snapshot charts are in testdata/manifest-generate/data-snapshot
	snapshotCharts chartSourceType = iota
	// Compiled in charts come from assets.gen.go
	compiledInCharts
	// Live charts come from manifests/
	liveCharts
)

const (
	istioTestVersion = "istio-1.5.0"
	testTGZFilename  = istioTestVersion + "-linux.tar.gz"
	testDataSubdir   = "cmd/mesh/testdata/manifest-generate"
)

// Golden output files add a lot of noise to pull requests. Use a unique suffix so
// we can hide them by default. This should match one of the `linuguist-generated=true`
// lines in istio.io/istio/.gitattributes.
const (
	goldenFileSuffixHideChangesInReview = ".golden.yaml"
	goldenFileSuffixShowChangesInReview = ".golden-show-in-gh-pull-request.yaml"
)

var (
	// By default, tests only run with manifest generate, since it doesn't require any external fake test environment.
	testedManifestCmds = []cmdType{cmdGenerate}

	// All below paths are dynamically derived and absolute.

	// Path to the operator root dir.
	operatorRootDir string
	// Dir for testdata for istioctl commands.
	testDataDir string
	// Path to the manifests/ dir in istio root dir.
	manifestsDir string
	// A release dir with the live profiles and charts is created in this dir for tests.
	liveReleaseDir string
	// Path to the operator install base dir in the live release.
	liveInstallPackageDir string

	// Only used if kubebuilder is installed.
	testenv               *envtest.Environment
	testClient            client.Client
	testReconcileOperator *istiocontrolplane.ReconcileIstioOperator

	allNamespacedGVKs = []schema.GroupVersionKind{
		{Group: "autoscaling", Version: "v2beta1", Kind: "HorizontalPodAutoscaler"},
		{Group: "policy", Version: "v1beta1", Kind: "PodDisruptionBudget"},
		{Group: "apps", Version: "v1", Kind: "Deployment"},
		{Group: "apps", Version: "v1", Kind: "DaemonSet"},
		{Group: "", Version: "v1", Kind: "Service"},
		{Group: "", Version: "v1", Kind: "ConfigMap"},
		{Group: "", Version: "v1", Kind: "PersistentVolumeClaim"},
		{Group: "", Version: "v1", Kind: "Pod"},
		{Group: "", Version: "v1", Kind: "Secret"},
		{Group: "", Version: "v1", Kind: "ServiceAccount"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "RoleBinding"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "Role"},
		{Group: "networking.istio.io", Version: "v1alpha3", Kind: "DestinationRule"},
		{Group: "networking.istio.io", Version: "v1alpha3", Kind: "EnvoyFilter"},
		{Group: "networking.istio.io", Version: "v1alpha3", Kind: "Gateway"},
		{Group: "networking.istio.io", Version: "v1alpha3", Kind: "VirtualService"},
		{Group: "security.istio.io", Version: "v1beta1", Kind: "PeerAuthentication"},
	}

	// ordered by which types should be deleted, first to last
	allClusterGVKs = []schema.GroupVersionKind{
		{Group: "admissionregistration.k8s.io", Version: "v1beta1", Kind: "MutatingWebhookConfiguration"},
		{Group: "admissionregistration.k8s.io", Version: "v1beta1", Kind: "ValidatingWebhookConfiguration"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding"},
		{Group: "apiextensions.k8s.io", Version: "v1beta1", Kind: "CustomResourceDefinition"},
	}
)

// TestMain is required to create a local release package in /tmp from manifests and operator/data in the format that
// istioctl expects. It also brings up and tears down the kubebuilder test environment if it is installed.
func TestMain(m *testing.M) {
	var err error

	// If kubebuilder is installed, use that test env for apply and controller testing.
	if kubeBuilderInstalled() {
		testenv = &envtest.Environment{}
		testRestConfig, err = testenv.Start()
		checkExit(err)

		testK8Interface, err = kubernetes.NewForConfig(testRestConfig)
		checkExit(err)
		testClient, err = client.New(testRestConfig, client.Options{Scheme: scheme.Scheme})
		checkExit(err)
		// TestMode is required to not wait in the go client for resources that will never be created in the test server.
		helmreconciler.TestMode = true
		testReconcileOperator = istiocontrolplane.NewReconcileIstioOperator(testClient, testRestConfig, scheme.Scheme)

		// Add manifest apply and controller to the list of commands to run tests against.
		// XXX TEST
		// testedManifestCmds = append(testedManifestCmds, cmdApply, cmdController)
		testedManifestCmds = []cmdType{cmdController}
	}
	defer func() {
		if kubeBuilderInstalled() {
			testenv.Stop()
		}
	}()

	operatorRootDir = filepath.Join(env.IstioSrc, "operator")
	manifestsDir = filepath.Join(operatorRootDir, "manifests")
	liveReleaseDir, err = createLocalReleaseCharts()
	defer os.RemoveAll(liveReleaseDir)
	checkExit(err)
	liveInstallPackageDir = filepath.Join(liveReleaseDir, istioTestVersion, helm.OperatorSubdirFilePath)
	snapshotInstallPackageDir = filepath.Join(operatorRootDir, testDataSubdir, "data-snapshot")

	flag.Parse()
	code := m.Run()
	os.Exit(code)
}

// createLocalReleaseCharts creates local release charts using the create_release_charts.sh script and returns a path
// to the resulting dir.
func createLocalReleaseCharts() (string, error) {
	releaseDir, err := ioutil.TempDir(os.TempDir(), "istio-test-release-*")
	if err != nil {
		return "", err
	}
	releaseSubDir := filepath.Join(releaseDir, istioTestVersion, helm.OperatorSubdirFilePath)
	cmd := exec.Command("../../scripts/create_release_charts.sh", "-o", releaseSubDir)
	if stdo, err := cmd.Output(); err != nil {
		return "", fmt.Errorf("%s: \n%s", err, string(stdo))
	}
	return releaseDir, nil
}

// runManifestCommands runs all given commands with the given input IOP file, flags and chartSource. It returns
// an objectSet for each cmd type.
func runManifestCommands(inFile, flags string, chartSource chartSourceType) (map[cmdType]*objectSet, error) {
	out := make(map[cmdType]*objectSet)
	for _, cmd := range testedManifestCmds {
		switch cmd {
		case cmdGenerate:
			m, _, err := generateManifest(inFile, flags, chartSource)
			if err != nil {
				return nil, err
			}
			out[cmdGenerate], err = parseObjectSetFromManifest(m)
			if err != nil {
				return nil, err
			}

		case cmdApply:
			objs, err := fakeApplyManifest(inFile, flags, chartSource)
			if err != nil {
				return nil, err
			}
			out[cmdApply] = NewObjectSet(objs)
		case cmdController:
			objs, err := fakeControllerReconcile(inFile, chartSource)
			if err != nil {
				return nil, err
			}
			out[cmdController] = NewObjectSet(objs)
		default:
		}
	}

	return out, nil
}

// fakeApplyManifest runs manifest apply. It is assumed that
func fakeApplyManifest(inFile, flags string, chartSource chartSourceType) (object.K8sObjects, error) {
	inPath := filepath.Join(operatorRootDir, "cmd/mesh/testdata/manifest-generate/input", inFile+".yaml")
	manifest, err := runManifestCommand("apply", []string{inPath}, flags, chartSource)
	if err != nil {
		return nil, fmt.Errorf("error %s: %s", err, manifest)
	}
	return getAllIstioObjects()
}

func fakeControllerReconcile(inFile string, chartSource chartSourceType) (object.K8sObjects, error) {
	l := clog.NewDefaultLogger()
	inPath := filepath.Join(operatorRootDir, "cmd/mesh/testdata/manifest-generate/input", inFile+".yaml")

	ysf, err := yamlFromSetFlags([]string{"installPackagePath=" + chartSourceInstallPackagePath(chartSource)}, true, l)
	if err != nil {
		return nil, err
	}

	_, iops, err := GenerateConfig([]string{inPath}, ysf, false, testRestConfig, l)
	if err != nil {
		return nil, err
	}

	crName := installedSpecCRPrefix
	if iops.Revision != "" {
		crName += "-" + iops.Revision
	}
	iop, err := translate.IOPStoIOP(iops, crName, v1alpha1.Namespace(iops))
	if err != nil {
		return nil, err
	}
	iop.Spec.InstallPackagePath = chartSourceInstallPackagePath(chartSource)

	if err := createNamespace(testK8Interface, iop.Namespace); err != nil {
		return nil, err
	}

	reconciler, err := helmreconciler.NewHelmReconciler(testClient, testRestConfig, nil, nil)
	if err != nil {
		return nil, err
	}
	if err := removeAllIstioResourcesFromServer(reconciler); err != nil {
		return nil, err
	}

	iopStr, err := util.MarshalWithJSONPB(iop)
	if err != nil {
		return nil, err
	}
	if err := saveIOPToCluster(reconciler, iopStr); err != nil {
		return nil, err
	}

	req := reconcile.Request{}
	req.Namespace = iop.Namespace
	if _, err = testReconcileOperator.Reconcile(req); err != nil {
		return nil, err
	}
	return getAllIstioObjects()
}

// runManifestCommand runs the given manifest command. If filenames is set, passes the given filenames as -f flag,
// flags is passed to the command verbatim. If you set both flags and path, make sure to not use -f in flags.
func runManifestCommand(command string, filenames []string, flags string, chartSource chartSourceType) (string, error) {
	args := "manifest " + command
	for _, f := range filenames {
		args += " -f " + f
	}
	if flags != "" {
		args += " " + flags
	}
	args += " --set installPackagePath=" + chartSourceInstallPackagePath(chartSource)
	return runCommand(args)
}

// chartSourceInstallPackagePath returns the install path for the given chart source.
func chartSourceInstallPackagePath(chartSource chartSourceType) string {
	out := ""
	switch chartSource {
	case snapshotCharts:
		out = filepath.Join(testDataDir, "data-snapshot")
	case liveCharts:
		out = liveInstallPackageDir
	case compiledInCharts:
	default:
	}
	return out
}

// runCommand runs the given command string.
func runCommand(command string) (string, error) {
	var out bytes.Buffer
	rootCmd := GetRootCmd(strings.Split(command, " "))
	rootCmd.SetOut(&out)

	err := rootCmd.Execute()
	return out.String(), err
}

func removeAllIstioResourcesFromServer(reconciler *helmreconciler.HelmReconciler) error {
	// Needed in case we are running a test through this path that doesn't start a new process.
	cache.FlushObjectCaches()
	return reconciler.DeleteAll()
}

// getAllIstioObjects lists all Istio GVK resources from the testClient.
func getAllIstioObjects() (object.K8sObjects, error) {
	var out object.K8sObjects
	for _, gvk := range append(allClusterGVKs, allNamespacedGVKs...) {
		objects := &unstructured.UnstructuredList{}
		objects.SetGroupVersionKind(gvk)
		if err := testClient.List(context.TODO(), objects); err != nil {
			return nil, err
		}
		for _, o := range objects.Items {
			no := o.DeepCopy()
			out = append(out, object.NewK8sObject(no, nil, nil))
		}
	}
	return out, nil
}

// readFile reads a file and returns the contents.
func readFile(path string) (string, error) {
	b, err := ioutil.ReadFile(path)
	return string(b), err
}
