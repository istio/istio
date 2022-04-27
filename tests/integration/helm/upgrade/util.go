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

package helmupgrade

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	kubecluster "istio.io/istio/pkg/test/framework/components/cluster/kube"
	"istio.io/istio/pkg/test/helm"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/shell"
	"istio.io/istio/pkg/test/util/retry"
	helmtest "istio.io/istio/tests/integration/helm"
	"istio.io/istio/tests/util/sanitycheck"
)

const (
	gcrHub = "gcr.io/istio-release"

	defaultValues = `
global:
  hub: %s
  tag: %s

revision: "%s"
`
	tarGzSuffix = ".tar.gz"

	istioRevLabel     = "istio.io/rev"
	prodTag           = "prod"
	canaryTag         = "canary"
	latestRevisionTag = "latest"
)

// upgradeCharts upgrades Istio using Helm charts with the provided
// override values file to the latest charts in $ISTIO_SRC/manifests
func upgradeCharts(ctx framework.TestContext, h *helm.Helm, overrideValuesFile string) {
	execCmd := fmt.Sprintf(
		"kubectl apply -n %v -f %v",
		helmtest.IstioNamespace,
		filepath.Join(helmtest.ManifestsChartPath, helmtest.BaseChart, helmtest.CRDsFolder))
	_, err := shell.Execute(false, execCmd)
	if err != nil {
		ctx.Fatalf("couldn't run kubectl apply on crds folder: %v", err)
	}

	// Upgrade base chart
	err = h.UpgradeChart(helmtest.BaseReleaseName, filepath.Join(helmtest.ManifestsChartPath, helmtest.BaseChart),
		helmtest.IstioNamespace, overrideValuesFile, helmtest.Timeout, "--skip-crds")
	if err != nil {
		ctx.Fatalf("failed to upgrade istio %s chart", helmtest.BaseChart)
	}

	// Upgrade discovery chart
	err = h.UpgradeChart(helmtest.IstiodReleaseName, filepath.Join(helmtest.ManifestsChartPath, helmtest.ControlChartsDir, helmtest.DiscoveryChart),
		helmtest.IstioNamespace, overrideValuesFile, helmtest.Timeout)
	if err != nil {
		ctx.Fatalf("failed to upgrade istio %s chart", helmtest.DiscoveryChart)
	}

	// Upgrade ingress gateway chart
	err = h.UpgradeChart(helmtest.IngressReleaseName, filepath.Join(helmtest.ManifestsChartPath, helmtest.GatewayChartsDir, helmtest.IngressGatewayChart),
		helmtest.IstioNamespace, overrideValuesFile, helmtest.Timeout)
	if err != nil {
		ctx.Fatalf("failed to upgrade istio %s chart", helmtest.IngressGatewayChart)
	}

	// Upgrade egress gateway chart
	err = h.UpgradeChart(helmtest.EgressReleaseName, filepath.Join(helmtest.ManifestsChartPath, helmtest.GatewayChartsDir, helmtest.EgressGatewayChart),
		helmtest.IstioNamespace, overrideValuesFile, helmtest.Timeout)
	if err != nil {
		ctx.Fatalf("failed to upgrade istio %s chart", helmtest.EgressGatewayChart)
	}
}

// deleteIstio deletes installed Istio Helm charts and resources
func deleteIstio(cs cluster.Cluster, h *helm.Helm, gatewayChartsInstalled bool) error {
	scopes.Framework.Infof("cleaning up resources")
	if gatewayChartsInstalled {
		if err := h.DeleteChart(helmtest.EgressReleaseName, helmtest.IstioNamespace); err != nil {
			return fmt.Errorf("failed to delete %s release", helmtest.EgressReleaseName)
		}
		if err := h.DeleteChart(helmtest.IngressReleaseName, helmtest.IstioNamespace); err != nil {
			return fmt.Errorf("failed to delete %s release", helmtest.IngressReleaseName)
		}
	}

	if err := h.DeleteChart(helmtest.IstiodReleaseName, helmtest.IstioNamespace); err != nil {
		return fmt.Errorf("failed to delete %s release", helmtest.IstiodReleaseName)
	}

	return cleanupIstio(cs, h)
}

