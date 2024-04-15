//go:build integ
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

package helm

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/test/framework"
	kubecluster "istio.io/istio/pkg/test/framework/components/cluster/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/helm"
	"istio.io/istio/tests/util/sanitycheck"
)

// TestDefaultInstall tests Istio installation using Helm with default options
func TestDefaultInstall(t *testing.T) {
	overrideValuesStr := `
global:
  hub: %s
  tag: %s
  variant: %q
`
	framework.
		NewTest(t).
		Run(setupInstallation(overrideValuesStr, false, DefaultNamespaceConfig))
}

// TestAmbientInstall tests Istio ambient profile installation using Helm
func TestAmbientInstall(t *testing.T) {
	framework.
		NewTest(t).
		Run(setupInstallation(ambientProfileOverride, true, DefaultNamespaceConfig))
}

func TestAmbientInstallMultiNamespace(t *testing.T) {
	tests := []struct {
		name     string
		nsConfig NamespaceConfig
	}{{
		name: "isolated-istio-cni",
		nsConfig: NewNamespaceConfig(types.NamespacedName{
			Name: CniReleaseName, Namespace: "istio-cni",
		}),
	}, {
		name: "isolated-istio-cni-and-ztunnel",
		nsConfig: NewNamespaceConfig(types.NamespacedName{
			Name: CniReleaseName, Namespace: "istio-cni",
		}, types.NamespacedName{
			Name: ZtunnelReleaseName, Namespace: "kube-system",
		}),
	}, {
		name: "isolated-istio-cni-ztunnel-and-gateway",
		nsConfig: NewNamespaceConfig(types.NamespacedName{
			Name: CniReleaseName, Namespace: "istio-cni",
		}, types.NamespacedName{
			Name: ZtunnelReleaseName, Namespace: "ztunnel",
		}, types.NamespacedName{
			Name: IngressReleaseName, Namespace: "ingress-release",
		}),
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			framework.
				NewTest(t).
				Run(setupInstallation(ambientProfileOverride, true, tt.nsConfig))
		})
	}
}

func setupInstallation(overrideValuesStr string, isAmbient bool, config NamespaceConfig) func(t framework.TestContext) {
	return func(t framework.TestContext) {
		workDir, err := t.CreateTmpDirectory("helm-install-test")
		if err != nil {
			t.Fatal("failed to create test directory")
		}
		cs := t.Clusters().Default().(*kubecluster.Cluster)
		h := helm.New(cs.Filename())
		s := t.Settings()
		overrideValues := fmt.Sprintf(overrideValuesStr, s.Image.Hub, s.Image.Tag, s.Image.Variant)
		overrideValuesFile := filepath.Join(workDir, "values.yaml")
		if err := os.WriteFile(overrideValuesFile, []byte(overrideValues), os.ModePerm); err != nil {
			t.Fatalf("failed to write iop cr file: %v", err)
		}
		t.Cleanup(func() {
			if !t.Failed() {
				return
			}
			if t.Settings().CIMode {
				for _, ns := range config.AllNamespaces() {
					namespace.Dump(t, ns)
				}
			}
		})
		InstallIstio(t, cs, h, overrideValuesFile, "", true, isAmbient, config)

		VerifyInstallation(t, cs, config, true, isAmbient)
		verifyValidation(t)

		sanitycheck.RunTrafficTest(t, t)
		t.Cleanup(func() {
			DeleteIstio(t, h, cs, config, isAmbient)
		})
	}
}
