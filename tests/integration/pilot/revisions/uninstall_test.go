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

package revisions

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/version"

	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	stableRevision       = "stable"
	canaryRevision       = "canary"
	notFoundRevision     = "not-found"
	checkResourceTimeout = time.Second * 120
	checkResourceDelay   = time.Millisecond * 100

	revisionNotFound = "could not find target revision"
)

var ManifestPath = filepath.Join(env.IstioSrc, "manifests")

var allGVKs = append(helmreconciler.NamespacedResources(&version.Info{Major: "1", Minor: "24"}), helmreconciler.ClusterCPResources...)

func TestUninstallByRevision(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.istioctl.uninstall_revision").
		Run(func(t framework.TestContext) {
			t.NewSubTest("uninstall_revision").Run(func(t framework.TestContext) {
				istioCtl := istioctl.NewOrFail(t, t, istioctl.Config{})
				uninstallCmd := []string{
					"uninstall",
					"--revision=" + stableRevision, "--skip-confirmation",
				}
				out, _, err := istioCtl.Invoke(uninstallCmd)
				if err != nil {
					scopes.Framework.Errorf("failed to uninstall: %v, output: %v", err, out)
				}
				cs := t.Clusters().Default()
				ls := fmt.Sprintf("istio.io/rev=%s", stableRevision)
				checkCPResourcesUninstalled(t, cs, allGVKs, ls, false)
			})
		})
}

func TestUninstallByNotFoundRevision(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.istioctl.uninstall_revision").
		Run(func(t framework.TestContext) {
			t.NewSubTest("uninstall_revision_notfound").Run(func(t framework.TestContext) {
				istioCtl := istioctl.NewOrFail(t, t, istioctl.Config{})
				uninstallCmd := []string{
					"uninstall",
					"--revision=" + notFoundRevision, "--dry-run",
				}
				_, actualError, _ := istioCtl.Invoke(uninstallCmd)
				if !strings.Contains(actualError, revisionNotFound) {
					scopes.Framework.Errorf("istioctl uninstall command expects to fail with error message: %s, but got: %s", revisionNotFound, actualError)
				}
			})
		})
}

func TestUninstallWithSetFlag(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.istioctl.uninstall_revision").
		Run(func(t framework.TestContext) {
			t.NewSubTest("uninstall_revision").Run(func(t framework.TestContext) {
				istioCtl := istioctl.NewOrFail(t, t, istioctl.Config{})
				uninstallCmd := []string{
					"uninstall", "--set",
					"revision=" + stableRevision, "--skip-confirmation",
				}
				out, _, err := istioCtl.Invoke(uninstallCmd)
				if err != nil {
					scopes.Framework.Errorf("failed to uninstall: %v, output: %v", err, out)
				}
				cs := t.Clusters().Default()
				ls := fmt.Sprintf("istio.io/rev=%s", stableRevision)
				checkCPResourcesUninstalled(t, cs, allGVKs, ls, false)
			})
		})
}

func TestUninstallCustomFile(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.istioctl.uninstall_file").
		Run(func(t framework.TestContext) {
			tempFile, err := os.CreateTemp("", "custom-install.yaml")
			if err != nil {
				t.Fatalf("failed to create temp file: %v", err)
			}

			// remote profile will install a webhook, which will be pruned with uninstall -f, and other webhook will be kept.
			customFile := fmt.Sprintf(`apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  name: custom-install
  namespace: istio-system
spec:
  profile: remote
  revision: %s
`, canaryRevision)
			if _, err = tempFile.WriteString(customFile); err != nil {
				t.Fatalf("failed to write to temp file: %v", err)
			}
			if err = tempFile.Close(); err != nil {
				t.Fatalf("failed to close temp file: %v", err)
			}

			// install gateway component with custom file
			istioCtl := istioctl.NewOrFail(t, t, istioctl.Config{})
			installCmd := []string{
				"install",
				"-f", tempFile.Name(), "--skip-confirmation",
			}
			istioCtl.InvokeOrFail(t, installCmd)

			// check custom webhook is installed
			ls := fmt.Sprintf("%s=%s", helmreconciler.IstioComponentLabelStr, name.IstiodRemoteComponentName)
			cs := t.Clusters().Default()
			objs, _ := getRemainingResourcesCluster(cs, gvr.MutatingWebhookConfiguration, ls)
			if len(objs) == 0 {
				t.Fatalf("expect custom webhook to exist")
			}

			for _, ogvk := range allGVKs {
				ogvr, ok := gvk.ToGVR(config.GroupVersionKind{
					Group:   ogvk.Group,
					Version: ogvk.Version,
					Kind:    ogvk.Kind,
				})
				if !ok {
					continue
				}
				objs, _ = getRemainingResourcesCluster(cs, ogvr, ls)
				if len(objs) != 0 {
					t.Logf("hanxiaop: existing resources kind %v: %v", ogvk, objs)
				}
			}

			uninstallCmd := []string{
				"uninstall",
				"--filename=" + tempFile.Name(),
				"-r=" + canaryRevision,
				"--skip-confirmation",
			}
			istioCtl.InvokeOrFail(t, uninstallCmd)

			// should have no resource from the custom file
			checkCPResourcesUninstalled(t, cs, allGVKs, ls, true)

			// should still have other components that are not installed by the custom file
			ls = fmt.Sprintf("istio.io/rev=%s", canaryRevision)
			objs, _ = getRemainingResourcesCluster(cs, gvr.Deployment, ls)
			if len(objs) == 0 {
				t.Fatalf("expect other components to exist but were removed")
			}
		})
}

