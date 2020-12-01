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
	"context"
	"io/ioutil"
	"testing"

	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/verifier"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/image"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

func TestPostInstallControlPlaneVerification(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.istioctl.postinstall_verify").
		Run(func(ctx framework.TestContext) {
			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})
			cs := ctx.Environment().Clusters().Default()
			s, err := image.SettingsFromCommandLine()
			if err != nil {
				t.Fatal(err)
			}
			cleanupCluster(t, cs, istioCtl)
			installCmd := []string{
				"install",
				"--set", "hub=" + s.Hub,
				"--set", "tag=" + s.Tag,
				"--manifests=" + ManifestPath,
				"-y",
			}
			if _, _, err := istioCtl.Invoke(installCmd); err != nil {
				logClusterState(cs)
				t.Fatalf("Failed to install Istio: %v", err)
			}

			scopes.Framework.Infof("verify control-plane health")
			tfLogger := clog.NewConsoleLogger(ioutil.Discard, ioutil.Discard, scopes.Framework)
			statusVerifier := verifier.NewStatusVerifier(IstioNamespace, ManifestPath, "",
				"", []string{}, clioptions.ControlPlaneOptions{}, tfLogger, nil)
			if err := statusVerifier.Verify(); err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() {
				if err = purgeIstioResources(istioCtl); err != nil {
					logClusterState(cs)
					t.Fatalf("error during cleanup: %v", err)
				}
			})
		})
}

func cleanupCluster(t *testing.T, cs resource.Cluster, istioCtl istioctl.Instance) {
	cleanupInClusterCRs(t, cs)
	if err := purgeIstioResources(istioCtl); err != nil {
		scopes.Framework.Warnf("Failed to purge istio resources: %v", err)
	}
}

func purgeIstioResources(istioCtl istioctl.Instance) error {
	scopes.Framework.Infof("Cleaned up all Istio resources")
	uninstallCmd := []string{
		"experimental",
		"uninstall",
		"--purge", "-y",
	}
	_, _, err := istioCtl.Invoke(uninstallCmd)
	return err
}

func logClusterState(cs resource.Cluster) {
	scopes.Framework.Infof("Getting Istio pods")
	pods, err := cs.GetIstioPods(context.TODO(), IstioNamespace, map[string]string{})
	if err != nil {
		scopes.Framework.Infof("Failed to get istio pods: %v", err)
	}
	for _, pod := range pods {
		name, ns, phase := pod.Name, pod.Namespace, pod.Status.Phase
		scopes.Framework.Infof("pod: %s/%s (%v)", name, ns, phase)
		for _, cst := range pod.Status.ContainerStatuses {
			scopes.Framework.Infof("  %s: ready=%v, state=%v", cst.Name, cst.Ready, cst.State)
		}
	}
}
