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
	"strings"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
)

var (
	inst         istio.Instance
	apps         deployment.SingleNamespaceView
	echo1NS      namespace.Instance
	customConfig []echo.Config
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
		Setup(namespace.Setup(&echo1NS, namespace.Config{Prefix: "echo1", Inject: true})).
		Setup(func(ctx resource.Context) error {
			// TODO: due to issue https://github.com/istio/istio/issues/25286,
			// currently VM does not work in this test
			err := ingressutil.SetupTest(ctx, &customConfig, namespace.Future(&echo1NS))
			if err != nil {
				return err
			}
			return nil
		}).
		Setup(deployment.SetupSingleNamespace(&apps, deployment.Config{
			Namespaces: []namespace.Getter{
				namespace.Future(&echo1NS),
			},
			Configs: echo.ConfigFuture(&customConfig),
		})).
		Setup(func(ctx resource.Context) error {
			return ingressutil.SetInstances(apps.All)
		}).
		Run()
}

// TestTlsGatewaysWithQUIC deploys multiple TLS gateways with SDS enabled, and creates kubernetes that store
// private key and server certificate for each TLS gateway. Verifies that client can communicate by
// using both QUIC and TCP/TLS
func TestTlsGatewaysWithQUIC(t *testing.T) {
	// nolint: staticcheck
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Run(func(t framework.TestContext) {
			err := ingressutil.WaitForIngressQUICService(t, inst.Settings().SystemNamespace)
			if err != nil && strings.Contains(err.Error(), "the QUIC mixed service is not supported") {
				t.Skip("the QUIC mixed service is not supported - ", err)
			}

			t.NewSubTest("tcp").Run(func(t framework.TestContext) {
				ingressutil.RunTestMultiTLSGateways(t, inst, namespace.Future(&echo1NS))
			})
			t.NewSubTest("quic").Run(func(t framework.TestContext) {
				ingressutil.RunTestMultiQUICGateways(t, inst, ingressutil.TLS, namespace.Future(&echo1NS))
			})
		})
}

// TestMtlsGatewaysWithQUIC deploys multiple mTLS gateways with SDS enabled, and creates kubernetes that store
// private key, server certificate and CA certificate for each mTLS gateway. Verifies that client can communicate
// by using both QUIC and TCP/mTLS
func TestMtlsGatewaysWithQUIC(t *testing.T) {
	// nolint: staticcheck
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Run(func(t framework.TestContext) {
			err := ingressutil.WaitForIngressQUICService(t, inst.Settings().SystemNamespace)
			if err != nil && strings.Contains(err.Error(), "the QUIC mixed service is not supported") {
				t.Skip("the QUIC mixed service is not supported - ", err)
			}

			t.NewSubTest("tcp").Run(func(t framework.TestContext) {
				ingressutil.RunTestMultiTLSGateways(t, inst, namespace.Future(&echo1NS))
			})
			t.NewSubTest("quic").Run(func(t framework.TestContext) {
				ingressutil.RunTestMultiQUICGateways(t, inst, ingressutil.Mtls, namespace.Future(&echo1NS))
			})
		})
}
