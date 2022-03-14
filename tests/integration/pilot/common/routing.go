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

package common

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"time"

	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/sets"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/security"
	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test"
	echoClient "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/check"
	"istio.io/istio/pkg/test/echo/common/scheme"
	epb "istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/common/jwt"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
)

const httpVirtualServiceTmpl = `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: {{.VirtualServiceHost}}
spec:
  gateways:
  - {{.Gateway}}
  hosts:
  - {{.VirtualServiceHost}}
  http:
  - route:
    - destination:
        host: {{.VirtualServiceHost}}
        port:
          number: {{.Port}}
{{- if .MatchScheme }}
    match:
    - scheme:
        exact: {{.MatchScheme}}
    headers:
      request:
        add:
          istio-custom-header: user-defined-value
{{- end }}
---
`

func httpVirtualService(gateway, host string, port int) string {
	return tmpl.MustEvaluate(httpVirtualServiceTmpl, struct {
		Gateway            string
		VirtualServiceHost string
		Port               int
		MatchScheme        string
	}{gateway, host, port, ""})
}

const gatewayTmpl = `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: gateway
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: {{.GatewayPort}}
      name: {{.GatewayPortName}}
      protocol: {{.GatewayProtocol}}
{{- if .Credential }}
    tls:
      mode: SIMPLE
      credentialName: {{.Credential}}
{{- if .Ciphers }}
      cipherSuites:
{{- range $cipher := .Ciphers }}
      - "{{$cipher}}"
{{- end }}
{{- end }}
{{- end }}
    hosts:
    - "{{.GatewayHost}}"
---
`

func httpGateway(host string) string {
	return tmpl.MustEvaluate(gatewayTmpl, struct {
		GatewayHost     string
		GatewayPort     int
		GatewayPortName string
		GatewayProtocol string
		Credential      string
	}{
		host, 80, "http", "HTTP", "",
	})
}

func virtualServiceCases(skipVM bool) []TrafficTestCase {
	var cases []TrafficTestCase
	cases = append(cases,
		TrafficTestCase{
			name: "added header",
			config: `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
  - {{ .dstSvc }}
  http:
  - route:
    - destination:
        host: {{ .dstSvc }}
    headers:
      request:
        add:
          istio-custom-header: user-defined-value`,
			opts: echo.CallOptions{
				Port: echo.Port{
					Name: "http",
				},
				Count: 1,
				Check: check.And(
					check.OK(),
					check.RequestHeader("Istio-Custom-Header", "user-defined-value")),
			},
			workloadAgnostic: true,
		},
		TrafficTestCase{
			name: "set header",
			config: `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
  - {{ (index .dst 0).Config.Service }}
  http:
  - route:
    - destination:
        host: {{ (index .dst 0).Config.Service }}
    headers:
      request:
        set:
          x-custom: some-value`,
			opts: echo.CallOptions{
				Port: echo.Port{
					Name: "http",
				},
				Count: 1,
				Check: check.And(
					check.OK(),
					check.RequestHeader("X-Custom", "some-value")),
			},
			workloadAgnostic: true,
		},
		TrafficTestCase{
			name: "set authority header",
			config: `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
  - {{ (index .dst 0).Config.Service }}
  http:
  - route:
    - destination:
        host: {{ (index .dst 0).Config.Service }}
    headers:
      request:
        set:
          :authority: my-custom-authority`,
			opts: echo.CallOptions{
				Port: echo.Port{
					Name: "http",
				},
				Count: 1,
				Check: check.And(
					check.OK(),
					check.Host("my-custom-authority")),
			},
			workloadAgnostic: true,
			minIstioVersion:  "1.10.0",
		},
		TrafficTestCase{
			name: "set host header in destination",
			config: `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
  - {{ (index .dst 0).Config.Service }}
  http:
  - route:
    - destination:
        host: {{ (index .dst 0).Config.Service }}
      headers:
        request:
          set:
            Host: my-custom-authority`,
			opts: echo.CallOptions{
				Port: echo.Port{
					Name: "http",
				},
				Count: 1,
				Check: check.And(
					check.OK(),
					check.Host("my-custom-authority")),
			},
			workloadAgnostic: true,
			minIstioVersion:  "1.10.0",
		},
		TrafficTestCase{
			name: "set host header in route and destination",
			config: `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
  - {{ (index .dst 0).Config.Service }}
  http:
  - route:
    - destination:
        host: {{ (index .dst 0).Config.Service }}
      headers:
        request:
          set:
            Host: dest-authority
    headers:
      request:
        set:
          :authority: route-authority`,
			opts: echo.CallOptions{
				Port: echo.Port{
					Name: "http",
				},
				Count: 1,
				Check: check.And(
					check.OK(),
					check.Host("route-authority")),
			},
			workloadAgnostic: true,
			minIstioVersion:  "1.12.0",
		},
		TrafficTestCase{
			name: "set host header in route and multi destination",
			config: `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
  - {{ (index .dst 0).Config.Service }}
  http:
  - route:
    - destination:
        host: {{ (index .dst 0).Config.Service }}
      headers:
        request:
          set:
            Host: dest-authority
      weight: 50
    - destination:
        host: {{ (index .dst 0).Config.Service }}
      weight: 50
    headers:
      request:
        set:
          :authority: route-authority`,
			opts: echo.CallOptions{
				Port: echo.Port{
					Name: "http",
				},
				Count: 1,
				Check: check.And(
					check.OK(),
					check.Host("route-authority")),
			},
			workloadAgnostic: true,
			minIstioVersion:  "1.12.0",
		},
		TrafficTestCase{
			name: "set host header multi destination",
			config: `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
  - {{ (index .dst 0).Config.Service }}
  http:
  - route:
    - destination:
        host: {{ (index .dst 0).Config.Service }}
      headers:
        request:
          set:
            Host: dest-authority
      weight: 50
    - destination:
        host: {{ (index .dst 0).Config.Service }}
      headers:
        request:
          set:
            Host: dest-authority
      weight: 50`,
			opts: echo.CallOptions{
				Port: echo.Port{
					Name: "http",
				},
				Count: 1,
				Check: check.And(
					check.OK(),
					check.Host("dest-authority")),
			},
			workloadAgnostic: true,
			minIstioVersion:  "1.12.0",
		},
		TrafficTestCase{
			name: "redirect",
			config: `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
    - {{ .dstSvc }}
  http:
  - match:
    - uri:
        exact: /foo
    redirect:
      uri: /new/path
  - match:
    - uri:
        exact: /new/path
    route:
    - destination:
        host: {{ .dstSvc }}`,
			opts: echo.CallOptions{
				Port: echo.Port{
					Name: "http",
				},
				HTTP: echo.HTTP{
					Path:            "/foo?key=value",
					FollowRedirects: true,
				},
				Count: 1,
				Check: check.And(
					check.OK(),
					check.URL("/new/path?key=value")),
			},
			workloadAgnostic: true,
		},
		TrafficTestCase{
			name: "redirect port and scheme",
			config: `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
    - {{ .dstSvc }}
  http:
  - match:
    - uri:
        exact: /foo
    redirect:
      derivePort: FROM_REQUEST_PORT
      scheme: https
`,
			opts: echo.CallOptions{
				Port: echo.Port{
					Name: "http",
				},
				HTTP: echo.HTTP{
					Path:            "/foo",
					FollowRedirects: false,
				},
				Count: 1,
				Check: check.And(
					check.Status(http.StatusMovedPermanently),
					check.Each(
						func(r echoClient.Response) error {
							originalHostname, err := url.Parse(r.RequestURL)
							if err != nil {
								return err
							}
							return ExpectString(r.ResponseHeaders.Get("Location"),
								fmt.Sprintf("https://%s:%d/foo", originalHostname.Hostname(), ports.All().MustForName("http").ServicePort),
								"Location")
						})),
			},
			workloadAgnostic: true,
		},
		TrafficTestCase{
			name: "rewrite uri",
			config: `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
    - {{ .dstSvc }}
  http:
  - match:
    - uri:
        exact: /foo
    rewrite:
      uri: /new/path
    route:
    - destination:
        host: {{ .dstSvc }}`,
			opts: echo.CallOptions{
				Port: echo.Port{
					Name: "http",
				},
				HTTP: echo.HTTP{
					Path: "/foo?key=value#hash",
				},
				Count: 1,
				Check: check.And(
					check.OK(),
					check.URL("/new/path?key=value")),
			},
			workloadAgnostic: true,
		},
		TrafficTestCase{
			name: "rewrite authority",
			config: `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
    - {{ .dstSvc }}
  http:
  - match:
    - uri:
        exact: /foo
    rewrite:
      authority: new-authority
    route:
    - destination:
        host: {{ .dstSvc }}`,
			opts: echo.CallOptions{
				Port: echo.Port{
					Name: "http",
				},
				HTTP: echo.HTTP{
					Path: "/foo",
				},
				Count: 1,
				Check: check.And(
					check.OK(),
					check.Host("new-authority")),
			},
			workloadAgnostic: true,
		},
		TrafficTestCase{
			name: "cors",
			// TODO https://github.com/istio/istio/issues/31532
			targetMatchers: []match.Matcher{match.IsNotTProxy, match.IsNotVM},

			config: `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
    - {{ .dstSvc }}
  http:
  - corsPolicy:
      allowOrigins:
      - exact: cors.com
      allowMethods:
      - POST
      - GET
      allowCredentials: false
      allowHeaders:
      - X-Foo-Bar
      - X-Foo-Baz
      maxAge: "24h"
    route:
    - destination:
        host: {{ .dstSvc }}
`,
			children: []TrafficCall{
				{
					name: "preflight",
					opts: func() echo.CallOptions {
						return echo.CallOptions{
							Port: echo.Port{
								Name: "http",
							},
							HTTP: echo.HTTP{
								Method: "OPTIONS",
								Headers: headers.New().
									With(headers.Origin, "cors.com").
									With(headers.AccessControlRequestMethod, "DELETE").
									Build(),
							},
							Count: 1,
							Check: check.And(
								check.OK(),
								check.ResponseHeaders(map[string]string{
									"Access-Control-Allow-Origin":  "cors.com",
									"Access-Control-Allow-Methods": "POST,GET",
									"Access-Control-Allow-Headers": "X-Foo-Bar,X-Foo-Baz",
									"Access-Control-Max-Age":       "86400",
								})),
						}
					}(),
				},
				{
					name: "get",
					opts: func() echo.CallOptions {
						return echo.CallOptions{
							Port: echo.Port{
								Name: "http",
							},
							HTTP: echo.HTTP{
								Headers: headers.New().With(headers.Origin, "cors.com").Build(),
							},
							Count: 1,
							Check: check.And(
								check.OK(),
								check.ResponseHeader("Access-Control-Allow-Origin", "cors.com")),
						}
					}(),
				},
				{
					// GET without matching origin
					name: "get no origin match",
					opts: echo.CallOptions{
						Port: echo.Port{
							Name: "http",
						},
						Count: 1,
						Check: check.And(
							check.OK(),
							check.ResponseHeader("Access-Control-Allow-Origin", "")),
					},
				},
			},
			workloadAgnostic: true,
		},
		// Retry conditions have been added to just check that config is correct.
		// Retries are not specifically tested.
		TrafficTestCase{
			name: "retry conditions",
			config: `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
  - {{ .dstSvc }}
  http:
  - route:
    - destination:
        host: {{ .dstSvc }}
    retries:
      attempts: 3
      perTryTimeout: 2s
      retryOn: gateway-error,connect-failure,refused-stream
      retryRemoteLocalities: true`,
			opts: echo.CallOptions{
				Port: echo.Port{
					Name: "http",
				},
				Count: 1,
				Check: check.OK(),
			},
			workloadAgnostic: true,
		},
		TrafficTestCase{
			name: "fault abort",
			config: `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
  - {{ (index .dst 0).Config.Service }}
  http:
  - route:
    - destination:
        host: {{ (index .dst 0).Config.Service }}
    fault:
      abort:
        percentage:
          value: 100
        httpStatus: 418`,
			opts: echo.CallOptions{
				Port: echo.Port{
					Name: "http",
				},
				Count: 1,
				Check: check.Status(http.StatusTeapot),
			},
			workloadAgnostic: true,
		},
	)

	// reduce the total # of subtests that don't give valuable coverage or just don't work
	for i, tc := range cases {
		// TODO include proxyless as different features become supported
		tc.sourceMatchers = append(tc.sourceMatchers, match.IsNotNaked, match.IsNotHeadless, match.IsNotProxylessGRPC)
		tc.targetMatchers = append(tc.targetMatchers, match.IsNotNaked, match.IsNotHeadless, match.IsNotProxylessGRPC)
		cases[i] = tc
	}

	splits := [][]int{
		{50, 25, 25},
		{80, 10, 10},
	}
	if skipVM {
		splits = [][]int{
			{50, 50},
			{80, 20},
		}
	}
	for _, split := range splits {
		split := split
		cases = append(cases, TrafficTestCase{
			name:           fmt.Sprintf("shifting-%d", split[0]),
			toN:            len(split),
			sourceMatchers: []match.Matcher{match.IsNotHeadless, match.IsNotNaked},
			targetMatchers: []match.Matcher{match.IsNotHeadless, match.IsNotExternal},
			templateVars: func(_ echo.Callers, _ echo.Instances) map[string]interface{} {
				return map[string]interface{}{
					"split": split,
				}
			},
			config: `
{{ $split := .split }} 
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
    - {{ ( index .dstSvcs 0) }}
  http:
  - route:
{{- range $idx, $svc := .dstSvcs }}
    - destination:
        host: {{ $svc }}
      weight: {{ ( index $split $idx ) }}
{{- end }}
`,
			checkForN: func(src echo.Caller, dests echo.Services, opts *echo.CallOptions) check.Checker {
				return check.And(
					check.OK(),
					func(responses echoClient.Responses, err error) error {
						errorThreshold := 10
						if len(split) != len(dests) {
							// shouldn't happen
							return fmt.Errorf("split configured for %d destinations, but framework gives %d", len(split), len(dests))
						}
						splitPerHost := map[string]int{}
						for i, pct := range split {
							splitPerHost[dests.Services()[i]] = pct
						}
						for hostName, exp := range splitPerHost {
							hostResponses := responses.Match(func(r echoClient.Response) bool {
								return strings.HasPrefix(r.Hostname, hostName)
							})
							if !AlmostEquals(len(hostResponses), exp, errorThreshold) {
								return fmt.Errorf("expected %v calls to %q, got %v", exp, hostName, len(hostResponses))
							}
							// echotest should have filtered the deployment to only contain reachable clusters
							to := match.Service(hostName).GetMatches(dests.Instances())
							toClusters := to.Clusters()
							// don't check headless since lb is unpredictable
							headlessTarget := match.IsHeadless.Any(to)
							if !headlessTarget && len(toClusters.ByNetwork()[src.(echo.Instance).Config().Cluster.NetworkName()]) > 1 {
								// Conditionally check reached clusters to work around connection load balancing issues
								// See https://github.com/istio/istio/issues/32208 for details
								// We want to skip this for requests from the cross-network pod
								if err := check.ReachedClusters(toClusters).Check(hostResponses, nil); err != nil {
									return fmt.Errorf("did not reach all clusters for %s: %v", hostName, err)
								}
							}
						}
						return nil
					})
			},
			setupOpts: func(src echo.Caller, opts *echo.CallOptions) {
				// TODO force this globally in echotest?
				if src, ok := src.(echo.Instance); ok && src.Config().IsProxylessGRPC() {
					opts.Port.Name = "grpc"
				}
			},
			opts: echo.CallOptions{
				Port: echo.Port{
					Name: "http",
				},
				Count: 100,
			},
			workloadAgnostic: true,
		})
	}

	return cases
}

