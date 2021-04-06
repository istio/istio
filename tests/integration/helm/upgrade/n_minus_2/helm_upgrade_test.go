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

package nminus2

import (
	helmupgrade "istio.io/istio/tests/integration/helm/upgrade"
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
	gcrHub           = "gcr.io/istio-release"
	nMinusTwoVersion = "1.8.1"

	defaultValues = `
global:
  hub: %s
  tag: %s
`
)

// TestDefaultInPlaceUpgradeFromTwoMinorReleases tests Istio installation using Helm with default options for Istio 1.(n-2)
func TestDefaultInPlaceUpgradeFromTwoMinorReleases(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.helm.default.upgrade").
		Run(func(ctx framework.TestContext) {
			cs := ctx.Clusters().Default().(*kubecluster.Cluster)
			h := helm.New(cs.Filename(), filepath.Join(previousChartPath, nMinusTwoVersion))

			ctx.ConditionalCleanup(func() {
				// only need to do call this once as helm doesn't need to remove
				// all versions
				err := helmupgrade.DeleteIstio(cs, h)
				if err != nil {
					ctx.Fatalf("could not delete istio: %v", err)
				}
			})

			overrideValuesFile := helmupgrade.GetValuesOverrides(ctx, defaultValues, gcrHub, nMinusTwoVersion)
			helmupgrade.InstallIstio(t, cs, h, overrideValuesFile)
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

			overrideValuesFile = helmupgrade.GetValuesOverrides(ctx, defaultValues, s.Hub, s.Tag)
			helmupgrade.UpgradeCharts(ctx, h, overrideValuesFile)
			helmtest.VerifyInstallation(ctx, cs)

			newClient, newServer := sanitycheck.SetupTrafficTest(t, ctx)
			sanitycheck.RunTrafficTestClientServer(t, newClient, newServer)

			// now check that we are compatible with N-1 proxy with N proxy
			sanitycheck.RunTrafficTestClientServer(t, oldClient, newServer)
		})
}
