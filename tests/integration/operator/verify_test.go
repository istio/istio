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

package operator

import (
	"strings"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istioctl"
)

func TestPostInstallControlPlaneVerification(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.istioctl.postinstall_verify").
		Run(func(t framework.TestContext) {
			istioCtl := istioctl.NewOrFail(t, t, istioctl.Config{})
			cs := t.Environment().Clusters().Default()
			cleanupInClusterCRs(t, cs)
			t.Cleanup(func() {
				cleanupIstioResources(t, cs, istioCtl)
			})
			s := t.Settings()
			installCmd := []string{
				"install",
				"--set", "hub=" + s.Image.Hub,
				"--set", "tag=" + s.Image.Tag,
				"--manifests=" + ManifestPath,
				"-y",
			}
			istioCtl.InvokeOrFail(t, installCmd)

			verifyCmd := []string{
				"verify-install",
			}
			out, _ := istioCtl.InvokeOrFail(t, verifyCmd)
			if !strings.Contains(out, "verified successfully") {
				t.Fatalf("verify-install failed: %v", out)
			}
		})
}