func HostHeader(header string) http.Header {
	return headers.New().WithHost(header).Build()
}

// tlsOriginationCases contains tests TLS origination from DestinationRule
func tlsOriginationCases(apps *EchoDeployments) []TrafficTestCase {
	tc := TrafficTestCase{
		name: "",
		config: fmt.Sprintf(`
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: external
spec:
  host: %s
  trafficPolicy:
    tls:
      mode: SIMPLE
`, apps.External[0].Config().DefaultHostHeader),
		children: []TrafficCall{},
	}
	expects := []struct {
		port int
		alpn string
	}{
		{8888, "http/1.1"},
		{8882, "h2"},
	}
	for _, c := range apps.PodA {
		for _, e := range expects {
			c := c
			e := e

			tc.children = append(tc.children, TrafficCall{
				name: fmt.Sprintf("%s: %s", c.Config().Cluster.StableName(), e.alpn),
				opts: echo.CallOptions{
					Port:    echo.Port{ServicePort: e.port, Protocol: protocol.HTTP},
					Count:   1,
					Address: apps.External[0].Address(),
					HTTP: echo.HTTP{
						Headers: HostHeader(apps.External[0].Config().DefaultHostHeader),
					},
					Scheme: scheme.HTTP,
					Check: check.And(
						check.OK(),
						check.Alpn(e.alpn)),
				},
				call: c.CallOrFail,
			})
		}
	}
	return []TrafficTestCase{tc}
}

// useClientProtocolCases contains tests use_client_protocol from DestinationRule
func useClientProtocolCases(apps *EchoDeployments) []TrafficTestCase {
	var cases []TrafficTestCase
	client := apps.PodA
	to := apps.PodC
	cases = append(cases,
		TrafficTestCase{
			name:   "use client protocol with h2",
			config: useClientProtocolDestinationRule(to.Config().Service),
			call:   client[0].CallOrFail,
			opts: echo.CallOptions{
				To: to,
				Port: echo.Port{
					Name: "http",
				},
				Count: 1,
				HTTP: echo.HTTP{
					HTTP2: true,
				},
				Check: check.And(
					check.OK(),
					check.Protocol("HTTP/2.0"),
				),
			},
			minIstioVersion: "1.10.0",
		},
		TrafficTestCase{
			name:   "use client protocol with h1",
			config: useClientProtocolDestinationRule(to.Config().Service),
			call:   client[0].CallOrFail,
			opts: echo.CallOptions{
				Port: echo.Port{
					Name: "http",
				},
				Count: 1,
				To:    to,
				HTTP: echo.HTTP{
					HTTP2: false,
				},
				Check: check.And(
					check.OK(),
					check.Protocol("HTTP/1.1"),
				),
			},
		},
	)
	return cases
}

// destinationRuleCases contains tests some specific DestinationRule tests.
func destinationRuleCases(apps *EchoDeployments) []TrafficTestCase {
	var cases []TrafficTestCase
	from := apps.PodA
	to := apps.PodC
	cases = append(cases,
		// Validates the config is generated correctly when only idletimeout is specified in DR.
		TrafficTestCase{
			name:   "only idletimeout specified in DR",
			config: idletimeoutDestinationRule("idletimeout-dr", to.Config().Service),
			call:   from[0].CallOrFail,
			opts: echo.CallOptions{
				To: to,
				Port: echo.Port{
					Name: "http",
				},
				Count: 1,
				HTTP: echo.HTTP{
					HTTP2: true,
				},
				Check: check.OK(),
			},
			minIstioVersion: "1.10.0",
		},
	)
	return cases
}

// trafficLoopCases contains tests to ensure traffic does not loop through the sidecar
func trafficLoopCases(apps *EchoDeployments) []TrafficTestCase {
	var cases []TrafficTestCase
	for _, c := range apps.PodA {
		for _, d := range apps.PodB {
			for _, port := range []string{"15001", "15006"} {
				c, d, port := c, d, port
				cases = append(cases, TrafficTestCase{
					name: port,
					call: func(t test.Failer, options echo.CallOptions) echoClient.Responses {
						dwl := d.WorkloadsOrFail(t)[0]
						cwl := c.WorkloadsOrFail(t)[0]
						resp, err := cwl.ForwardEcho(context.Background(), &epb.ForwardEchoRequest{
							Url:   fmt.Sprintf("http://%s:%s", dwl.Address(), port),
							Count: 1,
						})
						// Ideally we would actually check to make sure we do not blow up the pod,
						// but I couldn't find a way to reliably detect this.
						if err == nil {
							t.Fatalf("expected request to fail, but it didn't: %v", resp)
						}
						return nil
					},
				})
			}
		}
	}
	return cases
}

// autoPassthroughCases tests that we cannot hit unexpected destinations when using AUTO_PASSTHROUGH
func autoPassthroughCases(apps *EchoDeployments) []TrafficTestCase {
	var cases []TrafficTestCase
	// We test the cross product of all Istio ALPNs (or no ALPN), all mTLS modes, and various backends
	alpns := []string{"istio", "istio-peer-exchange", "istio-http/1.0", "istio-http/1.1", "istio-h2", ""}
	modes := []string{"STRICT", "PERMISSIVE", "DISABLE"}

	mtlsHost := host.Name(apps.PodA[0].Config().ClusterLocalFQDN())
	nakedHost := host.Name(apps.Naked[0].Config().ClusterLocalFQDN())
	httpsPort := ports.All().MustForName("https").ServicePort
	httpsAutoPort := ports.All().MustForName("auto-https").ServicePort
	snis := []string{
		model.BuildSubsetKey(model.TrafficDirectionOutbound, "", mtlsHost, httpsPort),
		model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, "", mtlsHost, httpsPort),
		model.BuildSubsetKey(model.TrafficDirectionOutbound, "", nakedHost, httpsPort),
		model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, "", nakedHost, httpsPort),
		model.BuildSubsetKey(model.TrafficDirectionOutbound, "", mtlsHost, httpsAutoPort),
		model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, "", mtlsHost, httpsAutoPort),
		model.BuildSubsetKey(model.TrafficDirectionOutbound, "", nakedHost, httpsAutoPort),
		model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, "", nakedHost, httpsAutoPort),
	}
	for _, mode := range modes {
		var childs []TrafficCall
		for _, sni := range snis {
			for _, alpn := range alpns {
				alpn, sni, mode := alpn, sni, mode
				al := []string{alpn}
				if alpn == "" {
					al = nil
				}
				childs = append(childs, TrafficCall{
					name: fmt.Sprintf("mode:%v,sni:%v,alpn:%v", mode, sni, alpn),
					call: apps.Ingress.CallOrFail,
					opts: echo.CallOptions{
						Port: echo.Port{
							ServicePort: 443,
							Protocol:    protocol.HTTPS,
						},
						TLS: echo.TLS{
							ServerName: sni,
							Alpn:       al,
						},
						Check:   check.Error(),
						Timeout: 5 * time.Second,
					},
				},
				)
			}
		}
		cases = append(cases, TrafficTestCase{
			config: globalPeerAuthentication(mode) + `
---
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: cross-network-gateway-test
  namespace: istio-system
spec:
  selector:
    istio: ingressgateway
  servers:
    - port:
        number: 443
        name: tls
        protocol: TLS
      tls:
        mode: AUTO_PASSTHROUGH
      hosts:
        - "*.local"
`,
			children: childs,
		})
	}

	return cases
}

