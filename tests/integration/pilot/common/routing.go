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
	"net/netip"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"istio.io/api/annotation"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/security"
	"istio.io/istio/pkg/http/headers"
	echoClient "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/tests/common/jwt"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
)

const originateTLSTmpl = `
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: "{{.VirtualServiceHost|replace "*" "wild"}}"
  namespace: "{{.IngressNamespace}}"
spec:
  host: "{{.VirtualServiceHost}}"
  trafficPolicy:
    tls:
      mode: SIMPLE
      insecureSkipVerify: true
---
`

const httpVirtualServiceTmpl = `
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: "{{.VirtualServiceHost|replace "*" "wild"}}"
spec:
  gateways:
  - {{.Gateway}}
  hosts:
  - "{{.VirtualServiceHost}}"
  http:
  - route:
    - destination:
        host: "{{.DestinationHost | default .VirtualServiceHost}}"
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
		DestinationHost    string
		Port               int
		MatchScheme        string
	}{gateway, host, "", port, ""})
}

const tcpVirtualServiceTmpl = `
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: "{{.VirtualServiceHost|replace "*" "wild"}}"
spec:
  gateways:
  - {{.Gateway}}
  hosts:
  - "{{.VirtualServiceHost}}"
  tcp:
  - match:
    - port: {{.SourcePort}}
    route:
    - destination:
        host: "{{.DestinationHost | default .VirtualServiceHost}}"
        port:
          number: {{.TargetPort}}
---
`

func tcpVirtualService(gateway, host, destHost string, sourcePort, targetPort int) string {
	return tmpl.MustEvaluate(tcpVirtualServiceTmpl, struct {
		Gateway            string
		VirtualServiceHost string
		DestinationHost    string
		SourcePort         int
		TargetPort         int
	}{gateway, host, destHost, sourcePort, targetPort})
}

const gatewayTmpl = `
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: gateway
spec:
  selector:
    istio: {{.GatewayIstioLabel | default "ingressgateway"}}
  servers:
  - port:
      number: {{.GatewayPort}}
      name: {{.GatewayPortName}}
      protocol: {{.GatewayProtocol}}
{{- if .Credential }}
    tls:
      mode: {{.TLSMode}}
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

func httpGateway(host string, port int, portName, protocol string, gatewayIstioLabel string) string { //nolint: unparam
	return tmpl.MustEvaluate(gatewayTmpl, struct {
		GatewayHost       string
		GatewayPort       int
		GatewayPortName   string
		GatewayProtocol   string
		Credential        string
		GatewayIstioLabel string
		TLSMode           string
	}{
		host, port, portName, protocol, "", gatewayIstioLabel, "SIMPLE",
	})
}

