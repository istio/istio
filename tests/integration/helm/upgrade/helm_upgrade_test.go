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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	kubecluster "istio.io/istio/pkg/test/framework/components/cluster/kube"
	"istio.io/istio/pkg/test/framework/image"
	"istio.io/istio/pkg/test/helm"
	helmtest "istio.io/istio/tests/integration/helm"
	"istio.io/istio/tests/util/sanitycheck"
)

// previousChartPath is path of Helm charts for previous Istio deployments.
var previousChartPath = filepath.Join(env.IstioSrc, "tests/integration/helm/testdata/")

const (
	gcrHub                   = "gcr.io/istio-release"
	previousSupportedVersion = "1.8.1"
	tarGzSuffix              = ".tar.gz"

	defaultValues = `
global:
  hub: %s
  tag: %s
`
)

// TestDefaultInPlaceUpgrades tests Istio installation using Helm with default options
func TestDefaultInPlaceUpgrades(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.helm.default.upgrade").
		Run(func(ctx framework.TestContext) {
			cs := ctx.Clusters().Default().(*kubecluster.Cluster)
			h := helm.New(cs.Filename(), filepath.Join(previousChartPath, previousSupportedVersion))

			overrideValuesFile := getValuesOverrides(ctx, defaultValues, gcrHub, previousSupportedVersion)
			helmtest.InstallIstio(t, cs, h, tarGzSuffix, overrideValuesFile)
			t.Cleanup(func() {
				helmtest.DeleteIstio(t, h, cs)
			})

			helmtest.VerifyInstallation(ctx, cs)

			oldClient, oldServer := sanitycheck.SetupTrafficTest(t, ctx)
			sanitycheck.RunTrafficTestClientServer(t, oldClient, oldServer)

			// now upgrade istio to the latest version found in this branch
			// use the command line or environmental vars from the user to set
			// the hub/tag
			s, err := image.SettingsFromCommandLine()
			if err != nil {
				ctx.Fatal(err)
			}

			overrideValuesFile = getValuesOverrides(ctx, defaultValues, s.Hub, s.Tag)
			upgradeCharts(ctx, h, overrideValuesFile)
			helmtest.VerifyInstallation(ctx, cs)

			newClient, newServer := sanitycheck.SetupTrafficTest(t, ctx)
			sanitycheck.RunTrafficTestClientServer(t, newClient, newServer)

			// now check that we are compatible with N-1 proxy with N proxy
			sanitycheck.RunTrafficTestClientServer(t, oldClient, newServer)
		})
}

// upgradeCharts upgrades Istio using Helm charts with the provided
// override values file to the latest charts in $ISTIO_SRC/manifests
func upgradeCharts(ctx framework.TestContext, h *helm.Helm, overrideValuesFile string) {
	// Upgrade base chart
	err := h.UpgradeChart(helmtest.BaseReleaseName, filepath.Join(helmtest.ChartPath, helmtest.BaseChart),
		helmtest.IstioNamespace, overrideValuesFile, helmtest.HelmTimeout)
	if err != nil {
		ctx.Fatalf("failed to upgrade istio %s chart", helmtest.BaseChart)
	}

	// Upgrade discovery chart
	err = h.UpgradeChart(helmtest.IstiodReleaseName, filepath.Join(helmtest.ChartPath, helmtest.ControlChartsDir, helmtest.DiscoveryChart),
		helmtest.IstioNamespace, overrideValuesFile, helmtest.HelmTimeout)
	if err != nil {
		ctx.Fatalf("failed to upgrade istio %s chart", helmtest.DiscoveryChart)
	}

	// Upgrade ingress gateway chart
	err = h.UpgradeChart(helmtest.IngressReleaseName, filepath.Join(helmtest.ChartPath, helmtest.GatewayChartsDir, helmtest.IngressGatewayChart),
		helmtest.IstioNamespace, overrideValuesFile, helmtest.HelmTimeout)
	if err != nil {
		ctx.Fatalf("failed to upgrade istio %s chart", helmtest.IngressGatewayChart)
	}

	// Upgrade egress gateway chart
	err = h.UpgradeChart(helmtest.EgressReleaseName, filepath.Join(helmtest.ChartPath, helmtest.GatewayChartsDir, helmtest.EgressGatewayChart),
		helmtest.IstioNamespace, overrideValuesFile, helmtest.HelmTimeout)
	if err != nil {
		ctx.Fatalf("failed to upgrade istio %s chart", helmtest.EgressGatewayChart)
	}
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