func gatewayCases() []TrafficTestCase {
	templateParams := func(protocol protocol.Instance, src echo.Callers, dests echo.Instances, ciphers []string) map[string]interface{} {
		hostName, dest, portN, cred := "*", dests[0], 80, ""
		if protocol.IsTLS() {
			hostName, portN, cred = dest.Config().ClusterLocalFQDN(), 443, "cred"
		}
		return map[string]interface{}{
			"IngressNamespace":   src[0].(ingress.Instance).Namespace(),
			"GatewayHost":        hostName,
			"GatewayPort":        portN,
			"GatewayPortName":    strings.ToLower(string(protocol)),
			"GatewayProtocol":    string(protocol),
			"Gateway":            "gateway",
			"VirtualServiceHost": dest.Config().ClusterLocalFQDN(),
			"Port":               dest.PortForName("http").ServicePort,
			"Credential":         cred,
			"Ciphers":            ciphers,
		}
	}

	// clears the To to avoid echo internals trying to match the protocol with the port on echo.Config
	noTarget := func(_ echo.Caller, opts *echo.CallOptions) {
		opts.To = nil
	}
	// allows setting the target indirectly via the host header
	fqdnHostHeader := func(src echo.Caller, opts *echo.CallOptions) {
		if opts.HTTP.Headers == nil {
			opts.HTTP.Headers = make(http.Header)
		}
		opts.HTTP.Headers.Set(headers.Host, opts.To.Config().ClusterLocalFQDN())
		noTarget(src, opts)
	}

	// SingleRegualrPod is already applied leaving one regular pod, to only regular pods should leave a single workload.
	singleTarget := []match.Matcher{echotest.RegularPod}
	// the following cases don't actually target workloads, we use the singleTarget filter to avoid duplicate cases
	cases := []TrafficTestCase{
		{
			name:             "404",
			targetMatchers:   singleTarget,
			workloadAgnostic: true,
			viaIngress:       true,
			config:           httpGateway("*"),
			opts: echo.CallOptions{
				Count: 1,
				Port: echo.Port{
					Protocol: protocol.HTTP,
				},
				HTTP: echo.HTTP{
					Headers: headers.New().WithHost("foo.bar").Build(),
				},
				Check: check.Status(http.StatusNotFound),
			},
			setupOpts: noTarget,
		},
		{
			name:             "https redirect",
			targetMatchers:   singleTarget,
			workloadAgnostic: true,
			viaIngress:       true,
			config: `apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: gateway
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
    tls:
      httpsRedirect: true
---
`,
			opts: echo.CallOptions{
				Count: 1,
				Port: echo.Port{
					Protocol: protocol.HTTP,
				},
				Check: check.Status(http.StatusMovedPermanently),
			},
			setupOpts: fqdnHostHeader,
		},
		{
			// See https://github.com/istio/istio/issues/27315
			name:             "https with x-forwarded-proto",
			targetMatchers:   singleTarget,
			workloadAgnostic: true,
			viaIngress:       true,
			config: `apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: gateway
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
    tls:
      httpsRedirect: true
---
apiVersion: networking.istio.io/v1alpha3
kind: EnvoyFilter
metadata:
  name: ingressgateway-redirect-config
  namespace: istio-system
spec:
  configPatches:
  - applyTo: NETWORK_FILTER
    match:
      context: GATEWAY
      listener:
        filterChain:
          filter:
            name: envoy.filters.network.http_connection_manager
    patch:
      operation: MERGE
      value:
        typed_config:
          '@type': type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager
          xff_num_trusted_hops: 1
          normalize_path: true
  workloadSelector:
    labels:
      istio: ingressgateway
---
` + httpVirtualServiceTmpl,
			opts: echo.CallOptions{
				Count: 1,
				Port: echo.Port{
					Protocol: protocol.HTTP,
				},
				HTTP: echo.HTTP{
					// In real world, this may be set by a downstream LB that terminates the TLS
					Headers: headers.New().With(headers.XForwardedProto, "https").Build(),
				},
				Check: check.OK(),
			},
			setupOpts: fqdnHostHeader,
			templateVars: func(_ echo.Callers, dests echo.Instances) map[string]interface{} {
				dest := dests[0]
				return map[string]interface{}{
					"Gateway":            "gateway",
					"VirtualServiceHost": dest.Config().ClusterLocalFQDN(),
					"Port":               dest.PortForName("http").ServicePort,
				}
			},
		},
		{
			name: "cipher suite",
			config: gatewayTmpl + httpVirtualServiceTmpl +
				ingressutil.IngressKubeSecretYAML("cred", "{{.IngressNamespace}}", ingressutil.TLS, ingressutil.IngressCredentialA),
			templateVars: func(src echo.Callers, dests echo.Instances) map[string]interface{} {
				// Test all cipher suites, including a fake one. Envoy should accept all of the ones on the "valid" list,
				// and control plane should filter our invalid one.
				return templateParams(protocol.HTTPS, src, dests, append(security.ValidCipherSuites.SortedList(), "fake"))
			},
			setupOpts: fqdnHostHeader,
			opts: echo.CallOptions{
				Count: 1,
				Port: echo.Port{
					Protocol: protocol.HTTPS,
				},
			},
			viaIngress:       true,
			workloadAgnostic: true,
		},
		{
			// See https://github.com/istio/istio/issues/34609
			name:             "http redirect when vs port specify https",
			targetMatchers:   singleTarget,
			workloadAgnostic: true,
			viaIngress:       true,
			config: `apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: gateway
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
    tls:
      httpsRedirect: true
---
` + httpVirtualServiceTmpl,
			opts: echo.CallOptions{
				Count: 1,
				Port: echo.Port{
					Protocol: protocol.HTTP,
				},
				Check: check.Status(http.StatusMovedPermanently),
			},
			setupOpts: fqdnHostHeader,
			templateVars: func(_ echo.Callers, dests echo.Instances) map[string]interface{} {
				dest := dests[0]
				return map[string]interface{}{
					"Gateway":            "gateway",
					"VirtualServiceHost": dest.Config().ClusterLocalFQDN(),
					"Port":               443,
				}
			},
		},
		{
			// See https://github.com/istio/istio/issues/27315
			// See https://github.com/istio/istio/issues/34609
			name:             "http return 400 with with x-forwarded-proto https when vs port specify https",
			targetMatchers:   singleTarget,
			workloadAgnostic: true,
			viaIngress:       true,
			config: `apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: gateway
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
    tls:
      httpsRedirect: true
---
apiVersion: networking.istio.io/v1alpha3
kind: EnvoyFilter
metadata:
  name: ingressgateway-redirect-config
  namespace: istio-system
spec:
  configPatches:
  - applyTo: NETWORK_FILTER
    match:
      context: GATEWAY
      listener:
        filterChain:
          filter:
            name: envoy.filters.network.http_connection_manager
    patch:
      operation: MERGE
      value:
        typed_config:
          '@type': type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager
          xff_num_trusted_hops: 1
          normalize_path: true
  workloadSelector:
    labels:
      istio: ingressgateway
---
` + httpVirtualServiceTmpl,
			opts: echo.CallOptions{
				Count: 1,
				Port: echo.Port{
					Protocol: protocol.HTTP,
				},
				HTTP: echo.HTTP{
					// In real world, this may be set by a downstream LB that terminates the TLS
					Headers: headers.New().With(headers.XForwardedProto, "https").Build(),
				},
				Check: check.Status(http.StatusBadRequest),
			},
			setupOpts: fqdnHostHeader,
			templateVars: func(_ echo.Callers, dests echo.Instances) map[string]interface{} {
				dest := dests[0]
				return map[string]interface{}{
					"Gateway":            "gateway",
					"VirtualServiceHost": dest.Config().ClusterLocalFQDN(),
					"Port":               443,
				}
			},
		},
		{
			// https://github.com/istio/istio/issues/37196
			name:             "client protocol - http1",
			targetMatchers:   singleTarget,
			workloadAgnostic: true,
			viaIngress:       true,
			config: `apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: gateway
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
---
` + httpVirtualServiceTmpl,
			opts: echo.CallOptions{
				Count: 1,
				Port: echo.Port{
					Protocol: protocol.HTTP,
				},
				Check: check.And(
					check.OK(),
					check.Protocol("HTTP/1.1")),
			},
			setupOpts: fqdnHostHeader,
			templateVars: func(_ echo.Callers, dests echo.Instances) map[string]interface{} {
				dest := dests[0]
				return map[string]interface{}{
					"Gateway":            "gateway",
					"VirtualServiceHost": dest.Config().ClusterLocalFQDN(),
					"Port":               ports.All().MustForName("auto-http").ServicePort,
				}
			},
		},
		{
			// https://github.com/istio/istio/issues/37196
			name:             "client protocol - http2",
			targetMatchers:   singleTarget,
			workloadAgnostic: true,
			viaIngress:       true,
			config: `apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: gateway
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
---
` + httpVirtualServiceTmpl,
			opts: echo.CallOptions{
				HTTP: echo.HTTP{
					HTTP2: true,
				},
				Count: 1,
				Port: echo.Port{
					Protocol: protocol.HTTP,
				},
				Check: check.And(
					check.OK(),
					// Gateway doesn't implicitly use downstream
					check.Protocol("HTTP/1.1"),
					// Regression test; if this is set it means the inbound sidecar is treating it as TCP
					check.RequestHeader("X-Envoy-Peer-Metadata", "")),
			},
			setupOpts: fqdnHostHeader,
			templateVars: func(_ echo.Callers, dests echo.Instances) map[string]interface{} {
				dest := dests[0]
				return map[string]interface{}{
					"Gateway":            "gateway",
					"VirtualServiceHost": dest.Config().ClusterLocalFQDN(),
					"Port":               ports.All().MustForName("auto-http").ServicePort,
				}
			},
		},
	}
	for _, port := range []string{"auto-http", "http", "http2"} {
		for _, h2 := range []bool{true, false} {
			port, h2 := port, h2
			protoName := "http1"
			expectedProto := "HTTP/1.1"
			if h2 {
				protoName = "http2"
				expectedProto = "HTTP/2.0"
			}

			cases = append(cases,
				TrafficTestCase{
					// https://github.com/istio/istio/issues/37196
					name:             fmt.Sprintf("client protocol - %v use client with %v", protoName, port),
					targetMatchers:   singleTarget,
					workloadAgnostic: true,
					viaIngress:       true,
					config: `apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: gateway
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
---
` + httpVirtualServiceTmpl + useClientProtocolDestinationRuleTmpl,
					opts: echo.CallOptions{
						HTTP: echo.HTTP{
							HTTP2: h2,
						},
						Count: 1,
						Port: echo.Port{
							Protocol: protocol.HTTP,
						},
						Check: check.And(
							check.OK(),
							// We did configure to use client protocol
							check.Protocol(expectedProto),
							// Regression test; if this is set it means the inbound sidecar is treating it as TCP
							check.RequestHeader("X-Envoy-Peer-Metadata", "")),
					},
					setupOpts: fqdnHostHeader,
					templateVars: func(_ echo.Callers, dests echo.Instances) map[string]interface{} {
						dest := dests[0]
						return map[string]interface{}{
							"Gateway":            "gateway",
							"VirtualServiceHost": dest.Config().ClusterLocalFQDN(),
							"Port":               ports.All().MustForName(port).ServicePort,
						}
					},
				})
		}
	}

	for _, proto := range []protocol.Instance{protocol.HTTP, protocol.HTTPS} {
		proto, secret := proto, ""
		if proto.IsTLS() {
			secret = ingressutil.IngressKubeSecretYAML("cred", "{{.IngressNamespace}}", ingressutil.TLS, ingressutil.IngressCredentialA)
		}
		cases = append(
			cases,
			TrafficTestCase{
				name:   string(proto),
				config: gatewayTmpl + httpVirtualServiceTmpl + secret,
				templateVars: func(src echo.Callers, dests echo.Instances) map[string]interface{} {
					return templateParams(proto, src, dests, nil)
				},
				setupOpts: fqdnHostHeader,
				opts: echo.CallOptions{
					Count: 1,
					Port: echo.Port{
						Protocol: proto,
					},
				},
				viaIngress:       true,
				workloadAgnostic: true,
			},
			TrafficTestCase{
				name:   fmt.Sprintf("%s scheme match", proto),
				config: gatewayTmpl + httpVirtualServiceTmpl + secret,
				templateVars: func(src echo.Callers, dests echo.Instances) map[string]interface{} {
					params := templateParams(proto, src, dests, nil)
					params["MatchScheme"] = strings.ToLower(string(proto))
					return params
				},
				setupOpts: fqdnHostHeader,
				opts: echo.CallOptions{
					Count: 1,
					Port: echo.Port{
						Protocol: proto,
					},
					Check: check.And(
						check.OK(),
						check.RequestHeader("Istio-Custom-Header", "user-defined-value")),
				},
				// to keep tests fast, we only run the basic protocol test per-workload and scheme match once (per cluster)
				targetMatchers:   singleTarget,
				viaIngress:       true,
				workloadAgnostic: true,
			},
		)
	}

	return cases
}

