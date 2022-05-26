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

package cacustomroot

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/tests/integration/security/util/scheck"
)

const (
	POLICY = `
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: "mtls"
spec:
  mtls:
    mode: STRICT
---
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
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
		Run(func(t framework.TestContext) {
			testNS := apps.Namespace

			t.ConfigIstio().YAML(testNS.Name(), POLICY).ApplyOrFail(t)

			for _, cluster := range t.Clusters() {
				t.NewSubTest(fmt.Sprintf("From %s", cluster.StableName())).Run(func(t framework.TestContext) {
					verify := func(t framework.TestContext, from echo.Instance, to echo.Instances, s scheme.Instance, success bool) {
						want := "success"
						if !success {
							want = "fail"
						}
						name := fmt.Sprintf("server:%s[%s]", to[0].Config().Service, want)
						t.NewSubTest(name).Run(func(t framework.TestContext) {
							t.Helper()
							opts := echo.CallOptions{
								To:    to,
								Count: 1,
								Port: echo.Port{
									Name: "https",
								},
								Address: to.Config().Service,
								Scheme:  s,
							}
							if success {
								opts.Check = check.And(check.OK(), scheck.ReachedClusters(t.AllClusters(), &opts))
							} else {
								opts.Check = scheck.NotOK()
							}

							from.CallOrFail(t, opts)
						})
					}

					client := match.Cluster(cluster).FirstOrFail(t, apps.Client)
					cases := []struct {
						src    echo.Instance
						dest   echo.Instances
						expect bool
					}{
						{
							src:    client,
							dest:   apps.ServerNakedFoo,
							expect: true,
						},
						{
							src:    client,
							dest:   apps.ServerNakedBar,
							expect: false,
						},
					}

					for _, tc := range cases {
						verify(t, tc.src, tc.dest, scheme.HTTP, tc.expect)
					}
				})
			}
		})
}
