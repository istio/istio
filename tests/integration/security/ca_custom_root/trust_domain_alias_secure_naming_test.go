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

package cacustomroot

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/tests/integration/security/util/connection"
)

const (
	HTTPS  = "https"
	POLICY = `
apiVersion: "security.istio.io/v1beta1"
kind: "PeerAuthentication"
metadata:
  name: "mtls"
spec:
  mtls:
    mode: STRICT
---
apiVersion: "networking.istio.io/v1alpha3"
kind: "DestinationRule"
metadata:
  name: "server-naked"
spec:
  host: "*.local"
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
`
)

// TestTrustDomainAliasSecureNaming scope:
// The client side mTLS connection should validate the trust domain alias during secure naming validation.
//
// Setup:
// 1. Setup Istio with custom CA cert. This is because we need to use that root cert to sign customized
//    certificate for server workloads to give them different trust domains.
// 2. One client workload with sidecar injected.
// 3. Two naked server workloads with custom certs whose URI SAN have different SPIFFE trust domains.
// 4. PeerAuthentication with strict mtls, to enforce the mtls connection.
// 5. DestinaitonRule with tls ISTIO_MUTUAL mode, because Istio auto mTLS will let client send plaintext to naked servers by default.
// 6. MeshConfig.TrustDomainAliases contains one of the trust domain "server-naked-foo".
//
// Expectation:
// When the "server-naked-foo" is in the list of MeshConfig.TrustDomainAliases, client requests to
// "server-naked-foo" succeeds, and requests to "server-naked-bar" fails.
func TestTrustDomainAliasSecureNaming(t *testing.T) {
	framework.NewTest(t).
		Features("security.peer.trust-domain-alias-secure-naming").
		Run(func(ctx framework.TestContext) {
			// TODO: remove the skip when https://github.com/istio/istio/issues/28798 is fixed
			if ctx.Clusters().IsMulticluster() {
				ctx.Skip()
			}
			testNS := apps.Namespace

			ctx.Config().ApplyYAMLOrFail(ctx, testNS.Name(), POLICY)

			for _, cluster := range ctx.Clusters() {
				ctx.NewSubTest(fmt.Sprintf("From %s", cluster.StableName())).Run(func(ctx framework.TestContext) {
					verify := func(ctx framework.TestContext, src echo.Instance, dest echo.Instance, s scheme.Instance, success bool) {
						want := "success"
						if !success {
							want = "fail"
						}
						name := fmt.Sprintf("server:%s[%s]", dest.Config().Service, want)
						ctx.NewSubTest(name).Run(func(ctx framework.TestContext) {
							ctx.Helper()
							opt := echo.CallOptions{
								Target:   dest,
								PortName: HTTPS,
								Address:  dest.Config().Service,
								Scheme:   s,
							}
							checker := connection.Checker{
								From:          src,
								Options:       opt,
								ExpectSuccess: success,
								DestClusters:  ctx.Clusters(),
							}
							checker.CheckOrFail(ctx)
						})
					}

					client := apps.Client.GetOrFail(ctx, echo.InCluster(cluster))
					serverNakedFoo := apps.ServerNakedFoo.GetOrFail(ctx, echo.InCluster(cluster))
					serverNakedBar := apps.ServerNakedBar.GetOrFail(ctx, echo.InCluster(cluster))
					cases := []struct {
						src    echo.Instance
						dest   echo.Instance
						expect bool
					}{
						{
							src:    client,
							dest:   serverNakedFoo,
							expect: true,
						},
						{
							src:    client,
							dest:   serverNakedBar,
							expect: false,
						},
					}

					for _, tc := range cases {
						verify(ctx, tc.src, tc.dest, scheme.HTTP, tc.expect)
					}
				})
			}
		})
}