func XFFGatewayCase(apps *EchoDeployments, gateway string) []TrafficTestCase {
	var cases []TrafficTestCase

	destinationSets := []echo.Instances{
		apps.PodA,
	}

	for _, d := range destinationSets {
		d := d
		if len(d) == 0 {
			continue
		}
		fqdn := d[0].Config().ClusterLocalFQDN()
		cases = append(cases, TrafficTestCase{
			name:   d[0].Config().Service,
			config: httpGateway("*") + httpVirtualService("gateway", fqdn, d[0].PortForName("http").ServicePort),
			call:   apps.Naked[0].CallOrFail,
			opts: echo.CallOptions{
				Count:   1,
				Port:    echo.Port{ServicePort: 80},
				Scheme:  scheme.HTTP,
				Address: gateway,
				HTTP: echo.HTTP{
					Headers: headers.New().
						WithHost(fqdn).
						With(headers.XForwardedFor, "56.5.6.7, 72.9.5.6, 98.1.2.3").
						Build(),
				},
				Check: check.Each(
					func(r echoClient.Response) error {
						externalAddress, ok := r.RequestHeaders["X-Envoy-External-Address"]
						if !ok {
							return fmt.Errorf("missing X-Envoy-External-Address Header")
						}
						if err := ExpectString(externalAddress[0], "72.9.5.6", "envoy-external-address header"); err != nil {
							return err
						}
						xffHeader, ok := r.RequestHeaders["X-Forwarded-For"]
						if !ok {
							return fmt.Errorf("missing X-Forwarded-For Header")
						}

						xffIPs := strings.Split(xffHeader[0], ",")
						if len(xffIPs) != 4 {
							return fmt.Errorf("did not receive expected 4 hosts in X-Forwarded-For header")
						}

						return ExpectString(strings.TrimSpace(xffIPs[1]), "72.9.5.6", "ip in xff header")
					}),
			},
		})
	}
	return cases
}

func envoyFilterCases(apps *EchoDeployments) []TrafficTestCase {
	var cases []TrafficTestCase
	// Test adding envoyfilter to inbound and outbound route/cluster/listeners
	cfg := `
apiVersion: networking.istio.io/v1alpha3
kind: EnvoyFilter
metadata:
  name: outbound
spec:
  workloadSelector:
    labels:
      app: a
  configPatches:
  - applyTo: HTTP_FILTER
    match:
      context: SIDECAR_OUTBOUND
      listener:
        filterChain:
          filter:
            name: "envoy.filters.network.http_connection_manager"
            subFilter:
              name: "envoy.filters.http.router"
    patch:
      operation: INSERT_BEFORE
      value:
       name: envoy.lua
       typed_config:
          "@type": "type.googleapis.com/envoy.extensions.filters.http.lua.v3.Lua"
          inlineCode: |
            function envoy_on_request(request_handle)
              request_handle:headers():add("x-lua-outbound", "hello world")
            end
  - applyTo: VIRTUAL_HOST
    match:
      context: SIDECAR_OUTBOUND
    patch:
      operation: MERGE
      value:
        request_headers_to_add:
        - header:
            key: x-vhost-outbound
            value: "hello world"
  - applyTo: CLUSTER
    match:
      context: SIDECAR_OUTBOUND
      cluster: {}
    patch:
      operation: MERGE
      value:
        http2_protocol_options: {}
---
apiVersion: networking.istio.io/v1alpha3
kind: EnvoyFilter
metadata:
  name: inbound
spec:
  workloadSelector:
    labels:
      app: b
  configPatches:
  - applyTo: HTTP_FILTER
    match:
      context: SIDECAR_INBOUND
      listener:
        filterChain:
          filter:
            name: "envoy.filters.network.http_connection_manager"
            subFilter:
              name: "envoy.filters.http.router"
    patch:
      operation: INSERT_BEFORE
      value:
       name: envoy.lua
       typed_config:
          "@type": "type.googleapis.com/envoy.extensions.filters.http.lua.v3.Lua"
          inlineCode: |
            function envoy_on_request(request_handle)
              request_handle:headers():add("x-lua-inbound", "hello world")
            end
  - applyTo: VIRTUAL_HOST
    match:
      context: SIDECAR_INBOUND
    patch:
      operation: MERGE
      value:
        request_headers_to_add:
        - header:
            key: x-vhost-inbound
            value: "hello world"
  - applyTo: CLUSTER
    match:
      context: SIDECAR_INBOUND
      cluster: {}
    patch:
      operation: MERGE
      value:
        http2_protocol_options: {}
`
	for _, c := range apps.PodA {
		cases = append(cases, TrafficTestCase{
			config: cfg,
			call:   c.CallOrFail,
			opts: echo.CallOptions{
				To: apps.PodB,
				Port: echo.Port{
					Name: "http",
				},
				Check: check.And(
					check.OK(),
					check.Protocol("HTTP/2.0"),
					check.RequestHeaders(map[string]string{
						"X-Vhost-Inbound":  "hello world",
						"X-Vhost-Outbound": "hello world",
						"X-Lua-Inbound":    "hello world",
						"X-Lua-Outbound":   "hello world",
					}),
				),
			},
		})
	}
	return cases
}

// hostCases tests different forms of host header to use
func hostCases(apps *EchoDeployments) ([]TrafficTestCase, error) {
	var cases []TrafficTestCase
	for _, c := range apps.PodA {
		cfg := apps.Headless[0].Config()
		port := ports.All().MustForName("auto-http").WorkloadPort
		wl, err := apps.Headless[0].Workloads()
		if err != nil {
			return nil, err
		}
		if len(wl) == 0 {
			return nil, fmt.Errorf("no workloads found")
		}
		address := wl[0].Address()
		hosts := []string{
			cfg.ClusterLocalFQDN(),
			fmt.Sprintf("%s:%d", cfg.ClusterLocalFQDN(), port),
			fmt.Sprintf("%s.%s.svc", cfg.Service, cfg.Namespace.Name()),
			fmt.Sprintf("%s.%s.svc:%d", cfg.Service, cfg.Namespace.Name(), port),
			cfg.Service,
			fmt.Sprintf("%s:%d", cfg.Service, port),
			fmt.Sprintf("some-instances.%s:%d", cfg.ClusterLocalFQDN(), port),
			fmt.Sprintf("some-instances.%s.%s.svc", cfg.Service, cfg.Namespace.Name()),
			fmt.Sprintf("some-instances.%s.%s.svc:%d", cfg.Service, cfg.Namespace.Name(), port),
			fmt.Sprintf("some-instances.%s", cfg.Service),
			fmt.Sprintf("some-instances.%s:%d", cfg.Service, port),
			address,
			fmt.Sprintf("%s:%d", address, port),
		}
		for _, h := range hosts {
			name := strings.Replace(h, address, "ip", -1) + "/auto-http"
			cases = append(cases, TrafficTestCase{
				name: name,
				call: c.CallOrFail,
				opts: echo.CallOptions{
					To: apps.Headless,
					Port: echo.Port{
						Name: "auto-http",
					},
					HTTP: echo.HTTP{
						Headers: HostHeader(h),
					},
					Check: check.OK(),
				},
			})
		}
		port = ports.All().MustForName("http").WorkloadPort
		hosts = []string{
			cfg.ClusterLocalFQDN(),
			fmt.Sprintf("%s:%d", cfg.ClusterLocalFQDN(), port),
			fmt.Sprintf("%s.%s.svc", cfg.Service, cfg.Namespace.Name()),
			fmt.Sprintf("%s.%s.svc:%d", cfg.Service, cfg.Namespace.Name(), port),
			cfg.Service,
			fmt.Sprintf("%s:%d", cfg.Service, port),
			fmt.Sprintf("some-instances.%s:%d", cfg.ClusterLocalFQDN(), port),
			fmt.Sprintf("some-instances.%s.%s.svc", cfg.Service, cfg.Namespace.Name()),
			fmt.Sprintf("some-instances.%s.%s.svc:%d", cfg.Service, cfg.Namespace.Name(), port),
			fmt.Sprintf("some-instances.%s", cfg.Service),
			fmt.Sprintf("some-instances.%s:%d", cfg.Service, port),
			address,
			fmt.Sprintf("%s:%d", address, port),
		}
		for _, h := range hosts {
			name := strings.Replace(h, address, "ip", -1) + "/http"
			cases = append(cases, TrafficTestCase{
				name: name,
				call: c.CallOrFail,
				opts: echo.CallOptions{
					To: apps.Headless,
					Port: echo.Port{
						Name: "http",
					},
					HTTP: echo.HTTP{
						Headers: HostHeader(h),
					},
					Check: check.OK(),
				},
			})
		}
	}
	return cases, nil
}