func cleanupIstio(cs cluster.Cluster, h *helm.Helm) error {
	if err := h.DeleteChart(helmtest.BaseReleaseName, helmtest.IstioNamespace); err != nil {
		return fmt.Errorf("failed to delete %s release", helmtest.BaseReleaseName)
	}
	if err := cs.CoreV1().Namespaces().Delete(context.TODO(), helmtest.IstioNamespace, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("failed to delete istio namespace: %v", err)
	}
	if err := kubetest.WaitForNamespaceDeletion(cs, helmtest.IstioNamespace, retry.Timeout(helmtest.RetryTimeOut)); err != nil {
		return fmt.Errorf("waiting for istio namespace to be deleted: %v", err)
	}
	return nil
}

// deleteIstioCanary deletes installed Istio Helm charts and resources
func deleteIstioRevision(h *helm.Helm, revision string) error {
	scopes.Framework.Infof("cleaning up revision resources (%s)", revision)
	name := helmtest.IstiodReleaseName + "-" + strings.ReplaceAll(revision, ".", "-")
	if err := h.DeleteChart(name, helmtest.IstioNamespace); err != nil {
		return fmt.Errorf("failed to delete revision (%s)", name)
	}

	return nil
}

// getValuesOverrides returns the values file created to pass into Helm override default values
// for the hub and tag
func getValuesOverrides(ctx framework.TestContext, hub, tag, revision string) string {
	workDir := ctx.CreateTmpDirectoryOrFail("helm")
	overrideValues := fmt.Sprintf(defaultValues, hub, tag, revision)
	overrideValuesFile := filepath.Join(workDir, "values.yaml")
	if err := os.WriteFile(overrideValuesFile, []byte(overrideValues), os.ModePerm); err != nil {
		ctx.Fatalf("failed to write iop cr file: %v", err)
	}

	return overrideValuesFile
}

// performInPlaceUpgradeFunc returns the provided function necessary to run inside an integration test
// for upgrade capability
func performInPlaceUpgradeFunc(previousVersion string) func(framework.TestContext) {
	return func(t framework.TestContext) {
		cs := t.Clusters().Default().(*kubecluster.Cluster)
		h := helm.New(cs.Filename())

		t.CleanupConditionally(func() {
			// only need to do call this once as helm doesn't need to remove
			// all versions
			err := deleteIstio(cs, h, true)
			if err != nil {
				t.Fatalf("could not delete istio: %v", err)
			}
		})

		overrideValuesFile := getValuesOverrides(t, gcrHub, previousVersion, "")
		helmtest.InstallIstio(t, cs, h, tarGzSuffix, overrideValuesFile, helmtest.TestDataChartPath, previousVersion, true)
		helmtest.VerifyInstallation(t, cs, true)

		helmtest.VerifyMutatingWebhookConfigurations(t, cs, []string{
			"istio-sidecar-injector",
		})

		helmtest.ValidatingWebhookConfigurations(t, cs, []string{
			"istio-validator-istio-system",
		})

		_, oldClient, oldServer := sanitycheck.SetupTrafficTest(t, t, "")
		sanitycheck.RunTrafficTestClientServer(t, oldClient, oldServer)

		s := t.Settings()
		overrideValuesFile = getValuesOverrides(t, s.Image.Hub, s.Image.Tag, "")
		upgradeCharts(t, h, overrideValuesFile)
		helmtest.VerifyInstallation(t, cs, true)

		helmtest.VerifyMutatingWebhookConfigurations(t, cs, []string{
			"istio-sidecar-injector",
		})

		// in-place upgrades will only have the default validator from the new version
		helmtest.ValidatingWebhookConfigurations(t, cs, []string{
			"istiod-default-validator",
		})

		_, newClient, newServer := sanitycheck.SetupTrafficTest(t, t, "")
		sanitycheck.RunTrafficTestClientServer(t, newClient, newServer)

		// now check that we are compatible with N-1 proxy with N proxy
		sanitycheck.RunTrafficTestClientServer(t, oldClient, newServer)
	}
}

