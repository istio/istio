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
	"istio.io/istio/pkg/kube"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/pkg/log"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	testClient         kube.CLIClient

	allNamespacedGVKs = append(helmreconciler.NamespacedResources(),
		schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Endpoints"})
	// CRDs are not in the prune list, but must be considered for tests.
	allClusterGVKs = helmreconciler.ClusterResources
)

func init() {
	helmreconciler.TestMode = true
}

// recreateSimpleTestEnv mocks fake kube api server which relies on a simple object tracker
func recreateSimpleTestEnv() {
	log.Infof("Creating simple test environment\n")
	helmreconciler.TestMode = true
	// TODO: interceptorFunc
	testClient = kube.NewFakeClient()
}

// runManifestCommands runs all testedManifestCmds commands with the given input IOP file, flags and chartSource.
// It returns an ObjectSet for each cmd type.
// nolint: unparam
func runManifestCommands(inFile, flags string, chartSource chartSourceType, fileSelect []string) (map[cmdType]*ObjectSet, error) {
	out := make(map[cmdType]*ObjectSet)
	for _, cmd := range testedManifestCmds {
		log.Infof("\nRunning test command using %s\n", cmd)
		switch cmd {
		case cmdApply, cmdController:
			if err := fakeApplyExtraResources(inFile); err != nil {
				return nil, err
			}
		default:
		}

		var objs *ObjectSet
		var err error
		switch cmd {
		case cmdGenerate:
			m, err := generateManifest(inFile, flags, chartSource, fileSelect)
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
	manifest, err := runManifestCommand("install", []string{inPath}, flags, chartSource, nil)
	if err != nil {
		return nil, fmt.Errorf("error %s: %s", err, manifest)
	}
	return NewObjectSet(getAllIstioObjects()), nil
}

// fakeApplyExtraResources applies any extra resources for the given test name.
func fakeApplyExtraResources(inFile string) error {
	// TODO
	//reconciler, err := helmreconciler.NewHelmReconciler(testClient, nil, nil, nil)
	//if err != nil {
	//	return err
	//}
	//
	//if rs, err := readFile(filepath.Join(testDataDir, "input-extra-resources", inFile+".yaml")); err == nil {
	//	if err := applyWithReconciler(reconciler, rs); err != nil {
	//		return err
	//	}
	//}
	return nil
}

func fakeControllerReconcile(inFile string, chartSource chartSourceType) (*ObjectSet, error) {
	//TOOO
	//c := kube.NewFakeClientWithVersion("25")
	//l := clog.NewDefaultLogger()
	//_, iop, err := manifest.GenerateIstioOperator(
	//	[]string{inFileAbsolutePath(inFile)},
	//	[]string{"installPackagePath=" + string(chartSource)},
	//	false, c, l)
	//if err != nil {
	//	return nil, err
	//}
	//
	//iop.Spec.InstallPackagePath = string(chartSource)

	// TODO
	//reconciler, err := helmreconciler.NewHelmReconciler(testClient, c, iop, nil)
	//if err != nil {
	//	return nil, err
	//}
	//
	//if err := reconciler.Reconcile(); err != nil {
	//	return nil, err
	//}

	return NewObjectSet(getAllIstioObjects()), nil
}

// runManifestCommand runs the given manifest command. If filenames is set, passes the given filenames as -f flag,
// flags is passed to the command verbatim. If you set both flags and path, make sure to not use -f in flags.
func runManifestCommand(command string, filenames []string, flags string, chartSource chartSourceType, fileSelect []string) (string, error) {
	cmd := InstallCmd
	args := ""
	if command != "install" {
		args = command
		cmd = ManifestCmd
	}
	for _, f := range filenames {
		args += " -f " + f
	}
	if flags != "" {
		args += " " + flags
	}
	if fileSelect != nil {
		filters := []string{}
		filters = append(filters, fileSelect...)
		// Everything needs these
		filters = append(filters, "templates/_affinity.tpl", "templates/_helpers.tpl", "templates/zzz_profile.yaml", "zzy_descope_legacy.yaml")
		args += " --filter " + strings.Join(filters, ",")
	}
	args += " --set installPackagePath=" + string(chartSource)
	return runCommand(cmd, args)
}

// runCommand runs the given command string.
func runCommand(cmdGen func(ctx cli.Context) *cobra.Command, args string) (string, error) {
	var out bytes.Buffer
	cli := cli.NewFakeContext(&cli.NewFakeContextOption{
		Version: "25",
	})
	sargs := strings.Split(args, " ")
	cmd := cmdGen(cli)
	cmd.SetOut(&out)
	cmd.SetArgs(sargs)

	err := cmd.Execute()
	return out.String(), err
}

// getAllIstioObjects lists all Istio GVK resources from the testClient.
func getAllIstioObjects() object.K8sObjects {
	var out object.K8sObjects
	for _, gvk := range append(allClusterGVKs, allNamespacedGVKs...) {
		c, err := testClient.DynamicClientFor(gvk, nil, "")
		if err != nil {
			log.Error(err.Error())
			continue
		}
		objects, err := c.List(context.Background(), metav1.ListOptions{})
		if err != nil {
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
	b, err := os.ReadFile(path)
	return string(b), err
}

// writeFile writes a file and returns an error if operation is unsuccessful.
func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}

// inFileAbsolutePath returns the absolute path for an input file like "gateways".
func inFileAbsolutePath(inFile string) string {
	return filepath.Join(testDataDir, "input", inFile+".yaml")
}
