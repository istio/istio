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
	"io"
	"testing"

	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/verifier"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/scopes"
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
			tfLogger := clog.NewConsoleLogger(io.Discard, io.Discard, scopes.Framework)
			statusVerifier, err := verifier.NewStatusVerifier(IstioNamespace, ManifestPath, "",
				"", []string{}, clioptions.ControlPlaneOptions{}, verifier.WithLogger(tfLogger))
			if err != nil {
				t.Fatal(err)
			}
			if err := statusVerifier.Verify(); err != nil {
				t.Fatal(err)
			}
		})
}
