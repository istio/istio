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

package revisions

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	stableRevision = "stable"
)

var (
	ManifestPath = filepath.Join(env.IstioSrc, "manifests")
)

func TestUninstallByRevision(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.istioctl.uninstall_revision").
		Run(func(ctx framework.TestContext) {
			ctx.NewSubTest("uninstall_revision").Run(func(ctx framework.TestContext) {
				istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})
				uninstallCmd := []string{
					"x", "uninstall",
					"--revision=" + stableRevision, "--skip-confirmation",
				}
				out, _, err := istioCtl.Invoke(uninstallCmd)
				if err != nil {
					scopes.Framework.Errorf("failed to uninstall: %v, output: %v", err, out)
				}
				cs := ctx.Environment().(*kube.Environment).KubeClusters[0]

				retry.UntilSuccessOrFail(t, func() error {
					for _, gvk := range append(helmreconciler.NamespacedResources, helmreconciler.ClusterCPResources...) {
						resources := strings.ToLower(gvk.Kind) + "s"
						gvr := schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version, Resource: resources}
						ls := fmt.Sprintf("istio.io/rev=%s", stableRevision)
						if err := checkResourcesNotInCluster(cs, gvr, ls); err != nil {
							return err
						}
					}
					return nil
				}, retry.Delay(time.Millisecond*100), retry.Timeout(time.Second*120))
			})
		})
}

func TestUninstallByManifest(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.istioctl.uninstall_manifest").
		Run(func(ctx framework.TestContext) {
			workDir, err := ctx.CreateTmpDirectory("uninstall-test")
			if err != nil {
				t.Fatal("failed to create test directory")
			}
			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})
			iopFile := filepath.Join(workDir, "iop.yaml")
			iopYAML := `
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  revision: %s
  installPackagePath: %s
`
			iopYAML = fmt.Sprintf(iopYAML, stableRevision, ManifestPath)
			if err := ioutil.WriteFile(iopFile, []byte(iopYAML), os.ModePerm); err != nil {
				t.Fatalf("failed to write iop cr file: %v", err)
			}
			uninstallCmd := []string{
				"x", "uninstall",
				"--filename=" + iopFile, "--skip-confirmation",
			}
			istioCtl.InvokeOrFail(t, uninstallCmd)
			cs := ctx.Environment().(*kube.Environment).KubeClusters[0]
			retry.UntilSuccessOrFail(t, func() error {
				manifestMap, _, err := manifest.GenManifests([]string{iopFile}, []string{}, true, nil, nil)
				if err != nil {
					t.Fatalf("failed to generate manifest: %v", err)
				}
				manifests := manifestMap[name.PilotComponentName]
				objects, err := object.ParseK8sObjectsFromYAMLManifest(strings.Join(manifests, "---"))
				if err != nil {
					t.Fatalf("failed parse k8s objects from yaml: %v", err)
				}
				objMap := objects.ToMap()
				for _, obj := range objMap {
					resources := strings.ToLower(obj.Kind) + "s"
					gvr := schema.GroupVersionResource{Group: obj.Group, Version: obj.Version(), Resource: resources}
					ls := fmt.Sprintf("istio.io/rev=%s", stableRevision)
					if err := checkResourcesNotInCluster(cs, gvr, ls); err != nil {
						return err
					}
				}
				return nil
			}, retry.Delay(time.Millisecond*100), retry.Timeout(time.Second*120))
		})
}

func TestUninstallPurge(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.istioctl.uninstall_purge").
		Run(func(ctx framework.TestContext) {
			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})
			uninstallCmd := []string{
				"x", "uninstall",
				"--purge", "--skip-confirmation",
			}
			istioCtl.InvokeOrFail(t, uninstallCmd)
			cs := ctx.Environment().(*kube.Environment).KubeClusters[0]

			retry.UntilSuccessOrFail(t, func() error {
				for _, gvk := range append(helmreconciler.NamespacedResources, helmreconciler.AllClusterResources...) {
					resources := strings.ToLower(gvk.Kind) + "s"
					gvr := schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version, Resource: resources}
					if err := checkResourcesNotInCluster(cs, gvr, helmreconciler.IstioComponentLabelStr); err != nil {
						return err
					}
				}
				return nil
			}, retry.Delay(time.Millisecond*100), retry.Timeout(time.Second*180))
		})
}

func checkResourcesNotInCluster(cs kube.Cluster, gvr schema.GroupVersionResource, ls string) error {
	usList, _ := cs.Dynamic().Resource(gvr).List(context.TODO(), kubeApiMeta.ListOptions{LabelSelector: ls})
	if usList != nil && len(usList.Items) != 0 {
		var stalelist []string
		for _, item := range usList.Items {
			stalelist = append(stalelist, item.GroupVersionKind().String()+"/"+item.GetName())
		}
		msg := fmt.Sprintf("resources expected to be pruned but still exist in the cluster: %s",
			strings.Join(stalelist, " "))
		scopes.Framework.Warnf(msg)
		return fmt.Errorf(msg)
	}
	return nil
}
