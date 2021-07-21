// +build integ
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

package cni

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/pilot/common"
)

var (
	i istio.Instance

	apps = &common.EchoDeployments{}
)

const (
	NMinusOne    = "1.10.3"
	CNIConfigDir = "tests/integration/pilot/testdata/upgrade"
)

// Currently only test CNI with one version behind.
var versions = []string{NMinusOne}

func TestCNIVersionSkew(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.cni").
		Run(func(t framework.TestContext) {
			for _, v := range versions {
				installCNIOrFail(t, v)
				podFetchFn := kube.NewSinglePodFetch(t.Clusters().Default(), "kube-system", "k8s-app=istio-cni-node")
				// Make sure CNI image has applied version.
				retry.UntilSuccessOrFail(t, func() error {
					pods, err := podFetchFn()
					if err != nil || len(pods) != 0 {
						return fmt.Errorf("failed to get CNI pods")
					}
					if len(pods) == 0 {
						return fmt.Errorf("cannot find any ready CNI pods")
					}
					for _, p := range pods {
						if !strings.Contains(p.Spec.Containers[0].Image, v) {
							return fmt.Errorf("pods does not match wanted CNI version")
						}
					}
					return nil
				})

				// Make sure CNI pod is ready
				_, err := kube.WaitUntilPodsAreReady(podFetchFn)
				if err != nil {
					t.Fatal(err)
				}

				// Restart all apps so that newly deployed CNI could configure IPTables for it.
				for _, app := range apps.All {
					app.Restart()
				}
				common.RunAllTrafficTests(t, i, apps)
			}
		})
}

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		// Label(label.Postsubmit).
		Setup(istio.Setup(&i, nil)).
		Setup(func(t resource.Context) error {
			return common.SetupApps(t, i, apps)
		}).
		Run()
}

func installCNIOrFail(t framework.TestContext, ver string) {
	cniFilePath := filepath.Join(env.IstioSrc, CNIConfigDir,
		fmt.Sprintf("%s-cni-install.yaml.tar", ver))
	config, err := file.ReadTarFile(cniFilePath)
	if err != nil {
		t.Fatalf("Failed to read CNI manifest %v", err)
	}
	t.Config().ApplyYAMLOrFail(t, "", config)
}
