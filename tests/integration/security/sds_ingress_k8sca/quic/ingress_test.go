// +build integ
//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package quic

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/security/sds_ingress/util"
)

var (
	inst istio.Instance
	apps = &util.EchoDeployments{}
)

func TestMain(m *testing.M) {
	// Integration test for the ingress SDS multiple Gateway flow when
	// the control plane certificate provider is k8s CA.
	framework.
		NewSuite(m).
		RequireSingleCluster().
		// https://github.com/istio/istio/issues/22161. 1.22 drops support for legacy-unknown signer
		RequireMaxVersion(21).
		Setup(istio.Setup(&inst, setupConfig)).
		Setup(func(ctx resource.Context) (err error) {
			// Skip VM as eastwest gateway is disabled.
			ctx.Settings().SkipVM = true
			return util.SetupTest(ctx, apps)
		}).
		Run()
}

func setupConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.ControlPlaneValues = `
values:
  global:
    pilotCertProvider: kubernetes
`
	cfg.DeployEastWestGW = false
	cfg.PrimaryClusterIOPFile = istio.IntegrationTestDefaultsIOPWithQUIC
}

func TestTlsGatewaysK8scaWithQUIC(t *testing.T) {
	framework.
		NewTest(t).
		Features("security.ingress.quic.sds-k8sca.tls").
		Run(func(t framework.TestContext) {
			for _, cluster := range t.Clusters() {
				if !cluster.MinKubeVersion(20) {
					t.Skipf("k8s version not supported for %s (<%s)", t.Name(), "1.21")
				}
			}
			t.NewSubTest("tcp").Run(func(t framework.TestContext) {
				util.RunTestMultiTLSGateways(t, inst, apps)
			})
			t.NewSubTest("quic").Run(func(t framework.TestContext) {
				util.RunTestMultiQUICGateways(t, inst, util.TLS, apps)
			})
		})
}

func TestMtlsGatewaysK8scaWithQUIC(t *testing.T) {
	framework.
		NewTest(t).
		Features("security.ingress.quic.sds-k8sca.mtls").
		Run(func(t framework.TestContext) {
			for _, cluster := range t.Clusters() {
				if !cluster.MinKubeVersion(20) {
					t.Skipf("k8s version not supported for %s (<%s)", t.Name(), "1.21")
				}
			}
			t.NewSubTest("tcp").Run(func(t framework.TestContext) {
				util.RunTestMultiMtlsGateways(t, inst, apps)
			})
			t.NewSubTest("quic").Run(func(t framework.TestContext) {
				util.RunTestMultiQUICGateways(t, inst, util.Mtls, apps)
			})
		})
}
