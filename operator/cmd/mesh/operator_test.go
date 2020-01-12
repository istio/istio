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
	"testing"
	"time"

	"github.com/kr/pretty"

	"istio.io/operator/pkg/kubectlcmd"
	"istio.io/operator/pkg/util"
)

// applyParams is used to capture the inputs to operatorInit applyManifest call.
type applyParams struct {
	manifest      string
	componentName string
	opts          kubectlcmd.Options
}

var (
	applyOutput  []applyParams
	deleteOutput = ""
)

func TestOperatorInit(t *testing.T) {
	goldenFilepath := filepath.Join(repoRootDir, "cmd/mesh/testdata/operator/output/operator-init.yaml")

	rootArgs := &rootArgs{}
	oiArgs := &operatorInitArgs{
		hub:               "foo.io/istio",
		tag:               "1.2.3",
		operatorNamespace: "operator-test-namespace",
		istioNamespace:    "istio-test-namespace",
	}

	operatorInit(rootArgs, oiArgs, NewLogger(rootArgs.logToStdErr, os.Stdout, os.Stderr), mockApplyManifest)
	gotYAML := ""
	for _, ao := range applyOutput {
		gotYAML += ao.manifest
	}

	fmt.Println(gotYAML)
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

	wantOpts := kubectlcmd.Options{
		WaitTimeout: time.Minute,
		Prune:       nil,
		ExtraArgs:   nil,
	}

	wantParams := []applyParams{
		{
			componentName: istioControllerComponentName,
			opts:          wantOpts,
		},
		{
			componentName: istioNamespaceComponentName,
			opts:          wantOpts,
		},
		{
			componentName: istioOperatorCRComponentName,
			opts:          wantOpts,
		},
	}

	for i, ao := range applyOutput {
		if got, want := ao.componentName, wantParams[i].componentName; got != want {
			t.Fatalf("wrong component name: got:%s, want:%s", got, want)
		}
		if !reflect.DeepEqual(ao.opts, wantParams[i].opts) {
			t.Fatalf("wrong opts: got:%v, want:%v", pretty.Sprint(ao.opts), pretty.Sprint(wantParams[i].opts))
		}
	}
}

func mockApplyManifest(manifestStr, componentName string, opts *kubectlcmd.Options, _ bool, _ *Logger) bool {
	applyOutput = append(applyOutput, applyParams{
		componentName: componentName,
		manifest:      manifestStr,
		opts:          *opts,
	})
	return true
}

func TestOperatorRemove(t *testing.T) {
	goldenFilepath := filepath.Join(repoRootDir, "cmd/mesh/testdata/operator/output/operator-remove.yaml")

	rootArgs := &rootArgs{}
	orArgs := &operatorRemoveArgs{
		operatorInitArgs: operatorInitArgs{
			hub:               "foo.io/istio",
			tag:               "1.2.3",
			operatorNamespace: "operator-test-namespace",
			istioNamespace:    "istio-test-namespace",
		},
		force: true,
	}

	operatorRemove(rootArgs, orArgs, NewLogger(rootArgs.logToStdErr, os.Stdout, os.Stderr), mockDeleteManifest)
	gotYAML := deleteOutput

	fmt.Println(gotYAML)
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

func mockDeleteManifest(manifestStr, _ string, _ *kubectlcmd.Options, _ *Logger) bool {
	deleteOutput = manifestStr
	return true
}
