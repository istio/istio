//go:build integ

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
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/operator/pkg/component"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/uninstall"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/test"
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

var allGVKs = append(uninstall.NamespacedResources(), uninstall.ClusterCPResources...)

func TestUninstallByRevision(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			t.NewSubTest("uninstall_revision").Run(func(t framework.TestContext) {
				istioCtl := istioctl.NewOrFail(t, istioctl.Config{})
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
		Run(func(t framework.TestContext) {
			t.NewSubTest("uninstall_revision_notfound").Run(func(t framework.TestContext) {
				istioCtl := istioctl.NewOrFail(t, istioctl.Config{})
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
		Run(func(t framework.TestContext) {
			t.NewSubTest("uninstall_revision").Run(func(t framework.TestContext) {
				istioCtl := istioctl.NewOrFail(t, istioctl.Config{})
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
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			istioCtl := istioctl.NewOrFail(t, istioctl.Config{})

			createIstioOperatorTempFile := func(name, revision string) (fileName string) {
				tempFile, err := os.CreateTemp("", name)
				if err != nil {
					t.Fatalf("failed to create temp file: %v", err)
				}
				defer tempFile.Close()
				contents := fmt.Sprintf(`apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  name: %s
  namespace: istio-system
spec:
  profile: remote
  revision: %s
`, name, revision)
				if _, err = tempFile.WriteString(contents); err != nil {
					t.Fatalf("failed to write to temp file: %v", err)
				}
				return tempFile.Name()
			}

			// Creating custom installation and empty uninstallation files
			customFileName := createIstioOperatorTempFile("custom-install", canaryRevision)
			randomFileName := createIstioOperatorTempFile("random-uninstall", canaryRevision)

			// Install webhook with custom file
			istioCtl.InvokeOrFail(t, []string{"install", "-f", customFileName, "--skip-confirmation"})

			// Check if custom webhook is installed
			validateWebhookExistence := func() {
				ls := fmt.Sprintf("%s=%s", manifest.IstioComponentLabel, component.PilotComponentName)
				cs := t.Clusters().Default()
				objs, _ := getRemainingResourcesCluster(cs, gvr.MutatingWebhookConfiguration, ls)
				if len(objs) == 0 {
					t.Fatal("expected custom webhook to exist")
				}
			}

			validateWebhookExistence()

			// Uninstall with a different file (should have no effect)
			istioCtl.InvokeOrFail(t, []string{"uninstall", "-f", randomFileName, "-r" + canaryRevision, "--skip-confirmation"})

			// Check the webhook still exists
			validateWebhookExistence()

			// Uninstall with the correct file
			istioCtl.InvokeOrFail(t, []string{"uninstall", "-f=" + customFileName, "-r=" + canaryRevision, "--skip-confirmation"})

			// Check no resources from the custom file exist
			checkCPResourcesUninstalled(t, t.Clusters().Default(), allGVKs,
				fmt.Sprintf("%s=%s", manifest.IstioComponentLabel, component.PilotComponentName), true)
		})
}

func TestUninstallPurge(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			istioCtl := istioctl.NewOrFail(t, istioctl.Config{})
			uninstallCmd := []string{
				"uninstall",
				"--purge", "--skip-confirmation",
			}
			istioCtl.InvokeOrFail(t, uninstallCmd)
			cs := t.Clusters().Default()
			checkCPResourcesUninstalled(t, cs, allGVKs, manifest.IstioComponentLabel, true)
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
			return errors.New(msg)
		}
		return nil
	}
	// for other cases, we expect base component resources to be kept.
	if len(reStrList) != 0 {
		for _, remaining := range reItemList {
			labels := remaining.GetLabels()
			cn, ok := labels["operator.istio.io/component"]
			// we don't need to check the legacy addons here because we would not install that in test anymore.
			if ok && cn != string(component.BaseComponentName) {
				return fmt.Errorf("expect only base component resources still exist")
			}
		}
	} else {
		return fmt.Errorf("expect base component resources to exist but they were removed")
	}
	return nil
}
