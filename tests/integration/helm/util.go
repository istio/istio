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
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/helm"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/pkg/log"
)

const (
	IstioNamespace      = "istio-system"
	ReleasePrefix       = "istio-"
	BaseChart           = "base"
	TarGzSuffix         = ".tar.gz"
	DiscoveryChart      = "istio-discovery"
	IngressGatewayChart = "istio-ingress"
	EgressGatewayChart  = "istio-egress"
	BaseReleaseName     = ReleasePrefix + BaseChart
	IstiodReleaseName   = "istiod"
	IngressReleaseName  = IngressGatewayChart
	EgressReleaseName   = EgressGatewayChart
	ControlChartsDir    = "istio-control"
	GatewayChartsDir    = "gateways"
	retryDelay          = 2 * time.Second
	RetryTimeOut        = 5 * time.Minute
	HelmTimeout         = 2 * time.Minute
)

// ChartPath is path of local Helm charts used for testing.
var ChartPath = filepath.Join(env.IstioSrc, "manifests/charts")

// InstallGatewaysCharts install Istio using Helm charts with the provided
// override values file and fails the tests on any failures.
func InstallGatewaysCharts(t test.Failer, cs cluster.Cluster,
	h *helm.Helm, suffix, namespace, overrideValuesFile string) {
	CreateNamespace(t, cs, namespace)

	// Install ingress gateway chart
	err := h.InstallChart(IngressReleaseName, filepath.Join(GatewayChartsDir, IngressGatewayChart)+suffix,
		namespace, overrideValuesFile, HelmTimeout)
	if err != nil {
		t.Fatalf("failed to install istio %s chart", IngressGatewayChart)
	}

	// Install egress gateway chart
	err = h.InstallChart(EgressReleaseName, filepath.Join(GatewayChartsDir, EgressGatewayChart)+suffix,
		namespace, overrideValuesFile, HelmTimeout)
	if err != nil {
		t.Fatalf("failed to install istio %s chart", EgressGatewayChart)
	}
}

func CreateNamespace(t test.Failer, cs cluster.Cluster, namespace string) {
	if _, err := cs.CoreV1().Namespaces().Create(context.TODO(), &v1.Namespace{
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

// deleteGatewayCharts deletes installed Istio Helm charts and resources
func deleteGatewayCharts(t *testing.T, h *helm.Helm) {
	scopes.Framework.Infof("cleaning up resources")
	if err := h.DeleteChart(EgressReleaseName, IstioNamespace); err != nil {
		t.Errorf("failed to delete %s release", EgressReleaseName)
	}
	if err := h.DeleteChart(IngressReleaseName, IstioNamespace); err != nil {
		t.Errorf("failed to delete %s release", IngressReleaseName)
	}
}

// VerifyInstallation verify that the Helm installation is successful
func VerifyInstallation(ctx framework.TestContext, cs cluster.Cluster) {
	scopes.Framework.Infof("=== verifying istio installation === ")

	retry.UntilSuccessOrFail(ctx, func() error {
		if _, err := kubetest.CheckPodsAreReady(kubetest.NewPodFetch(cs, IstioNamespace, "app=istiod")); err != nil {
			return fmt.Errorf("istiod pod is not ready: %v", err)
		}
		if _, err := kubetest.CheckPodsAreReady(kubetest.NewPodFetch(cs, IstioNamespace, "app=istio-ingressgateway")); err != nil {
			return fmt.Errorf("istio ingress gateway pod is not ready: %v", err)
		}
		if _, err := kubetest.CheckPodsAreReady(kubetest.NewPodFetch(cs, IstioNamespace, "app=istio-egressgateway")); err != nil {
			return fmt.Errorf("istio egress gateway pod is not ready: %v", err)
		}
		return nil
	}, retry.Timeout(RetryTimeOut), retry.Delay(retryDelay))
	scopes.Framework.Infof("=== succeeded ===")
}