// serviceCases tests overlapping Services. There are a few cases.
// Consider we have our base service B, with service port P and target port T
// 1) Another service, B', with P -> T. In this case, both the listener and the cluster will conflict.
//    Because everything is workload oriented, this is not a problem unless they try to make them different
//    protocols (this is explicitly called out as "not supported") or control inbound connectionPool settings
//    (which is moving to Sidecar soon)
// 2) Another service, B', with P -> T'. In this case, the listener will be distinct, since its based on the target.
//    The cluster, however, will be shared, which is broken, because we should be forwarding to T when we call B, and T' when we call B'.
// 3) Another service, B', with P' -> T. In this case, the listener is shared. This is fine, with the exception of different protocols
//    The cluster is distinct.
// 4) Another service, B', with P' -> T'. There is no conflicts here at all.
func serviceCases(apps *EchoDeployments) []TrafficTestCase {
	var cases []TrafficTestCase
	for _, c := range apps.PodA {
		c := c

		// Case 1
		// Identical to port "http" or service B, just behind another service name
		svc := fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: b-alt-1
  labels:
    app: b
spec:
  ports:
  - name: http
    port: %d
    targetPort: %d
  selector:
    app: b`, ports.All().MustForName("http").ServicePort, ports.All().MustForName("http").WorkloadPort)
		cases = append(cases, TrafficTestCase{
			name:   fmt.Sprintf("case 1 both match in cluster %v", c.Config().Cluster.StableName()),
			config: svc,
			call:   c.CallOrFail,
			opts: echo.CallOptions{
				Count:   1,
				Address: "b-alt-1",
				Port:    echo.Port{ServicePort: ports.All().MustForName("http").ServicePort, Protocol: protocol.HTTP},
				Timeout: time.Millisecond * 100,
				Check:   check.OK(),
			},
		})

		// Case 2
		// We match the service port, but forward to a different port
		// Here we make the new target tcp so the test would fail if it went to the http port
		svc = fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: b-alt-2
  labels:
    app: b
spec:
  ports:
  - name: tcp
    port: %d
    targetPort: %d
  selector:
    app: b`, ports.All().MustForName("http").ServicePort, ports.All().GetWorkloadOnlyPorts()[0].WorkloadPort)
		cases = append(cases, TrafficTestCase{
			name:   fmt.Sprintf("case 2 service port match in cluster %v", c.Config().Cluster.StableName()),
			config: svc,
			call:   c.CallOrFail,
			opts: echo.CallOptions{
				Count:   1,
				Address: "b-alt-2",
				Port:    echo.Port{ServicePort: ports.All().MustForName("http").ServicePort, Protocol: protocol.TCP},
				Scheme:  scheme.TCP,
				Timeout: time.Millisecond * 100,
				Check:   check.OK(),
			},
		})

		// Case 3
		// We match the target port, but front with a different service port
		svc = fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: b-alt-3
  labels:
    app: b
spec:
  ports:
  - name: http
    port: 12345
    targetPort: %d
  selector:
    app: b`, ports.All().MustForName("http").WorkloadPort)
		cases = append(cases, TrafficTestCase{
			name:   fmt.Sprintf("case 3 target port match in cluster %v", c.Config().Cluster.StableName()),
			config: svc,
			call:   c.CallOrFail,
			opts: echo.CallOptions{
				Count:   1,
				Address: "b-alt-3",
				Port:    echo.Port{ServicePort: 12345, Protocol: protocol.HTTP},
				Timeout: time.Millisecond * 100,
				Check:   check.OK(),
			},
		})

		// Case 4
		// Completely new set of ports
		svc = fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: b-alt-4
  labels:
    app: b
spec:
  ports:
  - name: http
    port: 12346
    targetPort: %d
  selector:
    app: b`, ports.All().GetWorkloadOnlyPorts()[1].WorkloadPort)
		cases = append(cases, TrafficTestCase{
			name:   fmt.Sprintf("case 4 no match in cluster %v", c.Config().Cluster.StableName()),
			config: svc,
			call:   c.CallOrFail,
			opts: echo.CallOptions{
				Count:   1,
				Address: "b-alt-4",
				Port:    echo.Port{ServicePort: 12346, Protocol: protocol.HTTP},
				Timeout: time.Millisecond * 100,
				Check:   check.OK(),
			},
		})
	}

	return cases
}

// consistentHashCases tests destination rule's consistent hashing mechanism
func consistentHashCases(apps *EchoDeployments) []TrafficTestCase {
	var cases []TrafficTestCase
	for _, app := range []echo.Instances{apps.PodA, apps.PodB} {
		app := app
		for _, c := range app {
			c := c

			// First setup a service selecting a few services. This is needed to ensure we can load balance across many pods.
			svcName := "consistent-hash"
			if nw := c.Config().Cluster.NetworkName(); nw != "" {
				svcName += "-" + nw
			}
			svc := tmpl.MustEvaluate(`apiVersion: v1
kind: Service
metadata:
  name: {{.Service}}
spec:
  ports:
  - name: http
    port: {{.Port}}
    targetPort: {{.TargetPort}}
  - name: tcp
    port: {{.TcpPort}}
    targetPort: {{.TcpTargetPort}}
  selector:
    test.istio.io/class: standard
    {{- if .Network }}
    topology.istio.io/network: {{.Network}}
	{{- end }}
`, map[string]interface{}{
				"Service":        svcName,
				"Network":        c.Config().Cluster.NetworkName(),
				"Port":           ports.All().MustForName("http").ServicePort,
				"TargetPort":     ports.All().MustForName("http").WorkloadPort,
				"TcpPort":        ports.All().MustForName("tcp").ServicePort,
				"TcpTargetPort":  ports.All().MustForName("tcp").WorkloadPort,
				"GrpcPort":       ports.All().MustForName("grpc").ServicePort,
				"GrpcTargetPort": ports.All().MustForName("grpc").WorkloadPort,
			})

			destRule := fmt.Sprintf(`
---
apiVersion: networking.istio.io/v1beta1
kind: DestinationRule
metadata:
  name: %s
spec:
  host: %s
  trafficPolicy:
    loadBalancer:
      consistentHash:
        {{. | indent 8}}
`, svcName, svcName)
			// Add a negative test case. This ensures that the test is actually valid; its not a super trivial check
			// and could be broken by having only 1 pod so its good to have this check in place
			cases = append(cases, TrafficTestCase{
				name:   "no consistent",
				config: svc,
				call:   c.CallOrFail,
				opts: echo.CallOptions{
					Count:   10,
					Address: svcName,
					Port:    echo.Port{ServicePort: ports.All().MustForName("http").ServicePort, Protocol: protocol.HTTP},
					Check: check.And(
						check.OK(),
						func(responses echoClient.Responses, rerr error) error {
							err := ConsistentHostChecker.Check(responses, rerr)
							if err == nil {
								return fmt.Errorf("expected inconsistent hash, but it was consistent")
							}
							return nil
						},
					),
				},
			})
			callOpts := echo.CallOptions{
				Count:   10,
				Address: svcName,
				HTTP: echo.HTTP{
					Path:    "/?some-query-param=bar",
					Headers: headers.New().With("x-some-header", "baz").Build(),
				},
				Port: echo.Port{ServicePort: ports.All().MustForName("http").ServicePort, Protocol: protocol.HTTP},
				Check: check.And(
					check.OK(),
					ConsistentHostChecker,
				),
			}
			tcpCallopts := echo.CallOptions{
				Count:   10,
				Address: svcName,
				Port:    echo.Port{ServicePort: ports.All().MustForName("tcp").ServicePort, Protocol: protocol.TCP},
				Check: check.And(
					check.OK(),
					ConsistentHostChecker,
				),
			}
			if c.Config().WorkloadClass() == echo.Proxyless {
				callOpts.Port = echo.Port{ServicePort: ports.All().MustForName("grpc").ServicePort, Protocol: protocol.GRPC}
			}
			// Setup tests for various forms of the API
			// TODO: it may be necessary to vary the inputs of the hash and ensure we get a different backend
			// But its pretty hard to test that, so for now just ensure we hit the same one.
			cases = append(cases, TrafficTestCase{
				name:   "source ip",
				config: svc + tmpl.MustEvaluate(destRule, "useSourceIp: true"),
				call:   c.CallOrFail,
				opts:   callOpts,
			}, TrafficTestCase{
				name:   "query param",
				config: svc + tmpl.MustEvaluate(destRule, "httpQueryParameterName: some-query-param"),
				call:   c.CallOrFail,
				opts:   callOpts,
			}, TrafficTestCase{
				name:   "http header",
				config: svc + tmpl.MustEvaluate(destRule, "httpHeaderName: x-some-header"),
				call:   c.CallOrFail,
				opts:   callOpts,
			}, TrafficTestCase{
				name:   "source ip",
				config: svc + tmpl.MustEvaluate(destRule, "useSourceIp: true"),
				call:   c.CallOrFail,
				opts:   tcpCallopts,
				skip: skip{
					skip:   c.Config().WorkloadClass() == echo.Proxyless,
					reason: "", // TODO: is this a bug or WAI?
				},
			})
		}
	}

	return cases
}

var ConsistentHostChecker check.Checker = func(responses echoClient.Responses, _ error) error {
	hostnames := make([]string, len(responses))
	for i, r := range responses {
		hostnames[i] = r.Hostname
	}
	scopes.Framework.Infof("requests landed on hostnames: %v", hostnames)
	unique := sets.NewSet(hostnames...).SortedList()
	if len(unique) != 1 {
		return fmt.Errorf("excepted only one destination, got: %v", unique)
	}
	return nil
}

func flatten(clients ...[]echo.Instance) []echo.Instance {
	var instances []echo.Instance
	for _, c := range clients {
		instances = append(instances, c...)
	}
	return instances
}

// selfCallsCases checks that pods can call themselves
func selfCallsCases() []TrafficTestCase {
	cases := []TrafficTestCase{
		// Calls to the Service will go through envoy outbound and inbound, so we get envoy headers added
		{
			name:             "to service",
			workloadAgnostic: true,
			opts: echo.CallOptions{
				Count: 1,
				Port: echo.Port{
					Name: "http",
				},
				Check: check.And(
					check.OK(),
					check.RequestHeader("X-Envoy-Attempt-Count", "1")),
			},
		},
		// Localhost calls will go directly to localhost, bypassing Envoy. No envoy headers added.
		{
			name:             "to localhost",
			workloadAgnostic: true,
			setupOpts: func(_ echo.Caller, opts *echo.CallOptions) {
				// the framework will try to set this when enumerating test cases
				opts.To = nil
			},
			opts: echo.CallOptions{
				Count:   1,
				Address: "localhost",
				Port:    echo.Port{ServicePort: 8080},
				Scheme:  scheme.HTTP,
				Check: check.And(
					check.OK(),
					check.RequestHeader("X-Envoy-Attempt-Count", "")),
			},
		},
		// PodIP calls will go directly to podIP, bypassing Envoy. No envoy headers added.
		{
			name:             "to podIP",
			workloadAgnostic: true,
			setupOpts: func(srcCaller echo.Caller, opts *echo.CallOptions) {
				src := srcCaller.(echo.Instance)
				workloads, _ := src.Workloads()
				opts.Address = workloads[0].Address()
				// the framework will try to set this when enumerating test cases
				opts.To = nil
			},
			opts: echo.CallOptions{
				Count:  1,
				Scheme: scheme.HTTP,
				Port:   echo.Port{ServicePort: 8080},
				Check: check.And(
					check.OK(),
					check.RequestHeader("X-Envoy-Attempt-Count", "")),
			},
		},
	}
	for i, tc := range cases {
		// proxyless doesn't get valuable coverage here
		tc.sourceMatchers = []match.Matcher{
			match.IsNotExternal,
			match.IsNotNaked,
			match.IsNotHeadless,
			match.IsNotProxylessGRPC,
		}
		tc.comboFilters = []echotest.CombinationFilter{func(from echo.Instance, to echo.Instances) echo.Instances {
			return match.FQDN(from.Config().ClusterLocalFQDN()).GetMatches(to)
		}}
		cases[i] = tc
	}

	return cases
}

