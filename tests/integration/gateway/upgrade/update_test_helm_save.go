// +build integSave
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

package gatewayupgrade

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/helm"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"

	kubecluster "istio.io/istio/pkg/test/framework/components/cluster/kube"
	"istio.io/istio/pkg/test/framework/resource"
	kubetest "istio.io/istio/pkg/test/kube"
	helmtest "istio.io/istio/tests/integration/helm"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
// previousChartPath is path of Helm charts for previous Istio deployments.
// previousChartPath = filepath.Join(env.IstioSrc, "tests/integration/helm/testdata/")
)

const (
// 	gcrHub                   = "gcr.io/istio-release"
// 	previousSupportedVersion = "1.8.1"

// 	defaultValues = `
// global:
//   hub: %s
//   tag: %s
// `
)

// TestAccessAppViaCustomGateway tests access to an aplication using a custom gateway
func TestUpdateWithCustomGatewaySave(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.ingress.gateway").
		Run(func(ctx framework.TestContext) {
			cs := ctx.Clusters().Default().(*kubecluster.Cluster)
			h := helm.New(cs.Filename(), filepath.Join(previousChartPath, previousSupportedVersion))

			ctx.ConditionalCleanup(func() {
				// only need to do call this once as helm doesn't need to remove
				// all versions
				deleteIstio(cs, h)
			})

			overrideValuesFile := getValuesOverrides(ctx, defaultValues, gcrHub, previousSupportedVersion)
			installIstio(t, cs, h, overrideValuesFile)
			helmtest.VerifyInstallation(ctx, cs)

			// // Unable to find the ingress for the custom gateway install via the framework so retrieve URL and
			// // use in the echo call.
			// gwIngressURL, err := getIngressURL(customGWNamespace.Name(), customServiceGateway)
			// if err != nil {
			// 	t.Fatalf("failed to get custom gateway URL: %v", err)
			// }
			// gwAddress := (strings.Split(gwIngressURL, ":"))[0]
			// ingress := cgwInst.IngressFor(ctx.Clusters().Default())

			// // Apply a gateway to the custom-gateway and a virtual service for appplication A in its namespace.
			// // Application A will then be exposed externally on the custom-gateway
			// gwYaml := fmt.Sprintf(gwTemplate, aSvc+"-gateway", customServiceGateway)
			// ctx.Config().ApplyYAMLOrFail(ctx, apps.appANamespace.Name(), gwYaml)
			// vsYaml := fmt.Sprintf(vsTemplate, aSvc, aSvc+"-gateway", aSvc)
			// ctx.Config().ApplyYAMLOrFail(ctx, apps.appANamespace.Name(), vsYaml)

			// // Verify that one can access application A on the custom-gateway
			// ctx.NewSubTest("before update").Run(func(ctx framework.TestContext) {
			// 	ingress.CallEchoWithRetryOrFail(ctx, echo.CallOptions{
			// 		Port: &echo.Port{
			// 			Protocol: protocol.HTTP,
			// 		},
			// 		Address: gwAddress,
			// 		Path:    "/",
			// 		Headers: map[string][]string{
			// 			"Host": {"my.domain.example"},
			// 		},
			// 		Validator: echo.ExpectOK(),
			// 	}, retry.Timeout(time.Minute))
			// })
		})
}

// installIstio install Istio using Helm charts with the provided
// override values file and fails the tests on any failures.
func installIstio(t *testing.T, cs resource.Cluster,
	h *helm.Helm, overrideValuesFile string) {
	helmtest.CreateIstioSystemNamespace(t, cs)

	// Install base chart
	err := h.InstallChart(helmtest.BaseReleaseName, helmtest.BaseChart+helmtest.TarGzSuffix,
		helmtest.IstioNamespace, overrideValuesFile, helmtest.HelmTimeout)
	if err != nil {
		t.Errorf("failed to install istio %s chart", helmtest.BaseChart)
	}

	// Install discovery chart
	err = h.InstallChart(helmtest.IstiodReleaseName, filepath.Join(helmtest.ControlChartsDir, helmtest.DiscoveryChart)+helmtest.TarGzSuffix,
		helmtest.IstioNamespace, overrideValuesFile, helmtest.HelmTimeout)
	if err != nil {
		t.Errorf("failed to install istio %s chart", helmtest.DiscoveryChart)
	}

	helmtest.InstallGatewaysCharts(t, cs, h, helmtest.TarGzSuffix, overrideValuesFile)
}

// deleteIstio deletes installed Istio Helm charts and resources
func deleteIstio(cs resource.Cluster, h *helm.Helm) error {
	scopes.Framework.Infof("cleaning up resources")
	if err := h.DeleteChart(helmtest.EgressReleaseName, helmtest.IstioNamespace); err != nil {
		return fmt.Errorf("failed to delete %s release", helmtest.EgressReleaseName)
	}
	if err := h.DeleteChart(helmtest.IngressReleaseName, helmtest.IstioNamespace); err != nil {
		return fmt.Errorf("failed to delete %s release", helmtest.IngressReleaseName)
	}
	if err := h.DeleteChart(helmtest.IstiodReleaseName, helmtest.IstioNamespace); err != nil {
		return fmt.Errorf("failed to delete %s release", helmtest.IngressReleaseName)
	}
	if err := h.DeleteChart(helmtest.BaseReleaseName, helmtest.IstioNamespace); err != nil {
		return fmt.Errorf("failed to delete %s release", helmtest.BaseReleaseName)
	}
	if err := cs.CoreV1().Namespaces().Delete(context.TODO(), helmtest.IstioNamespace, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("failed to delete istio namespace: %v", err)
	}
	if err := kubetest.WaitForNamespaceDeletion(cs, helmtest.IstioNamespace, retry.Timeout(helmtest.RetryTimeOut)); err != nil {
		return fmt.Errorf("wating for istio namespace to be deleted: %v", err)
	}

	return nil
}

func getValuesOverrides(ctx framework.TestContext, valuesStr, hub, tag string) string {
	workDir := ctx.CreateTmpDirectoryOrFail("helm")
	overrideValues := fmt.Sprintf(valuesStr, hub, tag)
	overrideValuesFile := filepath.Join(workDir, "values.yaml")
	if err := ioutil.WriteFile(overrideValuesFile, []byte(overrideValues), os.ModePerm); err != nil {
		ctx.Fatalf("failed to write iop cr file: %v", err)
	}

	return overrideValuesFile
}