// performRevisionUpgradeFunc returns the provided function necessary to run inside an integration test
// for upgrade capability with revisions
func performRevisionUpgradeFunc(previousVersion, previousValidatingWebhookName string, validatingWebhookCarriesOver bool) func(framework.TestContext) {
	return func(t framework.TestContext) {
		cs := t.Clusters().Default().(*kubecluster.Cluster)
		h := helm.New(cs.Filename())

		t.CleanupConditionally(func() {
			err := deleteIstioRevision(h, canaryTag)
			if err != nil {
				t.Fatalf("could not delete istio: %v", err)
			}
			err = deleteIstio(cs, h, false)
			if err != nil {
				t.Fatalf("could not delete istio: %v", err)
			}
		})

		overrideValuesFile := getValuesOverrides(t, gcrHub, previousVersion, "")
		helmtest.InstallIstio(t, cs, h, tarGzSuffix, overrideValuesFile, helmtest.TestDataChartPath, previousVersion, false)
		helmtest.VerifyInstallation(t, cs, false)

		helmtest.VerifyMutatingWebhookConfigurations(t, cs, []string{
			"istio-sidecar-injector",
		})

		helmtest.ValidatingWebhookConfigurations(t, cs, []string{previousValidatingWebhookName})

		_, oldClient, oldServer := sanitycheck.SetupTrafficTest(t, t, "")
		sanitycheck.RunTrafficTestClientServer(t, oldClient, oldServer)

		s := t.Settings()
		overrideValuesFile = getValuesOverrides(t, s.Image.Hub, s.Image.Tag, canaryTag)
		helmtest.InstallIstioWithRevision(t, cs, h, "", "", canaryTag, overrideValuesFile, true, false)
		helmtest.VerifyInstallation(t, cs, false)

		helmtest.VerifyMutatingWebhookConfigurations(t, cs, []string{
			"istio-sidecar-injector",
			"istio-sidecar-injector-canary",
		})

		validatingWebhooks := []string{
			"istiod-default-validator",
		}

		if validatingWebhookCarriesOver {
			validatingWebhooks = append(validatingWebhooks, previousValidatingWebhookName)
		}

		helmtest.ValidatingWebhookConfigurations(t, cs, validatingWebhooks)

		_, newClient, newServer := sanitycheck.SetupTrafficTest(t, t, canaryTag)
		sanitycheck.RunTrafficTestClientServer(t, newClient, newServer)

		// now check that we are compatible with N-1 proxy with N proxy
		sanitycheck.RunTrafficTestClientServer(t, oldClient, newServer)
	}
}