// TODO: merge with security TestReachability code
func protocolSniffingCases(apps *EchoDeployments) []TrafficTestCase {
	var cases []TrafficTestCase

	type protocolCase struct {
		// The port we call
		port string
		// The actual type of traffic we send to the port
		scheme scheme.Instance
	}
	protocols := []protocolCase{
		{"http", scheme.HTTP},
		{"auto-http", scheme.HTTP},
		{"tcp", scheme.TCP},
		{"auto-tcp", scheme.TCP},
		{"grpc", scheme.GRPC},
		{"auto-grpc", scheme.GRPC},
	}

	// so we can check all clusters are hit
	for _, call := range protocols {
		call := call
		cases = append(cases, TrafficTestCase{
			skip: skip{
				skip:   call.scheme == scheme.TCP,
				reason: "https://github.com/istio/istio/issues/26798: enable sniffing tcp",
			},
			name: call.port,
			opts: echo.CallOptions{
				Count: 1,
				Port: echo.Port{
					Name: call.port,
				},
				Scheme:  call.scheme,
				Timeout: time.Second * 5,
			},
			check: func(src echo.Caller, opts *echo.CallOptions) check.Checker {
				if call.scheme == scheme.TCP || src.(echo.Instance).Config().IsProxylessGRPC() {
					// no host header for TCP
					// TODO understand why proxyless adds the port to :authority md
					return check.OK()
				}
				return check.And(
					check.OK(),
					check.Host(opts.GetHost()))
			},
			comboFilters: func() []echotest.CombinationFilter {
				if call.scheme != scheme.GRPC {
					return []echotest.CombinationFilter{func(from echo.Instance, to echo.Instances) echo.Instances {
						if from.Config().IsProxylessGRPC() && match.IsVM.Any(to) {
							return nil
						}
						return to
					}}
				}
				return nil
			}(),
			workloadAgnostic: true,
		})
	}

	autoPort := ports.All().MustForName("auto-http")
	httpPort := ports.All().MustForName("http")
	// Tests for http1.0. Golang does not support 1.0 client requests at all
	// To simulate these, we use TCP and hand-craft the requests.
	cases = append(cases, TrafficTestCase{
		name: "http10 to http",
		call: apps.PodA[0].CallOrFail,
		opts: echo.CallOptions{
			To:    apps.PodB,
			Count: 1,
			Port: echo.Port{
				Name: "http",
			},
			Scheme: scheme.TCP,
			Message: `GET / HTTP/1.0
`,
			Timeout: time.Second * 5,
			TCP: echo.TCP{
				// Explicitly declared as HTTP, so we always go through http filter which fails
				ExpectedResponse: &wrappers.StringValue{Value: `HTTP/1.1 426 Upgrade Required`},
			},
		},
	},
		TrafficTestCase{
			name: "http10 to auto",
			call: apps.PodA[0].CallOrFail,
			opts: echo.CallOptions{
				To:    apps.PodB,
				Count: 1,
				Port: echo.Port{
					Name: "auto-http",
				},
				Scheme: scheme.TCP,
				Message: `GET / HTTP/1.0
`,
				Timeout: time.Second * 5,
				TCP: echo.TCP{
					// Auto should be detected as TCP
					ExpectedResponse: &wrappers.StringValue{Value: `HTTP/1.0 200 OK`},
				},
			},
		},
		TrafficTestCase{
			name: "http10 to external",
			call: apps.PodA[0].CallOrFail,
			opts: echo.CallOptions{
				Address: apps.External[0].Address(),
				HTTP: echo.HTTP{
					Headers: HostHeader(apps.External[0].Config().DefaultHostHeader),
				},
				Port:   httpPort,
				Count:  1,
				Scheme: scheme.TCP,
				Message: `GET / HTTP/1.0
`,
				Timeout: time.Second * 5,
				TCP: echo.TCP{
					// There is no VIP so we fall back to 0.0.0.0 listener which sniffs
					ExpectedResponse: &wrappers.StringValue{Value: `HTTP/1.0 200 OK`},
				},
			},
		},
		TrafficTestCase{
			name: "http10 to external auto",
			call: apps.PodA[0].CallOrFail,
			opts: echo.CallOptions{
				Address: apps.External[0].Address(),
				HTTP: echo.HTTP{
					Headers: HostHeader(apps.External[0].Config().DefaultHostHeader),
				},
				Port:   autoPort,
				Count:  1,
				Scheme: scheme.TCP,
				Message: `GET / HTTP/1.0
`,
				Timeout: time.Second * 5,
				TCP: echo.TCP{
					// Auto should be detected as TCP
					ExpectedResponse: &wrappers.StringValue{Value: `HTTP/1.0 200 OK`},
				},
			},
		},
	)
	//check: func(src echo.Caller, dst echo.Instances, opts *echo.CallOptions) echo.Validator {
	//	if call.scheme == scheme.TCP || src.(echo.Instance).Config().IsProxylessGRPC() {
	//		// no host header for TCP
	//		// TODO understand why proxyless adds the port to :authority md
	//		return echo.ExpectOK()
	//	}
	//	return echo.And(
	//		echo.ExpectOK(),
	//		echo.ExpectHost(opts.GetHost()))
	//},
	//comboFilters: func() []echotest.CombinationFilter {
	//	if call.scheme != scheme.GRPC {
	//		return []echotest.CombinationFilter{func(from echo.Instance, to echo.Instances) echo.Instances {
	//			if from.Config().IsProxylessGRPC() && to.ContainsMatch(echo.IsVM()) {
	//				return nil
	//			}
	//			return to
	//		}}
	//	}
	//	return nil
	//}(),
	return cases
}

// Todo merge with security TestReachability code
func instanceIPTests(apps *EchoDeployments) []TrafficTestCase {
	var cases []TrafficTestCase
	ipCases := []struct {
		name            string
		endpoint        string
		disableSidecar  bool
		port            string
		code            int
		minIstioVersion string
	}{
		// instance IP bind
		{
			name:           "instance IP without sidecar",
			disableSidecar: true,
			port:           "http-instance",
			code:           http.StatusOK,
		},
		{
			name:     "instance IP with wildcard sidecar",
			endpoint: "0.0.0.0",
			port:     "http-instance",
			code:     http.StatusOK,
		},
		{
			name:     "instance IP with localhost sidecar",
			endpoint: "127.0.0.1",
			port:     "http-instance",
			code:     http.StatusServiceUnavailable,
		},
		{
			name:     "instance IP with empty sidecar",
			endpoint: "",
			port:     "http-instance",
			code:     http.StatusOK,
		},

		// Localhost bind
		{
			name:           "localhost IP without sidecar",
			disableSidecar: true,
			port:           "http-localhost",
			code:           http.StatusServiceUnavailable,
			// when testing with pre-1.10 versions this request succeeds
			minIstioVersion: "1.10.0",
		},
		{
			name:     "localhost IP with wildcard sidecar",
			endpoint: "0.0.0.0",
			port:     "http-localhost",
			code:     http.StatusServiceUnavailable,
		},
		{
			name:     "localhost IP with localhost sidecar",
			endpoint: "127.0.0.1",
			port:     "http-localhost",
			code:     http.StatusOK,
		},
		{
			name:     "localhost IP with empty sidecar",
			endpoint: "",
			port:     "http-localhost",
			code:     http.StatusServiceUnavailable,
			// when testing with pre-1.10 versions this request succeeds
			minIstioVersion: "1.10.0",
		},

		// Wildcard bind
		{
			name:           "wildcard IP without sidecar",
			disableSidecar: true,
			port:           "http",
			code:           http.StatusOK,
		},
		{
			name:     "wildcard IP with wildcard sidecar",
			endpoint: "0.0.0.0",
			port:     "http",
			code:     http.StatusOK,
		},
		{
			name:     "wildcard IP with localhost sidecar",
			endpoint: "127.0.0.1",
			port:     "http",
			code:     http.StatusOK,
		},
		{
			name:     "wildcard IP with empty sidecar",
			endpoint: "",
			port:     "http",
			code:     http.StatusOK,
		},
	}
	for _, ipCase := range ipCases {
		for _, client := range apps.PodA {
			ipCase := ipCase
			client := client
			to := apps.PodB
			var config string
			if !ipCase.disableSidecar {
				config = fmt.Sprintf(`
apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  name: sidecar
spec:
  workloadSelector:
    labels:
      app: b
  egress:
  - hosts:
    - "./*"
  ingress:
  - port:
      number: %d
      protocol: HTTP
    defaultEndpoint: %s:%d
`, ports.All().MustForName(ipCase.port).WorkloadPort, ipCase.endpoint, ports.All().MustForName(ipCase.port).WorkloadPort)
			}
			cases = append(cases,
				TrafficTestCase{
					name:   ipCase.name,
					call:   client.CallOrFail,
					config: config,
					opts: echo.CallOptions{
						Count: 1,
						To:    to,
						Port: echo.Port{
							Name: ipCase.port,
						},
						Scheme:  scheme.HTTP,
						Timeout: time.Second * 5,
						Check:   check.Status(ipCase.code),
					},
					minIstioVersion: ipCase.minIstioVersion,
				})
		}
	}

	for _, tc := range cases {
		// proxyless doesn't get valuable coverage here
		tc.sourceMatchers = append(tc.sourceMatchers, match.IsNotProxylessGRPC)
		tc.targetMatchers = append(tc.targetMatchers, match.IsNotProxylessGRPC)
	}

	return cases
}

type vmCase struct {
	name string
	from echo.Instance
	to   echo.Instances
	host string
}

