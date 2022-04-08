//go:build integ
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
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
)

var (
	inst istio.Instance
	apps = &ingressutil.EchoDeployments{}
)

func TestMain(m *testing.M) {
	// Integration test for the ingress SDS Gateway flow.
	// nolint: staticcheck
	framework.
		NewSuite(m).
		// Need support for MixedProtocolLBService
		RequireMinVersion(20).
		RequireMultiPrimary().
		Setup(istio.Setup(&inst, func(_ resource.Context, cfg *istio.Config) {
			cfg.PrimaryClusterIOPFile = istio.IntegrationTestDefaultsIOPWithQUIC
		})).
		Setup(func(ctx resource.Context) error {
			return ingressutil.SetupTest(ctx, apps)
		}).
		Run()
}

// TestTlsGatewaysWithQUIC deploys multiple TLS gateways with SDS enabled, and creates kubernetes that store
// private key and server certificate for each TLS gateway. Verifies that client can communicate by
// using both QUIC and TCP/TLS
func TestTlsGatewaysWithQUIC(t *testing.T) {
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Features("security.ingress.quic.sds.tls").
		Run(func(t framework.TestContext) {
			t.NewSubTest("tcp").Run(func(t framework.TestContext) {
				ingressutil.RunTestMultiTLSGateways(t, inst, apps)
			})
			t.NewSubTest("quic").Run(func(t framework.TestContext) {
				ingressutil.RunTestMultiQUICGateways(t, inst, ingressutil.TLS, apps)
			})
		})
}

// TestMtlsGatewaysWithQUIC deploys multiple mTLS gateways with SDS enabled, and creates kubernetes that store
// private key, server certificate and CA certificate for each mTLS gateway. Verifies that client can communicate
// by using both QUIC and TCP/mTLS
func TestMtlsGatewaysWithQUIC(t *testing.T) {
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Features("security.ingress.quic.sds.mtls").
		Run(func(t framework.TestContext) {
			t.NewSubTest("tcp").Run(func(t framework.TestContext) {
				ingressutil.RunTestMultiTLSGateways(t, inst, apps)
			})
			t.NewSubTest("quic").Run(func(t framework.TestContext) {
				ingressutil.RunTestMultiQUICGateways(t, inst, ingressutil.Mtls, apps)
			})
		})
}
