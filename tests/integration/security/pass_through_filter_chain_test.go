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
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/echo/common/response"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/pkg/test/util/yml"
	"istio.io/istio/tests/integration/security/util"
)

// TestPassThroughFilterChain tests the authN and authZ policy on the pass through filter chain.
func TestPassThroughFilterChain(t *testing.T) {
	framework.
		NewTest(t).
		Features("security.filterchain").
		Run(func(ctx framework.TestContext) {
			ns := apps.Namespace1
			type expect struct {
				port *echo.Port
				want bool
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
							port: &echo.Port{ServicePort: 8085, Protocol: protocol.HTTP},
							want: true,
						},
						{
							port: &echo.Port{ServicePort: 8086, Protocol: protocol.HTTP},
							want: true,
						},
						{
							port: &echo.Port{ServicePort: 8087, Protocol: protocol.TCP},
							want: true,
						},
						{
							port: &echo.Port{ServicePort: 8088, Protocol: protocol.TCP},
							want: true,
						},
					},
				},
				{
					// There is only authZ policy that allows access to port 8085 and 8087.
					// Only request to port 8085, 8087 should be allowed.
					name: "DISABLE with authz",
					config: `apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: mtls
spec:
  mtls:
    mode: DISABLE
---
apiVersion: "security.istio.io/v1beta1"
kind: AuthorizationPolicy
metadata:
  name: authz
spec:
  rules:
  - to:
    - operation:
        ports: ["8085", "8087"]`,
					expected: []expect{
						{
							port: &echo.Port{ServicePort: 8085, Protocol: protocol.HTTP},
							want: true,
						},
						{
							port: &echo.Port{ServicePort: 8086, Protocol: protocol.HTTP},
							want: false,
						},
						{
							port: &echo.Port{ServicePort: 8087, Protocol: protocol.TCP},
							want: true,
						},
						{
							port: &echo.Port{ServicePort: 8088, Protocol: protocol.TCP},
							want: false,
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
							port: &echo.Port{ServicePort: 8085, Protocol: protocol.HTTP},
							want: false,
						},
						{
							port: &echo.Port{ServicePort: 8086, Protocol: protocol.HTTP},
							want: false,
						},
						{
							port: &echo.Port{ServicePort: 8087, Protocol: protocol.TCP},
							want: false,
						},
						{
							port: &echo.Port{ServicePort: 8088, Protocol: protocol.TCP},
							want: false,
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
							port: &echo.Port{ServicePort: 8085, Protocol: protocol.HTTP},
							want: true,
						},
						{
							port: &echo.Port{ServicePort: 8086, Protocol: protocol.HTTP},
							want: true,
						},
						{
							port: &echo.Port{ServicePort: 8087, Protocol: protocol.TCP},
							want: true,
						},
						{
							port: &echo.Port{ServicePort: 8088, Protocol: protocol.TCP},
							want: true,
						},
					},
				},

				{
					// There is only authN policy that disables mTLS by default and enables mTLS strict on port 8086 and 8088.
					// The request should be denied on port 8086 and 8088.
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
      mode: STRICT`,
					expected: []expect{
						{
							port: &echo.Port{ServicePort: 8085, Protocol: protocol.HTTP},
							want: true,
						},
						{
							port: &echo.Port{ServicePort: 8086, Protocol: protocol.HTTP},
							want: false,
						},
						{
							port: &echo.Port{ServicePort: 8087, Protocol: protocol.TCP},
							want: true,
						},
						{
							port: &echo.Port{ServicePort: 8088, Protocol: protocol.TCP},
							want: false,
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
      mode: DISABLE`,
					expected: []expect{
						{
							port: &echo.Port{ServicePort: 8085, Protocol: protocol.HTTP},
							want: false,
						},
						{
							port: &echo.Port{ServicePort: 8086, Protocol: protocol.HTTP},
							want: true,
						},
						{
							port: &echo.Port{ServicePort: 8087, Protocol: protocol.TCP},
							want: false,
						},
						{
							port: &echo.Port{ServicePort: 8088, Protocol: protocol.TCP},
							want: true,
						},
					},
				},
			}

			// srcFilter finds the naked app as client.
			// TODO(slandow) replace this with built-in framework filters (blocked by https://github.com/istio/istio/pull/31565)
			srcFilter := []echotest.SimpleFilter{func(instances echo.Instances) echo.Instances {
				return apps.Naked.Match(echo.Namespace(ns.Name()))
			}}
			for _, tc := range cases {
				echotest.New(ctx, apps.All).
					SetupForPair(func(t framework.TestContext, src, dst echo.Instances) error {
						cfg := yml.MustApplyNamespace(t, tmpl.MustEvaluate(
							tc.config,
							map[string]string{
								"dst": dst[0].Config().Service,
							},
						), ns.Name())
						if err := t.Config().ApplyYAML(ns.Name(), cfg); err != nil {
							return err
						}
						util.WaitForConfig(t, cfg, ns)
						defer t.Config().DeleteYAMLOrFail(t, ns.Name(), cfg)
						return nil
					}).
					From(srcFilter...).
					ConditionallyTo(echotest.ReachableDestinations).
					To(
						echotest.SingleSimplePodServiceAndAllSpecial(),
						echotest.Not(func(instances echo.Instances) echo.Instances { return instances.Match(echo.IsHeadless()) }),
						echotest.Not(func(instances echo.Instances) echo.Instances { return instances.Match(echo.IsNaked()) }),
						echotest.Not(func(instances echo.Instances) echo.Instances { return instances.Match(echo.IsExternal()) }),
						echotest.Not(func(instances echo.Instances) echo.Instances { return instances.Match(util.IsMultiversion()) }),
						func(instances echo.Instances) echo.Instances { return instances.Match(echo.Namespace(ns.Name())) },
					).
					Run(func(t framework.TestContext, src echo.Instance, dest echo.Instances) {
						clusterName := src.Config().Cluster.StableName()
						if dest[0].Config().Cluster.StableName() != clusterName {
							t.Skip()
						}
						for _, expect := range tc.expected {
							name := fmt.Sprintf("In %s/%v/port %d[%t]", clusterName, tc.name, expect.port.ServicePort, expect.want)
							host := fmt.Sprintf("%s:%d", getWorkload(dest[0], t).Address(), expect.port.ServicePort)
							callOpt := echo.CallOptions{
								Count: util.CallsPerCluster * len(dest),
								Port:  expect.port,
								Headers: map[string][]string{
									"Host": {host},
								},
								Message: "HelloWorld",
								// Do not set Target to dest, otherwise fillInCallOptions() will
								// complain with port does not match.
								Address: getWorkload(dest[0], t).Address(),
								Validator: echo.And(echo.ValidatorFunc(
									func(responses client.ParsedResponses, err error) error {
										if expect.want {
											if err != nil {
												return fmt.Errorf("want allow but got error: %v", err)
											}
											if responses.Len() < 1 {
												return fmt.Errorf("received no responses from request to %s", host)
											}
											if okErr := responses.CheckOK(); okErr != nil && expect.port.Protocol == protocol.HTTP {
												return fmt.Errorf("want status %s but got %s", response.StatusCodeOK, okErr.Error())
											}
										} else {
											// Check HTTP forbidden response
											if responses.Len() >= 1 && responses.CheckCode(response.StatusCodeForbidden) == nil {
												return nil
											}

											if err == nil {
												return fmt.Errorf("want error but got none: %v", responses.String())
											}
										}
										return nil
									})),
							}
							t.NewSubTest(name).Run(func(t framework.TestContext) {
								src.CallWithRetryOrFail(t, callOpt, echo.DefaultCallRetryOptions()...)
							})
						}
					})
			}
		})
}

func getWorkload(instance echo.Instance, t test.Failer) echo.Workload {
	workloads, err := instance.Workloads()
	if err != nil {
		t.Fatalf(fmt.Sprintf("failed to get Subsets: %v", err))
	}
	if len(workloads) < 1 {
		t.Fatalf("want at least 1 workload but found 0")
	}
	return workloads[0]
}
