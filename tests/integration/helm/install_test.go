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
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/image"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/helm"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/util/sanitycheck"
	"istio.io/pkg/log"
)

const (
	IstioNamespace      = "istio-system"
	ReleasePrefix       = "istio-"
	BaseChart           = "base"
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
	retryTimeOut        = 5 * time.Minute
)

var (
	// ChartPath is path of local Helm charts used for testing.
	ChartPath = filepath.Join(env.IstioSrc, "manifests/charts")
)

// TestDefaultInstall tests Istio installation using Helm with default options
func TestDefaultInstall(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.helm.default").
		Run(func(ctx framework.TestContext) {
			workDir, err := ctx.CreateTmpDirectory("helm-install-test")
			if err != nil {
				t.Fatal("failed to create test directory")
			}
			cs := ctx.Clusters().Default().(*kube.Cluster)
			h := helm.New(cs.Filename(), ChartPath)
			s, err := image.SettingsFromCommandLine()
			if err != nil {
				t.Fatal(err)
			}
			overrideValuesStr := `
global:
  hub: %s
  tag: %s
`
			overrideValues := fmt.Sprintf(overrideValuesStr, s.Hub, s.Tag)
			overrideValuesFile := filepath.Join(workDir, "values.yaml")
			if err := ioutil.WriteFile(overrideValuesFile, []byte(overrideValues), os.ModePerm); err != nil {
				t.Fatalf("failed to write iop cr file: %v", err)
			}
			installIstio(t, ctx, cs, h, overrideValuesFile)

			verifyInstallation(t, ctx, cs)

			t.Cleanup(func() {
				deleteIstio(t, cs, h)
			})
		})
}

// TestInstallWithFirstPartyJwt tests Istio installation using Helm
// with first-party-jwt enabled
func TestInstallWithFirstPartyJwt(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.helm.firstpartyjwt").
		Run(func(ctx framework.TestContext) {
			workDir, err := ctx.CreateTmpDirectory("helm-install-test")
			if err != nil {
				t.Fatal("failed to create test directory")
			}
			cs := ctx.Clusters().Default().(*kube.Cluster)
			h := helm.New(cs.Filename(), ChartPath)
			s, err := image.SettingsFromCommandLine()
			if err != nil {
				t.Fatal(err)
			}
			overrideValuesStr := `
global:
  hub: %s
  tag: %s
  jwtPolicy: first-party-jwt
`
			overrideValues := fmt.Sprintf(overrideValuesStr, s.Hub, s.Tag)
			overrideValuesFile := filepath.Join(workDir, "values.yaml")
			if err := ioutil.WriteFile(overrideValuesFile, []byte(overrideValues), os.ModePerm); err != nil {
				t.Fatalf("failed to write iop cr file: %v", err)
			}
			installIstio(t, ctx, cs, h, overrideValuesFile)

			verifyInstallation(t, ctx, cs)

			t.Cleanup(func() {
				deleteIstio(t, cs, h)
			})
		})
}

// installIstio install Istio using Helm charts with the provided
// override values file and fails the tests on any failures.
func installIstio(t *testing.T, ctx resource.Context, cs resource.Cluster,
	h *helm.Helm, overrideValuesFile string) {
	if _, err := cs.CoreV1().Namespaces().Create(context.TODO(), &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: IstioNamespace,
		},
	}, metav1.CreateOptions{}); err != nil {
		if kerrors.IsAlreadyExists(err) {
			log.Info("istio namespace already exist")
		} else {
			t.Errorf("failed to create istio namespace: %v", err)
		}
	}

	// Install base chart
	err := h.InstallChart(BaseReleaseName, BaseChart,
		IstioNamespace, overrideValuesFile)
	if err != nil {
		t.Errorf("failed to install istio %s chart", BaseChart)
	}

	// Install discovery chart
	err = h.InstallChart(IstiodReleaseName, filepath.Join(ControlChartsDir, DiscoveryChart),
		IstioNamespace, overrideValuesFile)
	if err != nil {
		t.Errorf("failed to install istio %s chart", DiscoveryChart)
	}

	// Install ingress gateway chart
	err = h.InstallChart(IngressReleaseName, filepath.Join(GatewayChartsDir, IngressGatewayChart),
		IstioNamespace, overrideValuesFile)
	if err != nil {
		t.Errorf("failed to install istio %s chart", IngressGatewayChart)
	}

	// Install egress gateway chart
	err = h.InstallChart(EgressReleaseName, filepath.Join(GatewayChartsDir, EgressGatewayChart),
		IstioNamespace, overrideValuesFile)
	if err != nil {
		t.Errorf("failed to install istio %s chart", EgressGatewayChart)
	}
}

// deleteIstio deletes installed Istio Helm charts and resources
func deleteIstio(t *testing.T, cs resource.Cluster, h *helm.Helm) {
	scopes.Framework.Infof("cleaning up resources")
	if err := h.DeleteChart(EgressReleaseName, IstioNamespace); err != nil {
		t.Errorf("failed to delete %s release", EgressReleaseName)
	}
	if err := h.DeleteChart(IngressReleaseName, IstioNamespace); err != nil {
		t.Errorf("failed to delete %s release", IngressReleaseName)
	}
	if err := h.DeleteChart(IstiodReleaseName, IstioNamespace); err != nil {
		t.Errorf("failed to delete %s release", IngressReleaseName)
	}
	if err := h.DeleteChart(BaseReleaseName, IstioNamespace); err != nil {
		t.Errorf("failed to delete %s release", BaseReleaseName)
	}
}

// verifyInstallation verify that the Helm installation is successfull
func verifyInstallation(t *testing.T, ctx resource.Context, cs resource.Cluster) {
	scopes.Framework.Infof("=== verifying istio installation === ")

	retry.UntilSuccessOrFail(t, func() error {
		if _, err := kubetest.CheckPodsAreReady(kubetest.NewSinglePodFetch(cs, IstioNamespace, "app=istiod")); err != nil {
			return fmt.Errorf("istiod pod is not ready: %v", err)
		}
		if _, err := kubetest.CheckPodsAreReady(kubetest.NewSinglePodFetch(cs, IstioNamespace, "app=istio-ingressgateway")); err != nil {
			return fmt.Errorf("istio ingress gateway pod is not ready: %v", err)
		}
		if _, err := kubetest.CheckPodsAreReady(kubetest.NewSinglePodFetch(cs, IstioNamespace, "app=istio-egressgateway")); err != nil {
			return fmt.Errorf("istio egress gateway pod is not ready: %v", err)
		}
		return nil
	}, retry.Timeout(retryTimeOut), retry.Delay(retryDelay))
	sanitycheck.RunTrafficTest(t, ctx)
	scopes.Framework.Infof("=== succeeded ===")
}