func virtualServiceCases(t TrafficContext) {
	// reduce the total # of subtests that don't give valuable coverage or just don't work
	// TODO include proxyless as different features become supported
	t.SetDefaultSourceMatchers(match.NotNaked, match.NotHeadless, match.NotProxylessGRPC)
	t.SetDefaultTargetMatchers(match.NotNaked, match.NotHeadless, match.NotProxylessGRPC)
	includeProxyless := []match.Matcher{match.NotNaked, match.NotHeadless}

	skipVM := t.Settings().Skip(echo.VM)
	t.RunTraffic(TrafficTestCase{
		name: "added header",
		config: `
apiVersion: networking.istio.io/v1
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
	})
	t.RunTraffic(TrafficTestCase{
		name: "set header",
		config: `
apiVersion: networking.istio.io/v1
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
	})
	t.RunTraffic(TrafficTestCase{
		name: "set authority header",
		config: `
apiVersion: networking.istio.io/v1
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
	})
	t.RunTraffic(TrafficTestCase{
		name: "set host header in destination",
		config: `
apiVersion: networking.istio.io/v1
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
	})
	t.RunTraffic(TrafficTestCase{
		name: "set authority header in destination",
		config: `
apiVersion: networking.istio.io/v1
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
	})
	t.RunTraffic(TrafficTestCase{
		name: "set host header in route and destination",
		config: `
apiVersion: networking.istio.io/v1
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
	})
	t.RunTraffic(TrafficTestCase{
		name: "set host header in route and multi destination",
		config: `
apiVersion: networking.istio.io/v1
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
	})
	t.RunTraffic(TrafficTestCase{
		name: "set host header multi destination",
		config: `
apiVersion: networking.istio.io/v1
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
	})
	t.RunTraffic(TrafficTestCase{
		name: "redirect",
		config: `
apiVersion: networking.istio.io/v1
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
	})
	t.RunTraffic(TrafficTestCase{
		name: "redirect port and scheme",
		config: `
apiVersion: networking.istio.io/v1
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
				LocationHeader("https://{{.Hostname}}:80/foo")),
		},
		workloadAgnostic: true,
	})
	t.RunTraffic(TrafficTestCase{
		name: "redirect request port",
		config: `
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
    - b
  http:
  - match:
    - uri:
        exact: /foo
    redirect:
      derivePort: FROM_REQUEST_PORT
`,
		children: []TrafficCall{
			{
				name: "to default",
				call: t.Apps.A[0].CallOrFail,
				opts: echo.CallOptions{
					To: t.Apps.B,
					Port: echo.Port{
						Name: "http",
					},
					HTTP: echo.HTTP{
						Path:            "/foo",
						Headers:         HostHeader("b"),
						FollowRedirects: false,
					},

					Count: 1,
					Check: check.And(
						check.Status(http.StatusMovedPermanently),
						// Note: there is no "80" added, as its already the protocol default
						LocationHeader("http://b/foo")),
				},
			},
			{
				name: "to default with host port",
				call: t.Apps.A[0].CallOrFail,
				opts: echo.CallOptions{
					To: t.Apps.B,
					Port: echo.Port{
						Name: "http",
					},
					HTTP: echo.HTTP{
						Path:            "/foo",
						Headers:         HostHeader("b:80"),
						FollowRedirects: false,
					},
					Count: 1,
					Check: check.And(
						check.Status(http.StatusMovedPermanently),
						// Note: 80 is set as it was explicit in the host header
						LocationHeader("http://b:80/foo")),
				},
			},
			{
				name: "non-default",
				call: t.Apps.A[0].CallOrFail,
				opts: echo.CallOptions{
					To: t.Apps.B,
					Port: echo.Port{
						Name: "http2",
					},
					HTTP: echo.HTTP{
						Path:            "/foo",
						Headers:         HostHeader("b"),
						FollowRedirects: false,
					},
					Count: 1,
					Check: check.And(
						check.Status(http.StatusMovedPermanently),
						// Note: there is "85" added, as its already NOT the protocol default
						LocationHeader("http://b:85/foo")),
				},
			},
			{
				name: "non-default with host port",
				call: t.Apps.A[0].CallOrFail,
				opts: echo.CallOptions{
					To: t.Apps.B,
					Port: echo.Port{
						Name: "http2",
					},
					HTTP: echo.HTTP{
						Path:            "/foo",
						Headers:         HostHeader("b:85"),
						FollowRedirects: false,
					},
					Count: 1,
					Check: check.And(
						check.Status(http.StatusMovedPermanently),
						// Note: there is "85" added, as its already NOT the protocol default
						LocationHeader("http://b:85/foo")),
				},
			},
		},
	})
	// Contain ever special char allowed in a header
	absurdHeader := "a!#$%&'*+-.^_`|~z"
	t.RunTraffic(TrafficTestCase{
		name: "weird header matches",
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{"header": absurdHeader}
		},
		config: `
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
    - {{ .dstSvc }}
  http:
  - match:
    - headers:
        {{.header|quote}}:
          exact: why
    route:
    - destination:
        host: {{ .dstSvc }}
`,
		workloadAgnostic: true,
		children: []TrafficCall{
			{
				name: "no match",
				opts: echo.CallOptions{
					Port:  echo.Port{Name: "http"},
					HTTP:  echo.HTTP{},
					Check: check.Status(http.StatusNotFound),
				},
			},
			{
				name: "match",
				opts: echo.CallOptions{
					Port:  echo.Port{Name: "http"},
					HTTP:  echo.HTTP{Headers: headers.New().With(absurdHeader, "why").Build()},
					Check: check.OK(),
				},
			},
		},
	})
	t.RunTraffic(TrafficTestCase{
		name: "pseudo header matches",
		config: `
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
    - {{ .dstSvc }}
  http:
  - match:
    - headers:
        :method:
          exact: GET
    route:
    - destination:
        host: {{ .dstSvc }}
`,
		workloadAgnostic: true,
		children: []TrafficCall{
			{
				name: "no match",
				opts: echo.CallOptions{
					Port:  echo.Port{Name: "http"},
					HTTP:  echo.HTTP{Method: "POST"},
					Check: check.Status(http.StatusNotFound),
				},
			},
			{
				name: "match",
				opts: echo.CallOptions{
					Port:  echo.Port{Name: "http"},
					HTTP:  echo.HTTP{Method: "GET"},
					Check: check.OK(),
				},
			},
		},
	})
	t.RunTraffic(TrafficTestCase{
		name: "rewrite uri",
		config: `
apiVersion: networking.istio.io/v1
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
	})
	t.RunTraffic(TrafficTestCase{
		name: "rewrite authority",
		config: `
apiVersion: networking.istio.io/v1
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
	})
	t.RunTraffic(TrafficTestCase{
		name: "cors",
		// TODO https://github.com/istio/istio/issues/31532
		targetMatchers: []match.Matcher{match.NotTProxy, match.NotVM, match.NotNaked, match.NotHeadless, match.NotProxylessGRPC},

		config: `
apiVersion: networking.istio.io/v1
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
	})
	// Retry conditions have been added to just check that config is correct.
	// Retries are not specifically tested. TODO if we actually test retries, include proxyless
	t.RunTraffic(TrafficTestCase{
		name: "retry conditions",
		config: `
apiVersion: networking.istio.io/v1
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
      retryOn: gateway-error,connect-failure,refused-stream,reset-before-request
      retryRemoteLocalities: true`,
		opts: echo.CallOptions{
			Port: echo.Port{
				Name: "http",
			},
			Count: 1,
			Check: check.OK(),
		},
		workloadAgnostic: true,
	})
	t.RunTraffic(TrafficTestCase{
		name: "fault abort",
		config: `
apiVersion: networking.istio.io/v1
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
	})
	t.RunTraffic(TrafficTestCase{
		name: "fault abort gRPC",
		config: `
apiVersion: networking.istio.io/v1
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
        grpcStatus: "UNAVAILABLE"`,
		opts: echo.CallOptions{
			Port: echo.Port{
				Name: "grpc",
			},
			Scheme: scheme.GRPC,
			Count:  1,
			Check:  check.GRPCStatus(codes.Unavailable),
		},
		workloadAgnostic: true,
		sourceMatchers:   includeProxyless,
	})
	t.RunTraffic(TrafficTestCase{
		name: "catch all route short circuit the other routes",
		config: `
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
  - {{ (index .dst 0).Config.Service }}
  http:
  - match:
    - uri:
        regex: .*
    route:
    - destination:
        host: {{ (index .dst 0).Config.Service }}
    fault:
      abort:
        percentage:
          value: 100
        httpStatus: 418
  - route:
    - destination:
        host: {{ .dstSvc }}`,
		opts: echo.CallOptions{
			Port: echo.Port{
				Name: "http",
			},
			Count: 1,
			Check: check.Status(http.StatusTeapot),
		},
		workloadAgnostic: true,
	})

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
		t.RunTraffic(TrafficTestCase{
			name:           fmt.Sprintf("shifting-%d", split[0]),
			skip:           skipAmbient(t, "https://github.com/istio/istio/issues/44948"),
			toN:            len(split),
			sourceMatchers: []match.Matcher{match.NotHeadless, match.NotNaked},
			targetMatchers: []match.Matcher{match.NotHeadless, match.NotNaked, match.NotExternal, match.NotProxylessGRPC},
			templateVars: func(_ echo.Callers, _ echo.Instances) map[string]any {
				return map[string]any{
					"split": split,
				}
			},
			config: `
{{ $split := .split }}
apiVersion: networking.istio.io/v1
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
			checkForN: func(src echo.Caller, dests echo.Services, opts *echo.CallOptions) echo.Checker {
				return check.And(
					check.OK(),
					func(result echo.CallResult, err error) error {
						errorThreshold := 10
						if len(split) != len(dests) {
							// shouldn't happen
							return fmt.Errorf("split configured for %d destinations, but framework gives %d", len(split), len(dests))
						}
						splitPerHost := map[echo.NamespacedName]int{}
						destNames := dests.NamespacedNames()
						for i, pct := range split {
							splitPerHost[destNames[i]] = pct
						}
						for serviceName, exp := range splitPerHost {
							hostResponses := result.Responses.Match(func(r echoClient.Response) bool {
								return strings.HasPrefix(r.Hostname, serviceName.Name)
							})
							if !AlmostEquals(len(hostResponses), exp, errorThreshold) {
								return fmt.Errorf("expected %v calls to %s, got %v", exp, serviceName, len(hostResponses))
							}
							// echotest should have filtered the deployment to only contain reachable clusters
							to := match.ServiceName(serviceName).GetMatches(dests.Instances())
							fromCluster := src.(echo.Instance).Config().Cluster
							toClusters := to.Clusters()
							// don't check headless since lb is unpredictable
							headlessTarget := match.Headless.Any(to)
							if !headlessTarget && len(toClusters.ByNetwork()[fromCluster.NetworkName()]) > 1 {
								// Conditionally check reached clusters to work around connection load balancing issues
								// See https://github.com/istio/istio/issues/32208 for details
								// We want to skip this for requests from the cross-network pod
								if err := check.ReachedClusters(t.AllClusters(), toClusters).Check(echo.CallResult{
									From:      result.From,
									Opts:      result.Opts,
									Responses: hostResponses,
								}, nil); err != nil {
									return fmt.Errorf("did not reach all clusters for %s: %v", serviceName, err)
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

		// access is denied when the request header `end-user` is not jason.
		t.RunTraffic(TrafficTestCase{
			name: "without headers",
			config: `
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: vs
spec:
  hosts:
  - {{ .dstSvc }}
  http:
  - match:
    - withoutHeaders:
        end-user:
          exact: jason
    route:
    - destination:
        host: {{ .dstSvc }}
    fault:
      abort:
        percentage:
          value: 100
        httpStatus: 403
  - route:
    - destination:
        host: {{ .dstSvc }}
`,
			children: []TrafficCall{
				{
					name: "end-user is jason",
					opts: func() echo.CallOptions {
						return echo.CallOptions{
							Port: echo.Port{
								Name: "http",
							},
							HTTP: echo.HTTP{
								Path: "/foo",
								Headers: headers.New().
									With("end-user", "jason").
									Build(),
							},
							Count: 1,
							Check: check.Status(http.StatusOK),
						}
					}(),
				},
				{
					name: "end-user is not jason",
					opts: func() echo.CallOptions {
						return echo.CallOptions{
							Port: echo.Port{
								Name: "http",
							},
							HTTP: echo.HTTP{
								Path: "/foo",
								Headers: headers.New().
									With("end-user", "not-jason").
									Build(),
							},
							Count: 1,
							Check: check.Status(http.StatusForbidden),
						}
					}(),
				},
				{
					name: "do not have end-user header",
					opts: func() echo.CallOptions {
						return echo.CallOptions{
							Port: echo.Port{
								Name: "http",
							},
							HTTP: echo.HTTP{
								Path: "/foo",
							},
							Count: 1,
							Check: check.Status(http.StatusForbidden),
						}
					}(),
				},
			},
			workloadAgnostic: true,
		})

		// allow only when `end-user` header present.
		t.RunTraffic(TrafficTestCase{
			name: "without headers regex convert to present_match",
			config: `
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: vs
spec:
  hosts:
  - {{ .dstSvc }}
  http:
  - match:
    - withoutHeaders:
        end-user:
          regex: "*"
    route:
    - destination:
        host: {{ .dstSvc }}
    fault:
      abort:
        percentage:
          value: 100
        httpStatus: 403
  - route:
    - destination:
        host: {{ .dstSvc }}
`,
			children: []TrafficCall{
				{
					name: "have end-user header and value",
					opts: func() echo.CallOptions {
						return echo.CallOptions{
							Port: echo.Port{
								Name: "http",
							},
							HTTP: echo.HTTP{
								Path: "/foo",
								Headers: headers.New().
									With("end-user", "jason").
									Build(),
							},
							Count: 1,
							Check: check.Status(http.StatusOK),
						}
					}(),
				},
				{
					name: "have end-user header but value is empty",
					opts: func() echo.CallOptions {
						return echo.CallOptions{
							Port: echo.Port{
								Name: "http",
							},
							HTTP: echo.HTTP{
								Path: "/foo",
								Headers: headers.New().
									With("end-user", "").
									Build(),
							},
							Count: 1,
							Check: check.Status(http.StatusOK),
						}
					}(),
				},
				{
					name: "do not have end-user header",
					opts: func() echo.CallOptions {
						return echo.CallOptions{
							Port: echo.Port{
								Name: "http",
							},
							HTTP: echo.HTTP{
								Path: "/foo",
							},
							Count: 1,
							Check: check.Status(http.StatusForbidden),
						}
					}(),
				},
			},
			workloadAgnostic: true,
		})

		// allow all access.
		t.RunTraffic(TrafficTestCase{
			name: "without headers regex match any string",
			config: `
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: vs
spec:
  hosts:
  - {{ .dstSvc }}
  http:
  - match:
    - withoutHeaders:
        end-user:
          regex: .*
    route:
    - destination:
        host: {{ .dstSvc }}
    fault:
      abort:
        percentage:
          value: 100
        httpStatus: 403
  - route:
    - destination:
        host: {{ .dstSvc }}
`,
			children: []TrafficCall{
				{
					name: "have end-user header and value",
					opts: func() echo.CallOptions {
						return echo.CallOptions{
							Port: echo.Port{
								Name: "http",
							},
							HTTP: echo.HTTP{
								Path: "/foo",
								Headers: headers.New().
									With("end-user", "jason").
									Build(),
							},
							Count: 1,
							Check: check.Status(http.StatusOK),
						}
					}(),
				},
				{
					name: "have end-user header but value is empty",
					opts: func() echo.CallOptions {
						return echo.CallOptions{
							Port: echo.Port{
								Name: "http",
							},
							HTTP: echo.HTTP{
								Path: "/foo",
								Headers: headers.New().
									With("end-user", "").
									Build(),
							},
							Count: 1,
							Check: check.Status(http.StatusOK),
						}
					}(),
				},
				{
					name: "do not have end-user header",
					opts: func() echo.CallOptions {
						return echo.CallOptions{
							Port: echo.Port{
								Name: "http",
							},
							HTTP: echo.HTTP{
								Path: "/foo",
							},
							Count: 1,
							Check: check.Status(http.StatusOK),
						}
					}(),
				},
			},
			workloadAgnostic: true,
		})

	}
}

func HostHeader(header string) http.Header {
	return headers.New().WithHost(header).Build()
}

// tlsOriginationCases contains tests TLS origination from DestinationRule
func tlsOriginationCases(t TrafficContext) {
	tc := TrafficTestCase{
		name: "DNS",
		config: fmt.Sprintf(`
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: external
spec:
  host: %s
  trafficPolicy:
    tls:
      mode: SIMPLE
      insecureSkipVerify: true
`, t.Apps.External.All.Config().DefaultHostHeader),
		children: []TrafficCall{},
	}
	expects := []struct {
		port int
		alpn string
	}{
		{8888, "http/1.1"},
		{8882, "h2"},
	}
	for _, c := range t.Apps.A {
		for _, e := range expects {
			tc.children = append(tc.children, TrafficCall{
				name: fmt.Sprintf("%s: %s", c.Config().Cluster.StableName(), e.alpn),
				opts: echo.CallOptions{
					Port:  echo.Port{ServicePort: e.port, Protocol: protocol.HTTP},
					Count: 1,
					// Failed requests will go to non-existent port which hangs forever
					// Set a low timeout to fail faster
					Timeout: time.Millisecond * 500,
					Address: t.Apps.External.All[0].Address(),
					HTTP: echo.HTTP{
						Headers: HostHeader(t.Apps.External.All[0].Config().DefaultHostHeader),
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
	t.RunTraffic(tc)

	tc = TrafficTestCase{
		name: "NONE",
		config: fmt.Sprintf(`
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: external
spec:
  host: %s
  trafficPolicy:
    tls:
      mode: SIMPLE
      insecureSkipVerify: true
---
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: alt-external-service
spec:
  exportTo: [.]
  hosts:
  - %s
  resolution: NONE
  ports:
  - name: http-tls-origination
    number: 8888
    protocol: http
    targetPort: 443
  - name: http2-tls-origination
    number: 8882
    protocol: http2
    targetPort: 443`,
			"external."+t.Apps.External.Namespace.Name()+".svc.cluster.local",
			"external."+t.Apps.External.Namespace.Name()+".svc.cluster.local",
		),
		children: []TrafficCall{},
	}
	for _, c := range t.Apps.A {
		for _, e := range expects {
			tc.children = append(tc.children, TrafficCall{
				name: fmt.Sprintf("%s: %s", c.Config().Cluster.StableName(), e.alpn),
				opts: echo.CallOptions{
					Port:  echo.Port{ServicePort: e.port, Protocol: protocol.HTTP},
					Count: 1,
					// Failed requests will go to non-existent port which hangs forever
					// Set a low timeout to fail faster
					Timeout: time.Millisecond * 500,
					Address: t.Apps.External.All.ForCluster(c.Config().Cluster.Name())[0].Address(),
					HTTP: echo.HTTP{
						Headers: HostHeader(t.Apps.External.All[0].Config().ClusterLocalFQDN()),
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
	t.RunTraffic(tc)

	tc = TrafficTestCase{
		name: "Redirect NONE",
		config: tmpl.MustEvaluate(`
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: external
spec:
  host: {{.}}
  trafficPolicy:
    tls:
      mode: SIMPLE
      insecureSkipVerify: true
---
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: httpbin.org
spec:
  hosts:
  - {{.}}
  http:
  - route:
    - destination:
        host: {{.}}
        port:
          number: 443
---
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: alt-external-service
spec:
  exportTo: [.]
  hosts:
  - {{.}}
  resolution: NONE
  ports:
  - name: http
    number: 8888
    protocol: http
  - name: http2
    number: 8882
    protocol: http2
  - name: https
    number: 443
    targetPort: 443
    protocol: https
    `,
			"external."+t.Apps.External.Namespace.Name()+".svc.cluster.local",
		),
		children: []TrafficCall{},
	}
	expects = []struct {
		port int
		alpn string
	}{
		{8888, "http/1.1"},
		// Note: here we expect HTTP/1.1, because the 443 port is not configured to be HTTP2!
		{8882, "http/1.1"},
	}
	for _, c := range t.Apps.A {
		for _, e := range expects {
			tc.children = append(tc.children, TrafficCall{
				name: fmt.Sprintf("%s: %s", c.Config().Cluster.StableName(), e.alpn),
				opts: echo.CallOptions{
					Port:  echo.Port{ServicePort: e.port, Protocol: protocol.HTTP},
					Count: 1,
					// Failed requests will go to non-existent port which hangs forever
					// Set a low timeout to fail faster
					Timeout: time.Millisecond * 500,
					Address: t.Apps.External.All.ForCluster(c.Config().Cluster.Name())[0].Address(),
					HTTP: echo.HTTP{
						Headers: HostHeader(t.Apps.External.All[0].Config().ClusterLocalFQDN()),
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
	t.RunTraffic(tc)
}

// useClientProtocolCases contains tests use_client_protocol from DestinationRule
func useClientProtocolCases(t TrafficContext) {
	client := t.Apps.A
	to := t.Apps.C
	t.RunTraffic(TrafficTestCase{
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
	})
	t.RunTraffic(TrafficTestCase{
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
	})
}

// destinationRuleCases contains tests some specific DestinationRule tests.
func destinationRuleCases(t TrafficContext) {
	from := t.Apps.A
	to := t.Apps.C
	// Validates the config is generated correctly when only idletimeout is specified in DR.
	t.RunTraffic(TrafficTestCase{
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
	})
}

// trafficLoopCases contains tests to ensure traffic does not loop through the sidecar
func trafficLoopCases(t TrafficContext) {
	for _, c := range t.Apps.A {
		for _, d := range t.Apps.B {
			for _, port := range []int{15001, 15006} {
				t.RunTraffic(TrafficTestCase{
					name: fmt.Sprint(port),
					call: c.CallOrFail,
					opts: echo.CallOptions{
						ToWorkload: d,
						Port:       echo.Port{ServicePort: port, Protocol: protocol.HTTP},
						// Ideally we would actually check to make sure we do not blow up the pod,
						// but I couldn't find a way to reliably detect this.
						Check: check.Error(),
					},
				})
			}
		}
	}
}

// autoPassthroughCases tests that we cannot hit unexpected destinations when using AUTO_PASSTHROUGH
func autoPassthroughCases(t TrafficContext) {
	// We test the cross product of all Istio ALPNs (or no ALPN), all mTLS modes, and various backends
	alpns := []string{"istio", "istio-peer-exchange", "istio-http/1.0", "istio-http/1.1", "istio-h2", ""}
	modes := []string{"STRICT", "PERMISSIVE", "DISABLE"}

	mtlsHost := host.Name(t.Apps.A.Config().ClusterLocalFQDN())
	nakedHost := host.Name(t.Apps.Naked.Config().ClusterLocalFQDN())
	httpsPort := ports.HTTP.ServicePort
	httpsAutoPort := ports.AutoHTTPS.ServicePort
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
	defaultIngress := istio.DefaultIngressOrFail(t, t)
	for _, mode := range modes {
		var childs []TrafficCall
		for _, sni := range snis {
			for _, alpn := range alpns {
				al := []string{alpn}
				if alpn == "" {
					al = nil
				}
				childs = append(childs, TrafficCall{
					name: fmt.Sprintf("mode:%v,sni:%v,alpn:%v", mode, sni, alpn),
					call: defaultIngress.CallOrFail,
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
		t.RunTraffic(TrafficTestCase{
			config: globalPeerAuthentication(mode) + `
---
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: cross-network-gateway-test
  namespace: {{.SystemNamespace | default "istio-system"}}
spec:
  selector:
    istio: {{.GatewayIstioLabel | default "ingressgateway"}}
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
			templateVars: func(_ echo.Callers, dests echo.Instances) map[string]any {
				systemNamespace := "istio-system"
				if t.Istio.Settings().SystemNamespace != "" {
					systemNamespace = t.Istio.Settings().SystemNamespace
				}
				return map[string]any{
					"SystemNamespace":   systemNamespace,
					"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
				}
			},
		})
	}
}

