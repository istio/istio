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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/util/assert"
)

var (
	extendedClient kube.ExtendedClient
	kubeClient     client.Client
)

// TODO: rewrite this with running the actual top level command.
func TestOperatorDump(t *testing.T) {
	goldenFilepath := filepath.Join(env.IstioSrc, "operator/cmd/mesh/testdata/operator/output/operator-dump.yaml")

	odArgs := &operatorDumpArgs{
		common: operatorCommonArgs{
			hub:               "foo.io/istio",
			tag:               "1.2.3",
			imagePullSecrets:  []string{"imagePullSecret1,imagePullSecret2"},
			operatorNamespace: "operator-test-namespace",
			watchedNamespaces: "istio-test-namespace1,istio-test-namespace2",
		},
	}

	cmd := "operator dump --hub " + odArgs.common.hub
	cmd += " --tag " + odArgs.common.tag
	cmd += " --imagePullSecrets " + strings.Join(odArgs.common.imagePullSecrets, ",")
	cmd += " --operatorNamespace " + odArgs.common.operatorNamespace
	cmd += " --watchedNamespaces " + odArgs.common.watchedNamespaces
	cmd += " --manifests=" + string(snapshotCharts)

	gotYAML, err := runCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}

	if refreshGoldenFiles() {
		t.Logf("Refreshing golden file for %s", goldenFilepath)
		if err := os.WriteFile(goldenFilepath, []byte(gotYAML), 0o644); err != nil {
			t.Error(err)
		}
	}

	wantYAML, err := readFile(goldenFilepath)
	if err != nil {
		t.Fatal(err)
	}

	if diff := util.YAMLDiff(wantYAML, gotYAML); diff != "" {
		t.Fatalf("diff: %s", diff)
	}
}

func TestOperatorDumpJSONFormat(t *testing.T) {
	goldenFilepath := filepath.Join(env.IstioSrc, "operator/cmd/mesh/testdata/operator/output/operator-dump.json")

	odArgs := &operatorDumpArgs{
		common: operatorCommonArgs{
			hub:               "foo.io/istio",
			tag:               "1.2.3",
			imagePullSecrets:  []string{"imagePullSecret1,imagePullSecret2"},
			operatorNamespace: "operator-test-namespace",
			watchedNamespaces: "istio-test-namespace1,istio-test-namespace2",
			outputFormat:      jsonOutput,
		},
	}

	cmd := "operator dump --hub " + odArgs.common.hub
	cmd += " --tag " + odArgs.common.tag
	cmd += " --imagePullSecrets " + strings.Join(odArgs.common.imagePullSecrets, ",")
	cmd += " --operatorNamespace " + odArgs.common.operatorNamespace
	cmd += " --watchedNamespaces " + odArgs.common.watchedNamespaces
	cmd += " --manifests=" + string(snapshotCharts)
	cmd += " --output " + odArgs.common.outputFormat

	gotJSON, err := runCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}

	if refreshGoldenFiles() {
		t.Logf("Refreshing golden file for %s", goldenFilepath)
		if err := os.WriteFile(goldenFilepath, []byte(gotJSON), 0o644); err != nil {
			t.Error(err)
		}
	}

	wantJSON, err := readFile(goldenFilepath)
	if err != nil {
		t.Fatal(err)
	}

	var want, got []byte
	if got, err = yaml.JSONToYAML([]byte(gotJSON)); err != nil {
		t.Fatal(err)
	}
	if want, err = yaml.JSONToYAML([]byte(wantJSON)); err != nil {
		t.Fatal(err)
	}

	if diff := util.YAMLDiff(string(want), string(got)); diff != "" {
		t.Fatalf("diff: %s", diff)
	}
}

// TODO: rewrite this with running the actual top level command.
func TestOperatorInit(t *testing.T) {
	goldenFilepath := filepath.Join(operatorRootDir, "cmd/mesh/testdata/operator/output/operator-init.yaml")
	rootArgs := &RootArgs{}
	oiArgs := &operatorInitArgs{
		common: operatorCommonArgs{
			hub:               "foo.io/istio",
			tag:               "1.2.3",
			operatorNamespace: "operator-test-namespace",
			watchedNamespaces: "istio-test-namespace1,istio-test-namespace2",
			manifestsPath:     string(snapshotCharts),
		},
	}

	l := clog.NewConsoleLogger(os.Stdout, os.Stderr, installerScope)
	_, gotYAML, err := renderOperatorManifest(rootArgs, &oiArgs.common)
	if err != nil {
		l.LogAndFatal(err)
	}

	if refreshGoldenFiles() {
		t.Logf("Refreshing golden file for %s", goldenFilepath)
		if err := os.WriteFile(goldenFilepath, []byte(gotYAML), 0o644); err != nil {
			t.Error(err)
		}
	}

	wantYAML, err := readFile(goldenFilepath)
	if err != nil {
		t.Fatal(err)
	}

	if diff := util.YAMLDiff(wantYAML, gotYAML); diff != "" {
		t.Fatalf("diff: %s", diff)
	}
}

func MockKubernetesClients(_, _ string, _ clog.Logger) (kube.ExtendedClient, client.Client, error) {
	extendedClient = kube.MockClient{
		Interface: fake.NewSimpleClientset(),
	}
	kubeClient, _ = client.New(&rest.Config{}, client.Options{})
	return extendedClient, kubeClient, nil
}

func TestOperatorInitDryRun(t *testing.T) {
	tests := []struct {
		operatorNamespace string
		watchedNamespaces string
	}{
		{
			// default nss
			operatorNamespace: "",
			watchedNamespaces: "",
		},
		{
			operatorNamespace: "test",
			watchedNamespaces: "test1",
		},
		{
			operatorNamespace: "",
			watchedNamespaces: "test4, test5",
		},
	}

	kubeClients = MockKubernetesClients

	for _, test := range tests {
		t.Run("", func(t *testing.T) {
			args := []string{"operator", "init", "--dry-run"}
			if test.operatorNamespace != "" {
				args = append(args, "--operatorNamespace", test.operatorNamespace)
			}
			if test.watchedNamespaces != "" {
				args = append(args, "--watchedNamespaces", test.watchedNamespaces)
			}

			rootCmd := GetRootCmd(args)
			err := rootCmd.Execute()
			assert.NoError(t, err)

			readActions := map[string]bool{
				"get":   true,
				"list":  true,
				"watch": true,
			}

			actions := extendedClient.Kube().(*fake.Clientset).Actions()
			for _, action := range actions {
				if v := readActions[action.GetVerb()]; !v {
					t.Fatalf("unexpected action: %+v, expected %s", action.GetVerb(), "get")
				}
			}
		})
	}
}
