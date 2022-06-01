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

package uproxy

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	echot "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
)

func IsL7() echo.Checker {
	return check.Each(func(r echot.Response) error {
		// TODO: response headers?
		_, f := r.RequestHeaders[http.CanonicalHeaderKey("x-b3-traceid")]
		if !f {
			return fmt.Errorf("x-b3-traceid not set, is L7 processing enabled?")
		}
		return nil
	})
}

func IsL4() echo.Checker {
	return check.Each(func(r echot.Response) error {
		// TODO: response headers?
		_, f := r.RequestHeaders[http.CanonicalHeaderKey("x-b3-traceid")]
		if f {
			return fmt.Errorf("x-b3-traceid set, is L7 processing enabled unexpectedly?")
		}
		return nil
	})
}

var (
	httpValidator = check.And(check.OK(), IsL7())
	tcpValidator  = check.And(check.OK(), IsL4())
	callOptions   = []echo.CallOptions{
		{
			Port:    echo.Port{Name: "http"},
			Scheme:  scheme.HTTP,
			Count:   1, // TODO use more
			Timeout: time.Second * 2,
		},
		//{
		//	Port: echo.Port{Name: "http"},
		//	Scheme:   scheme.WebSocket,
		//	Count:    4,
		//	Timeout:  time.Second * 2,
		//},
		{
			Port:    echo.Port{Name: "tcp"},
			Scheme:  scheme.TCP,
			Count:   1,
			Timeout: time.Second * 2,
		},
		//{
		//	Port: echo.Port{Name: "grpc"},
		//	Scheme:   scheme.GRPC,
		//	Count:    4,
		//	Timeout:  time.Second * 2,
		//},
		//{
		//	Port: echo.Port{Name: "https"},
		//	Scheme:   scheme.HTTPS,
		//	Count:    4,
		//	Timeout:  time.Second * 2,
		//},
	}
)

func supportsL7(opt echo.CallOptions, instances ...echo.Instance) bool {
	hasRemote := false
	hasSidecar := false
	for _, i := range instances {
		hasRemote = hasRemote || i.Config().IsRemote()
		hasSidecar = hasSidecar || i.Config().HasSidecar()
	}
	isL7Scheme := opt.Scheme == scheme.HTTP || opt.Scheme == scheme.GRPC || opt.Scheme == scheme.WebSocket
	return (hasRemote || hasSidecar) && isL7Scheme
}

func TestServices(t *testing.T) {
	runTest(t, func(t framework.TestContext, src echo.Instance, dst echo.Instance, opt echo.CallOptions) {
		if supportsL7(opt, src, dst) {
			opt.Check = httpValidator
		} else {
			opt.Check = tcpValidator
		}

		if src.Config().IsUncaptured() && dst.Config().IsRemote() {
			// For this case, it is broken if the src and dst are on the same node.
			// Because client request is not captured to perform the hairpin
			// TODO: fix this and remove this skip
			opt.Check = check.OK()
		}
		// TODO test from all source workloads as well
		src.CallOrFail(t, opt)
	})
}

func TestPodIP(t *testing.T) {
	t.Skip("https://github.com/solo-io/istio-sidecarless/issues/106")
	framework.NewTest(t).Run(func(t framework.TestContext) {
		for _, src := range apps.All {
			for _, srcWl := range src.WorkloadsOrFail(t) {
				srcWl := srcWl
				t.NewSubTestf("from %v %v", src.Config().Service, srcWl.Address()).Run(func(t framework.TestContext) {
					for _, dst := range apps.All {
						for _, dstWl := range dst.WorkloadsOrFail(t) {
							t.NewSubTestf("to %v %v", dst.Config().Service, dstWl.Address()).Run(func(t framework.TestContext) {
								src, dst, srcWl, dstWl := src, dst, srcWl, dstWl
								if src.Config().IsRemote() || dst.Config().IsRemote() {
									t.Skip("not supported yet")
								}
								for _, opt := range callOptions {
									opt := opt.DeepCopy()
									selfSend := dstWl.Address() == srcWl.Address()
									if supportsL7(opt, src, dst) {
										opt.Check = httpValidator
									} else {
										opt.Check = tcpValidator
									}
									if selfSend {
										// Calls to ourself (by pod IP) are not captured
										opt.Check = tcpValidator
									} else if src.Config().IsUncaptured() && dst.Config().IsRemote() {
										// For this case, it is broken if the src and dst are on the same node.
										// Because client request is not captured to perform the hairpin
										// TODO: fix this and remove this skip
										opt.Check = check.OK()
									}
									opt.Address = dstWl.Address()
									opt.Check = check.And(opt.Check, check.Hostname(dstWl.PodName()))

									opt.Port = echo.Port{ServicePort: ports.All().MustForName(opt.Port.Name).WorkloadPort}
									opt.ToWorkload = dst.WithWorkloads(dstWl)
									t.NewSubTestf("%v", opt.Scheme).RunParallel(func(t framework.TestContext) {
										src.WithWorkloads(srcWl).CallOrFail(t, opt)
									})
								}
							})
						}
					}
				})
			}
		}
	})
}

