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
	"context"
	"fmt"
	"path/filepath"
	"time"

	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/cluster/kube"
	"istio.io/istio/pkg/test/helm"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/pkg/log"
)

const (
	IstioNamespace     = "istio-system"
	ReleasePrefix      = "istio-"
	BaseChart          = "base"
	BaseChartPath      = "istio/base"
	CRDsFolder         = "crds"
	DiscoveryChart     = "istio-discovery"
	BaseReleaseName    = ReleasePrefix + BaseChart
	DiscoveryChartPath = "istio/istiod"
	GatewayChartPath   = "istio/gateway"
	IstiodReleaseName  = "istiod"
	IngressReleaseName = "istio-ingress"
	ControlChartsDir   = "istio-control"
	GatewayChartsDir   = "gateway"

	RetryDelay   = 2 * time.Second
	RetryTimeOut = 5 * time.Minute
	Timeout      = 2 * time.Minute
)

// ManifestsChartPath is path of local Helm charts used for testing.
var ManifestsChartPath = filepath.Join(env.IstioSrc, "manifests/charts")

// TestDataChartPath is path of local Helm charts used for testing.
var TestDataChartPath = filepath.Join(env.IstioSrc, "tests/integration/helm/testdata")

// InstallIstio install Istio using Helm charts with the provided
// override values file and fails the tests on any failures.
func InstallIstio(t test.Failer, cs cluster.Cluster,
	h *helm.Helm, overrideValuesFile, relPath, version string, installGateway bool,
) {
	CreateNamespace(t, cs, IstioNamespace)

	// Install base chart
	err := h.InstallChartWithVersion(BaseReleaseName, BaseChartPath, version, IstioNamespace, overrideValuesFile, Timeout)
	if err != nil {
		t.Fatalf("failed to install istio %s chart: %v", BaseReleaseName, err)
	}

	// Install discovery chart
	err = h.InstallChartWithVersion(IstiodReleaseName, DiscoveryChartPath, version, IstioNamespace, overrideValuesFile, Timeout)
	if err != nil {
		t.Fatalf("failed to install istio %s chart: %v", IstiodReleaseName, err)
	}

	if installGateway {
		err = h.InstallChartWithVersion(IngressReleaseName, GatewayChartPath, version, IstioNamespace, overrideValuesFile, Timeout)
		if err != nil {
			t.Fatalf("failed to install istio %s chart: %v", IngressReleaseName, err)
		}
	}
}

// InstallIstioWithRevision install Istio using Helm charts with the provided
// override values file and fails the tests on any failures.
func InstallIstioWithRevision(t test.Failer, cs cluster.Cluster,
	h *helm.Helm, version, revision, overrideValuesFile string, upgradeBaseChart, useTestData bool,
) {
	CreateNamespace(t, cs, IstioNamespace)

	// base chart may already be installed if the Istio was previously already installed
	if upgradeBaseChart {
		// upgrade is always done with ../manifests/charts/base
		err := h.UpgradeChart(BaseReleaseName, filepath.Join(ManifestsChartPath, BaseChart),
			IstioNamespace, overrideValuesFile, Timeout)
		if err != nil {
			t.Fatalf("failed to upgrade istio %s chart", BaseChart)
		}
	} else {
		err := h.InstallChartWithVersion(BaseReleaseName, BaseChartPath, version, IstioNamespace, overrideValuesFile, Timeout)
		if err != nil {
			t.Fatalf("failed to upgrade istio %s chart", BaseReleaseName)
		}
	}

	// install discovery chart with --set revision=NAME
	if useTestData {
		err := h.InstallChartWithVersion(IstiodReleaseName+"-"+revision, DiscoveryChartPath, version, IstioNamespace, overrideValuesFile, Timeout)
		if err != nil {
			t.Fatalf("failed to install istio %s chart", IstiodReleaseName)
		}
	} else {
		err := h.InstallChart(IstiodReleaseName+"-"+revision, filepath.Join(ManifestsChartPath, ControlChartsDir, DiscoveryChart),
			IstioNamespace, overrideValuesFile, Timeout)
		if err != nil {
			t.Fatalf("failed to install istio %s chart", IstiodReleaseName)
		}

	}
}

func CreateNamespace(t test.Failer, cs cluster.Cluster, namespace string) {
	if _, err := cs.Kube().CoreV1().Namespaces().Create(context.TODO(), &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}, metav1.CreateOptions{}); err != nil {
		if kerrors.IsAlreadyExists(err) {
			log.Debugf("%v namespace already exist", IstioNamespace)
		} else {
			t.Fatalf("failed to create %v namespace: %v", IstioNamespace, err)
		}
	}
}

// deleteIstio deletes installed Istio Helm charts and resources
func deleteIstio(t framework.TestContext, h *helm.Helm, cs *kube.Cluster) {
	scopes.Framework.Infof("cleaning up resources")
	if err := h.DeleteChart(IngressReleaseName, IstioNamespace); err != nil {
		t.Errorf("failed to delete %s release: %v", IngressReleaseName, err)
	}
	if err := h.DeleteChart(IstiodReleaseName, IstioNamespace); err != nil {
		t.Errorf("failed to delete %s release: %v", IstiodReleaseName, err)
	}
	if err := h.DeleteChart(BaseReleaseName, IstioNamespace); err != nil {
		t.Errorf("failed to delete %s release: %v", BaseReleaseName, err)
	}
	if err := cs.Kube().CoreV1().Namespaces().Delete(context.TODO(), IstioNamespace, metav1.DeleteOptions{}); err != nil {
		t.Errorf("failed to delete istio namespace: %v", err)
	}
	if err := kubetest.WaitForNamespaceDeletion(cs.Kube(), IstioNamespace, retry.Timeout(RetryTimeOut)); err != nil {
		t.Errorf("waiting for istio namespace to be deleted: %v", err)
	}
}