func DNSTestCases(apps *EchoDeployments, cniEnabled bool) []TrafficTestCase {
	makeSE := func(ips ...string) string {
		return tmpl.MustEvaluate(`
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: dns
spec:
  hosts:
  - "fake.service.local"
  addresses:
{{ range $ip := .IPs }}
  - "{{$ip}}"
{{ end }}
  resolution: STATIC
  endpoints: []
  ports:
  - number: 80
    name: http
    protocol: HTTP
`, map[string]interface{}{"IPs": ips})
	}
	var tcases []TrafficTestCase
	ipv4 := "1.2.3.4"
	ipv6 := "1234:1234:1234::1234:1234:1234"
	dummyLocalhostServer := "127.0.0.1"
	cases := []struct {
		name string
		// TODO(https://github.com/istio/istio/issues/30282) support multiple vips
		ips      string
		protocol string
		server   string
		skipCNI  bool
		expected []string
	}{
		{
			name:     "tcp ipv4",
			ips:      ipv4,
			expected: []string{ipv4},
			protocol: "tcp",
		},
		{
			name:     "udp ipv4",
			ips:      ipv4,
			expected: []string{ipv4},
			protocol: "udp",
		},
		{
			name:     "tcp ipv6",
			ips:      ipv6,
			expected: []string{ipv6},
			protocol: "tcp",
		},
		{
			name:     "udp ipv6",
			ips:      ipv6,
			expected: []string{ipv6},
			protocol: "udp",
		},
		{
			// We should only capture traffic to servers in /etc/resolv.conf nameservers
			// This checks we do not capture traffic to other servers.
			// This is important for cases like app -> istio dns server -> dnsmasq -> upstream
			// If we captured all DNS traffic, we would loop dnsmasq traffic back to our server.
			name:     "tcp localhost server",
			ips:      ipv4,
			expected: nil,
			protocol: "tcp",
			skipCNI:  true,
			server:   dummyLocalhostServer,
		},
		{
			name:     "udp localhost server",
			ips:      ipv4,
			expected: nil,
			protocol: "udp",
			skipCNI:  true,
			server:   dummyLocalhostServer,
		},
	}
	for _, client := range flatten(apps.VM, apps.PodA, apps.PodTproxy) {
		for _, tt := range cases {
			if tt.skipCNI && cniEnabled {
				continue
			}
			tt, client := tt, client
			address := "fake.service.local?"
			if tt.protocol != "" {
				address += "&protocol=" + tt.protocol
			}
			if tt.server != "" {
				address += "&server=" + tt.server
			}
			var checker check.Checker = func(responses echoClient.Responses, _ error) error {
				for _, r := range responses {
					if !reflect.DeepEqual(r.Body(), tt.expected) {
						return fmt.Errorf("unexpected dns response: wanted %v, got %v", tt.expected, r.Body())
					}
				}
				return nil
			}
			if tt.expected == nil {
				checker = check.Error()
			}
			tcases = append(tcases, TrafficTestCase{
				name:   fmt.Sprintf("%s/%s", client.Config().Service, tt.name),
				config: makeSE(tt.ips),
				call:   client.CallOrFail,
				opts: echo.CallOptions{
					Scheme:  scheme.DNS,
					Count:   1,
					Address: address,
					Check:   checker,
				},
			})
		}
	}
	svcCases := []struct {
		name     string
		protocol string
		server   string
	}{
		{
			name:     "tcp",
			protocol: "tcp",
		},
		{
			name:     "udp",
			protocol: "udp",
		},
	}
	for _, client := range flatten(apps.VM, apps.PodA, apps.PodTproxy) {
		for _, tt := range svcCases {
			tt, client := tt, client
			aInCluster := match.InCluster(client.Config().Cluster).GetMatches(apps.PodA)
			if len(aInCluster) == 0 {
				// The cluster doesn't contain A, but connects to a cluster containing A
				aInCluster = match.InCluster(client.Config().Cluster.Config()).GetMatches(apps.PodA)
			}
			address := aInCluster[0].Config().ClusterLocalFQDN() + "?"
			if tt.protocol != "" {
				address += "&protocol=" + tt.protocol
			}
			if tt.server != "" {
				address += "&server=" + tt.server
			}
			expected := aInCluster[0].Address()
			tcases = append(tcases, TrafficTestCase{
				name: fmt.Sprintf("svc/%s/%s", client.Config().Service, tt.name),
				call: client.CallOrFail,
				opts: echo.CallOptions{
					Count:   1,
					Scheme:  scheme.DNS,
					Address: address,
					Check: func(responses echoClient.Responses, _ error) error {
						for _, r := range responses {
							ips := r.Body()
							sort.Strings(ips)
							exp := []string{expected}
							if !reflect.DeepEqual(ips, exp) {
								return fmt.Errorf("unexpected dns response: wanted %v, got %v", exp, ips)
							}
						}
						return nil
					},
				},
			})
		}
	}
	return tcases
}

func VMTestCases(vms echo.Instances, apps *EchoDeployments) []TrafficTestCase {
	var testCases []vmCase

	for _, vm := range vms {
		testCases = append(testCases,
			vmCase{
				name: "dns: VM to k8s cluster IP service name.namespace host",
				from: vm,
				to:   apps.PodA,
				host: PodASvc + "." + apps.Namespace.Name(),
			},
			vmCase{
				name: "dns: VM to k8s cluster IP service fqdn host",
				from: vm,
				to:   apps.PodA,
				host: apps.PodA[0].Config().ClusterLocalFQDN(),
			},
			vmCase{
				name: "dns: VM to k8s cluster IP service short name host",
				from: vm,
				to:   apps.PodA,
				host: PodASvc,
			},
			vmCase{
				name: "dns: VM to k8s headless service",
				from: vm,
				to:   match.InCluster(vm.Config().Cluster.Config()).GetMatches(apps.Headless),
				host: apps.Headless[0].Config().ClusterLocalFQDN(),
			},
			vmCase{
				name: "dns: VM to k8s statefulset service",
				from: vm,
				to:   match.InCluster(vm.Config().Cluster.Config()).GetMatches(apps.StatefulSet),
				host: apps.StatefulSet[0].Config().ClusterLocalFQDN(),
			},
			// TODO(https://github.com/istio/istio/issues/32552) re-enable
			//vmCase{
			//	name: "dns: VM to k8s statefulset instance.service",
			//	from: vm,
			//	to:   apps.StatefulSet.Match(echo.InCluster(vm.Config().Cluster.Config())),
			//	host: fmt.Sprintf("%s-v1-0.%s", StatefulSetSvc, StatefulSetSvc),
			//},
			//vmCase{
			//	name: "dns: VM to k8s statefulset instance.service.namespace",
			//	from: vm,
			//	to:   apps.StatefulSet.Match(echo.InCluster(vm.Config().Cluster.Config())),
			//	host: fmt.Sprintf("%s-v1-0.%s.%s", StatefulSetSvc, StatefulSetSvc, apps.Namespace.Name()),
			//},
			//vmCase{
			//	name: "dns: VM to k8s statefulset instance.service.namespace.svc",
			//	from: vm,
			//	to:   apps.StatefulSet.Match(echo.InCluster(vm.Config().Cluster.Config())),
			//	host: fmt.Sprintf("%s-v1-0.%s.%s.svc", StatefulSetSvc, StatefulSetSvc, apps.Namespace.Name()),
			//},
			//vmCase{
			//	name: "dns: VM to k8s statefulset instance FQDN",
			//	from: vm,
			//	to:   apps.StatefulSet.Match(echo.InCluster(vm.Config().Cluster.Config())),
			//	host: fmt.Sprintf("%s-v1-0.%s", StatefulSetSvc, apps.StatefulSet[0].Config().ClusterLocalFQDN()),
			//},
		)
	}
	for _, podA := range apps.PodA {
		testCases = append(testCases, vmCase{
			name: "k8s to vm",
			from: podA,
			to:   vms,
		})
	}
	cases := make([]TrafficTestCase, 0)
	for _, c := range testCases {
		c := c
		checker := check.OK()
		if !match.IsHeadless.Any(c.to) {
			// headless load-balancing can be inconsistent
			checker = check.And(checker, check.ReachedClusters(c.to.Clusters()))
		}
		cases = append(cases, TrafficTestCase{
			name: fmt.Sprintf("%s from %s", c.name, c.from.Config().Cluster.StableName()),
			call: c.from.CallOrFail,
			opts: echo.CallOptions{
				// assume that all echos in `to` only differ in which cluster they're deployed in
				To: c.to,
				Port: echo.Port{
					Name: "http",
				},
				Address: c.host,
				Count:   callCountMultiplier * c.to.MustWorkloads().Clusters().Len(),
				Check:   checker,
			},
		})
	}
	return cases
}

func destinationRule(app, mode string) string {
	return fmt.Sprintf(`apiVersion: networking.istio.io/v1beta1
kind: DestinationRule
metadata:
  name: %s
spec:
  host: %s
  trafficPolicy:
    tls:
      mode: %s
---
`, app, app, mode)
}

const useClientProtocolDestinationRuleTmpl = `apiVersion: networking.istio.io/v1beta1
kind: DestinationRule
metadata:
  name: use-client-protocol
spec:
  host: {{.VirtualServiceHost}}
  trafficPolicy:
    tls:
      mode: DISABLE
    connectionPool:
      http:
        useClientProtocol: true
---
`

func useClientProtocolDestinationRule(app string) string {
	return tmpl.MustEvaluate(useClientProtocolDestinationRuleTmpl, map[string]string{"VirtualServiceHost": app})
}

func idletimeoutDestinationRule(name, app string) string {
	return fmt.Sprintf(`apiVersion: networking.istio.io/v1beta1
kind: DestinationRule
metadata:
  name: %s
spec:
  host: %s
  trafficPolicy:
    tls:
      mode: DISABLE
    connectionPool:
      http:
        idleTimeout: 100s
---
`, name, app)
}

func peerAuthentication(app, mode string) string {
	return fmt.Sprintf(`apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: %s
spec:
  selector:
    matchLabels:
      app: %s
  mtls:
    mode: %s
---
`, app, app, mode)
}

func globalPeerAuthentication(mode string) string {
	return fmt.Sprintf(`apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
spec:
  mtls:
    mode: %s
---
`, mode)
}

func serverFirstTestCases(apps *EchoDeployments) []TrafficTestCase {
	cases := make([]TrafficTestCase, 0)
	from := apps.PodA
	to := apps.PodC
	configs := []struct {
		port    string
		dest    string
		auth    string
		checker check.Checker
	}{
		// TODO: All these cases *should* succeed (except the TLS mismatch cases) - but don't due to issues in our implementation

		// For auto port, outbound request will be delayed by the protocol sniffer, regardless of configuration
		{"auto-tcp-server", "DISABLE", "DISABLE", check.Error()},
		{"auto-tcp-server", "DISABLE", "PERMISSIVE", check.Error()},
		{"auto-tcp-server", "DISABLE", "STRICT", check.Error()},
		{"auto-tcp-server", "ISTIO_MUTUAL", "DISABLE", check.Error()},
		{"auto-tcp-server", "ISTIO_MUTUAL", "PERMISSIVE", check.Error()},
		{"auto-tcp-server", "ISTIO_MUTUAL", "STRICT", check.Error()},

		// These is broken because we will still enable inbound sniffing for the port. Since there is no tls,
		// there is no server-first "upgrading" to client-first
		{"tcp-server", "DISABLE", "DISABLE", check.OK()},
		{"tcp-server", "DISABLE", "PERMISSIVE", check.Error()},

		// Expected to fail, incompatible configuration
		{"tcp-server", "DISABLE", "STRICT", check.Error()},
		{"tcp-server", "ISTIO_MUTUAL", "DISABLE", check.Error()},

		// In these cases, we expect success
		// There is no sniffer on either side
		{"tcp-server", "DISABLE", "DISABLE", check.OK()},

		// On outbound, we have no sniffer involved
		// On inbound, the request is TLS, so its not server first
		{"tcp-server", "ISTIO_MUTUAL", "PERMISSIVE", check.OK()},
		{"tcp-server", "ISTIO_MUTUAL", "STRICT", check.OK()},
	}
	for _, client := range from {
		for _, c := range configs {
			client, c := client, c
			cases = append(cases, TrafficTestCase{
				name: fmt.Sprintf("%v:%v/%v", c.port, c.dest, c.auth),
				skip: skip{
					skip:   apps.All.Clusters().IsMulticluster(),
					reason: "https://github.com/istio/istio/issues/37305: stabilize tcp connection breaks",
				},
				config: destinationRule(to.Config().Service, c.dest) + peerAuthentication(to.Config().Service, c.auth),
				call:   client.CallOrFail,
				opts: echo.CallOptions{
					To: to,
					Port: echo.Port{
						Name: c.port,
					},
					Scheme: scheme.TCP,
					// Inbound timeout is 1s. We want to test this does not hit the listener filter timeout
					Timeout: time.Millisecond * 100,
					Count:   1,
					Check:   c.checker,
				},
			})
		}
	}

	return cases
}