// performRevisionTagsUpgradeFunc returns the provided function necessary to run inside an integration test
// for upgrade capability with stable label revision upgrades
func performRevisionTagsUpgradeFunc(previousVersion, previousValidatingWebhookName string, validatingWebhookCarriesOver bool) func(framework.TestContext) {
	return func(t framework.TestContext) {
		cs := t.Clusters().Default().(*kubecluster.Cluster)
		h := helm.New(cs.Filename())

		t.CleanupConditionally(func() {
			err := deleteIstioRevision(h, latestRevisionTag)
			if err != nil {
				t.Fatalf("could not delete istio revision (%v): %v", latestRevisionTag, err)
			}
			err = deleteIstioRevision(h, previousVersion)
			if err != nil {
				t.Fatalf("could not delete istio revision (%v): %v", previousVersion, err)
			}

			err = cleanupIstio(cs, h)
			if err != nil {
				t.Fatalf("could not cleanup istio: %v", err)
			}
		})

		// install MAJOR.MINOR.PATCH charts with revision set to "MAJOR-MINOR-PATCH" name. For example,
		// helm install istio-base ../tests/integration/helm/testdata/1.10.0/base.tar.gz --namespace istio-system -f values.yaml
		// helm install istiod-1-10 ../tests/integration/helm/testdata/1.10.0/istio-control/istio-discovery.tar.gz -f values.yaml
		previousRevision := strings.ReplaceAll(previousVersion, ".", "-")
		overrideValuesFile := getValuesOverrides(t, gcrHub, previousVersion, previousRevision)
		helmtest.InstallIstioWithRevision(t, cs, h, tarGzSuffix, previousVersion, previousRevision, overrideValuesFile, false, true)
		helmtest.VerifyInstallation(t, cs, false)

		// helm template istiod-1-10-0 ../tests/integration/helm/testdata/1.10.0/istio-control/istio-discovery.tar.gz
		//    -s templates/revision-tags.yaml --set revision=1-10-0 --set revisionTags={prod}
		helmtest.SetRevisionTag(t, h, tarGzSuffix, previousRevision, prodTag, helmtest.TestDataChartPath, previousVersion)
		helmtest.VerifyMutatingWebhookConfigurations(t, cs, []string{
			"istio-revision-tag-prod",
			fmt.Sprintf("istio-sidecar-injector-%s", previousRevision),
		})

		helmtest.ValidatingWebhookConfigurations(t, cs, []string{previousValidatingWebhookName})

		// setup istio.io/rev=1-10-0 for the default-1 namespace
		oldNs, oldClient, oldServer := sanitycheck.SetupTrafficTest(t, t, previousRevision)
		sanitycheck.RunTrafficTestClientServer(t, oldClient, oldServer)

		// install the charts from this branch with revision set to "latest"
		// helm upgrade istio-base ../manifests/charts/base --namespace istio-system -f values.yaml
		// helm install istiod-latest ../manifests/charts/istio-control/istio-discovery -f values.yaml
		s := t.Settings()
		overrideValuesFile = getValuesOverrides(t, s.Image.Hub, s.Image.Tag, latestRevisionTag)
		helmtest.InstallIstioWithRevision(t, cs, h, "", "", latestRevisionTag, overrideValuesFile, true, false)
		helmtest.VerifyInstallation(t, cs, false)

		// helm template istiod-latest ../manifests/charts/istio-control/istio-discovery --namespace istio-system
		//    -s templates/revision-tags.yaml --set revision=latest --set revisionTags={canary}
		helmtest.SetRevisionTag(t, h, "", latestRevisionTag, canaryTag, helmtest.ManifestsChartPath, "")
		helmtest.VerifyMutatingWebhookConfigurations(t, cs, []string{
			"istio-revision-tag-prod",
			fmt.Sprintf("istio-sidecar-injector-%v", previousRevision),
			"istio-revision-tag-canary",
			"istio-sidecar-injector-latest",
		})

		validatingWebhooks := []string{
			"istiod-default-validator",
		}

		if validatingWebhookCarriesOver {
			validatingWebhooks = append(validatingWebhooks, previousValidatingWebhookName)
		}

		// when installing from the latest charts default validator will be installed
		helmtest.ValidatingWebhookConfigurations(t, cs, validatingWebhooks)

		// setup istio.io/rev=latest for the default-2 namespace
		_, newClient, newServer := sanitycheck.SetupTrafficTest(t, t, latestRevisionTag)
		sanitycheck.RunTrafficTestClientServer(t, newClient, newServer)

		// now check that we are compatible with N-1 proxy with N proxy between a client
		// in default-1 namespace and a server in the default-2 namespace, respectively
		sanitycheck.RunTrafficTestClientServer(t, oldClient, newServer)

		// change the mutating webhook configuration to use the latest revision (istiod-latest service in istio-system)
		// helm template istiod-latest ../manifests/charts/istio-control/istio-discovery --namespace istio-system
		//    -s templates/revision-tags.yaml --set revision=latest --set revisionTags={prod}
		helmtest.SetRevisionTag(t, h, "", latestRevisionTag, prodTag, helmtest.ManifestsChartPath, "")

		// change the old namespace that was pointing to the old prod (1-10-0) to point to the
		// 'latest' revision by setting the `istio.io/rev=prod` label on the namespace
		err := oldNs.SetLabel(istioRevLabel, prodTag)
		if err != nil {
			t.Fatal("could not remove istio.io/rev from old namespace")
		}

		err = oldClient.Restart()
		if err != nil {
			t.Fatal("could not restart old client")
		}
		err = oldServer.Restart()
		if err != nil {
			t.Fatal("could not restart old server")
		}

		// make sure the restarted pods in default-1 namespace do not use
		// the previous version (check for the previousVersion in the image string)
		err = checkVersion(t, oldNs.Name(), previousVersion)
		if err != nil {
			t.Fatalf("found a pod in namespace (%s) with the previous version: %v", oldNs.Name(), err)
		}

		// now check traffic still works between the proxies
		sanitycheck.RunTrafficTestClientServer(t, oldClient, newServer)
	}
}

func checkVersion(t framework.TestContext, namespace, version string) error {
	// func NewPodFetch(a istioKube.ExtendedClient, namespace string, selectors ...string) PodFetchFunc {
	fetch := kubetest.NewPodFetch(t.Clusters().Default(), namespace)
	pods, err := kubetest.CheckPodsAreReady(fetch)
	if err != nil {
		return fmt.Errorf("failed to retrieve pods: %v", err)
	}
	for _, p := range pods {
		for _, c := range p.Spec.Containers {
			if strings.Contains(c.Image, version) {
				return fmt.Errorf("expected container image to not include version %q, got %q", version, c.Image)
			}
		}
	}

	return nil
}
