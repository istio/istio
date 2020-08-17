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
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/gogo/protobuf/proto"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/cache"
	"istio.io/istio/operator/pkg/controller/istiocontrolplane"
	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/pkg/log"
)

// cmdType is one of the commands used to generate and optionally apply a manifest.
type cmdType string

const (
	// istioctl manifest generate
	cmdGenerate cmdType = "istioctl manifest generate"
	// istioctl install
	cmdApply cmdType = "istioctl install"
	// in-cluster controller
	cmdController cmdType = "operator controller"
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

	// Only used if kubebuilder is installed.
	testenv               *envtest.Environment
	testClient            client.Client
	testReconcileOperator *istiocontrolplane.ReconcileIstioOperator

	allNamespacedGVKs = append(helmreconciler.NamespacedResources,
		schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Endpoints"})
	// CRDs are not in the prune list, but must be considered for tests.
	allClusterGVKs = append(helmreconciler.ClusterResources,
		schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1beta1", Kind: "CustomResourceDefinition"})
)

func init() {
	if kubeBuilderInstalled() {
		// TestMode is required to not wait in the go client for resources that will never be created in the test server.
		helmreconciler.TestMode = true
		// Add install and controller to the list of commands to run tests against.
		testedManifestCmds = append(testedManifestCmds, cmdApply, cmdController)
	}
}

// recreateTestEnv (re)creates a kubebuilder fake API server environment. This is required for testing of the
// controller runtime, which is used in the operator.
func recreateTestEnv() error {
	// If kubebuilder is installed, use that test env for apply and controller testing.
	log.Infof("Recreating kubebuilder test environment\n")

	if testenv != nil {
		testenv.Stop()
	}

	var err error
	testenv = &envtest.Environment{}
	testRestConfig, err = testenv.Start()
	if err != nil {
		return err
	}

	testK8Interface, err = kubernetes.NewForConfig(testRestConfig)
	testRestConfig.QPS = 50
	testRestConfig.Burst = 100
	if err != nil {
		return err
	}

	s := scheme.Scheme
	s.AddKnownTypes(v1alpha1.SchemeGroupVersion, &v1alpha1.IstioOperator{})

	testClient, err = client.New(testRestConfig, client.Options{Scheme: s})
	if err != nil {
		return err
	}

	testReconcileOperator = istiocontrolplane.NewReconcileIstioOperator(testClient, testRestConfig, s)

	return nil
}

// runManifestCommands runs all testedManifestCmds commands with the given input IOP file, flags and chartSource.
// It returns an ObjectSet for each cmd type.
// nolint: unparam
func runManifestCommands(inFile, flags string, chartSource chartSourceType) (map[cmdType]*ObjectSet, error) {
	out := make(map[cmdType]*ObjectSet)
	for _, cmd := range testedManifestCmds {
		log.Infof("\nRunning test command using %s\n", cmd)
		switch cmd {
		case cmdApply, cmdController:
			if err := cleanTestCluster(); err != nil {
				return nil, err
			}
			if err := fakeApplyExtraResources(inFile); err != nil {
				return nil, err
			}
		default:
		}

		var objs *ObjectSet
		var err error
		switch cmd {
		case cmdGenerate:
			m, _, err := generateManifest(inFile, flags, chartSource)
			if err != nil {
				return nil, err
			}
			objs, err = parseObjectSetFromManifest(m)
			if err != nil {
				return nil, err
			}
		case cmdApply:
			objs, err = fakeApplyManifest(inFile, flags, chartSource)
		case cmdController:
			objs, err = fakeControllerReconcile(inFile, chartSource)
		default:
		}
		if err != nil {
			return nil, err
		}
		out[cmd] = objs
	}

	return out, nil
}

// fakeApplyManifest runs istioctl install.
func fakeApplyManifest(inFile, flags string, chartSource chartSourceType) (*ObjectSet, error) {
	inPath := filepath.Join(testDataDir, "input", inFile+".yaml")
	manifest, err := runManifestCommand("install", []string{inPath}, flags, chartSource)
	if err != nil {
		return nil, fmt.Errorf("error %s: %s", err, manifest)
	}
	return NewObjectSet(getAllIstioObjects()), nil
}

// fakeApplyExtraResources applies any extra resources for the given test name.
func fakeApplyExtraResources(inFile string) error {
	reconciler, err := helmreconciler.NewHelmReconciler(testClient, testRestConfig, nil, nil)
	if err != nil {
		return err
	}

	if rs, err := readFile(filepath.Join(testDataDir, "input-extra-resources", inFile+".yaml")); err == nil {
		if err := applyWithReconciler(reconciler, rs); err != nil {
			return err
		}
	}
	return nil
}

func fakeControllerReconcile(inFile string, chartSource chartSourceType) (*ObjectSet, error) {
	l := clog.NewDefaultLogger()
	_, iops, err := manifest.GenerateConfig(
		[]string{inFileAbsolutePath(inFile)},
		[]string{"installPackagePath=" + string(chartSource)},
		false, testRestConfig, l)
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
	iop.Spec.InstallPackagePath = string(chartSource)

	if err := createNamespace(testK8Interface, iop.Namespace); err != nil {
		return nil, err
	}

	reconciler, err := helmreconciler.NewHelmReconciler(testClient, testRestConfig, iop, nil)
	if err != nil {
		return nil, err
	}
	if err := fakeInstallOperator(reconciler, chartSource, iop); err != nil {
		return nil, err
	}

	if _, err := reconciler.Reconcile(); err != nil {
		return nil, err
	}

	return NewObjectSet(getAllIstioObjects()), nil
}

// fakeInstallOperator installs the operator manifest resources into a cluster using the given reconciler.
// The installation is for testing with a kubebuilder fake cluster only, since no functional Deployment will be
// created.
func fakeInstallOperator(reconciler *helmreconciler.HelmReconciler, chartSource chartSourceType, iop proto.Message) error {
	ocArgs := &operatorCommonArgs{
		manifestsPath:     string(chartSource),
		istioNamespace:    istioDefaultNamespace,
		watchedNamespaces: istioDefaultNamespace,
		operatorNamespace: operatorDefaultNamespace,
		// placeholders, since the fake API server does not actually pull images and create pods.
		hub: "fake hub",
		tag: "fake tag",
	}

	_, mstr, err := renderOperatorManifest(nil, ocArgs)
	if err != nil {
		return err
	}
	if err := applyWithReconciler(reconciler, mstr); err != nil {
		return err
	}
	iopStr, err := util.MarshalWithJSONPB(iop)
	if err != nil {
		return err
	}
	if err := saveIOPToCluster(reconciler, iopStr); err != nil {
		return err
	}

	return err
}

// applyWithReconciler applies the given manifest string using the given reconciler.
func applyWithReconciler(reconciler *helmreconciler.HelmReconciler, manifest string) error {
	m := name.Manifest{
		// Name is not important here, only Content will be applied.
		Name:    name.IstioOperatorComponentName,
		Content: manifest,
	}
	_, _, err := reconciler.ApplyManifest(m)
	return err
}

// runManifestCommand runs the given manifest command. If filenames is set, passes the given filenames as -f flag,
// flags is passed to the command verbatim. If you set both flags and path, make sure to not use -f in flags.
func runManifestCommand(command string, filenames []string, flags string, chartSource chartSourceType) (string, error) {
	var args string
	if command == "install" {
		args = "install"
	} else {
		args = "manifest " + command
	}
	for _, f := range filenames {
		args += " -f " + f
	}
	if flags != "" {
		args += " " + flags
	}
	args += " --set installPackagePath=" + string(chartSource)
	return runCommand(args)
}

// runCommand runs the given command string.
func runCommand(command string) (string, error) {
	var out bytes.Buffer
	rootCmd := GetRootCmd(strings.Split(command, " "))
	rootCmd.SetOut(&out)

	err := rootCmd.Execute()
	return out.String(), err
}

// cleanTestCluster resets the test cluster.
func cleanTestCluster() error {
	// Needed in case we are running a test through this path that doesn't start a new process.
	cache.FlushObjectCaches()
	if !kubeBuilderInstalled() {
		return nil
	}
	return recreateTestEnv()
}

// getAllIstioObjects lists all Istio GVK resources from the testClient.
func getAllIstioObjects() object.K8sObjects {
	var out object.K8sObjects
	for _, gvk := range append(allClusterGVKs, allNamespacedGVKs...) {
		objects := &unstructured.UnstructuredList{}
		objects.SetGroupVersionKind(gvk)
		if err := testClient.List(context.TODO(), objects); err != nil {
			log.Error(err.Error())
			continue
		}
		for _, o := range objects.Items {
			no := o.DeepCopy()
			out = append(out, object.NewK8sObject(no, nil, nil))
		}
	}
	return out
}

// readFile reads a file and returns the contents.
func readFile(path string) (string, error) {
	b, err := ioutil.ReadFile(path)
	return string(b), err
}

// inFileAbsolutePath returns the absolute path for an input file like "gateways".
func inFileAbsolutePath(inFile string) string {
	return filepath.Join(testDataDir, "input", inFile+".yaml")
}
