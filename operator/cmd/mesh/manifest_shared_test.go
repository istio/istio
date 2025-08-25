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
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/testing"
	"sigs.k8s.io/yaml"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/operator/pkg/install"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/render"
	uninstall2 "istio.io/istio/operator/pkg/uninstall"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/operator/pkg/util/progress"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test"
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

	allNamespacedGVKs = append(uninstall2.NamespacedResources(),
		schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Endpoints"})
	// CRDs are not in the prune list, but must be considered for tests.
	allClusterGVKs = uninstall2.ClusterResources
)

// recreateSimpleTestEnv mocks fake kube api server which relies on a simple object tracker
func recreateSimpleTestEnv() {
	log.Infof("Creating simple test environment\n")
	testClient = SetupFakeClient()
}

func SetupFakeClient() kube.CLIClient {
	testClient := kube.NewFakeClient()
	df := testClient.Dynamic().(*dynamicfake.FakeDynamicClient)
	df.PrependReactor("patch", "*", func(action testing.Action) (bool, runtime.Object, error) {
		patch := action.(testing.PatchAction)
		// Apply patches are supposed to upsert, but fake client fails if the object doesn't exist,
		// if an apply patch occurs for an object that doesn't yet exist, create it.
		if patch.GetPatchType() != types.ApplyPatchType {
			return false, nil, nil
		}
		_, err := df.Tracker().Get(patch.GetResource(), patch.GetNamespace(), patch.GetName())
		if err != nil && kerrors.IsNotFound(err) {
			us := &unstructured.Unstructured{}
			if err := yaml.Unmarshal(patch.GetPatch(), us); err != nil {
				return true, nil, err
			}
			if err := df.Tracker().Create(patch.GetResource(), us, patch.GetNamespace()); err != nil {
				return true, nil, err
			}
			o, err := df.Tracker().Get(patch.GetResource(), patch.GetNamespace(), patch.GetName())
			return true, o, err
		}
		return false, nil, nil
	})
	return testClient
}

// runManifestCommands runs all testedManifestCmds commands with the given input IOP file, flags and chartSource.
// It returns an ObjectSet for each cmd type.
// nolint: unparam
func runManifestCommands(t test.Failer, inFile, flags string, chartSource chartSourceType, fileSelect []string) map[cmdType]*ObjectSet {
	out := make(map[cmdType]*ObjectSet)
	for _, cmd := range testedManifestCmds {
		log.Infof("\nRunning test command using %s\n", cmd)

		var objs *ObjectSet
		switch cmd {
		case cmdGenerate:
			m := generateManifest(t, inFile, flags, chartSource, fileSelect)
			objs = parseObjectSetFromManifest(t, m)
		case cmdApply:
			objs = fakeApplyManifest(t, inFile, flags, chartSource)
		case cmdController:
			objs = fakeControllerReconcile(t, inFile, chartSource)
		default:
		}
		out[cmd] = objs
	}

	return out
}

// fakeApplyManifest runs istioctl install.
func fakeApplyManifest(t test.Failer, inFile, flags string, chartSource chartSourceType) *ObjectSet {
	inPath := filepath.Join(testDataDir, "input", inFile+".yaml")
	manifest, err := runManifestCommand("install", []string{inPath}, flags, chartSource, nil)
	if err != nil {
		t.Fatalf("error %s: %s", err, manifest)
	}
	return NewObjectSet(getAllIstioObjects(testClient))
}

func fakeControllerReconcile(t test.Failer, inFile string, chartSource chartSourceType) *ObjectSet {
	t.Helper()
	c := SetupFakeClient()
	res, err := fakeControllerReconcileInternal(c, inFile, chartSource)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func fakeControllerReconcileInternal(c kube.CLIClient, inFile string, chartSource chartSourceType) (*ObjectSet, error) {
	l := clog.NewDefaultLogger()
	manifests, values, err := render.GenerateManifest([]string{inFileAbsolutePath(inFile)}, []string{"installPackagePath=" + string(chartSource)}, false, c, nil)
	if err != nil {
		return nil, err
	}

	installer := install.Installer{
		Force:          false,
		DryRun:         false,
		SkipWait:       true,
		Kube:           c,
		Logger:         l,
		Values:         values,
		ProgressLogger: progress.NewLog(),
	}
	if err := installer.InstallManifests(manifests); err != nil {
		return nil, err
	}

	return NewObjectSet(getAllIstioObjects(c)), nil
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
		filters = append(filters, "templates/_affinity.tpl", "templates/_helpers.tpl", "templates/zzz_profile.yaml")
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
func getAllIstioObjects(c kube.CLIClient) []manifest.Manifest {
	var out []manifest.Manifest
	for _, gvk := range append(allClusterGVKs, allNamespacedGVKs...) {
		c, err := c.DynamicClientFor(gvk, nil, "")
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
			mf, err := manifest.FromObject(&o)
			if err != nil {
				log.Error(err.Error())
				continue
			}
			out = append(out, mf)
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