func jwtClaimRoute(apps *EchoDeployments) []TrafficTestCase {
	configRoute := `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: gateway
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
  - foo.bar
  gateways:
  - gateway
  http:
  - match:
    - uri:
        prefix: /
      {{- if .Headers }}
      headers:
        {{- range $data := .Headers }}
          "{{$data.Name}}":
            {{$data.Match}}: {{$data.Value}}
        {{- end }}
      {{- end }}
      {{- if .WithoutHeaders }}
      withoutHeaders:
        {{- range $data := .WithoutHeaders }}
          "{{$data.Name}}":
            {{$data.Match}}: {{$data.Value}}
        {{- end }}
      {{- end }}
    route:
    - destination:
        host: {{ .dstSvc }}
---
`
	configAll := configRoute + `
apiVersion: security.istio.io/v1beta1
kind: RequestAuthentication
metadata:
  name: default
  namespace: istio-system
spec:
  jwtRules:
  - issuer: "test-issuer-1@istio.io"
    jwksUri: "https://raw.githubusercontent.com/istio/istio/master/tests/common/jwt/jwks.json"
---
`
	podB := []match.Matcher{match.SameDeployment(apps.PodB[0])}
	headersWithToken := map[string][]string{
		"Host":          {"foo.bar"},
		"Authorization": {"Bearer " + jwt.TokenIssuer1WithNestedClaims1},
	}
	headersWithInvalidToken := map[string][]string{
		"Host":          {"foo.bar"},
		"Authorization": {"Bearer " + jwt.TokenExpired},
	}
	headersWithNoToken := map[string][]string{"Host": {"foo.bar"}}
	headersWithNoTokenButSameHeader := map[string][]string{
		"Host":                            {"foo.bar"},
		"request.auth.claims.nested.key1": {"valueA"},
	}

	type configData struct {
		Name, Match, Value string
	}
	cases := []TrafficTestCase{
		{
			name:             "matched with nested claims:200",
			targetMatchers:   podB,
			workloadAgnostic: true,
			viaIngress:       true,
			config:           configAll,
			templateVars: func(src echo.Callers, dest echo.Instances) map[string]interface{} {
				return map[string]interface{}{
					"Headers": []configData{{"@request.auth.claims.nested.key1", "exact", "valueA"}},
				}
			},
			opts: echo.CallOptions{
				Count: 1,
				Port: echo.Port{
					Name:     "http",
					Protocol: protocol.HTTP,
				},
				HTTP: echo.HTTP{
					Headers: headersWithToken,
				},
				Check: check.Status(http.StatusOK),
			},
		},
		{
			name:             "matched with single claim:200",
			targetMatchers:   podB,
			workloadAgnostic: true,
			viaIngress:       true,
			config:           configAll,
			templateVars: func(src echo.Callers, dest echo.Instances) map[string]interface{} {
				return map[string]interface{}{
					"Headers": []configData{{"@request.auth.claims.sub", "prefix", "sub"}},
				}
			},
			opts: echo.CallOptions{
				Count: 1,
				Port: echo.Port{
					Name:     "http",
					Protocol: protocol.HTTP,
				},
				HTTP: echo.HTTP{
					Headers: headersWithToken,
				},
				Check: check.Status(http.StatusOK),
			},
		},
		{
			name:             "matched multiple claims:200",
			targetMatchers:   podB,
			workloadAgnostic: true,
			viaIngress:       true,
			config:           configAll,
			templateVars: func(src echo.Callers, dest echo.Instances) map[string]interface{} {
				return map[string]interface{}{
					"Headers": []configData{
						{"@request.auth.claims.nested.key1", "exact", "valueA"},
						{"@request.auth.claims.sub", "prefix", "sub"},
					},
				}
			},
			opts: echo.CallOptions{
				Count: 1,
				Port: echo.Port{
					Name:     "http",
					Protocol: protocol.HTTP,
				},
				HTTP: echo.HTTP{
					Headers: headersWithToken,
				},
				Check: check.Status(http.StatusOK),
			},
		},
		{
			name:             "matched without claim:200",
			targetMatchers:   podB,
			workloadAgnostic: true,
			viaIngress:       true,
			config:           configAll,
			templateVars: func(src echo.Callers, dest echo.Instances) map[string]interface{} {
				return map[string]interface{}{
					"WithoutHeaders": []configData{{"@request.auth.claims.nested.key1", "exact", "value-not-matched"}},
				}
			},
			opts: echo.CallOptions{
				Count: 1,
				Port: echo.Port{
					Name:     "http",
					Protocol: protocol.HTTP,
				},
				HTTP: echo.HTTP{
					Headers: headersWithToken,
				},
				Check: check.Status(http.StatusOK),
			},
		},
		{
			name:             "unmatched without claim:404",
			targetMatchers:   podB,
			workloadAgnostic: true,
			viaIngress:       true,
			config:           configAll,
			templateVars: func(src echo.Callers, dest echo.Instances) map[string]interface{} {
				return map[string]interface{}{
					"WithoutHeaders": []configData{{"@request.auth.claims.nested.key1", "exact", "valueA"}},
				}
			},
			opts: echo.CallOptions{
				Count: 1,
				Port: echo.Port{
					Name:     "http",
					Protocol: protocol.HTTP,
				},
				HTTP: echo.HTTP{
					Headers: headersWithToken,
				},
				Check: check.Status(http.StatusNotFound),
			},
		},
		{
			name:             "matched both with and without claims:200",
			targetMatchers:   podB,
			workloadAgnostic: true,
			viaIngress:       true,
			config:           configAll,
			templateVars: func(src echo.Callers, dest echo.Instances) map[string]interface{} {
				return map[string]interface{}{
					"Headers":        []configData{{"@request.auth.claims.sub", "prefix", "sub"}},
					"WithoutHeaders": []configData{{"@request.auth.claims.nested.key1", "exact", "value-not-matched"}},
				}
			},
			opts: echo.CallOptions{
				Count: 1,
				Port: echo.Port{
					Name:     "http",
					Protocol: protocol.HTTP,
				},
				HTTP: echo.HTTP{
					Headers: headersWithToken,
				},
				Check: check.Status(http.StatusOK),
			},
		},
		{
			name:             "unmatched multiple claims:404",
			targetMatchers:   podB,
			workloadAgnostic: true,
			viaIngress:       true,
			config:           configAll,
			templateVars: func(src echo.Callers, dest echo.Instances) map[string]interface{} {
				return map[string]interface{}{
					"Headers": []configData{
						{"@request.auth.claims.nested.key1", "exact", "valueA"},
						{"@request.auth.claims.sub", "prefix", "value-not-matched"},
					},
				}
			},
			opts: echo.CallOptions{
				Count: 1,
				Port: echo.Port{
					Name:     "http",
					Protocol: protocol.HTTP,
				},
				HTTP: echo.HTTP{
					Headers: headersWithToken,
				},
				Check: check.Status(http.StatusNotFound),
			},
		},
		{
			name:             "unmatched token:404",
			targetMatchers:   podB,
			workloadAgnostic: true,
			viaIngress:       true,
			config:           configAll,
			templateVars: func(src echo.Callers, dest echo.Instances) map[string]interface{} {
				return map[string]interface{}{
					"Headers": []configData{{"@request.auth.claims.sub", "exact", "value-not-matched"}},
				}
			},
			opts: echo.CallOptions{
				Count: 1,
				Port: echo.Port{
					Name:     "http",
					Protocol: protocol.HTTP,
				},
				HTTP: echo.HTTP{
					Headers: headersWithToken,
				},
				Check: check.Status(http.StatusNotFound),
			},
		},
		{
			name:             "unmatched with invalid token:401",
			targetMatchers:   podB,
			workloadAgnostic: true,
			viaIngress:       true,
			config:           configAll,
			templateVars: func(src echo.Callers, dest echo.Instances) map[string]interface{} {
				return map[string]interface{}{
					"Headers": []configData{{"@request.auth.claims.nested.key1", "exact", "valueA"}},
				}
			},
			opts: echo.CallOptions{
				Count: 1,
				Port: echo.Port{
					Name:     "http",
					Protocol: protocol.HTTP,
				},
				HTTP: echo.HTTP{
					Headers: headersWithInvalidToken,
				},
				Check: check.Status(http.StatusUnauthorized),
			},
		},
		{
			name:             "unmatched with no token:404",
			targetMatchers:   podB,
			workloadAgnostic: true,
			viaIngress:       true,
			config:           configAll,
			templateVars: func(src echo.Callers, dest echo.Instances) map[string]interface{} {
				return map[string]interface{}{
					"Headers": []configData{{"@request.auth.claims.nested.key1", "exact", "valueA"}},
				}
			},
			opts: echo.CallOptions{
				Count: 1,
				Port: echo.Port{
					Name:     "http",
					Protocol: protocol.HTTP,
				},
				HTTP: echo.HTTP{
					Headers: headersWithNoToken,
				},
				Check: check.Status(http.StatusNotFound),
			},
		},
		{
			name:             "unmatched with no token but same header:404",
			targetMatchers:   podB,
			workloadAgnostic: true,
			viaIngress:       true,
			config:           configAll,
			templateVars: func(src echo.Callers, dest echo.Instances) map[string]interface{} {
				return map[string]interface{}{
					"Headers": []configData{{"@request.auth.claims.nested.key1", "exact", "valueA"}},
				}
			},
			opts: echo.CallOptions{
				Count: 1,
				Port: echo.Port{
					Name:     "http",
					Protocol: protocol.HTTP,
				},
				HTTP: echo.HTTP{
					// Include a header @request.auth.claims.nested.key1 and value same as the JWT claim, should not be routed.
					Headers: headersWithNoTokenButSameHeader,
				},
				Check: check.Status(http.StatusNotFound),
			},
		},
		{
			name:             "unmatched with no request authentication:404",
			targetMatchers:   podB,
			workloadAgnostic: true,
			viaIngress:       true,
			config:           configRoute,
			templateVars: func(src echo.Callers, dest echo.Instances) map[string]interface{} {
				return map[string]interface{}{
					"Headers": []configData{{"@request.auth.claims.nested.key1", "exact", "valueA"}},
				}
			},
			opts: echo.CallOptions{
				Count: 1,
				Port: echo.Port{
					Name:     "http",
					Protocol: protocol.HTTP,
				},
				HTTP: echo.HTTP{
					Headers: headersWithToken,
				},
				Check: check.Status(http.StatusNotFound),
			},
		},
	}
	return cases
}