func gatewayCases(t TrafficContext) {
	// TODO fix for ambient
	skipEnvoyPeerMeta := skipAmbient(t, "X-Envoy-Peer-Metadata present in response")
	templateParams := func(protocol protocol.Instance, src echo.Callers, dests echo.Instances, ciphers []string, port string) map[string]any {
		hostName, dest, portN, cred := "*", dests[0], 80, ""
		if protocol.IsTLS() {
			hostName, portN, cred = dest.Config().ClusterLocalFQDN(), 443, "cred"
		}
		return map[string]any{
			"IngressNamespace":   src[0].(ingress.Instance).Namespace(),
			"GatewayHost":        hostName,
			"GatewayPort":        portN,
			"GatewayPortName":    strings.ToLower(string(protocol)),
			"GatewayProtocol":    string(protocol),
			"Gateway":            "gateway",
			"VirtualServiceHost": dest.Config().ClusterLocalFQDN(),
			"Port":               dest.PortForName(port).ServicePort,
			"Credential":         cred,
			"Ciphers":            ciphers,
			"TLSMode":            "SIMPLE",
			"GatewayIstioLabel":  t.Istio.Settings().IngressGatewayIstioLabel,
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
	// the following cases don't actually target workloads, we use the singleTarget filter to avoid duplicate cases
	// Gateways don't support talking directly to waypoint, so we skip that here as well.
	matchers := []match.Matcher{match.RegularPod, match.NotWaypoint}

	gatewayListenPort := 80
	gatewayListenPortName := "http"
	t.RunTraffic(TrafficTestCase{
		name:             "404",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           httpGateway("*", gatewayListenPort, gatewayListenPortName, "HTTP", t.Istio.Settings().IngressGatewayIstioLabel),
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
	})
	t.RunTraffic(TrafficTestCase{
		name:             "https redirect",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config: `apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: gateway
spec:
  selector:
    istio: {{.GatewayIstioLabel | default "ingressgateway"}}
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
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
			}
		},
		setupOpts: fqdnHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		// See https://github.com/istio/istio/issues/27315
		name:             "https with x-forwarded-proto",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config: `apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: gateway
spec:
  selector:
    istio: {{.GatewayIstioLabel | default "ingressgateway"}}
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
  namespace: {{.SystemNamespace | default "istio-system"}}
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
      istio: {{.GatewayIstioLabel | default "ingressgateway"}}
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
		templateVars: func(_ echo.Callers, dests echo.Instances) map[string]any {
			dest := dests[0]
			systemNamespace := "istio-system"
			if t.Istio.Settings().SystemNamespace != "" {
				systemNamespace = t.Istio.Settings().SystemNamespace
			}
			return map[string]any{
				"Gateway":            "gateway",
				"VirtualServiceHost": dest.Config().ClusterLocalFQDN(),
				"Port":               dest.PortForName(ports.HTTP.Name).ServicePort,
				"SystemNamespace":    systemNamespace,
				"GatewayIstioLabel":  t.Istio.Settings().IngressGatewayIstioLabel,
			}
		},
	})
	t.RunTraffic(TrafficTestCase{
		name:           "cipher suite",
		targetMatchers: []match.Matcher{match.NotWaypoint},
		config: gatewayTmpl + httpVirtualServiceTmpl +
			ingressutil.IngressKubeSecretYAML("cred", "{{.IngressNamespace}}", ingressutil.TLS, ingressutil.IngressCredentialA),
		templateVars: func(src echo.Callers, dests echo.Instances) map[string]any {
			// Test all cipher suites, including a fake one. Envoy should accept all of the ones on the "valid" list,
			// and control plane should filter our invalid one.

			params := templateParams(protocol.HTTPS, src, dests, append(sets.SortedList(security.ValidCipherSuites), "fake"), ports.HTTP.Name)
			params["GatewayIstioLabel"] = t.Istio.Settings().IngressGatewayIstioLabel
			return params
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
	})
	t.RunTraffic(TrafficTestCase{
		name:           "optional mTLS",
		targetMatchers: []match.Matcher{match.NotWaypoint},
		config: gatewayTmpl + httpVirtualServiceTmpl +
			ingressutil.IngressKubeSecretYAML("cred", "{{.IngressNamespace}}", ingressutil.TLS, ingressutil.IngressCredentialA),
		templateVars: func(src echo.Callers, dests echo.Instances) map[string]any {
			params := templateParams(protocol.HTTPS, src, dests, nil, ports.HTTP.Name)
			params["GatewayIstioLabel"] = t.Istio.Settings().IngressGatewayIstioLabel
			params["TLSMode"] = "OPTIONAL_MUTUAL"
			return params
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
	})
	t.RunTraffic(TrafficTestCase{
		// See https://github.com/istio/istio/issues/34609
		name:             "http redirect when vs port specify https",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config: `apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: gateway
spec:
  selector:
    istio: {{.GatewayIstioLabel | default "ingressgateway"}}
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
		templateVars: func(_ echo.Callers, dests echo.Instances) map[string]any {
			dest := dests[0]
			return map[string]any{
				"Gateway":            "gateway",
				"VirtualServiceHost": dest.Config().ClusterLocalFQDN(),
				"Port":               443,
				"GatewayIstioLabel":  t.Istio.Settings().IngressGatewayIstioLabel,
			}
		},
	})
	t.RunTraffic(TrafficTestCase{
		// See https://github.com/istio/istio/issues/27315
		// See https://github.com/istio/istio/issues/34609
		name:             "http return 400 with with x-forwarded-proto https when vs port specify https",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config: `apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: gateway
spec:
  selector:
    istio: {{.GatewayIstioLabel | default "ingressgateway"}}
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
  namespace: {{.SystemNamespace | default "istio-system"}}
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
      istio: {{.GatewayIstioLabel | default "ingressgateway"}}
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
		templateVars: func(_ echo.Callers, dests echo.Instances) map[string]any {
			systemNamespace := "istio-system"
			if t.Istio.Settings().SystemNamespace != "" {
				systemNamespace = t.Istio.Settings().SystemNamespace
			}
			dest := dests[0]
			return map[string]any{
				"Gateway":            "gateway",
				"VirtualServiceHost": dest.Config().ClusterLocalFQDN(),
				"Port":               443,
				"SystemNamespace":    systemNamespace,
				"GatewayIstioLabel":  t.Istio.Settings().IngressGatewayIstioLabel,
			}
		},
	})
	t.RunTraffic(TrafficTestCase{
		// https://github.com/istio/istio/issues/37196
		name:             "client protocol - http1",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config: `apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: gateway
spec:
  selector:
    istio: {{.GatewayIstioLabel | default "ingressgateway"}}
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
		templateVars: func(_ echo.Callers, dests echo.Instances) map[string]any {
			dest := dests[0]
			return map[string]any{
				"Gateway":            "gateway",
				"VirtualServiceHost": dest.Config().ClusterLocalFQDN(),
				"Port":               dest.PortForName(ports.AutoHTTP.Name).ServicePort,
				"GatewayIstioLabel":  t.Istio.Settings().IngressGatewayIstioLabel,
			}
		},
	})
	t.RunTraffic(TrafficTestCase{
		// https://github.com/istio/istio/issues/37196
		name:             "client protocol - http2",
		skip:             skipEnvoyPeerMeta,
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config: `apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: gateway
spec:
  selector:
    istio: {{.GatewayIstioLabel | default "ingressgateway"}}
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
		templateVars: func(_ echo.Callers, dests echo.Instances) map[string]any {
			dest := dests[0]
			systemNamespace := "istio-system"
			if t.Istio.Settings().SystemNamespace != "" {
				systemNamespace = t.Istio.Settings().SystemNamespace
			}
			return map[string]any{
				"Gateway":            "gateway",
				"VirtualServiceHost": dest.Config().ClusterLocalFQDN(),
				"Port":               dest.PortForName(ports.AutoHTTP.Name).ServicePort,
				"SystemNamespace":    systemNamespace,
				"GatewayIstioLabel":  t.Istio.Settings().IngressGatewayIstioLabel,
			}
		},
	})
	t.RunTraffic(TrafficTestCase{
		name:             "wildcard hostname",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config: `apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: gateway
spec:
  selector:
    istio: {{.GatewayIstioLabel | default "ingressgateway"}}
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*.example.com"
---
` + httpVirtualServiceTmpl,
		children: []TrafficCall{
			{
				name: "no port",
				call: nil,
				opts: echo.CallOptions{
					HTTP: echo.HTTP{
						HTTP2:   true,
						Headers: headers.New().WithHost("foo.example.com").Build(),
					},
					Port: echo.Port{
						Protocol: protocol.HTTP,
					},
					Check: check.OK(),
				},
			},
			{
				name: "correct port",
				call: nil,
				opts: echo.CallOptions{
					HTTP: echo.HTTP{
						HTTP2:   true,
						Headers: headers.New().WithHost("foo.example.com:80").Build(),
					},
					Port: echo.Port{
						Protocol: protocol.HTTP,
					},
					Check: check.OK(),
				},
			},
			{
				name: "random port",
				call: nil,
				opts: echo.CallOptions{
					HTTP: echo.HTTP{
						HTTP2:   true,
						Headers: headers.New().WithHost("foo.example.com:12345").Build(),
					},
					Port: echo.Port{
						Protocol: protocol.HTTP,
					},
					Check: check.OK(),
				},
			},
		},
		setupOpts: noTarget,
		templateVars: func(_ echo.Callers, dests echo.Instances) map[string]any {
			return map[string]any{
				"Gateway":            "gateway",
				"VirtualServiceHost": "*.example.com",
				"DestinationHost":    dests[0].Config().ClusterLocalFQDN(),
				"Port":               ports.HTTP.ServicePort,
				"GatewayIstioLabel":  t.Istio.Settings().IngressGatewayIstioLabel,
			}
		},
	})

	for _, port := range []echo.Port{ports.AutoHTTP, ports.HTTP, ports.HTTP2} {
		for _, h2 := range []bool{true, false} {
			protoName := "http1"
			expectedProto := "HTTP/1.1"
			if h2 {
				protoName = "http2"
				expectedProto = "HTTP/2.0"
			}

			t.RunTraffic(TrafficTestCase{
				// https://github.com/istio/istio/issues/37196
				name:             fmt.Sprintf("client protocol - %v use client with %v", protoName, port),
				skip:             skipEnvoyPeerMeta,
				targetMatchers:   matchers,
				workloadAgnostic: true,
				viaIngress:       true,
				config: `apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: gateway
spec:
  selector:
    istio: {{.GatewayIstioLabel | default "ingressgateway"}}
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
				templateVars: func(_ echo.Callers, dests echo.Instances) map[string]any {
					dest := dests[0]
					return map[string]any{
						"Gateway":            "gateway",
						"VirtualServiceHost": dest.Config().ClusterLocalFQDN(),
						"Port":               port.ServicePort,
						"GatewayIstioLabel":  t.Istio.Settings().IngressGatewayIstioLabel,
					}
				},
			})
		}
	}

	for _, proto := range []protocol.Instance{protocol.HTTP, protocol.HTTPS} {
		secret := ""
		if proto.IsTLS() {
			secret = ingressutil.IngressKubeSecretYAML("cred", "{{.IngressNamespace}}", ingressutil.TLS, ingressutil.IngressCredentialA)
		}
		t.RunTraffic(TrafficTestCase{
			name:   string(proto),
			config: gatewayTmpl + httpVirtualServiceTmpl + secret,
			templateVars: func(src echo.Callers, dests echo.Instances) map[string]any {
				params := templateParams(proto, src, dests, nil, ports.HTTP.Name)
				params["GatewayIstioLabel"] = t.Istio.Settings().IngressGatewayIstioLabel
				return params
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
		})
		t.RunTraffic(TrafficTestCase{
			name:   fmt.Sprintf("%s scheme match", proto),
			config: gatewayTmpl + httpVirtualServiceTmpl + secret,
			templateVars: func(src echo.Callers, dests echo.Instances) map[string]any {
				params := templateParams(proto, src, dests, nil, ports.HTTP.Name)
				params["MatchScheme"] = strings.ToLower(string(proto))
				params["GatewayIstioLabel"] = t.Istio.Settings().IngressGatewayIstioLabel
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
			targetMatchers:   matchers,
			viaIngress:       true,
			workloadAgnostic: true,
		})
	}
	secret := ingressutil.IngressKubeSecretYAML("cred", "{{.IngressNamespace}}", ingressutil.TLS, ingressutil.IngressCredentialA)
	t.RunTraffic(TrafficTestCase{
		name:   "HTTPS re-encrypt",
		config: gatewayTmpl + httpVirtualServiceTmpl + originateTLSTmpl + secret,
		templateVars: func(src echo.Callers, dests echo.Instances) map[string]any {
			return templateParams(protocol.HTTPS, src, dests, nil, ports.HTTPS.Name)
		},
		setupOpts: fqdnHostHeader,
		opts: echo.CallOptions{
			Port: echo.Port{
				Protocol: protocol.HTTPS,
			},
			Check: check.OK(),
		},
		viaIngress:       true,
		workloadAgnostic: true,
	})
}

// 1. Creates a TCP Gateway and VirtualService listener
// 2. Configures the echoserver to call itself via the TCP gateway using PROXY protocol https://www.haproxy.org/download/1.8/doc/proxy-protocol.txt
// 3. Assumes that the proxy filter EnvoyFilter is applied
func ProxyProtocolFilterAppliedGatewayCase(apps *deployment.SingleNamespaceView, gateway string) []TrafficTestCase {
	var cases []TrafficTestCase
	gatewayListenPort := 80
	gatewayListenPortName := "tcp"

	destinationSets := []echo.Instances{
		apps.A,
	}

	for _, d := range destinationSets {
		if len(d) == 0 {
			continue
		}

		fqdn := d[0].Config().ClusterLocalFQDN()
		cases = append(cases, TrafficTestCase{
			name: d[0].Config().Service,
			// This creates a Gateway with a TCP listener that will accept TCP traffic from host
			// `fqdn` and forward that traffic back to `fqdn`, from srcPort to targetPort
			config: httpGateway("*", gatewayListenPort, gatewayListenPortName, "TCP", "") + // use the default label since this test creates its own gateway
				tcpVirtualService("gateway", fqdn, "", 80, ports.TCP.ServicePort),
			call: apps.Naked[0].CallOrFail,
			opts: echo.CallOptions{
				Count:   1,
				Port:    echo.Port{ServicePort: 80},
				Scheme:  scheme.TCP,
				Address: gateway,
				// Envoy requires PROXY protocol TCP payloads have a minimum size, see:
				// https://www.envoyproxy.io/docs/envoy/latest/api-v3/extensions/filters/listener/proxy_protocol/v3/proxy_protocol.proto
				// If the PROXY protocol filter is enabled, Envoy should parse and consume the header out of the TCP payload, otherwise echo it back as-is.
				Message:              "This is a test TCP message",
				ProxyProtocolVersion: 1,
				Check: check.Each(
					func(r echoClient.Response) error {
						body := r.RawContent
						ok := strings.Contains(body, "PROXY TCP4")
						if ok {
							return fmt.Errorf("sent proxy protocol header, and it was echoed back")
						}
						return nil
					}),
			},
		})
	}
	return cases
}

func TestUpstreamProxyProtocol(t TrafficContext) {
	d := t.Apps.B
	fqdn := d.Config().ClusterLocalFQDN()
	destRule := fmt.Sprintf(`
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: proxy
spec:
  host: %s
  trafficPolicy:
    tls:
      mode: DISABLE
    proxyProtocol:
      version: V1
---
`, fqdn)

	t.RunTraffic(TrafficTestCase{
		name:   "proxy",
		config: destRule,
		call:   t.Apps.A[0].CallOrFail,
		opts: echo.CallOptions{
			To:    d,
			Count: 1,
			Port:  ports.HTTPWithProxy,
			Check: check.ProxyProtocolVersion("1"),
		},
	})
}

func XFFGatewayCase(apps *deployment.SingleNamespaceView, gateway string) []TrafficTestCase {
	var cases []TrafficTestCase
	gatewayListenPort := 80

	destinationSets := []echo.Instances{
		apps.A,
	}

	for _, d := range destinationSets {
		if len(d) == 0 {
			continue
		}
		fqdn := d[0].Config().ClusterLocalFQDN()
		cases = append(cases, TrafficTestCase{
			name: d[0].Config().Service,
			config: httpGateway("*", gatewayListenPort, ports.HTTP.Name, "HTTP", "") +
				httpVirtualService("gateway", fqdn, ports.HTTP.ServicePort),
			call: apps.Naked[0].CallOrFail,
			opts: echo.CallOptions{
				Count:   1,
				Port:    echo.Port{ServicePort: gatewayListenPort},
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

func envoyFilterCases(t TrafficContext) {
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
	for _, c := range t.Apps.A {
		t.RunTraffic(TrafficTestCase{
			config: cfg,
			call:   c.CallOrFail,
			opts: echo.CallOptions{
				To:    t.Apps.B,
				Count: 1,
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
}

// hostCases tests different forms of host header to use
func hostCases(t TrafficContext) {
	for _, c := range t.Apps.A {
		cfg := t.Apps.Headless.Config()
		port := ports.AutoHTTP.WorkloadPort
		wl := t.Apps.Headless[0].WorkloadsOrFail(t)
		if len(wl) == 0 {
			t.Fatal("no workloads found")
		}
		address := wl[0].Address()
		// We test all variants with no port, the expected port, and a random port.
		hosts := []string{
			cfg.ClusterLocalFQDN(),
			fmt.Sprintf("%s:%d", cfg.ClusterLocalFQDN(), port),
			fmt.Sprintf("%s:12345", cfg.ClusterLocalFQDN()),
			fmt.Sprintf("%s.%s.svc", cfg.Service, cfg.Namespace.Name()),
			fmt.Sprintf("%s.%s.svc:%d", cfg.Service, cfg.Namespace.Name(), port),
			fmt.Sprintf("%s.%s.svc:12345", cfg.Service, cfg.Namespace.Name()),
			cfg.Service,
			fmt.Sprintf("%s:%d", cfg.Service, port),
			fmt.Sprintf("%s:12345", cfg.Service),
			fmt.Sprintf("some-instances.%s:%d", cfg.ClusterLocalFQDN(), port),
			fmt.Sprintf("some-instances.%s:12345", cfg.ClusterLocalFQDN()),
			fmt.Sprintf("some-instances.%s.%s.svc", cfg.Service, cfg.Namespace.Name()),
			fmt.Sprintf("some-instances.%s.%s.svc:12345", cfg.Service, cfg.Namespace.Name()),
			fmt.Sprintf("some-instances.%s", cfg.Service),
			fmt.Sprintf("some-instances.%s:%d", cfg.Service, port),
			fmt.Sprintf("some-instances.%s:12345", cfg.Service),
			address,
			fmt.Sprintf("%s:%d", address, port),
		}
		for _, h := range hosts {
			name := strings.Replace(h, address, "ip", -1) + "/auto-http"
			t.RunTraffic(TrafficTestCase{
				name: name,
				call: c.CallOrFail,
				opts: echo.CallOptions{
					To:    t.Apps.Headless,
					Count: 1,
					Port: echo.Port{
						Name: "auto-http",
					},
					HTTP: echo.HTTP{
						Headers: HostHeader(h),
					},
					// check mTLS to ensure we are not hitting pass-through cluster
					Check: check.And(check.OK(), check.MTLSForHTTP()),
				},
			})
		}
		port = ports.HTTP.WorkloadPort
		hosts = []string{
			cfg.ClusterLocalFQDN(),
			fmt.Sprintf("%s:%d", cfg.ClusterLocalFQDN(), port),
			fmt.Sprintf("%s:12345", cfg.ClusterLocalFQDN()),
			fmt.Sprintf("%s.%s.svc", cfg.Service, cfg.Namespace.Name()),
			fmt.Sprintf("%s.%s.svc:%d", cfg.Service, cfg.Namespace.Name(), port),
			fmt.Sprintf("%s.%s.svc:12345", cfg.Service, cfg.Namespace.Name()),
			cfg.Service,
			fmt.Sprintf("%s:%d", cfg.Service, port),
			fmt.Sprintf("%s:12345", cfg.Service),
			fmt.Sprintf("some-instances.%s:%d", cfg.ClusterLocalFQDN(), port),
			fmt.Sprintf("some-instances.%s:12345", cfg.ClusterLocalFQDN()),
			fmt.Sprintf("some-instances.%s.%s.svc", cfg.Service, cfg.Namespace.Name()),
			fmt.Sprintf("some-instances.%s.%s.svc:%d", cfg.Service, cfg.Namespace.Name(), port),
			fmt.Sprintf("some-instances.%s.%s.svc:12345", cfg.Service, cfg.Namespace.Name()),
			fmt.Sprintf("some-instances.%s", cfg.Service),
			fmt.Sprintf("some-instances.%s:%d", cfg.Service, port),
			fmt.Sprintf("some-instances.%s:12345", cfg.Service),
			address,
			fmt.Sprintf("%s:%d", address, port),
		}
		for _, h := range hosts {
			name := strings.Replace(h, address, "ip", -1) + "/http"
			assertion := check.And(check.OK(), check.MTLSForHTTP())
			if strings.Contains(name, "ip") {
				// we expect to actually do passthrough for the IP case
				assertion = check.OK()
			}
			t.RunTraffic(TrafficTestCase{
				name: name,
				call: c.CallOrFail,
				opts: echo.CallOptions{
					To: t.Apps.Headless,
					Port: echo.Port{
						Name: "http",
					},
					HTTP: echo.HTTP{
						Headers: HostHeader(h),
					},
					// check mTLS to ensure we are not hitting pass-through cluster
					Check: assertion,
				},
			})
		}
	}
}

// serviceCases tests overlapping Services. There are a few cases.
// Consider we have our base service B, with service port P and target port T
//  1. Another service, B', with P -> T. In this case, both the listener and the cluster will conflict.
//     Because everything is workload oriented, this is not a problem unless they try to make them different
//     protocols (this is explicitly called out as "not supported") or control inbound connectionPool settings
//     (which is moving to Sidecar soon)
//  2. Another service, B', with P -> T'. In this case, the listener will be distinct, since its based on the target.
//     The cluster, however, will be shared, which is broken, because we should be forwarding to T when we call B, and T' when we call B'.
//  3. Another service, B', with P' -> T. In this case, the listener is shared. This is fine, with the exception of different protocols
//     The cluster is distinct.
//  4. Another service, B', with P' -> T'. There is no conflicts here at all.
func serviceCases(t TrafficContext) {
	for _, c := range t.Apps.A {
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
    app: b`, ports.HTTP.ServicePort, ports.HTTP.WorkloadPort)
		t.RunTraffic(TrafficTestCase{
			name:   fmt.Sprintf("case 1 both match in cluster %v", c.Config().Cluster.StableName()),
			config: svc,
			call:   c.CallOrFail,
			opts: echo.CallOptions{
				Count:   1,
				Address: "b-alt-1",
				Port:    echo.Port{ServicePort: ports.HTTP.ServicePort, Protocol: protocol.HTTP},
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
    app: b`, ports.HTTP.ServicePort, ports.All().GetWorkloadOnlyPorts()[0].WorkloadPort)
		t.RunTraffic(TrafficTestCase{
			name:   fmt.Sprintf("case 2 service port match in cluster %v", c.Config().Cluster.StableName()),
			config: svc,
			call:   c.CallOrFail,
			opts: echo.CallOptions{
				Count:   1,
				Address: "b-alt-2",
				Port:    echo.Port{ServicePort: ports.HTTP.ServicePort, Protocol: protocol.TCP},
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
    app: b`, ports.HTTP.WorkloadPort)
		t.RunTraffic(TrafficTestCase{
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
		t.RunTraffic(TrafficTestCase{
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
}

func externalNameCases(t TrafficContext) {
	calls := func(name string, checks ...echo.Checker) []TrafficCall {
		checks = append(checks, check.OK())
		ch := []TrafficCall{}
		for _, c := range t.Apps.A {
			for _, port := range []echo.Port{ports.HTTP, ports.AutoHTTP, ports.TCP, ports.HTTPS} {
				ch = append(ch, TrafficCall{
					name: port.Name,
					call: c.CallOrFail,
					opts: echo.CallOptions{
						Address: name,
						Port:    port,
						Timeout: time.Millisecond * 250,
						Check:   check.And(checks...),
					},
				})
			}
		}
		return ch
	}

	t.RunTraffic(TrafficTestCase{
		name:         "without port",
		globalConfig: true,
		config: fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: b-ext-no-port
spec:
  type: ExternalName
  externalName: b.%s.svc.cluster.local`, t.Apps.Namespace.Name()),
		children: calls("b-ext-no-port", check.MTLSForHTTP()),
	})

	t.RunTraffic(TrafficTestCase{
		name:         "with port",
		globalConfig: true,
		config: fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: b-ext-port
spec:
  ports:
  - name: http
    port: %d
    protocol: TCP
    targetPort: %d
  type: ExternalName
  externalName: b.%s.svc.cluster.local`,
			ports.HTTP.ServicePort, ports.HTTP.WorkloadPort, t.Apps.Namespace.Name()),
		children: calls("b-ext-port", check.MTLSForHTTP()),
	})

	t.RunTraffic(TrafficTestCase{
		name: "service entry",
		skip: skip{
			skip:   true,
			reason: "not currently working, as SE doesn't have a VIP",
		},
		globalConfig: true,
		config: fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: b-ext-se
spec:
  type: ExternalName
  externalName: %s`,
			t.Apps.External.All.Config().HostHeader()),
		children: calls("b-ext-se"),
	})

	t.RunTraffic(TrafficTestCase{
		name: "routed",
		skip: skip{
			skip:   t.Clusters().IsMulticluster(),
			reason: "we need to apply service to all but Istio config to only Istio clusters, which we don't support",
		},
		globalConfig: true,
		config: fmt.Sprintf(`apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: ext-route
spec:
  gateways:
  - mesh
  hosts:
  - c
  http:
  - route:
    - destination:
        host: b-ext-route.%s.svc.cluster.local
        port:
          number: 80
---
apiVersion: v1
kind: Service
metadata:
  name: b-ext-route
spec:
  type: ExternalName
  externalName: b.%s.svc.cluster.local
  ports:
  - name: http
    port: 80
    protocol: TCP
    targetPort: 80`, t.Apps.Namespace.Name(), t.Apps.Namespace.Name()),
		children: calls("c", check.MTLSForHTTP()),
	})

	gatewayListenPort := 80
	gatewayListenPortName := "http"
	t.RunTraffic(TrafficTestCase{
		name: "gateway",
		skip: skip{
			skip:   t.Clusters().IsMulticluster(),
			reason: "we need to apply service to all but Istio config to only Istio clusters, which we don't support",
		},
		globalConfig: true,
		config: fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: b-ext-se
spec:
  type: ExternalName
  externalName: b.%s.svc.cluster.local
---`,
			t.Apps.Namespace.Name()) +
			httpGateway("*", gatewayListenPort, gatewayListenPortName, "HTTP", t.Istio.Settings().IngressGatewayIstioLabel) +
			httpVirtualService("gateway", fmt.Sprintf("b-ext-se.%s.svc.cluster.local", t.Apps.Namespace.Name()), ports.HTTP.ServicePort),
		call: t.Istio.Ingresses().Callers()[0].CallOrFail,
		opts: echo.CallOptions{
			Address: fmt.Sprintf("b-ext-se.%s.svc.cluster.local", t.Apps.Namespace.Name()),
			Port:    ports.HTTP,
			Check:   check.OK(),
		},
	})
}

// consistentHashCases tests destination rule's consistent hashing mechanism
func consistentHashCases(t TrafficContext) {
	if len(t.Clusters().ByNetwork()) != 1 {
		// Consistent hashing does not work for multinetwork. The first request will consistently go to a
		// gateway, but that gateway will tcp_proxy it to a random pod.
		t.Skip("multi-network is not supported")
	}
	for _, app := range []echo.Instances{t.Apps.A, t.Apps.B} {
		for _, c := range app {
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
`, map[string]any{
				"Service":        svcName,
				"Network":        c.Config().Cluster.NetworkName(),
				"Port":           ports.HTTP.ServicePort,
				"TargetPort":     ports.HTTP.WorkloadPort,
				"TcpPort":        ports.TCP.ServicePort,
				"TcpTargetPort":  ports.TCP.WorkloadPort,
				"GrpcPort":       ports.GRPC.ServicePort,
				"GrpcTargetPort": ports.GRPC.WorkloadPort,
			})

			destRule := fmt.Sprintf(`
---
apiVersion: networking.istio.io/v1
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

			cookieWithTTLDest := fmt.Sprintf(`
---
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: %s
spec:
  host: %s
  trafficPolicy:
    loadBalancer:
      consistentHash:
        httpCookie:
          name: session-cookie
          ttl: 3600s
`, svcName, svcName)

			cookieWithoutTTLDest := fmt.Sprintf(`
---
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: %s
spec:
  host: %s
  trafficPolicy:
    loadBalancer:
      consistentHash:
        httpCookie:
          name: session-cookie
`, svcName, svcName)
			// Add a negative test case. This ensures that the test is actually valid; its not a super trivial check
			// and could be broken by having only 1 pod so its good to have this check in place
			t.RunTraffic(TrafficTestCase{
				name:   "no consistent",
				config: svc,
				call:   c.CallOrFail,
				opts: echo.CallOptions{
					Count:   10,
					Address: svcName,
					Port:    echo.Port{ServicePort: ports.HTTP.ServicePort, Protocol: protocol.HTTP},
					Check: check.And(
						check.OK(),
						func(result echo.CallResult, rerr error) error {
							err := ConsistentHostChecker.Check(result, rerr)
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
				Port: echo.Port{ServicePort: ports.HTTP.ServicePort, Protocol: protocol.HTTP},
				Check: check.And(
					check.OK(),
					ConsistentHostChecker,
				),
			}
			cookieCallOpts := echo.CallOptions{
				Count:   10,
				Address: svcName,
				HTTP: echo.HTTP{
					Path:    "/?some-query-param=bar",
					Headers: headers.New().With("x-some-header", "baz").Build(),
				},
				Port: echo.Port{ServicePort: ports.HTTP.ServicePort, Protocol: protocol.HTTP},
				Check: check.And(
					check.OK(),
					ConsistentHostChecker,
				),
				PropagateResponse: func(req *http.Request, res *http.Response) {
					scopes.Framework.Infof("invoking propagate response")
					if res == nil {
						scopes.Framework.Infof("no response")
						return
					}
					if res.Cookies() == nil {
						scopes.Framework.Infof("no cookies")
						return
					}
					var sessionCookie *http.Cookie
					for _, cookie := range res.Cookies() {
						if cookie.Name == "session-cookie" {
							sessionCookie = cookie
							break
						}
					}
					if sessionCookie != nil {
						scopes.Framework.Infof("setting the request cookie back in the request: %v %v",
							sessionCookie.Value, sessionCookie.Expires)
						req.AddCookie(sessionCookie)
					} else {
						scopes.Framework.Infof("no session cookie found in the response")
					}
				},
			}
			cookieWithoutTTLCallOpts := cookieCallOpts
			cookieWithoutTTLCallOpts.HTTP.Headers = headers.New().With("Cookie", "session-cookie=somecookie").Build()
			tcpCallopts := echo.CallOptions{
				Count:   10,
				Address: svcName,
				Port:    echo.Port{ServicePort: ports.TCP.ServicePort, Protocol: protocol.TCP},
				Check: check.And(
					check.OK(),
					ConsistentHostChecker,
				),
			}
			if c.Config().WorkloadClass() == echo.Proxyless {
				callOpts.Port = echo.Port{ServicePort: ports.GRPC.ServicePort, Protocol: protocol.GRPC}
			}
			// Setup tests for various forms of the API
			// TODO: it may be necessary to vary the inputs of the hash and ensure we get a different backend
			// But its pretty hard to test that, so for now just ensure we hit the same one.
			t.RunTraffic(TrafficTestCase{
				name:   "source ip " + c.Config().Service,
				config: svc + tmpl.MustEvaluate(destRule, "useSourceIp: true"),
				call:   c.CallOrFail,
				opts:   callOpts,
			})
			t.RunTraffic(TrafficTestCase{
				name:   "query param" + c.Config().Service,
				config: svc + tmpl.MustEvaluate(destRule, "httpQueryParameterName: some-query-param"),
				call:   c.CallOrFail,
				opts:   callOpts,
			})
			t.RunTraffic(TrafficTestCase{
				name:   "http header" + c.Config().Service,
				config: svc + tmpl.MustEvaluate(destRule, "httpHeaderName: x-some-header"),
				call:   c.CallOrFail,
				opts:   callOpts,
			})
			t.RunTraffic(TrafficTestCase{
				name:   "tcp source ip " + c.Config().Service,
				config: svc + tmpl.MustEvaluate(destRule, "useSourceIp: true"),
				call:   c.CallOrFail,
				opts:   tcpCallopts,
				skip: skip{
					skip:   c.Config().WorkloadClass() == echo.Proxyless,
					reason: "", // TODO: is this a bug or WAI?
				},
			})
			t.RunTraffic(TrafficTestCase{
				name:   "http cookie with ttl" + c.Config().Service,
				config: svc + tmpl.MustEvaluate(cookieWithTTLDest, ""),
				call:   c.CallOrFail,
				opts:   cookieCallOpts,
				skip: skip{
					skip:   true,
					reason: "https://github.com/istio/istio/issues/48156: not currently working, as test framework is not passing the cookies back",
				},
			})
			t.RunTraffic(TrafficTestCase{
				name:   "http cookie without ttl" + c.Config().Service,
				config: svc + tmpl.MustEvaluate(cookieWithoutTTLDest, ""),
				call:   c.CallOrFail,
				opts:   cookieWithoutTTLCallOpts,
				skip: skip{
					skip:   true,
					reason: "https://github.com/istio/istio/issues/48156: not currently working, as test framework is not passing the cookies back",
				},
			})
		}
	}
}

var ConsistentHostChecker echo.Checker = func(result echo.CallResult, _ error) error {
	hostnames := make([]string, len(result.Responses))
	for i, r := range result.Responses {
		hostnames[i] = r.Hostname
	}
	scopes.Framework.Infof("requests landed on hostnames: %v", hostnames)
	unique := sets.SortedList(sets.New(hostnames...))
	if len(unique) != 1 {
		return fmt.Errorf("expected only one destination, got: %v", unique)
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
func selfCallsCases(t TrafficContext) {
	t.SetDefaultSourceMatchers(match.NotExternal, match.NotNaked, match.NotHeadless, match.NotProxylessGRPC)
	t.SetDefaultComboFilter(func(from echo.Instance, to echo.Instances) echo.Instances {
		return match.ServiceName(from.NamespacedName()).GetMatches(to)
	})
	// Calls to the Service will go through envoy outbound and inbound, so we get envoy headers added
	t.RunTraffic(TrafficTestCase{
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
	})
	// Localhost calls will go directly to localhost, bypassing Envoy. No envoy headers added.
	t.RunTraffic(TrafficTestCase{
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
	})
	// PodIP calls will go directly to podIP, bypassing Envoy. No envoy headers added.
	t.RunTraffic(TrafficTestCase{
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
	})
}

// TODO: merge with security TestReachability code
func protocolSniffingCases(t TrafficContext) {
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
		t.RunTraffic(TrafficTestCase{
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
			check: func(src echo.Caller, opts *echo.CallOptions) echo.Checker {
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
						if from.Config().IsProxylessGRPC() && match.VM.Any(to) {
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

	autoPort := ports.AutoHTTP
	httpPort := ports.HTTP
	// Tests for http1.0. Golang does not support 1.0 client requests at all
	// To simulate these, we use TCP and hand-craft the requests.
	t.RunTraffic(TrafficTestCase{
		name: "http10 to http",
		call: t.Apps.A[0].CallOrFail,
		opts: echo.CallOptions{
			To:    t.Apps.B,
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
	})
	t.RunTraffic(TrafficTestCase{
		name: "http10 to auto",
		call: t.Apps.A[0].CallOrFail,
		opts: echo.CallOptions{
			To:    t.Apps.B,
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
	})
	t.RunTraffic(TrafficTestCase{
		name: "http10 to external",
		call: t.Apps.A[0].CallOrFail,
		opts: echo.CallOptions{
			Address: t.Apps.External.All[0].Address(),
			HTTP: echo.HTTP{
				Headers: HostHeader(t.Apps.External.All.Config().DefaultHostHeader),
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
	})
	t.RunTraffic(TrafficTestCase{
		name: "http10 to external auto",
		call: t.Apps.A[0].CallOrFail,
		opts: echo.CallOptions{
			Address: t.Apps.External.All[0].Address(),
			HTTP: echo.HTTP{
				Headers: HostHeader(t.Apps.External.All.Config().DefaultHostHeader),
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
}

// Todo merge with security TestReachability code
func instanceIPTests(t TrafficContext) {
	// proxyless doesn't get valuable coverage here
	t.SetDefaultTargetMatchers(match.NotProxylessGRPC)
	t.SetDefaultSourceMatchers(match.NotProxylessGRPC)

	ipCases := []struct {
		name            string
		endpoint        string
		disableSidecar  bool
		port            echo.Port
		code            int
		minIstioVersion string
	}{
		// instance IP bind
		{
			name:           "instance IP without sidecar",
			disableSidecar: true,
			port:           ports.HTTPInstance,
			code:           http.StatusOK,
		},
		{
			name:     "instance IP with wildcard sidecar",
			endpoint: "0.0.0.0",
			port:     ports.HTTPInstance,
			code:     http.StatusOK,
		},
		{
			name:     "instance IP with localhost sidecar",
			endpoint: "127.0.0.1",
			port:     ports.HTTPInstance,
			code:     http.StatusServiceUnavailable,
		},
		{
			name:     "instance IP with empty sidecar",
			endpoint: "",
			port:     ports.HTTPInstance,
			code:     http.StatusOK,
		},

		// Localhost bind
		{
			name:           "localhost IP without sidecar",
			disableSidecar: true,
			port:           ports.HTTPLocalHost,
			code:           http.StatusServiceUnavailable,
		},
		{
			name:     "localhost IP with wildcard sidecar",
			endpoint: "0.0.0.0",
			port:     ports.HTTPLocalHost,
			code:     http.StatusServiceUnavailable,
		},
		{
			name:     "localhost IP with localhost sidecar",
			endpoint: "127.0.0.1",
			port:     ports.HTTPLocalHost,
			code:     http.StatusOK,
		},
		{
			name:     "localhost IP with empty sidecar",
			endpoint: "",
			port:     ports.HTTPLocalHost,
			code:     http.StatusServiceUnavailable,
		},

		// Wildcard bind
		{
			name:           "wildcard IP without sidecar",
			disableSidecar: true,
			port:           ports.HTTP,
			code:           http.StatusOK,
		},
		{
			name:     "wildcard IP with wildcard sidecar",
			endpoint: "0.0.0.0",
			port:     ports.HTTP,
			code:     http.StatusOK,
		},
		{
			name:     "wildcard IP with localhost sidecar",
			endpoint: "127.0.0.1",
			port:     ports.HTTP,
			code:     http.StatusOK,
		},
		{
			name:     "wildcard IP with empty sidecar",
			endpoint: "",
			port:     ports.HTTP,
			code:     http.StatusOK,
		},
	}
	for _, ipCase := range ipCases {
		for _, client := range t.Apps.A {
			to := t.Apps.B
			var config string
			if !ipCase.disableSidecar {
				config = fmt.Sprintf(`
apiVersion: networking.istio.io/v1
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
`, ipCase.port.WorkloadPort, ipCase.endpoint, ipCase.port.WorkloadPort)
			}
			t.RunTraffic(TrafficTestCase{
				name:   ipCase.name,
				call:   client.CallOrFail,
				config: config,
				opts: echo.CallOptions{
					Count:   1,
					To:      to,
					Port:    ipCase.port,
					Scheme:  scheme.HTTP,
					Timeout: time.Second * 5,
					Check:   check.Status(ipCase.code),
				},
				minIstioVersion: ipCase.minIstioVersion,
			})
		}
	}
}

type vmCase struct {
	name string
	from echo.Instance
	to   echo.Instances
	host string
}

func DNSTestCases(t TrafficContext) {
	makeSE := func(ips ...string) string {
		return tmpl.MustEvaluate(`
apiVersion: networking.istio.io/v1
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
  endpoints:
  - address: "10.0.0.1"
  ports:
  - number: 80
    name: http
    protocol: HTTP
`, map[string]any{"IPs": ips})
	}
	ipv4 := []string{"1.2.3.4", "1.2.3.5"}
	ipv6 := []string{"1234:1234:1234::1234:1234:1234", "1235:1235:1235::1235:1235:1235"}
	dummyLocalhostServer := "127.0.0.1"

	for _, client := range flatten(t.Apps.VM, t.Apps.A, t.Apps.Tproxy) {
		v4, v6 := getSupportedIPFamilies(t, client)
		log := scopes.Framework.WithLabels("client", client.ServiceName())

		expectedIPv4 := ipv4
		expectedIPv6 := ipv6
		log.Infof("v4=%v v6=%v wantv4=%v wantv6=%v", v4, v6, expectedIPv4, expectedIPv6)
		cases := []struct {
			name     string
			ips      []string
			protocol string
			server   string
			skipCNI  bool
			expected []string
		}{
			{
				name:     "tcp ipv4",
				ips:      ipv4,
				expected: expectedIPv4,
				protocol: "tcp",
			},
			{
				name:     "udp ipv4",
				ips:      ipv4,
				expected: expectedIPv4,
				protocol: "udp",
			},
			{
				name:     "tcp ipv6",
				ips:      ipv6,
				expected: expectedIPv6,
				protocol: "tcp",
			},
			{
				name:     "udp ipv6",
				ips:      ipv6,
				expected: expectedIPv6,
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
		for _, tt := range cases {
			if tt.skipCNI && t.Istio.Settings().EnableCNI {
				continue
			}
			address := "fake.service.local?"
			if tt.protocol != "" {
				address += "&protocol=" + tt.protocol
			}
			if tt.server != "" {
				address += "&server=" + tt.server
			}
			var checker echo.Checker = func(result echo.CallResult, _ error) error {
				if len(result.Responses) == 0 {
					return fmt.Errorf("no responses")
				}
				for _, r := range result.Responses {
					if !sets.New(r.Body()...).Equals(sets.New(tt.expected...)) {
						return fmt.Errorf("unexpected dns response: wanted %v, got %v", tt.expected, r.Body())
					}
				}
				return nil
			}
			if tt.expected == nil {
				checker = check.Error()
			}
			t.RunTraffic(TrafficTestCase{
				name:   fmt.Sprintf("%s/%s", client.Config().Service, tt.name),
				config: makeSE(tt.ips...),
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
	for _, client := range flatten(t.Apps.VM, t.Apps.A, t.Apps.Tproxy) {
		for _, tt := range svcCases {
			aInCluster := match.Cluster(client.Config().Cluster).GetMatches(t.Apps.A)
			if len(aInCluster) == 0 {
				// The cluster doesn't contain A, but connects to a cluster containing A
				aInCluster = match.Cluster(client.Config().Cluster.Config()).GetMatches(t.Apps.A)
			}
			address := aInCluster[0].Config().ClusterLocalFQDN() + "?"
			if tt.protocol != "" {
				address += "&protocol=" + tt.protocol
			}
			if tt.server != "" {
				address += "&server=" + tt.server
			}
			expected := aInCluster[0].Addresses()
			t.RunTraffic(TrafficTestCase{
				name: fmt.Sprintf("svc/%s/%s/%s", client.Config().Service, client.Config().Cluster.StableName(), tt.name),
				call: client.CallOrFail,
				opts: echo.CallOptions{
					Count:   1,
					Scheme:  scheme.DNS,
					Address: address,
					Check: func(result echo.CallResult, _ error) error {
						for _, r := range result.Responses {
							ips := r.Body()
							sort.Strings(expected)
							if !reflect.DeepEqual(ips, expected) {
								return fmt.Errorf("unexpected dns response: wanted %v, got %v", expected, ips)
							}
						}
						return nil
					},
				},
			})
		}
	}
}

func VMTestCases(vms echo.Instances) func(t TrafficContext) {
	return func(t TrafficContext) {
		if t.Settings().Skip(echo.VM) {
			t.Skip("VMs are disabled")
		}
		var testCases []vmCase

		for _, vm := range vms {
			testCases = append(testCases,
				vmCase{
					name: "dns: VM to k8s cluster IP service name.namespace host",
					from: vm,
					to:   t.Apps.A,
					host: deployment.ASvc + "." + t.Apps.Namespace.Name(),
				},
				vmCase{
					name: "dns: VM to k8s cluster IP service fqdn host",
					from: vm,
					to:   t.Apps.A,
					host: t.Apps.A[0].Config().ClusterLocalFQDN(),
				},
				vmCase{
					name: "dns: VM to k8s cluster IP service short name host",
					from: vm,
					to:   t.Apps.A,
					host: deployment.ASvc,
				},
				vmCase{
					name: "dns: VM to k8s headless service",
					from: vm,
					to:   match.Cluster(vm.Config().Cluster.Config()).GetMatches(t.Apps.Headless),
					host: t.Apps.Headless.Config().ClusterLocalFQDN(),
				},
				vmCase{
					name: "dns: VM to k8s statefulset service",
					from: vm,
					to:   match.Cluster(vm.Config().Cluster.Config()).GetMatches(t.Apps.StatefulSet),
					host: t.Apps.StatefulSet.Config().ClusterLocalFQDN(),
				},
				// TODO(https://github.com/istio/istio/issues/32552) re-enable
				//vmCase{
				//	name: "dns: VM to k8s statefulset instance.service",
				//	from: vm,
				//	to:   apps.StatefulSet.Match(echo.Cluster(vm.Config().Cluster.Config())),
				//	host: fmt.Sprintf("%s-v1-0.%s", StatefulSetSvc, StatefulSetSvc),
				//},
				//vmCase{
				//	name: "dns: VM to k8s statefulset instance.service.namespace",
				//	from: vm,
				//	to:   apps.StatefulSet.Match(echo.Cluster(vm.Config().Cluster.Config())),
				//	host: fmt.Sprintf("%s-v1-0.%s.%s", StatefulSetSvc, StatefulSetSvc, apps.Namespace.Name()),
				//},
				//vmCase{
				//	name: "dns: VM to k8s statefulset instance.service.namespace.svc",
				//	from: vm,
				//	to:   apps.StatefulSet.Match(echo.Cluster(vm.Config().Cluster.Config())),
				//	host: fmt.Sprintf("%s-v1-0.%s.%s.svc", StatefulSetSvc, StatefulSetSvc, apps.Namespace.Name()),
				//},
				//vmCase{
				//	name: "dns: VM to k8s statefulset instance FQDN",
				//	from: vm,
				//	to:   apps.StatefulSet.Match(echo.Cluster(vm.Config().Cluster.Config())),
				//	host: fmt.Sprintf("%s-v1-0.%s", StatefulSetSvc, apps.StatefulSet[0].Config().ClusterLocalFQDN()),
				//},
			)
		}
		for _, podA := range t.Apps.A {
			testCases = append(testCases, vmCase{
				name: "k8s to vm",
				from: podA,
				to:   vms,
			})
		}
		for _, c := range testCases {
			checker := check.OK()
			if !match.Headless.Any(c.to) {
				// headless load-balancing can be inconsistent
				checker = check.And(checker, check.ReachedTargetClusters(t))
			}
			t.RunTraffic(TrafficTestCase{
				name: fmt.Sprintf("%s from %s", c.name, c.from.Config().Cluster.StableName()),
				call: c.from.CallOrFail,
				opts: echo.CallOptions{
					// assume that all echos in `to` only differ in which cluster they're deployed in
					To: c.to,
					Port: echo.Port{
						Name: "http",
					},
					Address: c.host,
					Check:   checker,
				},
			})
		}
	}
}

func TestExternalService(t TrafficContext) {
	// Let us enable outboundTrafficPolicy REGISTRY_ONLY
	// on one of the workloads, to verify selective external connectivity
	SidecarScope := fmt.Sprintf(`apiVersion: networking.istio.io/v1
kind: Sidecar
metadata:
  name: restrict-external-service
  namespace: %s
spec:
  workloadSelector:
    labels:
      app: a
  outboundTrafficPolicy:
    mode: "REGISTRY_ONLY"
`, t.Apps.EchoNamespace.Namespace.Name())

	if len(t.Apps.External.All) == 0 {
		t.Skip("no external service instances")
	}
	fakeExternalAddress := t.Apps.External.All[0].Address()
	parsedIP, err := netip.ParseAddr(fakeExternalAddress)
	if err == nil {
		if parsedIP.Is6() {
			// CI has some issues with ipv6 DNS resolution, due to which
			// we are not able to directly use the host name in the connectivity tests.
			// Hence using a bogus IPv6 address with -HHost option as a workaround to
			// simulate ServiceEntry based wildcard listener scenarios.
			fakeExternalAddress = "2002::1"
		} else {
			fakeExternalAddress = "1.1.1.1"
		}
	}

	testCases := []struct {
		name       string
		statusCode int
		from       echo.Instances
		to         string
		protocol   protocol.Instance
		port       int
	}{
		// TC1: Test connectivity to external service from outboundTrafficPolicy restricted pod.
		// The external service is exposed through a ServiceEntry, so the traffic should go through
		{
			name:       "traffic from outboundTrafficPolicy REGISTRY_ONLY to allowed host",
			statusCode: http.StatusOK,
			from:       t.Apps.A,
			to:         t.Apps.External.All[0].Address(),
			protocol:   protocol.HTTPS,
			port:       443,
		},
		// TC2: Same test as TC1, but use a fake external ip in destination for connectivity.
		{
			name:       "traffic from outboundTrafficPolicy REGISTRY_ONLY to allowed host",
			statusCode: http.StatusOK,
			from:       t.Apps.A,
			to:         fakeExternalAddress,
			protocol:   protocol.HTTP,
			port:       80,
		},
		// TC3: Test connectivity to external service from outboundTrafficPolicy=PASS_THROUGH pod.
		// Traffic should go through without the need for any explicit ServiceEntry
		{
			name:       "traffic from outboundTrafficPolicy PASS_THROUGH to any host",
			statusCode: http.StatusOK,
			from:       t.Apps.B,
			to:         t.Apps.External.All[0].Address(),
			protocol:   protocol.HTTP,
			port:       80,
		},
		// TC4: Same test as TC3, but use a fake external ip in destination for connectivity.
		{
			name:       "traffic from outboundTrafficPolicy PASS_THROUGH to any host",
			statusCode: http.StatusOK,
			from:       t.Apps.B,
			to:         fakeExternalAddress,
			protocol:   protocol.HTTP,
			port:       80,
		},
	}

	for _, tc := range testCases {
		t.RunTraffic(TrafficTestCase{
			name:   fmt.Sprintf("%v to external service %v", tc.from[0].NamespacedName(), tc.to),
			config: SidecarScope,
			opts: echo.CallOptions{
				Address: tc.to,
				HTTP: echo.HTTP{
					Headers: HostHeader(t.Apps.External.All[0].Config().DefaultHostHeader),
				},
				Port: echo.Port{Protocol: tc.protocol, ServicePort: tc.port},
				Check: check.And(
					check.Status(tc.statusCode),
				),
			},
			call: tc.from[0].CallOrFail,
		})
	}
}

func testServiceEntryWithMultipleVIPsAndResolutionNone(t TrafficContext) {
	if len(t.Apps.External.All) == 0 {
		t.Skip("no external service instances")
	}

	clusterIPs := createService(t, "external", t.Apps.External.All.NamespaceName(), "external", 2)
	if len(clusterIPs) < 2 {
		t.Errorf("failed to get 2 cluster IPs, got %v", clusterIPs)
	}

	// Create CIDRs from IPs. We want to check if CIDR matching in a wildcard listener works.
	var cidrs []string
	for _, clusterIP := range clusterIPs {
		if ip, err := netip.ParseAddr(clusterIP); err != nil {
			t.Errorf("failed to parse cluster IP address '%s': %v", clusterIP, err)
		} else if ip.Is4() {
			cidrs = append(cidrs, fmt.Sprintf("%s/24", clusterIP))
		} else if ip.Is6() {
			cidrs = append(cidrs, clusterIP)
		}
	}

	serviceEntry := tmpl.MustEvaluate(`
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: server-default-svc
  namespace: {{.Namespace}}
spec:
  exportTo:
  - "."
  hosts:
  - server.default.svc
  addresses:
{{ range $ip := .IPs }}
  - "{{$ip}}"
{{ end }}
  location: MESH_EXTERNAL
  ports:
  - number: 443
    name: tcp
    protocol: TCP
  resolution: NONE
`, map[string]any{"Namespace": t.Apps.A.NamespaceName(), "IPs": cidrs})

	t.RunTraffic(TrafficTestCase{
		name:   "send a request to one of the service entry VIPs",
		config: serviceEntry,
		opts: echo.CallOptions{
			// Use second IP address to make sure that Istio matches all CIDRs, not only the first one.
			Address: clusterIPs[1],
			Port: echo.Port{
				Protocol:    protocol.HTTPS,
				ServicePort: 443,
			},
			Check: check.Status(http.StatusOK),
		},
		call: t.Apps.A[0].CallOrFail,
	})
}

func destinationRule(app, mode string) string {
	return fmt.Sprintf(`apiVersion: networking.istio.io/v1
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

const useClientProtocolDestinationRuleTmpl = `apiVersion: networking.istio.io/v1
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
	return fmt.Sprintf(`apiVersion: networking.istio.io/v1
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
	return fmt.Sprintf(`apiVersion: security.istio.io/v1
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
	return fmt.Sprintf(`apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: default
spec:
  mtls:
    mode: %s
---
`, mode)
}

func serverFirstTestCases(t TrafficContext) {
	from := t.Apps.A
	to := t.Apps.C
	configs := []struct {
		port    string
		dest    string
		auth    string
		checker echo.Checker
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
			t.RunTraffic(TrafficTestCase{
				name: fmt.Sprintf("%v:%v/%v", c.port, c.dest, c.auth),
				skip: skip{
					skip:   t.Apps.All.Instances().Clusters().IsMulticluster(),
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
}

func jwtClaimRoute(t TrafficContext) {
	if t.Settings().Selector.Excludes(label.NewSet(label.IPv4)) {
		t.Skip("https://github.com/istio/istio/issues/35835")
	}
	configRoute := `
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: gateway
spec:
  selector:
    istio: {{.GatewayIstioLabel | default "ingressgateway"}}
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
---
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: default
spec:
  hosts:
  - {{ .dstSvc }}.foo.bar
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
apiVersion: security.istio.io/v1
kind: RequestAuthentication
metadata:
  name: default
  namespace: {{.SystemNamespace | default "istio-system"}}
spec:
  jwtRules:
  - issuer: "test-issuer-1@istio.io"
    jwksUri: "https://raw.githubusercontent.com/istio/istio/master/tests/common/jwt/jwks.json"
    outputClaimToHeaders:
    - header: "x-jwt-nested-key"
      claim: "nested.nested-2.key2"
    - header: "x-jwt-iss"
      claim: "iss"
    - header: "x-jwt-wrong-header"
      claim: "wrong_claim"
---
`
	matchers := []match.Matcher{match.And(
		// No waypoint here, these are all via ingress which doesn't forward to waypoint
		match.NotWaypoint,
		match.Or(match.ServiceName(t.Apps.B.NamespacedName()), match.AmbientCaptured()),
	)}
	headersWithToken := map[string][]string{
		"Authorization": {"Bearer " + jwt.TokenIssuer1WithNestedClaims1},
	}
	headersWithInvalidToken := map[string][]string{
		"Authorization": {"Bearer " + jwt.TokenExpired},
	}
	headersWithNoToken := map[string][]string{"Host": {"foo.bar"}}
	headersWithNoTokenButSameHeader := map[string][]string{
		"request.auth.claims.nested.key1": {"valueA"},
	}
	headersWithToken2 := map[string][]string{
		"Authorization":    {"Bearer " + jwt.TokenIssuer1WithNestedClaims2},
		"X-Jwt-Nested-Key": {"value_to_be_replaced"},
	}
	headersWithToken2WithAddedHeader := map[string][]string{
		"Authorization":      {"Bearer " + jwt.TokenIssuer1WithNestedClaims2},
		"x-jwt-wrong-header": {"header_to_be_deleted"},
	}
	headersWithToken3 := map[string][]string{
		"Authorization": {"Bearer " + jwt.TokenIssuer1WithCollisionResistantName},
	}
	// the VirtualService for each test should be unique to avoid
	// one test passing because it's new config hasn't kicked in yet
	// and we're still testing the previous destination
	setHostHeader := func(src echo.Caller, opts *echo.CallOptions) {
		opts.HTTP.Headers["Host"] = []string{opts.To.ServiceName() + ".foo.bar"}
	}

	type configData struct {
		Name, Match, Value string
	}

	t.RunTraffic(TrafficTestCase{
		name:             "matched with nested claim using claim to header:200",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers":           []configData{{"X-Jwt-Nested-Key", "exact", "valueC"}},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
			}
		},
		opts: echo.CallOptions{
			Count: 1,
			Port: echo.Port{
				Name:     "http",
				Protocol: protocol.HTTP,
			},
			HTTP: echo.HTTP{
				Headers: headersWithToken2,
			},
			Check: check.Status(http.StatusOK),
		},
		setupOpts: setHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		name:             "matched with nested claim and single claim using claim to header:200",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers": []configData{
					{"X-Jwt-Nested-Key", "exact", "valueC"},
					{"X-Jwt-Iss", "exact", "test-issuer-1@istio.io"},
				},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
			}
		},
		opts: echo.CallOptions{
			Count: 1,
			Port: echo.Port{
				Name:     "http",
				Protocol: protocol.HTTP,
			},
			HTTP: echo.HTTP{
				Headers: headersWithToken2,
			},
			Check: check.Status(http.StatusOK),
		},
		setupOpts: setHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		name:             "unmatched with wrong claim and added header:404",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers":           []configData{{"x-jwt-wrong-header", "exact", "header_to_be_deleted"}},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
			}
		},
		opts: echo.CallOptions{
			Count: 1,
			Port: echo.Port{
				Name:     "http",
				Protocol: protocol.HTTP,
			},
			HTTP: echo.HTTP{
				Headers: headersWithToken2WithAddedHeader,
			},
			Check: check.Status(http.StatusNotFound),
		},
		setupOpts: setHostHeader,
	})

	// ---------------------------------------------
	// Usage 1: using `.` as a separator test cases
	// ---------------------------------------------

	t.RunTraffic(TrafficTestCase{
		name:             "matched with nested claims:200",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers":           []configData{{"@request.auth.claims.nested.key1", "exact", "valueA"}},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
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
		setupOpts: setHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		name:             "matched with single claim:200",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers":           []configData{{"@request.auth.claims.sub", "prefix", "sub"}},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
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
		setupOpts: setHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		name:             "matched multiple claims with regex:200",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers": []configData{
					{"@request.auth.claims.sub", "regex", "(\\W|^)(sub-1|sub-2)(\\W|$)"},
					{"@request.auth.claims.nested.key1", "regex", "(\\W|^)value[AB](\\W|$)"},
				},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
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
		setupOpts: setHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		name:             "matched multiple claims:200",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers": []configData{
					{"@request.auth.claims.nested.key1", "exact", "valueA"},
					{"@request.auth.claims.sub", "prefix", "sub"},
				},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
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
		setupOpts: setHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		name:             "matched without claim:200",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"WithoutHeaders":    []configData{{"@request.auth.claims.nested.key1", "exact", "value-not-matched"}},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
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
		setupOpts: setHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		name:             "unmatched without claim:404",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"WithoutHeaders":    []configData{{"@request.auth.claims.nested.key1", "exact", "valueA"}},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
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
		setupOpts: setHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		name:             "matched both with and without claims with regex:200",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers": []configData{{"@request.auth.claims.sub", "prefix", "sub"}},
				"WithoutHeaders": []configData{
					{"@request.auth.claims.nested.key1", "exact", "value-not-matched"},
					{"@request.auth.claims.nested.key1", "regex", "(\\W|^)value\\s{0,3}not{0,1}\\s{0,3}matched(\\W|$)"},
				},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
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
		setupOpts: setHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		name:             "unmatched multiple claims:404",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers": []configData{
					{"@request.auth.claims.nested.key1", "exact", "valueA"},
					{"@request.auth.claims.sub", "prefix", "value-not-matched"},
				},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
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
		setupOpts: setHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		name:             "unmatched token:404",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers":           []configData{{"@request.auth.claims.sub", "exact", "value-not-matched"}},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
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
		setupOpts: setHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		name:             "unmatched with invalid token:401",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers":           []configData{{"@request.auth.claims.nested.key1", "exact", "valueA"}},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
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
		setupOpts: setHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		name:             "unmatched with no token:404",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers":           []configData{{"@request.auth.claims.nested.key1", "exact", "valueA"}},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
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
		setupOpts: setHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		name:             "unmatched with no token but same header:404",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers":           []configData{{"@request.auth.claims.nested.key1", "exact", "valueA"}},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
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
		setupOpts: setHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		name:             "unmatched with no request authentication:404",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configRoute,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers":           []configData{{"@request.auth.claims.nested.key1", "exact", "valueA"}},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
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
		setupOpts: setHostHeader,
	})

	// ---------------------------------------------
	// Usage 2: using `[]` as a separator test cases
	// ---------------------------------------------

	t.RunTraffic(TrafficTestCase{
		name:             "usage2: matched with nested claims:200",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers":           []configData{{"@request.auth.claims[nested][key1]", "exact", "valueA"}},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
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
		setupOpts: setHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		name:             "usage2: matched with single claim:200",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers":           []configData{{"@request.auth.claims[sub]", "prefix", "sub"}},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
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
		setupOpts: setHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		name:             "usage2: matched multiple claims with regex:200",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers": []configData{
					{"@request.auth.claims[sub]", "regex", "(\\W|^)(sub-1|sub-2)(\\W|$)"},
					{"@request.auth.claims[nested][key1]", "regex", "(\\W|^)value[AB](\\W|$)"},
				},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
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
		setupOpts: setHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		name:             "usage2: matched multiple claims:200",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers": []configData{
					{"@request.auth.claims[nested][key1]", "exact", "valueA"},
					{"@request.auth.claims[sub]", "prefix", "sub"},
				},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
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
		setupOpts: setHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		name:             "usage2: matched without claim:200",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"WithoutHeaders":    []configData{{"@request.auth.claims[nested][key1]", "exact", "value-not-matched"}},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
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
		setupOpts: setHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		name:             "usage2: unmatched without claim:404",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"WithoutHeaders":    []configData{{"@request.auth.claims[nested][key1]", "exact", "valueA"}},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
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
		setupOpts: setHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		name:             "usage2: matched both with and without claims with regex:200",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers": []configData{{"@request.auth.claims[sub]", "prefix", "sub"}},
				"WithoutHeaders": []configData{
					{"@request.auth.claims[nested][key1]", "exact", "value-not-matched"},
					{"@request.auth.claims[nested][key1]", "regex", "(\\W|^)value\\s{0,3}not{0,1}\\s{0,3}matched(\\W|$)"},
				},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
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
		setupOpts: setHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		name:             "usage2: unmatched multiple claims:404",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers": []configData{
					{"@request.auth.claims[nested][key1]", "exact", "valueA"},
					{"@request.auth.claims[sub]", "prefix", "value-not-matched"},
				},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
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
		setupOpts: setHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		name:             "usage2: unmatched token:404",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers":           []configData{{"@request.auth.claims[sub]", "exact", "value-not-matched"}},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
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
		setupOpts: setHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		name:             "usage2: unmatched with invalid token:401",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers":           []configData{{"@request.auth.claims[nested][key1]", "exact", "valueA"}},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
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
		setupOpts: setHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		name:             "usage2: unmatched with no token:404",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers":           []configData{{"@request.auth.claims[nested][key1]", "exact", "valueA"}},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
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
		setupOpts: setHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		name:             "usage2: unmatched with no token but same header:404",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers":           []configData{{"@request.auth.claims[nested][key1]", "exact", "valueA"}},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
			}
		},
		opts: echo.CallOptions{
			Count: 1,
			Port: echo.Port{
				Name:     "http",
				Protocol: protocol.HTTP,
			},
			HTTP: echo.HTTP{
				// Include a header @request.auth.claims[nested][key1] and value same as the JWT claim, should not be routed.
				Headers: headersWithNoTokenButSameHeader,
			},
			Check: check.Status(http.StatusNotFound),
		},
		setupOpts: setHostHeader,
	})
	t.RunTraffic(TrafficTestCase{
		name:             "usage2: unmatched with no request authentication:404",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configRoute,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers":           []configData{{"@request.auth.claims[nested][key1]", "exact", "valueA"}},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
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
		setupOpts: setHostHeader,
	})

	t.RunTraffic(TrafficTestCase{
		name:             "usage2: matched with simple collision-resistant claim name:200",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers":           []configData{{"@request.auth.claims[test-issuer-1@istio.io/simple]", "exact", "valueC"}},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
			}
		},
		opts: echo.CallOptions{
			Count: 1,
			Port: echo.Port{
				Name:     "http",
				Protocol: protocol.HTTP,
			},
			HTTP: echo.HTTP{
				Headers: headersWithToken3,
			},
			Check: check.Status(http.StatusOK),
		},
		setupOpts: setHostHeader,
	})

	t.RunTraffic(TrafficTestCase{
		name:             "usage2: unmatched with simple collision-resistant claim name:404",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers":           []configData{{"@request.auth.claims[test-issuer-1@istio.io/simple]", "exact", "value-not-matched"}},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
			}
		},
		opts: echo.CallOptions{
			Count: 1,
			Port: echo.Port{
				Name:     "http",
				Protocol: protocol.HTTP,
			},
			HTTP: echo.HTTP{
				Headers: headersWithToken3,
			},
			Check: check.Status(http.StatusNotFound),
		},
		setupOpts: setHostHeader,
	})

	t.RunTraffic(TrafficTestCase{
		name:             "usage2: matched with nested collision-resistant claim name:200",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers":           []configData{{"@request.auth.claims[test-issuer-1@istio.io/nested][key1]", "exact", "valueC"}},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
			}
		},
		opts: echo.CallOptions{
			Count: 1,
			Port: echo.Port{
				Name:     "http",
				Protocol: protocol.HTTP,
			},
			HTTP: echo.HTTP{
				Headers: headersWithToken3,
			},
			Check: check.Status(http.StatusOK),
		},
		setupOpts: setHostHeader,
	})

	t.RunTraffic(TrafficTestCase{
		name:             "usage2: unmatched with nested collision-resistant claim name:404",
		targetMatchers:   matchers,
		workloadAgnostic: true,
		viaIngress:       true,
		config:           configAll,
		templateVars: func(src echo.Callers, dest echo.Instances) map[string]any {
			return map[string]any{
				"Headers":           []configData{{"@request.auth.claims[test-issuer-1@istio.io/nested][key1]", "exact", "value-not-matched"}},
				"SystemNamespace":   t.Istio.Settings().SystemNamespace,
				"GatewayIstioLabel": t.Istio.Settings().IngressGatewayIstioLabel,
			}
		},
		opts: echo.CallOptions{
			Count: 1,
			Port: echo.Port{
				Name:     "http",
				Protocol: protocol.HTTP,
			},
			HTTP: echo.HTTP{
				Headers: headersWithToken3,
			},
			Check: check.Status(http.StatusNotFound),
		},
		setupOpts: setHostHeader,
	})
}

func LocationHeader(expected string) echo.Checker {
	return check.Each(
		func(r echoClient.Response) error {
			originalHostname, err := url.Parse(r.RequestURL)
			if err != nil {
				return err
			}
			exp := tmpl.MustEvaluate(expected, map[string]string{
				"Hostname": originalHostname.Hostname(),
			})
			return ExpectString(r.ResponseHeaders.Get("Location"),
				exp,
				"Location")
		})
}

// createService creates additional Services for a given app to obtain routable VIPs inside the cluster.
func createService(t TrafficContext, name, ns, appLabelValue string, instances int) []string {
	var clusterIPs []string
	for i := 0; i < instances; i++ {
		svcName := fmt.Sprintf("%s-vip-%d", name, i)
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcName,
				Namespace: ns,
				Annotations: map[string]string{
					// Export the service nowhere, so that no proxy will receive it or its VIP.
					annotation.NetworkingExportTo.Name: "~",
				},
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{
					"app": appLabelValue,
				},
				Type: corev1.ServiceTypeClusterIP,
			},
		}
		for _, p := range ports.All() {
			if p.ServicePort == echo.NoServicePort {
				continue
			}
			svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{
				Name:       p.Name,
				Protocol:   corev1.ProtocolTCP,
				Port:       int32(p.ServicePort),
				TargetPort: intstr.IntOrString{IntVal: int32(ports.HTTPS.WorkloadPort)},
			})
		}
		_, err := t.Clusters().Default().Kube().CoreV1().Services(ns).Create(context.TODO(), svc, metav1.CreateOptions{})
		if err != nil && !kerrors.IsAlreadyExists(err) {
			t.Errorf("failed to create service %s: %s", svc, err)
		}

		// Wait until a ClusterIP has been assigned.
		err = retry.UntilSuccess(func() error {
			svc, err := t.Clusters().Default().Kube().CoreV1().Services(ns).Get(context.TODO(), svcName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if len(svc.Spec.ClusterIPs) == 0 {
				return fmt.Errorf("service VIP not set for service")
			}
			clusterIPs = append(clusterIPs, svc.Spec.ClusterIPs...)
			return nil
		}, retry.Timeout(10*time.Second))
		if err != nil {
			t.Errorf("failed to assign ClusterIP for %s service: %s", svcName, err)
		}
	}
	return clusterIPs
}

func getSupportedIPFamilies(t framework.TestContext, instance echo.Instance) (v4 bool, v6 bool) {
	for _, a := range instance.WorkloadsOrFail(t).Addresses() {
		ip, err := netip.ParseAddr(a)
		assert.NoError(t, err)
		if ip.Is4() {
			v4 = true
		} else if ip.Is6() {
			v6 = true
		}
	}
	if !v4 && !v6 {
		t.Fatalf("pod is neither v4 nor v6? %v", instance.WorkloadsOrFail(t).Addresses())
	}
	return v4, v6
}