// VerifyInstallation verify that the Helm installation is successful
func VerifyInstallation(ctx framework.TestContext, cs cluster.Cluster, verifyGateway bool) {
	scopes.Framework.Infof("=== verifying istio installation === ")

	VerifyValidatingWebhookConfigurations(ctx, cs, []string{
		"istiod-default-validator",
	})

	retry.UntilSuccessOrFail(ctx, func() error {
		if _, err := kubetest.CheckPodsAreReady(kubetest.NewPodFetch(cs, IstioNamespace, "app=istiod")); err != nil {
			return fmt.Errorf("istiod pod is not ready: %v", err)
		}

		if verifyGateway {
			if _, err := kubetest.CheckPodsAreReady(kubetest.NewPodFetch(cs, IstioNamespace, "app=istio-ingress")); err != nil {
				return fmt.Errorf("istio ingress gateway pod is not ready: %v", err)
			}
		}
		return nil
	}, retry.Timeout(RetryTimeOut), retry.Delay(RetryDelay))
	scopes.Framework.Infof("=== succeeded ===")
}

func SetRevisionTagWithVersion(ctx framework.TestContext, h *helm.Helm, revision, revisionTag, relPath, version string) {
	scopes.Framework.Infof("=== setting revision tag with version === ")
	template, err := h.Template(IstiodReleaseName+"-"+revision, DiscoveryChartPath,
		IstioNamespace, "templates/revision-tags.yaml", Timeout, "--version", version, "--set",
		fmt.Sprintf("revision=%s", revision), "--set", fmt.Sprintf("revisionTags={%s}", revisionTag))
	if err != nil {
		ctx.Fatalf("failed to install istio %s chart", DiscoveryChart)
	}

	err = ctx.ConfigIstio().YAML(IstioNamespace, template).Apply()
	if err != nil {
		ctx.Fatalf("failed to apply templated revision tags yaml: %v", err)
	}

	scopes.Framework.Infof("=== succeeded === ")
}

func SetRevisionTag(ctx framework.TestContext, h *helm.Helm, fileSuffix, revision, revisionTag, relPath, version string) {
	scopes.Framework.Infof("=== setting revision tag === ")
	template, err := h.Template(IstiodReleaseName+"-"+revision, filepath.Join(relPath, version, ControlChartsDir, DiscoveryChart)+fileSuffix,
		IstioNamespace, "templates/revision-tags.yaml", Timeout, "--set",
		fmt.Sprintf("revision=%s", revision), "--set", fmt.Sprintf("revisionTags={%s}", revisionTag))
	if err != nil {
		ctx.Fatalf("failed to install istio %s chart", DiscoveryChart)
	}

	err = ctx.ConfigIstio().YAML(IstioNamespace, template).Apply()
	if err != nil {
		ctx.Fatalf("failed to apply templated revision tags yaml: %v", err)
	}

	scopes.Framework.Infof("=== succeeded === ")
}

// VerifyMutatingWebhookConfigurations verifies that the proper number of mutating webhooks are running, used with
// revisions and revision tags
func VerifyMutatingWebhookConfigurations(ctx framework.TestContext, cs cluster.Cluster, names []string) {
	scopes.Framework.Infof("=== verifying mutating webhook configurations === ")
	if ok := kubetest.MutatingWebhookConfigurationsExists(cs.Kube(), names); !ok {
		ctx.Fatalf("Not all mutating webhook configurations were installed. Expected [%v]", names)
	}
	scopes.Framework.Infof("=== succeeded ===")
}

// VerifyValidatingWebhookConfigurations verifies that the proper number of validating webhooks are running, used with
// revisions and revision tags
func VerifyValidatingWebhookConfigurations(ctx framework.TestContext, cs cluster.Cluster, names []string) {
	scopes.Framework.Infof("=== verifying validating webhook configurations === ")
	if ok := kubetest.ValidatingWebhookConfigurationsExists(cs.Kube(), names); !ok {
		ctx.Fatalf("Not all validating webhook configurations were installed. Expected [%v]", names)
	}
	scopes.Framework.Infof("=== succeeded ===")
}

// verifyValidation verifies that Istio resource validation is active on the cluster.
func verifyValidation(ctx framework.TestContext) {
	ctx.Helper()
	invalidGateway := &v1alpha3.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "invalid-istio-gateway",
			Namespace: IstioNamespace,
		},
		Spec: networking.Gateway{},
	}

	createOptions := metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}}
	istioClient := ctx.Clusters().Default().Istio().NetworkingV1alpha3()
	retry.UntilOrFail(ctx, func() bool {
		_, err := istioClient.Gateways(IstioNamespace).Create(context.TODO(), invalidGateway, createOptions)
		rejected := err != nil
		return rejected
	})
}
