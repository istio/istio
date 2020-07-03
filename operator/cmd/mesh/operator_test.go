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
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/test/env"
)

// TODO: rewrite this with running the actual top level command.
func TestOperatorDump(t *testing.T) {
	goldenFilepath := filepath.Join(env.IstioSrc, "operator/cmd/mesh/testdata/operator/output/operator-init.yaml")

	odArgs := &operatorDumpArgs{
		common: operatorCommonArgs{
			hub:               "foo.io/istio",
			tag:               "1.2.3",
			operatorNamespace: "operator-test-namespace",
			watchedNamespaces: "istio-test-namespace1,istio-test-namespace2",
		},
	}

	cmd := "operator dump --hub " + odArgs.common.hub
	cmd += " --tag " + odArgs.common.tag
	cmd += " --operatorNamespace " + odArgs.common.operatorNamespace
	cmd += " --istioNamespace " + odArgs.common.istioNamespace
	cmd += " --manifests=" + string(snapshotCharts)

	gotYAML, err := runCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}

	if refreshGoldenFiles() {
		t.Logf("Refreshing golden file for %s", goldenFilepath)
		if err := ioutil.WriteFile(goldenFilepath, []byte(gotYAML), 0644); err != nil {
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

// TODO: rewrite this with running the actual top level command.
func TestOperatorInit(t *testing.T) {
	goldenFilepath := filepath.Join(operatorRootDir, "cmd/mesh/testdata/operator/output/operator-init.yaml")
	rootArgs := &rootArgs{}
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
		if err := ioutil.WriteFile(goldenFilepath, []byte(gotYAML), 0644); err != nil {
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