func TestServerSideLB(t *testing.T) {
	t.Skipf("test not implemented yet")
	// TODO: test that naked client reusing connections will load balance
}

func TestServerRouting(t *testing.T) {
	runTest(t, func(t framework.TestContext, src echo.Instance, dst echo.Instance, opt echo.CallOptions) {
		// Need at least one remote and HTTP
		if opt.Scheme != scheme.HTTP {
			return
		}
		if !dst.Config().IsRemote() && !src.Config().IsRemote() {
			return
		}
		if src.Config().IsUncaptured() {
			// For this case, it is broken if the src and dst are on the same node.
			// TODO: fix this and remove this skip
			t.Skip("https://github.com/solo-io/istio-sidecarless/issues/103")
		}
		t.ConfigIstio().Eval(apps.Namespace.Name(), map[string]string{
			"Destination": dst.Config().Service,
		}, `apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: route
spec:
  hosts:
  - "{{.Destination}}"
  http:
  - headers:
      request:
        add:
          istio-custom-header: user-defined-value
    route:
    - destination:
        host: "{{.Destination}}"
`).ApplyOrFail(t)
		opt.Check = check.And(
			check.OK(),
			check.RequestHeader("Istio-Custom-Header", "user-defined-value"))
		src.CallOrFail(t, opt)
	})
}

func TestAuthorization(t *testing.T) {
	runTest(t, func(t framework.TestContext, src echo.Instance, dst echo.Instance, opt echo.CallOptions) {
		if dst.Config().IsUncaptured() {
			// No destination means no RBAC to apply
			return
		}
		if opt.Scheme != scheme.HTTP {
			// TODO: add TCP
			t.Skip("https://github.com/solo-io/istio-sidecarless/issues/105")
		}
		if src.Config().IsUncaptured() {
			// For this case, it is broken if the src and dst are on the same node.
			// TODO: fix this and remove this skip
			t.Skip("https://github.com/solo-io/istio-sidecarless/issues/103")
		}
		if !dst.Config().IsRemote() {
			// Currently, uProxy inbound is just passthrough, no policy applied
			// TODO: fix this and remove this skip
			t.Skip("https://github.com/solo-io/istio-sidecarless/issues/104")
		}
		t.ConfigIstio().Eval(apps.Namespace.Name(), map[string]string{
			"Destination": dst.Config().Service,
		}, `apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
  name: policy
spec:
  selector:
    matchLabels:
      app: "{{ .Destination }}"
  rules:
  - to:
    - operation:
        paths: ["/allowed"]
        methods: ["GET"]`).ApplyOrFail(t)
		if src.Config().IsUncaptured() && dst.Config().IsRemote() {
			// For this case, it is broken if the src and dst are on the same node.
			// Because client request is not captured to perform the hairpin
			// TODO: fix this and remove this skip
			opt.Check = check.OK()
		}
		t.NewSubTest("deny").Run(func(t framework.TestContext) {
			opt = opt.DeepCopy()
			opt.HTTP.Path = "/deny"
			opt.Check = CheckDeny

			src.CallOrFail(t, opt)
		})
		t.NewSubTest("allow").Run(func(t framework.TestContext) {
			opt = opt.DeepCopy()
			opt.HTTP.Path = "/allowed"
			if supportsL7(opt, dst) {
				opt.Check = check.OK()
			} else {
				// If we do not support HTTP, we fail closed on HTTP policies
				opt.Check = CheckDeny
			}
			src.CallOrFail(t, opt)
		})
	})
}

var CheckDeny = check.Or(
	check.ErrorContains("rpc error: code = PermissionDenied"), // gRPC
	check.ErrorContains("EOF"),                                // TCP
	check.NoErrorAndStatus(http.StatusForbidden),              // HTTP
	check.NoErrorAndStatus(http.StatusServiceUnavailable),     // HTTP client, TCP server
)

func runTest(t *testing.T, f func(t framework.TestContext, src echo.Instance, dst echo.Instance, opt echo.CallOptions)) {
	framework.NewTest(t).Features("traffic.ambient").Run(func(t framework.TestContext) {
		svcs := apps.All
		for _, src := range svcs {
			t.NewSubTestf("from %v", src.Config().Service).Run(func(t framework.TestContext) {
				for _, dst := range svcs {
					t.NewSubTestf("to %v", dst.Config().Service).Run(func(t framework.TestContext) {
						for _, opt := range callOptions {
							src, dst, opt := src, dst, opt
							t.NewSubTestf("%v", opt.Scheme).Run(func(t framework.TestContext) {
								opt = opt.DeepCopy()
								opt.To = dst
								opt.Check = check.OK()
								f(t, src, dst, opt)
							})
						}
					})
				}
			})
		}
	})
}
