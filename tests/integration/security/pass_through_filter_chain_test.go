//go:build integ

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

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/config"
	"istio.io/istio/pkg/test/framework/components/echo/config/param"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/echo/match"
)

// TestPassThroughFilterChain tests the authN and authZ policy on the pass through filter chain.
func TestPassThroughFilterChain(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			type expect struct {
				port echo.Port
				// Plaintext will be sent from Naked pods.
				plaintextSucceeds bool
				// MTLS will be sent from all pods other than Naked.
				mtlsSucceeds bool
			}
			cases := []struct {
				name     string
				config   string
				expected []expect
			}{
				// There is no authN/authZ policy.
				// All requests should succeed, this is to verify the pass through filter chain and
				// the workload ports are working correctly.
				{
					name: "DISABLE",
					config: `apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: mtls
spec:
  mtls:
    mode: DISABLE`,
					expected: []expect{
						{
							port:              ports.TCPWorkloadOnly,
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
						{
							port:              ports.HTTPWorkloadOnly,
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
					},
				},
				{
					// There is only authZ policy that allows access to TCPWorkloadOnly should be allowed.
					name: "DISABLE with authz",
					config: `apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: mtls
spec:
  mtls:
    mode: DISABLE
---
apiVersion: security.istio.io/v1
kind: AuthorizationPolicy
metadata:
  name: authz
spec:
  rules:
  - to:
    - operation:
        ports:
        - "19092" # TCPWorkloadOnly`,
					expected: []expect{
						{
							port:              ports.TCPWorkloadOnly,
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
						{
							port:              ports.HTTPWorkloadOnly,
							plaintextSucceeds: false,
							mtlsSucceeds:      false,
						},
					},
				},
				{
					// There is only authN policy that enables mTLS (Strict).
					// The request should be denied because the client is always using plain text.
					name: "STRICT",
					config: `apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: mtls
spec:
  mtls:
    mode: STRICT`,
					expected: []expect{
						{
							port:              ports.TCPWorkloadOnly,
							plaintextSucceeds: false,
							mtlsSucceeds:      true,
						},
						{
							port:              ports.HTTPWorkloadOnly,
							plaintextSucceeds: false,
							mtlsSucceeds:      true,
						},
					},
				},
				{
					// There is only authN policy that enables mTLS (Permissive).
					// The request should be allowed because the client is always using plain text.
					name: "PERMISSIVE",
					config: `apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: mtls
spec:
  mtls:
    mode: PERMISSIVE`,
					expected: []expect{
						{
							port:              ports.TCPWorkloadOnly,
							plaintextSucceeds: true,
							mtlsSucceeds:      true,
						},
						{
							port:              ports.HTTPWorkloadOnly,
							plaintextSucceeds: true,
							mtlsSucceeds:      true,
						},
					},
				},
				{
					// There is only authN policy that disables mTLS by default and enables mTLS strict on port 8086, 8088, 8084.
					// The request should be denied on port 8086, 8088, 8084.
					name: "DISABLE with STRICT",
					config: `apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: {{ .To.ServiceName }}-mtls
spec:
  selector:
    matchLabels:
      app: {{ .To.ServiceName }}
  mtls:
    mode: DISABLE
  portLevelMtls:
    {{ (.To.PortForName "tcp-wl-only").WorkloadPort }}:
      mode: STRICT`,
					expected: []expect{
						{
							port:              ports.TCPWorkloadOnly,
							plaintextSucceeds: false,
							mtlsSucceeds:      true,
						},
						{
							port:              ports.HTTPWorkloadOnly,
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
					},
				},
				{
					// There is only authN policy that enables mTLS by default and disables mTLS strict on port 8086 and 8088.
					// The request should be denied on port 8085 and 8071.
					name: "STRICT with DISABLE",
					config: `apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: {{ .To.ServiceName }}-mtls
spec:
  selector:
    matchLabels:
      app: {{ .To.ServiceName }}
  mtls:
    mode: STRICT
  portLevelMtls:
    {{ (.To.PortForName "tcp-wl-only").WorkloadPort }}:
      mode: DISABLE`,
					expected: []expect{
						{
							port:              ports.TCPWorkloadOnly,
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
						{
							port:              ports.HTTPWorkloadOnly,
							plaintextSucceeds: false,
							mtlsSucceeds:      true,
						},
					},
				},
				{
					name: "PERMISSIVE with STRICT",
					config: `apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: {{ .To.ServiceName }}-mtls
spec:
  selector:
    matchLabels:
      app: {{ .To.ServiceName }}
  mtls:
    mode: PERMISSIVE
  portLevelMtls:
    {{ (.To.PortForName "tcp-wl-only").WorkloadPort }}:
      mode: STRICT`,
					expected: []expect{
						{
							port:              ports.TCPWorkloadOnly,
							plaintextSucceeds: false,
							mtlsSucceeds:      true,
						},
						{
							port:              ports.HTTPWorkloadOnly,
							plaintextSucceeds: true,
							mtlsSucceeds:      true,
						},
					},
				},
				{
					name: "STRICT with PERMISSIVE",
					config: `apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: {{ .To.ServiceName }}-mtls
spec:
  selector:
    matchLabels:
      app: {{ .To.ServiceName }}
  mtls:
    mode: STRICT
  portLevelMtls:
    {{ (.To.PortForName "tcp-wl-only").WorkloadPort }}:
      mode: PERMISSIVE`,
					expected: []expect{
						{
							port:              ports.TCPWorkloadOnly,
							plaintextSucceeds: true,
							mtlsSucceeds:      true,
						},
						{
							port:              ports.HTTPWorkloadOnly,
							plaintextSucceeds: false,
							mtlsSucceeds:      true,
						},
					},
				},
				{
					name: "PERMISSIVE with DISABLE",
					config: `apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: {{ .To.ServiceName }}-mtls
spec:
  selector:
    matchLabels:
      app: {{ .To.ServiceName }}
  mtls:
    mode: PERMISSIVE
  portLevelMtls:
    {{ (.To.PortForName "tcp-wl-only").WorkloadPort }}:
      mode: DISABLE`,
					expected: []expect{
						{
							port:              ports.TCPWorkloadOnly,
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
						{
							port:              ports.HTTPWorkloadOnly,
							plaintextSucceeds: true,
							mtlsSucceeds:      true,
						},
					},
				},
				{
					name: "DISABLE with PERMISSIVE",
					config: `apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: {{ .To.ServiceName }}-mtls
spec:
  selector:
    matchLabels:
      app: {{ .To.ServiceName }}
  mtls:
    mode: DISABLE
  portLevelMtls:
    {{ (.To.PortForName "tcp-wl-only").WorkloadPort }}:
      mode: PERMISSIVE`,
					expected: []expect{
						{
							port:              ports.TCPWorkloadOnly,
							plaintextSucceeds: true,
							mtlsSucceeds:      true,
						},
						{
							port:              ports.HTTPWorkloadOnly,
							plaintextSucceeds: true,
							mtlsSucceeds:      false,
						},
					},
				},
			}

			for _, tc := range cases {
				t.NewSubTest(tc.name).Run(func(t framework.TestContext) {
					// Create the PeerAuthentication for the test case.
					config.New(t).
						// Add any test-specific configuration.
						Source(config.YAML(tc.config).WithParams(param.Params{
							param.Namespace.String(): apps.Ns1.Namespace,
						})).
						// It's not trivial to force mTLS to pass-through ports. To work around this, we will
						// set up a SE and DR that forces it.
						//
						// Since our client will talk directly to pods via IP, we have to configure the ports
						// in the SE as TCP, since only TCP does will match based in IP address rather than host.
						// This means that even for ports that support HTTP, we won't be able to check headers
						// to confirm that mTLS was used. To work around this, we configure our 2 workload-only
						// ports differently for each test and rely on allow/deny for each to indicate whether
						// mtls was used.
						Source(config.YAML(`apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: {{ .To.ServiceName }}-se
spec:
  hosts:
  - fake.destination.{{ .To.ServiceName }}
  location: MESH_INTERNAL
  resolution: NONE
  addresses:
{{- range $ip := .To.MustWorkloads.Addresses }}
  - {{ $ip }}
{{- end }}
  ports:
{{- range $port := .To.Config.Ports.GetWorkloadOnlyPorts }}
  - number: {{ $port.WorkloadPort }}
    name: {{ $port.Name }}
    protocol: TCP
{{- end }}
---
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: {{ .To.ServiceName }}-dr
spec:
  host: "fake.destination.{{ .To.ServiceName }}"
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
---`)).
						BuildAll(nil, apps.Ns1.All).
						Apply()

					echotest.New(t, apps.Ns1.All.Instances()).
						WithDefaultFilters(1, 1).
						FromMatch(match.NotProxylessGRPC).
						ToMatch(match.And(
							// TODO(nmittler): Why not headless/multiversion?
							match.NotHeadless,
							match.NotMultiVersion,
							match.NotNaked,
							match.NotProxylessGRPC)).
						ConditionallyTo(echotest.NoSelfCalls).
						// TODO(nmittler): Why does passthrough not work?
						ConditionallyTo(echotest.SameNetwork).
						Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
							for _, exp := range tc.expected {
								p := exp.port
								opts := echo.CallOptions{
									// Do not set To, otherwise fillInCallOptions() will
									// complain with port does not match.
									ToWorkload: to.Instances()[0],
									Port: echo.Port{
										Protocol: p.Protocol,

										// The ServicePort tells the client which port to connect to.
										// This is a bit hacky, but since we're connecting directly
										// to a pod, we just set it to the WorkloadPort.
										ServicePort: p.WorkloadPort,
									},
									Count: echo.DefaultCallsPerWorkload() * to.WorkloadsOrFail(t).Len(),
								}

								allow := allowValue(exp.mtlsSucceeds)
								if from.Config().IsNaked() {
									allow = allowValue(exp.plaintextSucceeds)
								}

								if allow {
									opts.Check = check.OK()
								} else {
									opts.Check = check.ErrorOrStatus(http.StatusForbidden)
								}

								mtlsString := "mtls"
								if from.Config().IsNaked() {
									mtlsString = "plaintext"
								}
								testName := fmt.Sprintf("%s/%s(%s)", mtlsString, p.Name, allow)
								t.NewSubTest(testName).Run(func(t framework.TestContext) {
									from.CallOrFail(t, opts)
								})
							}
						})
				})
			}
		})
}