func TestUninstallPurge(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.istioctl.uninstall_purge").
		Run(func(t framework.TestContext) {
			istioCtl := istioctl.NewOrFail(t, t, istioctl.Config{})
			uninstallCmd := []string{
				"uninstall",
				"--purge", "--skip-confirmation",
			}
			istioCtl.InvokeOrFail(t, uninstallCmd)
			cs := t.Clusters().Default()
			checkCPResourcesUninstalled(t, cs, allGVKs, helmreconciler.IstioComponentLabelStr, true)
		})
}

// checkCPResourcesUninstalled is a helper function to check list of gvk resources matched with label are uninstalled
// If purge is set to true, we expect all resources are removed.
// Otherwise we expect only selected resources from control plane are removed, resources from base and the legacy addon installation would not be touched.
func checkCPResourcesUninstalled(t test.Failer, cs cluster.Cluster, gvkResources []schema.GroupVersionKind, label string, purge bool) {
	retry.UntilSuccessOrFail(t, func() error {
		var reStrlist []string
		var reItemList []unstructured.Unstructured
		for _, gvk := range gvkResources {
			resources := strings.ToLower(gvk.Kind) + "s"
			gvr := schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version, Resource: resources}
			reList, reStr := getRemainingResourcesCluster(cs, gvr, label)
			reItemList = append(reItemList, reList...)
			reStrlist = append(reStrlist, reStr...)
		}
		return inspectRemainingResources(reItemList, reStrlist, purge)
	}, retry.Delay(checkResourceDelay), retry.Timeout(checkResourceTimeout))
}

// getRemainingResourcesCluster get specific resources from the cluster
func getRemainingResourcesCluster(cs cluster.Cluster, gvr schema.GroupVersionResource, ls string) ([]unstructured.Unstructured, []string) {
	usList, _ := cs.Dynamic().Resource(gvr).List(context.TODO(), metav1.ListOptions{LabelSelector: ls})
	var remainingResources []unstructured.Unstructured
	var staleList []string
	if usList != nil && len(usList.Items) != 0 {
		for _, item := range usList.Items {
			// ignore IstioOperator CRD because the operator CR is not in the pruning list
			if item.GetName() == "istiooperators.install.istio.io" {
				continue
			}
			remainingResources = append(remainingResources, item)
			staleList = append(staleList, item.GroupVersionKind().String()+"/"+item.GetName())
		}
	}
	return remainingResources, staleList
}

func inspectRemainingResources(reItemList []unstructured.Unstructured, reStrList []string, purge bool) error {
	// for purge case we expect all resources removed
	if purge {
		if len(reStrList) != 0 {
			msg := fmt.Sprintf("resources expected to be pruned but still exist in the cluster: %s",
				strings.Join(reStrList, " "))
			scopes.Framework.Warnf(msg)
			return fmt.Errorf(msg)
		}
		return nil
	}
	// for other cases, we expect base component resources to be kept.
	if len(reStrList) != 0 {
		for _, remaining := range reItemList {
			labels := remaining.GetLabels()
			cn, ok := labels["operator.istio.io/component"]
			// we don't need to check the legacy addons here because we would not install that in test anymore.
			if ok && cn != string(name.IstioBaseComponentName) {
				return fmt.Errorf("expect only base component resources still exist")
			}
		}
	} else {
		return fmt.Errorf("expect base component resources to exist but they were removed")
	}
	return nil
}
