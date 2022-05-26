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

package security

import (
	"fmt"
	"net/http"
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/pkg/test/util/yml"
	"istio.io/istio/tests/integration/security/util"
)

// TestPassThroughFilterChain tests the authN and authZ policy on the pass through filter chain.
func TestPassThroughFilterChain(t *testing.T) {
	framework.
		NewTest(t).
		Features("security.filterchain").
		Run(func(t framework.TestContext) {
			ns := apps.Namespace1

			type expect struct {
				port              echo.Port
				plaintextSucceeds bool
				mtlsSucceeds      bool
			}
			cases := []struct {
				name     string
				config   string
				expected []expect
			}{
				// There is no authN/authZ policy.
				// All requests should success, this is to verify the pass through filter chain and
				// the workload ports are working correctly.
				{
					name: "DISABLE",
					config: `apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: mtls
spec:
  mtls:
    mode: DISABLE`,
					expected: []expect{
						{
							port:              echo.Port{ServicePort: 8085, Protocol: protocol.HTTP},
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
						{
							port:              echo.Port{ServicePort: 8086, Protocol: protocol.HTTP},
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
						{
							port:              echo.Port{ServicePort: 8087, Protocol: protocol.TCP},
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
						{
							port:              echo.Port{ServicePort: 8088, Protocol: protocol.TCP},
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
						{
							port:              echo.Port{ServicePort: 8089, Protocol: protocol.HTTPS},
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
					},
				},
				{
					// There is only authZ policy that allows access to port 8085, 8087, and 8089.
					// Only request to port 8085, 8087, 8089 should be allowed.
					name: "DISABLE with authz",
					config: `apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: mtls
spec:
  mtls:
    mode: DISABLE
---
apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
  name: authz
spec:
  rules:
  - to:
    - operation:
        ports: ["8085", "8087", "8089"]`,
					expected: []expect{
						{
							port:              echo.Port{ServicePort: 8085, Protocol: protocol.HTTP},
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
						{
							port:              echo.Port{ServicePort: 8086, Protocol: protocol.HTTP},
							plaintextSucceeds: false,
							mtlsSucceeds:      false,
						},
						{
							port:              echo.Port{ServicePort: 8087, Protocol: protocol.TCP},
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
						{
							port:              echo.Port{ServicePort: 8088, Protocol: protocol.TCP},
							plaintextSucceeds: false,
							mtlsSucceeds:      false,
						},
						{
							port:              echo.Port{ServicePort: 8089, Protocol: protocol.HTTPS},
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
						{
							port:              echo.Port{ServicePort: 8084, Protocol: protocol.HTTPS},
							plaintextSucceeds: false,
							mtlsSucceeds:      false,
						},
					},
				},
				{
					// There is only authN policy that enables mTLS (Strict).
					// The request should be denied because the client is always using plain text.
					name: "STRICT",
					config: `apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: mtls
spec:
  mtls:
    mode: STRICT`,
					expected: []expect{
						{
							port:              echo.Port{ServicePort: 8085, Protocol: protocol.HTTP},
							plaintextSucceeds: false,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8086, Protocol: protocol.HTTP},
							plaintextSucceeds: false,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8087, Protocol: protocol.TCP},
							plaintextSucceeds: false,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8088, Protocol: protocol.TCP},
							plaintextSucceeds: false,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8089, Protocol: protocol.HTTPS},
							plaintextSucceeds: false,
							mtlsSucceeds:      true,
						},
					},
				},
				{
					// There is only authN policy that enables mTLS (Permissive).
					// The request should be allowed because the client is always using plain text.
					name: "PERMISSIVE",
					config: `apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: mtls
spec:
  mtls:
    mode: PERMISSIVE`,
					expected: []expect{
						{
							port:              echo.Port{ServicePort: 8085, Protocol: protocol.HTTP},
							plaintextSucceeds: true,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8086, Protocol: protocol.HTTP},
							plaintextSucceeds: true,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8087, Protocol: protocol.TCP},
							plaintextSucceeds: true,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8088, Protocol: protocol.TCP},
							plaintextSucceeds: true,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8089, Protocol: protocol.HTTPS},
							plaintextSucceeds: true,
							mtlsSucceeds:      true,
						},
					},
				},
				{
					// There is only authN policy that disables mTLS by default and enables mTLS strict on port 8086, 8088, 8084.
					// The request should be denied on port 8086, 8088, 8084.
					name: "DISABLE with STRICT",
					config: `apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: mtls
spec:
  selector:
    matchLabels:
      app: {{ .dst }}
  mtls:
    mode: DISABLE
  portLevelMtls:
    8086:
      mode: STRICT
    8088:
      mode: STRICT
    8084:
      mode: STRICT`,
					expected: []expect{
						{
							port:              echo.Port{ServicePort: 8085, Protocol: protocol.HTTP},
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
						{
							port:              echo.Port{ServicePort: 8086, Protocol: protocol.HTTP},
							plaintextSucceeds: false,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8087, Protocol: protocol.TCP},
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
						{
							port:              echo.Port{ServicePort: 8088, Protocol: protocol.TCP},
							plaintextSucceeds: false,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8089, Protocol: protocol.HTTPS},
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
						{
							port:              echo.Port{ServicePort: 8084, Protocol: protocol.HTTPS},
							plaintextSucceeds: false,
							mtlsSucceeds:      true,
						},
					},
				},
				{
					// There is only authN policy that enables mTLS by default and disables mTLS strict on port 8086 and 8088.
					// The request should be denied on port 8085 and 8071.
					name: "STRICT with disable",
					config: `apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: mtls
spec:
  selector:
    matchLabels:
      app: {{ .dst }}
  mtls:
    mode: STRICT
  portLevelMtls:
    8086:
      mode: DISABLE
    8088:
      mode: DISABLE
    8084:
      mode: DISABLE`,
					expected: []expect{
						{
							port:              echo.Port{ServicePort: 8085, Protocol: protocol.HTTP},
							plaintextSucceeds: false,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8086, Protocol: protocol.HTTP},
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
						{
							port:              echo.Port{ServicePort: 8087, Protocol: protocol.TCP},
							plaintextSucceeds: false,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8088, Protocol: protocol.TCP},
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
						{
							port:              echo.Port{ServicePort: 8089, Protocol: protocol.HTTPS},
							plaintextSucceeds: false,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8084, Protocol: protocol.HTTPS},
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
					},
				},
				{
					name: "PERMISSIVE with strict",
					config: `apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: mtls
spec:
  selector:
    matchLabels:
      app: {{ .dst }}
  mtls:
    mode: PERMISSIVE
  portLevelMtls:
    8086:
      mode: STRICT
    8088:
      mode: STRICT
    8084:
      mode: STRICT`,
					expected: []expect{
						{
							port:              echo.Port{ServicePort: 8085, Protocol: protocol.HTTP},
							plaintextSucceeds: true,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8086, Protocol: protocol.HTTP},
							plaintextSucceeds: false,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8087, Protocol: protocol.TCP},
							plaintextSucceeds: true,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8088, Protocol: protocol.TCP},
							plaintextSucceeds: false,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8089, Protocol: protocol.HTTPS},
							plaintextSucceeds: true,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8084, Protocol: protocol.HTTPS},
							plaintextSucceeds: false,
							mtlsSucceeds:      true,
						},
					},
				},
				{
					name: "STRICT with PERMISSIVE",
					config: `apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: mtls
spec:
  selector:
    matchLabels:
      app: {{ .dst }}
  mtls:
    mode: STRICT
  portLevelMtls:
    8086:
      mode: PERMISSIVE
    8088:
      mode: PERMISSIVE
    8084:
      mode: PERMISSIVE`,
					expected: []expect{
						{
							port:              echo.Port{ServicePort: 8085, Protocol: protocol.HTTP},
							plaintextSucceeds: false,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8086, Protocol: protocol.HTTP},
							plaintextSucceeds: true,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8087, Protocol: protocol.TCP},
							plaintextSucceeds: false,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8088, Protocol: protocol.TCP},
							plaintextSucceeds: true,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8089, Protocol: protocol.HTTPS},
							plaintextSucceeds: false,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8084, Protocol: protocol.HTTPS},
							plaintextSucceeds: true,
							mtlsSucceeds:      true,
						},
					},
				},
				{
					name: "PERMISSIVE with disable",
					config: `apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: mtls
spec:
  selector:
    matchLabels:
      app: {{ .dst }}
  mtls:
    mode: PERMISSIVE
  portLevelMtls:
    8086:
      mode: DISABLE
    8088:
      mode: DISABLE
    8084:
      mode: DISABLE`,
					expected: []expect{
						{
							port:              echo.Port{ServicePort: 8085, Protocol: protocol.HTTP},
							plaintextSucceeds: true,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8086, Protocol: protocol.HTTP},
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
						{
							port:              echo.Port{ServicePort: 8087, Protocol: protocol.TCP},
							plaintextSucceeds: true,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8088, Protocol: protocol.TCP},
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
						{
							port:              echo.Port{ServicePort: 8089, Protocol: protocol.HTTPS},
							plaintextSucceeds: true,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8084, Protocol: protocol.HTTPS},
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
					},
				},
				{
					name: "DISABLE with PERMISSIVE",
					config: `apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: mtls
spec:
  selector:
    matchLabels:
      app: {{ .dst }}
  mtls:
    mode: DISABLE
  portLevelMtls:
    8086:
      mode: PERMISSIVE
    8088:
      mode: PERMISSIVE
    8084:
      mode: PERMISSIVE`,
					expected: []expect{
						{
							port:              echo.Port{ServicePort: 8085, Protocol: protocol.HTTP},
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
						{
							port:              echo.Port{ServicePort: 8086, Protocol: protocol.HTTP},
							plaintextSucceeds: true,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8087, Protocol: protocol.TCP},
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
						{
							port:              echo.Port{ServicePort: 8088, Protocol: protocol.TCP},
							plaintextSucceeds: true,
							mtlsSucceeds:      true,
						},
						{
							port:              echo.Port{ServicePort: 8089, Protocol: protocol.HTTPS},
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
						{
							port:              echo.Port{ServicePort: 8084, Protocol: protocol.HTTPS},
							plaintextSucceeds: true,
							mtlsSucceeds:      true,
						},
					},
				},
			}

			// TODO(slandow) replace this with built-in framework filters (blocked by https://github.com/istio/istio/pull/31565)
			srcMatcher := match.Or(
				match.ServiceName(echo.NamespacedName{
					Name:      util.NakedSvc,
					Namespace: ns,
				}),
				match.ServiceName(echo.NamespacedName{
					Name:      util.BSvc,
					Namespace: ns,
				}),
				match.ServiceName(echo.NamespacedName{
					Name:      util.VMSvc,
					Namespace: ns,
				}))
			for _, tc := range cases {
				t.NewSubTest(tc.name).Run(func(t framework.TestContext) {
					echotest.New(t, apps.All).
						SetupForDestination(func(t framework.TestContext, to echo.Target) error {
							cfg := yml.MustApplyNamespace(t, tmpl.MustEvaluate(
								tc.config,
								map[string]string{
									"dst": to.Config().Service,
								},
							), ns.Name())
							// Its not trivial to force mTLS to passthrough ports. To workaround this, we will
							// set up a SE and DR that forces it.
							fakesvc := yml.MustApplyNamespace(t, tmpl.MustEvaluate(
								`apiVersion: networking.istio.io/v1beta1
kind: ServiceEntry
metadata:
  name: dest-via-mtls
spec:
  hosts:
  - fake.destination
  addresses:
  - {{.IP}}
  ports:
  - number: 8084
    name: port-8084
    protocol: TCP
  - number: 8085
    name: port-8085
    protocol: TCP
  - number: 8086
    name: port-8086
    protocol: TCP
  - number: 8087
    name: port-8087
    protocol: TCP
  - number: 8088
    name: port-8088
    protocol: TCP
  - number: 8089
    name: port-8089
    protocol: TCP
  location: MESH_INTERNAL
  resolution: NONE
---
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: default
spec:
  host: "fake.destination"
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
---`,
								map[string]string{
									"IP": to.WorkloadsOrFail(t)[0].Address(),
								},
							), ns.Name())
							return t.ConfigIstio().YAML(ns.Name(), cfg, fakesvc).Apply()
						}).
						FromMatch(srcMatcher).
						ConditionallyTo(echotest.ReachableDestinations).
						To(
							echotest.SingleSimplePodServiceAndAllSpecial(),
							echotest.FilterMatch(match.And(
								match.Namespace(ns),
								match.NotHeadless,
								match.NotNaked,
								match.NotExternal,
								util.IsNotMultiversion))).
						Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
							clusterName := from.Config().Cluster.StableName()
							if to.Config().Cluster.StableName() != clusterName {
								// The workaround for mTLS does not work on cross cluster traffic.
								t.Skip()
							}
							if from.Config().Service == to.Config().Service {
								// The workaround for mTLS does not work on a workload calling itself.
								// Skip vm->vm requests.
								t.Skip()
							}
							nameSuffix := "mtls"
							if from.Config().IsNaked() {
								nameSuffix = "plaintext"
							}
							for _, expect := range tc.expected {
								want := expect.mtlsSucceeds
								if from.Config().IsNaked() {
									want = expect.plaintextSucceeds
								}
								c := check.OK()
								if !want {
									c = check.ErrorOrStatus(http.StatusForbidden)
								}
								name := fmt.Sprintf("%v/port %d[%t]", nameSuffix, expect.port.ServicePort, want)
								callOpt := echo.CallOptions{
									Count:   echo.DefaultCallsPerWorkload() * to.WorkloadsOrFail(t).Len(),
									Port:    expect.port,
									Message: "HelloWorld",
									// Do not set To to dest, otherwise fillInCallOptions() will
									// complain with port does not match.
									ToWorkload: to.Instances()[0],
									Check:      c,
								}
								t.NewSubTest(name).Run(func(t framework.TestContext) {
									from.CallOrFail(t, callOpt)
								})
							}
						})
				})
			}
		})
}
