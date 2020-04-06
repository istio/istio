// Copyright 2020 Istio Authors
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

package operator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"istio.io/istio/pkg/test/scopes"

	"github.com/golang/protobuf/jsonpb"
	"k8s.io/apimachinery/pkg/runtime/schema"

	api "istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/image"
	"istio.io/istio/pkg/test/framework/resource/environment"
)

const (
	IstioNamespace = "istio-system"
)

var (
	ManifestTestDataPath = filepath.Join(env.IstioSrc, "operator/cmd/mesh/testdata/manifest-generate/input")
	ProfilesPath         = filepath.Join(env.IstioSrc, "operator/data/profiles")
)

func TestController(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})
			workDir, err := ctx.CreateTmpDirectory("operator-controller-test")
			if err != nil {
				t.Fatal("failed to create test directory")
			}

			checkControllerInstallation(t, ctx, istioCtl, workDir, path.Join(ManifestTestDataPath, "all_on.yaml"))
			checkControllerInstallation(t, ctx, istioCtl, workDir, path.Join(ProfilesPath, "default.yaml"))
			checkControllerInstallation(t, ctx, istioCtl, workDir, path.Join(ProfilesPath, "demo.yaml"))
		})
}

// checkInstallStatus check the status of IstioOperator CR from the cluster
func checkInstallStatus(cs kube.Cluster) error {
	gvr := schema.GroupVersionResource{
		Group:    "install.istio.io",
		Version:  "v1alpha1",
		Resource: "istiooperators",
	}
	us, err := cs.GetUnstructured(gvr, "istio-system", "test-istiocontrolplane")
	if err != nil {
		return fmt.Errorf("failed to get istioOperator resource: %v", err)
	}
	usIOPStatus := us.UnstructuredContent()["status"].(map[string]interface{})
	iopStatusString, err := json.Marshal(usIOPStatus)
	if err != nil {
		return fmt.Errorf("failed to marshal istioOperator status: %v", err)
	}
	status := &api.InstallStatus{}
	jspb := jsonpb.Unmarshaler{AllowUnknownFields: true}
	if err := jspb.Unmarshal(bytes.NewReader(iopStatusString), status); err != nil {
		return fmt.Errorf("failed to unmarshal istioOperator status: %v", err)
	}
	if status.Status != api.InstallStatus_HEALTHY {
		return fmt.Errorf("expect IstioOperator status to be healthy, but got: %v", status.Status)
	}
	var errs util.Errors
	for cn, cnstatus := range status.ComponentStatus {
		if cnstatus.Status != api.InstallStatus_HEALTHY {
			errs = util.AppendErr(errs, fmt.Errorf("expect component: %s status to be healthy,"+
				" but got: %v", cn, cnstatus.Status))
		}
	}

	return errs.ToError()
}

func checkControllerInstallation(t *testing.T, ctx framework.TestContext, istioCtl istioctl.Instance, workDir string, iopFile string) {
	scopes.CI.Infof(fmt.Sprintf("=== Checking istio installation by operator controller with iop file: %s===\n", iopFile))
	s, err := image.SettingsFromCommandLine()
	if err != nil {
		t.Fatal(err)
	}
	iop, err := ioutil.ReadFile(iopFile)
	if err != nil {
		t.Fatalf("failed to read iop file: %v", err)
	}
	metadataYAML := `
metadata:
  name: test-istiocontrolplane
  namespace: istio-system
`
	iopcr, err := util.OverlayYAML(string(iop), metadataYAML)
	if err != nil {
		t.Fatalf("failed to overlay iop with metadata: %v", err)
	}
	iopCRFile := filepath.Join(workDir, "iop_cr.yaml")
	if err := ioutil.WriteFile(iopCRFile, []byte(iopcr), os.ModePerm); err != nil {
		t.Fatalf("failed to write iop cr file: %v", err)
	}
	initCmd := []string{
		"operator", "init",
		"--wait",
		"-f", iopCRFile,
		"--hub=" + s.Hub,
		"--tag=" + s.Tag,
	}
	// install istio using operator controller
	istioCtl.InvokeOrFail(t, initCmd)
	cs := ctx.Environment().(*kube.Environment).KubeClusters[0]

	// takes time for reconciliation to be done
	scopes.CI.Infof("waiting for reconciliation to be done")
	time.Sleep(60 * time.Second)
	if err := checkInstallStatus(cs); err != nil {
		t.Fatalf("IstioOperator status not healthy: %v", err)
	}
	if _, err := cs.CheckPodsAreReady(cs.NewPodFetch(IstioNamespace)); err != nil {
		t.Fatalf("pods are not ready: %v", err)
	}
	scopes.CI.Infof("=== Succeeded ===")
}
