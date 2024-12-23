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

package ambient

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/ptr"
	echot "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/ambient"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/common"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/config"
	"istio.io/istio/pkg/test/framework/components/echo/config/param"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/framework/resource/config/cleanup"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/tests/common/jwt"
	"istio.io/istio/tests/integration/security/util/reachability"
	util "istio.io/istio/tests/integration/telemetry"
)

const (
	templateFile = "manifests/charts/istio-control/istio-discovery/files/waypoint.yaml"
)

func IsL7() echo.Checker {
	return check.Each(func(r echot.Response) error {
		// TODO: response headers?
		_, f := r.RequestHeaders[http.CanonicalHeaderKey("X-Request-Id")]
		if !f {
			return fmt.Errorf("X-Request-Id not set, is L7 processing enabled?")
		}
		return nil
	})
}

func IsL4() echo.Checker {
	return check.Each(func(r echot.Response) error {
		// TODO: response headers?
		_, f := r.RequestHeaders[http.CanonicalHeaderKey("X-Request-Id")]
		if f {
			return fmt.Errorf("X-Request-Id set, is L7 processing enabled unexpectedly?")
		}
		return nil
	})
}

var (
	httpValidator = check.And(check.OK(), IsL7())
	tcpValidator  = check.And(check.OK(), IsL4())
	callOptions   = []echo.CallOptions{
		{
			Port:   echo.Port{Name: "http"},
			Scheme: scheme.HTTP,
			Count:  10, // TODO use more
		},
		//{
		//	Port: echo.Port{Name: "http"},
		//	Scheme:   scheme.WebSocket,
		//	Count:    4,
		//	Timeout:  time.Second * 2,
		//},
		{
			Port:   echo.Port{Name: "tcp"},
			Scheme: scheme.TCP,
			Count:  1,
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

func OriginalSourceCheck(t framework.TestContext, src echo.Instance) echo.Checker {
	// Check that each response saw one of the workload IPs for the src echo instance
	addresses := sets.New(src.WorkloadsOrFail(t).Addresses()...)
	return check.Each(func(response echot.Response) error {
		if !addresses.Contains(response.IP) {
			return fmt.Errorf("expected original source (%v) to be propogated, but got %v", addresses.UnsortedList(), response.IP)
		}
		return nil
	})
}

func supportsL7(opt echo.CallOptions, src, dst echo.Instance) bool {
	s := src.Config().HasSidecar()
	d := dst.Config().HasSidecar() || dst.Config().HasAnyWaypointProxy()
	isL7Scheme := opt.Scheme == scheme.HTTP || opt.Scheme == scheme.GRPC || opt.Scheme == scheme.WebSocket
	return (s || d) && isL7Scheme
}

// Assumption is ambient test suite sidecars will support HBONE
// If the assumption is incorrect hboneClient may return invalid result
func hboneClient(instance echo.Instance) bool {
	return instance.Config().ZTunnelCaptured()
}

func TestServices(t *testing.T) {
	runTest(t, func(t framework.TestContext, src echo.Instance, dst echo.Instance, opt echo.CallOptions) {
		if supportsL7(opt, src, dst) {
			opt.Check = httpValidator
		} else {
			opt.Check = tcpValidator
		}

		if !dst.Config().HasServiceAddressedWaypointProxy() &&
			!src.Config().HasServiceAddressedWaypointProxy() &&
			(src.Config().Service != dst.Config().Service) &&
			!dst.Config().HasSidecar() {
			// Check original source, unless there is a waypoint in the path. For waypoint, we don't (yet?) propagate original src.
			// Self call is also (temporarily) broken
			// Sidecars lose the original src
			opt.Check = check.And(opt.Check, OriginalSourceCheck(t, src))
		}

		// Non-HBONE clients will attempt to bypass the waypoint
		if !src.Config().WaypointClient() && dst.Config().HasAnyWaypointProxy() && !src.Config().HasSidecar() {
			// TODO currently leads to no L7 processing, in the future it might be denied
			// opt.Check = check.Error()
			opt.Check = tcpValidator
		}

		// Any client will attempt to bypass a workload waypoint (not both service and workload waypoint)
		// because this test always addresses by service.
		if dst.Config().HasWorkloadAddressedWaypointProxy() && !dst.Config().HasServiceAddressedWaypointProxy() {
			// TODO currently leads to no L7 processing, in the future it might be denied
			// opt.Check = check.Error()
			opt.Check = tcpValidator
		}

		if src.Config().HasSidecar() && dst.Config().HasWorkloadAddressedWaypointProxy() {
			// We are testing to svc traffic but presently sidecar has not been updated to know that to svc traffic should not
			// go to a workload-attached waypoint
			t.Skip("https://github.com/istio/istio/pull/50182")
		}

		// TODO test from all source workloads as well
		src.CallOrFail(t, opt)
	})
}

func TestPodIP(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		for _, src := range apps.All {
			for _, srcWl := range src.WorkloadsOrFail(t) {
				srcWl := srcWl
				t.NewSubTestf("from %v %v", src.Config().Service, srcWl.Address()).Run(func(t framework.TestContext) {
					for _, dst := range apps.All {
						for _, dstWl := range dst.WorkloadsOrFail(t) {
							t.NewSubTestf("to %v %v", dst.Config().Service, dstWl.Address()).Run(func(t framework.TestContext) {
								src, dst, srcWl, dstWl := src, dst, srcWl, dstWl
								if src.Config().HasSidecar() {
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

									opt.Address = dstWl.Address()
									opt.Check = check.And(opt.Check, check.Hostname(dstWl.PodName()))

									opt.Port = echo.Port{ServicePort: ports.All().MustForName(opt.Port.Name).WorkloadPort}
									opt.ToWorkload = dst.WithWorkloads(dstWl)

									// Uncaptured means we won't traverse the waypoint
									// We cannot bypass the waypoint, so this fails.
									if !src.Config().WaypointClient() && dst.Config().HasAnyWaypointProxy() {
										// TODO currently leads to no L7 processing, in the future it might be denied
										// opt.Check = check.NotOK()
										opt.Check = tcpValidator
									}

									// Only marked to use service waypoint. We'll deny since it's not traversed.
									// Not traversed, since traffic is to-workload IP.
									if dst.Config().HasServiceAddressedWaypointProxy() && !dst.Config().HasWorkloadAddressedWaypointProxy() {
										// TODO currently leads to no L7 processing, in the future it might be denied
										// opt.Check = check.NotOK()
										opt.Check = tcpValidator
									}

									if selfSend {
										// Calls to ourself (by pod IP) are not captured
										opt.Check = tcpValidator
									}

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
	// TODO: test that naked client reusing connections will load balance
	runTest(t, func(t framework.TestContext, src echo.Instance, dst echo.Instance, opt echo.CallOptions) {
		if src.Config().ZTunnelCaptured() && dst.Config().HasWorkloadAddressedWaypointProxy() && !dst.Config().HasServiceAddressedWaypointProxy() {
			// This is to-service traffic without a service waypoint but with a workload waypoint
			// Ztunnel is going to specifically skip the workload waypoint because this is service addressed but
			// there is a later check for having a waypoint but not coming from a waypoint which drops the traffic.
			// That's a bug in ztunnel to sort out
			t.Skip("TODO: ztunnel bug will cause this to fail")
		}

		// Need HTTP
		if opt.Scheme != scheme.HTTP {
			return
		}
		if src.Config().IsUncaptured() {
			// For this case, it is broken if the src and dst are on the same node.
			// TODO: fix this and remove this skip
			t.Skip("broken")
		}
		var singleHost echo.Checker = func(result echo.CallResult, _ error) error {
			hostnames := make([]string, len(result.Responses))
			for i, r := range result.Responses {
				hostnames[i] = r.Hostname
			}
			unique := sets.SortedList(sets.New(hostnames...))
			if len(unique) != 1 {
				return fmt.Errorf("excepted only one destination, got: %v", unique)
			}
			return nil
		}
		var multipleHost echo.Checker = func(result echo.CallResult, _ error) error {
			hostnames := make([]string, len(result.Responses))
			for i, r := range result.Responses {
				hostnames[i] = r.Hostname
			}
			unique := sets.SortedList(sets.New(hostnames...))
			want := dst.WorkloadsOrFail(t)
			wn := []string{}
			for _, w := range want {
				wn = append(wn, w.PodName())
			}
			if len(unique) != len(wn) {
				return fmt.Errorf("excepted all destinations (%v), got: %v", wn, unique)
			}
			return nil
		}

		shouldBalance := dst.Config().HasServiceAddressedWaypointProxy()
		// Istio client will not reuse connections for HTTP/1.1
		opt.HTTP.HTTP2 = true
		// Make sure we make multiple calls
		opt.Count = 10
		c := singleHost
		if shouldBalance {
			c = multipleHost
		}
		opt.Check = check.And(check.OK(), c)
		opt.NewConnectionPerRequest = false
		src.CallOrFail(t, opt)
	})
}

func TestWaypointChanges(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		getGracePeriod := func(want int64) bool {
			pods, err := kubetest.NewPodFetch(t.AllClusters()[0], apps.Namespace.Name(), constants.GatewayNameLabel+"=waypoint")()
			assert.NoError(t, err)
			for _, p := range pods {
				grace := p.Spec.TerminationGracePeriodSeconds
				if grace != nil && *grace == want {
					return true
				}
			}
			return false
		}
		// check that waypoint deployment is unmodified
		retry.UntilOrFail(t, func() bool {
			return getGracePeriod(2)
		})
		// change the waypoint template
		istio.GetOrFail(t, t).UpdateInjectionConfig(t, func(cfg *inject.Config) error {
			mainTemplate := file.MustAsString(filepath.Join(env.IstioSrc, templateFile))
			cfg.RawTemplates["waypoint"] = strings.ReplaceAll(mainTemplate, "terminationGracePeriodSeconds: 2", "terminationGracePeriodSeconds: 3")
			return nil
		}, cleanup.Always)

		retry.UntilOrFail(t, func() bool {
			return getGracePeriod(3)
		})
	})
}

func TestOtherRevisionIgnored(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		// This is a negative test, ensuring gateways with tags other
		// than my tags do not get controlled by me.
		nsConfig, err := namespace.New(t, namespace.Config{
			Prefix: "badgateway",
			Inject: false,
			Labels: map[string]string{
				constants.DataplaneModeLabel: "ambient",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		istioctl.NewOrFail(t, t, istioctl.Config{}).InvokeOrFail(t, []string{
			"x",
			"waypoint",
			"apply",
			"--namespace",
			nsConfig.Name(),
			"--revision",
			"foo",
		})
		waypointError := retry.UntilSuccess(func() error {
			fetch := kubetest.NewPodFetch(t.AllClusters()[0], nsConfig.Name(), constants.GatewayNameLabel+"="+"sa")
			if _, err := kubetest.CheckPodsAreReady(fetch); err != nil {
				return fmt.Errorf("gateway is not ready: %v", err)
			}
			return nil
		}, retry.Timeout(15*time.Second), retry.BackoffDelay(time.Millisecond*100))
		if waypointError == nil {
			t.Fatal("Waypoint for non-existent tag foo created deployment!")
		}
	})
}

func TestRemoveAddWaypoint(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		istioctl.NewOrFail(t, t, istioctl.Config{}).InvokeOrFail(t, []string{
			"x",
			"waypoint",
			"apply",
			"--namespace",
			apps.Namespace.Name(),
			"--name", "captured-waypoint",
			"--wait",
		})
		t.Cleanup(func() {
			istioctl.NewOrFail(t, t, istioctl.Config{}).InvokeOrFail(t, []string{
				"x",
				"waypoint",
				"delete",
				"--namespace",
				apps.Namespace.Name(),
				"captured-waypoint",
			})
		})

		t.NewSubTest("before").Run(func(t framework.TestContext) {
			dst := apps.Captured
			for _, src := range apps.All {
				if src.Config().IsUncaptured() {
					continue
				}
				t.NewSubTestf("from %v", src.Config().Service).Run(func(t framework.TestContext) {
					c := IsL4()
					if src.Config().HasSidecar() {
						c = IsL7()
					}
					opt := echo.CallOptions{
						To:     dst,
						Port:   echo.Port{Name: "http"},
						Scheme: scheme.HTTP,
						Count:  10,
						Check:  check.And(check.OK(), c),
					}
					src.CallOrFail(t, opt)
				})
			}
		})

		SetWaypoint(t, Captured, "captured-waypoint")

		// Now should always be L7
		t.NewSubTest("after").Run(func(t framework.TestContext) {
			dst := apps.Captured
			for _, src := range apps.All {
				if src.Config().IsUncaptured() {
					continue
				}
				t.NewSubTestf("from %v", src.Config().Service).Run(func(t framework.TestContext) {
					opt := echo.CallOptions{
						To:     dst,
						Port:   echo.Port{Name: "http"},
						Scheme: scheme.HTTP,
						Count:  10,
						Check:  check.And(check.OK(), IsL7()),
					}
					src.CallOrFail(t, opt)
				})
			}
		})
	})
}

func TestBogusUseWaypoint(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		check := func(t framework.TestContext) {
			dst := apps.Captured
			for _, src := range apps.All {
				if src.Config().IsUncaptured() {
					continue
				}
				t.NewSubTestf("from %v", src.Config().Service).Run(func(t framework.TestContext) {
					c := IsL4()
					if src.Config().HasSidecar() {
						c = IsL7()
					}
					opt := echo.CallOptions{
						To:     dst,
						Port:   echo.Port{Name: "http"},
						Scheme: scheme.HTTP,
						Count:  10,
						Check:  check.And(check.OK(), c),
					}
					src.CallOrFail(t, opt)
				})
			}
		}
		t.NewSubTest("before").Run(check)

		SetWaypoint(t, Captured, "bogus-waypoint")
		t.NewSubTest("with waypoint").Run(check)

		SetWaypoint(t, Captured, "")
		t.NewSubTest("waypoint removed").Run(check)
	})
}

func TestServerRouting(t *testing.T) {
	runTest(t, func(t framework.TestContext, src echo.Instance, dst echo.Instance, opt echo.CallOptions) {
		// Need waypoint proxy and HTTP
		if opt.Scheme != scheme.HTTP {
			return
		}
		if !dst.Config().HasServiceAddressedWaypointProxy() {
			return
		}
		if src.Config().IsUncaptured() {
			// TODO: fix this and remove this skip
			t.Skip("https://github.com/istio/istio/issues/43238")
		}
		t.NewSubTest("set header").Run(func(t framework.TestContext) {
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
		t.NewSubTest("subset").Run(func(t framework.TestContext) {
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
  - route:
    - destination:
        host: "{{.Destination}}"
        subset: v1
---
apiVersion: networking.istio.io/v1beta1
kind: DestinationRule
metadata:
  name: route
  namespace:
spec:
  host: "{{.Destination}}"
  subsets:
  - labels:
      version: v1
    name: v1
  - labels:
      version: v2
    name: v2
`).ApplyOrFail(t)
			var exp string
			for _, w := range dst.WorkloadsOrFail(t) {
				if strings.Contains(w.PodName(), "-v1") {
					exp = w.PodName()
				}
			}
			opt.Count = 10
			opt.Check = check.And(
				check.OK(),
				check.Hostname(exp))
			src.CallOrFail(t, opt)
		})
	})
}

func TestWaypointEnvoyFilter(t *testing.T) {
	runTest(t, func(t framework.TestContext, src echo.Instance, dst echo.Instance, opt echo.CallOptions) {
		// Need at least one waypoint proxy and HTTP
		if opt.Scheme != scheme.HTTP {
			return
		}
		if !dst.Config().HasServiceAddressedWaypointProxy() {
			return
		}
		if src.Config().IsUncaptured() {
			// TODO: fix this and remove this skip
			t.Skip("https://github.com/istio/istio/issues/43238")
		}
		t.ConfigIstio().Eval(apps.Namespace.Name(), map[string]string{
			"Destination": "waypoint",
		}, `apiVersion: networking.istio.io/v1alpha3
kind: EnvoyFilter
metadata:
  name: inbound
spec:
  workloadSelector:
    labels:
      gateway.networking.k8s.io/gateway-name: "{{.Destination}}"
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
`).ApplyOrFail(t)
		opt.Count = 5
		opt.Timeout = time.Second * 10
		opt.Check = check.And(
			check.OK(),
			check.RequestHeaders(map[string]string{
				"X-Lua-Inbound":   "hello world",
				"X-Vhost-Inbound": "hello world",
			}))
		src.CallOrFail(t, opt)
	})
}

func TestTrafficSplit(t *testing.T) {
	runTest(t, func(t framework.TestContext, src echo.Instance, dst echo.Instance, opt echo.CallOptions) {
		// Need at least one waypoint proxy and HTTP
		if opt.Scheme != scheme.HTTP {
			return
		}
		if !dst.Config().HasServiceAddressedWaypointProxy() {
			return
		}
		if src.Config().IsUncaptured() {
			// TODO: fix this and remove this skip
			t.Skip("https://github.com/istio/istio/issues/43238")
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
  - match:
    - headers:
        user:
          exact: istio-custom-user
    route:
    - destination:
        host: "{{.Destination}}"
        subset: v2
  - route:
    - destination:
        host: "{{.Destination}}"
        subset: v1
`).ApplyOrFail(t)
		t.ConfigIstio().Eval(apps.Namespace.Name(), map[string]string{
			"Destination": dst.Config().Service,
		}, `apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: dr
spec:
  host: "{{.Destination}}"
  subsets:
  - name: v1
    labels:
      version: v1
  - name: v2
    labels:
      version: v2
`).ApplyOrFail(t)
		t.NewSubTest("v1").Run(func(t framework.TestContext) {
			opt = opt.DeepCopy()
			opt.Count = 5
			opt.Timeout = time.Second * 10
			opt.Check = check.And(
				check.OK(),
				func(result echo.CallResult, _ error) error {
					for _, r := range result.Responses {
						if r.Version != "v1" {
							return fmt.Errorf("expected service version %q, got %q", "v1", r.Version)
						}
					}
					return nil
				})
			src.CallOrFail(t, opt)
		})

		t.NewSubTest("v2").Run(func(t framework.TestContext) {
			opt = opt.DeepCopy()
			opt.Count = 5
			opt.Timeout = time.Second * 10
			if opt.HTTP.Headers == nil {
				opt.HTTP.Headers = map[string][]string{}
			}
			opt.HTTP.Headers.Set("user", "istio-custom-user")
			opt.Check = check.And(
				check.OK(),
				func(result echo.CallResult, _ error) error {
					for _, r := range result.Responses {
						if r.Version != "v2" {
							return fmt.Errorf("expected service version %q, got %q", "v2", r.Version)
						}
					}
					return nil
				})
			src.CallOrFail(t, opt)
		})
	})
}

func TestPeerAuthentication(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		// Workaround https://github.com/istio/istio/issues/43239
		t.ConfigIstio().YAML(apps.Namespace.Name(), `apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: single-request
spec:
  host: '*.svc.cluster.local'
  trafficPolicy:
    connectionPool:
      http:
        maxRequestsPerConnection: 1`).ApplyOrFail(t)
		runTestContext(t, func(t framework.TestContext, src echo.Instance, dst echo.Instance, opt echo.CallOptions) {
			if opt.Scheme != scheme.TCP {
				return
			}
			// Ensure we don't get stuck on old connections with old RBAC rules. This causes 45s test times
			// due to draining.
			opt.NewConnectionPerRequest = true
			if src.Config().IsUncaptured() {
				// For this case, it is broken if the src and dst are on the same node.
				// TODO: fix this and remove this skip
				t.Skip("https://github.com/istio/istio/issues/43238")
			}

			if src.Config().ZTunnelCaptured() && dst.Config().HasWorkloadAddressedWaypointProxy() {
				// this case should bypass waypoints because traffic is svc addressed but
				// presently a ztunnel bug will drop this traffic because it doesn't differentiate
				// between svc and wl addressed traffic when determining if the connection
				// should have gone through a waypoint.
				t.Skip("TODO: open an issue to address this ztunnel issue")
			}

			t.NewSubTest("permissive").Run(func(t framework.TestContext) {
				t.ConfigIstio().Eval(apps.Namespace.Name(), map[string]string{
					"Destination": dst.Config().Service,
					"Source":      src.Config().Service,
					"Namespace":   apps.Namespace.Name(),
				}, `
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: global-permissive
spec:
  mtls:
    mode: PERMISSIVE
`).ApplyOrFail(t)
				opt = opt.DeepCopy()
				src.CallOrFail(t, opt)
			})
			t.NewSubTest("strict").Run(func(t framework.TestContext) {
				t.ConfigIstio().Eval(apps.Namespace.Name(), map[string]string{
					"Destination": dst.Config().Service,
					"Source":      src.Config().Service,
					"Namespace":   apps.Namespace.Name(),
				}, `
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: global-strict
spec:
  mtls:
    mode: STRICT
				`).ApplyOrFail(t)
				opt = opt.DeepCopy()
				if inMesh.All([]echo.Instance{src, dst}) { // If both src and dst are in the mesh, the request should succeed
					opt.Check = check.OK()
				} else { // If not, the request should fail
					opt.Check = CheckDeny
				}
				src.CallOrFail(t, opt)
			})
			// general workload peerauth == STRICT, but we have a port-specific allowlist that is PERMISSIVE,
			// so anything hitting that port should not be rejected.
			// NOTE: Using port 80 since that's what
			t.NewSubTest("strict-permissive-ports").Run(func(t framework.TestContext) {
				t.ConfigIstio().Eval(apps.Namespace.Name(), map[string]string{
					"Destination": dst.Config().Service,
					"Source":      src.Config().Service,
					"Namespace":   apps.Namespace.Name(),
				}, `
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: global-strict
spec:
  selector:
    matchLabels:
      app: "{{ .Destination }}"
  mtls:
    mode: STRICT
  portLevelMtls:
    18080:
      mode: PERMISSIVE
				`).ApplyOrFail(t)
				opt = opt.DeepCopy()
				// Should pass for all workloads, in or out of mesh, targeting this port
				src.CallOrFail(t, opt)
			})

			// global peer auth is strict, but we have a permissive port-level rule
			t.NewSubTest("global-strict-permissive-workload-ports").Run(func(t framework.TestContext) {
				t.ConfigIstio().YAML(i.Settings().SystemNamespace, `
apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: global-strict
spec:
  mtls:
    mode: STRICT
        `).ApplyOrFail(t)
				t.ConfigIstio().Eval(apps.Namespace.Name(), map[string]string{
					"Destination": dst.Config().Service,
					"Source":      src.Config().Service,
					"Namespace":   apps.Namespace.Name(),
				}, `
apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: local-port-override
spec:
  selector:
    matchLabels:
      app: "{{ .Destination }}"
  portLevelMtls:
    18080:
      mode: PERMISSIVE
        `).ApplyOrFail(t)
				opt = opt.DeepCopy()
				// Should pass for all workloads, in or out of mesh, targeting this port
				src.CallOrFail(t, opt)
			})

			t.NewSubTest("global-permissive-strict-workload-ports").Run(func(t framework.TestContext) {
				t.ConfigIstio().YAML(i.Settings().SystemNamespace, `
apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: global-strict
spec:
  mtls:
    mode: PERMISSIVE
        `).ApplyOrFail(t)
				t.ConfigIstio().Eval(apps.Namespace.Name(), map[string]string{
					"Destination": dst.Config().Service,
					"Source":      src.Config().Service,
					"Namespace":   apps.Namespace.Name(),
				}, `
apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: local-port-override
spec:
  selector:
    matchLabels:
      app: "{{ .Destination }}"
  portLevelMtls:
    18080:
      mode: STRICT
        `).ApplyOrFail(t)
				opt = opt.DeepCopy()
				if !src.Config().HasProxyCapabilities() && dst.Config().HasProxyCapabilities() {
					// Expect deny if the dest is in the mesh (enforcing mTLS) but src is not (not sending mTLS)
					opt.Check = CheckDeny
				}
				src.CallOrFail(t, opt)
			})
		})
	})
}

func TestAuthorizationL4(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		applyDrainingWorkaround(t)
		// pairs x allow/deny
		runTestContext(t, func(t framework.TestContext, src echo.Instance, dst echo.Instance, opt echo.CallOptions) {
			if opt.Scheme != scheme.TCP {
				return
			}
			// Ensure we don't get stuck on old connections with old RBAC rules. This causes 45s test times
			// due to draining.
			opt.NewConnectionPerRequest = true
			if src.Config().IsUncaptured() {
				// For this case, it is broken if the src and dst are on the same node.
				// TODO: fix this and remove this skip
				t.Skip("https://github.com/istio/istio/issues/43238")
			}

			overrideCheck := func(src echo.Instance, dst echo.Instance, opt *echo.CallOptions) {
				switch {
				case src.Config().IsUncaptured() && dst.Config().HasAnyWaypointProxy():
					// For this case, it is broken if the src and dst are on the same node.
					// Because client request is not captured to perform the hairpin
					// TODO: fix this and remove this skip
					opt.Check = check.OK()
				case dst.Config().IsUncaptured() && !dst.Config().HasSidecar():
					// No destination means no RBAC to apply. Make sure we do not accidentally reject
					opt.Check = check.OK()
				}
			}

			authzCases := []struct {
				name  string
				spec  string
				check echo.Checker
			}{
				{
					name:  "allow",
					check: check.OK(),
					spec: `
  rules:
  - from:
    - source:
        principals: ["cluster.local/ns/{{.Namespace}}/sa/{{.Source}}", "cluster.local/ns/{{.Namespace}}/sa/{{.WaypointName}}"]
`,
				},
				{
					name:  "not allow",
					check: CheckDeny,
					spec: `
  rules:
  - from:
    - source:
        principals: ["cluster.local/ns/something/sa/else"]
          `,
				},
			}

			for _, tc := range authzCases {
				t.NewSubTest(tc.name).Run(func(t framework.TestContext) {
					t.ConfigIstio().Eval(apps.Namespace.Name(), map[string]string{
						"Destination":  dst.Config().Service,
						"Source":       src.Config().Service,
						"Namespace":    apps.Namespace.Name(),
						"WaypointName": dst.Config().ServiceWaypointProxy,
					}, `
apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
  name: policy-waypoint
spec:
  targetRefs:
  # affects Waypoints
  - kind: Service
    group: core
    name: "{{ .Destination }}"
`+tc.spec+`
---
apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
  name: policy
spec:
  # affects zTunnels and Sidecars
  selector:
    matchLabels:
      app: "{{ .Destination }}"
`+tc.spec).ApplyOrFail(t)
					perCaseOpt := opt.DeepCopy()
					perCaseOpt.Check = tc.check
					overrideCheck(src, dst, &perCaseOpt)
					src.CallOrFail(t, perCaseOpt)
				})
			}
		})
	})
}

func TestAuthorizationServiceAttached(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		applyDrainingWorkaround(t)
		src := apps.Captured
		authzDst := apps.ServiceAddressedWaypoint
		otherDst := apps.WorkloadAddressedWaypoint

		// make another target use our waypoint, but don't expect authz there
		ambient.SetWaypointForService(t, apps.Namespace, otherDst.ServiceName(), authzDst.Config().ServiceWaypointProxy)

		t.ConfigIstio().Eval(apps.Namespace.Name(), map[string]string{
			"Destination": authzDst.Config().Service,
		}, `
apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
  name: policy-waypoint
spec:
  targetRefs:
  - kind: Service
    group: core
    name: "{{ .Destination }}"
  rules:
  - from:
    - source:
        principals: ["cluster.local/ns/something/sa/else"]
  `).ApplyOrFail(t)

		for _, src := range src.Instances() {
			t.NewSubTest(src.Config().Cluster.StableName()).Run(func(t framework.TestContext) {
				t.NewSubTest("authz target deny").RunParallel(func(t framework.TestContext) {
					opts := echo.CallOptions{
						To:     authzDst,
						Check:  CheckDeny,
						Port:   echo.Port{Name: "http"},
						Scheme: scheme.HTTP,
						Count:  10,
					}
					src.CallOrFail(t, opts)
				})
				t.NewSubTest("non-authz target allow").RunParallel(func(t framework.TestContext) {
					opts := echo.CallOptions{
						To:     otherDst,
						Check:  check.OK(),
						Port:   echo.Port{Name: "http"},
						Scheme: scheme.HTTP,
						Count:  10,
					}
					src.CallOrFail(t, opts)
				})
			})
		}
	})
}

func TestAuthorizationGateway(t *testing.T) {
	runTest := func(t framework.TestContext, f func(t framework.TestContext, src echo.Caller, dst echo.Instance, opt echo.CallOptions)) {
		svcs := apps.All
		for _, dst := range svcs {
			t.NewSubTestf("to %v", dst.Config().Service).Run(func(t framework.TestContext) {
				dst := dst
				opt := echo.CallOptions{
					Port:    echo.Port{Name: "http"},
					Scheme:  scheme.HTTP,
					Count:   5,
					Timeout: time.Second * 2,
					Check:   check.OK(),
					To:      dst,
				}
				f(t, istio.DefaultIngressOrFail(t, t), dst, opt)
			})
		}
	}
	framework.NewTest(t).Run(func(t framework.TestContext) {
		applyDrainingWorkaround(t)
		runTest(t, func(t framework.TestContext, src echo.Caller, dst echo.Instance, opt echo.CallOptions) {
			if opt.Scheme != scheme.HTTP {
				return
			}

			// Ensure we don't get stuck on old connections with old RBAC rules. This causes 45s test times
			// due to draining.
			opt.NewConnectionPerRequest = true

			policySpec := `
  rules:
  - from:
    - source:
        principals: ["cluster.local/ns/istio-system/sa/{{.Source}}"]
    to:
    - operation:
        ports: ["{{.PortAllowWorkload}}"]
  - from:
    - source:
        principals: ["cluster.local/ns/{{.Namespace}}/sa/someone-else"]
    to:
    - operation:
        ports: ["{{.PortDenyWorkload}}"]
`
			t.ConfigIstio().Eval(apps.Namespace.Name(), map[string]string{
				"Destination":       dst.Config().Service,
				"Source":            "istio-ingressgateway-service-account",
				"Namespace":         apps.Namespace.Name(),
				"PortAllow":         strconv.Itoa(ports.HTTP.ServicePort),
				"PortAllowWorkload": strconv.Itoa(ports.HTTP.WorkloadPort),
				"PortDeny":          strconv.Itoa(ports.HTTP2.ServicePort),
				"PortDenyWorkload":  strconv.Itoa(ports.HTTP2.WorkloadPort),
			}, `
apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
  name: policy
spec:
  selector:
    matchLabels:
      app: "{{ .Destination }}"
`+policySpec+`
---
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
    hosts: ["*"]
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: route
spec:
  gateways:
  - gateway
  hosts:
  - "*"
  http:
  - match:
    - uri:
        exact: /allowed
    route:
    - destination:
        host: "{{.Destination}}"
        port:
          number: {{.PortAllow}}
  - match:
    - uri:
        exact: /deny
    route:
    - destination:
        host: "{{.Destination}}"
        port:
          number: {{.PortDeny}}
`).ApplyOrFail(t)
			overrideCheck := func(opt *echo.CallOptions) {
				switch {
				case !dst.Config().HasProxyCapabilities():
					// No destination proxy means no RBAC to apply. Make sure we do not accidentally reject
					opt.Check = check.OK()
				}
			}
			t.NewSubTest("simple deny").Run(func(t framework.TestContext) {
				opt = opt.DeepCopy()
				opt.HTTP.Path = "/deny"
				opt.Check = CheckDeny
				overrideCheck(&opt)
				src.CallOrFail(t, opt)
			})
			t.NewSubTest("simple allow").Run(func(t framework.TestContext) {
				opt = opt.DeepCopy()
				opt.HTTP.Path = "/allowed"
				opt.Check = check.OK()
				overrideCheck(&opt)
				src.CallOrFail(t, opt)
			})
		})
	})
}

func TestAuthorizationL7(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		applyDrainingWorkaround(t)
		runTestContext(t, func(t framework.TestContext, src echo.Instance, dst echo.Instance, opt echo.CallOptions) {
			if opt.Scheme != scheme.HTTP {
				return
			}
			// Ensure we don't get stuck on old connections with old RBAC rules. This causes 45s test times
			// due to draining.
			opt.NewConnectionPerRequest = true
			if src.Config().IsUncaptured() {
				// TODO: fix this and remove this skip
				t.Skip("https://github.com/istio/istio/issues/43238")
			}

			policySpec := `
  rules:
  - to:
    - operation:
        paths: ["/allowed"]
        methods: ["GET"]
  - from:
    - source:
        principals: ["cluster.local/ns/{{.Namespace}}/sa/{{.Source}}"]
    to:
    - operation:
        paths: ["/allowed-identity"]
        methods: ["GET"]
  - from:
    - source:
        principals: ["cluster.local/ns/{{.Namespace}}/sa/someone-else"]
    to:
    - operation:
        paths: ["/denied-identity"]
        methods: ["GET"]
  - to:
    - operation:
        methods: ["GET"]
        paths: ["/allowed-wildcard*"]
  - to:
    - operation:
        methods: ["GET"]
        paths: ["/headers"]
    when:
    - key: request.headers[x-test-header]
      values: ["match"]
      notValues: ["do-not-match"]
  - to:
    - operation:
        methods: ["POST"]
`
			denySpec := `
  action: DENY
  rules:
  - to:
    - operation:
        paths: ["/explicit-deny"]
`
			// for most cases just use the normal policy spec
			policySpecWL := policySpec
			if dst.Config().HasAnyWaypointProxy() {
				// for svc addressed traffic we want the WL policy to allow Waypoint -> Workload
				policySpecWL = `
  rules:
  - from:
    - source:
        principals: ["cluster.local/ns/{{.Namespace}}/sa/{{.WaypointName}}"]
`
			}
			waypointName := "none"
			switch {
			case dst.Config().HasServiceAddressedWaypointProxy():
				waypointName = dst.Config().ServiceWaypointProxy
			case dst.Config().HasWorkloadAddressedWaypointProxy():
				waypointName = dst.Config().WorkloadWaypointProxy
			}
			t.ConfigIstio().Eval(apps.Namespace.Name(), map[string]string{
				"Destination":  dst.Config().Service,
				"Source":       src.Config().Service,
				"Namespace":    apps.Namespace.Name(),
				"WaypointName": waypointName,
			}, `
apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
  name: policy
spec:
  selector:
    matchLabels:
      app: "{{ .Destination }}"
`+policySpecWL+`
---
apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
  name: policy-waypoint
spec:
  targetRefs:
  - kind: Gateway
    group: gateway.networking.k8s.io
    name: waypoint
`+policySpec+`
---
apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
  name: deny-policy
spec:
  selector:
    matchLabels:
      app: "{{ .Destination }}"
`+denySpec+`
---
apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
  name: deny-policy-waypoint
spec:
  targetRefs:
  - kind: Gateway
    group: gateway.networking.k8s.io
    name: waypoint
`+denySpec).ApplyOrFail(t)
			overrideCheck := func(opt *echo.CallOptions) {
				switch {
				case dst.Config().IsUncaptured() && !dst.Config().HasSidecar():
					// No destination means no RBAC to apply. Make sure we do not accidentally reject
					opt.Check = check.OK()
				case !dst.Config().HasAnyWaypointProxy() && !dst.Config().HasSidecar():
					// Only waypoint proxy can handle L7 policies
					opt.Check = CheckDeny
				case dst.Config().HasWorkloadAddressedWaypointProxy() && !dst.Config().HasServiceAddressedWaypointProxy():
					// send traffic to the workload instead of the service so it will redirect to the WL waypoint
					opt.Address = dst.MustWorkloads().Addresses()[0]
					opt.Port = echo.Port{ServicePort: ports.All().MustForName(opt.Port.Name).WorkloadPort}
				}
			}
			if src == dst {
				t.Skip("self call is not captured, L7 features will not work")
			}
			t.NewSubTest("simple deny").Run(func(t framework.TestContext) {
				opt := opt.DeepCopy()
				opt.HTTP.Path = "/deny"
				opt.Check = CheckDeny
				overrideCheck(&opt)
				src.CallOrFail(t, opt)
			})
			t.NewSubTest("simple allow").Run(func(t framework.TestContext) {
				opt := opt.DeepCopy()
				opt.HTTP.Path = "/allowed"
				opt.Check = check.OK()
				overrideCheck(&opt)
				src.CallOrFail(t, opt)
			})
			t.NewSubTest("identity deny").Run(func(t framework.TestContext) {
				opt := opt.DeepCopy()
				opt.HTTP.Path = "/denied-identity"
				opt.Check = CheckDeny
				overrideCheck(&opt)
				src.CallOrFail(t, opt)
			})
			t.NewSubTest("identity allow").Run(func(t framework.TestContext) {
				opt := opt.DeepCopy()
				opt.HTTP.Path = "/allowed-identity"
				opt.Check = check.OK()
				if !src.Config().HasProxyCapabilities() && !dst.Config().HasServiceAddressedWaypointProxy() {
					// TODO: remove waypoint check (https://github.com/istio/istio/issues/42640)
					// No identity from uncaptured
					opt.Check = CheckDeny
				}
				overrideCheck(&opt)
				src.CallOrFail(t, opt)
			})
			t.NewSubTest("explicit deny").Run(func(t framework.TestContext) {
				opt := opt.DeepCopy()
				opt.HTTP.Path = "/explicit-deny"
				opt.HTTP.Method = http.MethodPost
				opt.Check = CheckDeny
				overrideCheck(&opt)
				src.CallOrFail(t, opt)
			})
			t.NewSubTest("wildcard allow").Run(func(t framework.TestContext) {
				opt := opt.DeepCopy()
				opt.HTTP.Path = "/allowed-wildcardtest"
				opt.Check = check.OK()
				overrideCheck(&opt)
				src.CallOrFail(t, opt)
			})
			t.NewSubTest("headers allow").Run(func(t framework.TestContext) {
				opt := opt.DeepCopy()
				opt.HTTP.Path = "/headers"
				if opt.HTTP.Headers == nil {
					opt.HTTP.Headers = map[string][]string{}
				}
				opt.HTTP.Headers.Set("x-test-header", "match")
				opt.Check = check.OK()
				overrideCheck(&opt)
				src.CallOrFail(t, opt)
			})
			t.NewSubTest("headers deny").Run(func(t framework.TestContext) {
				opt := opt.DeepCopy()
				opt.HTTP.Path = "/headers"
				if opt.HTTP.Headers == nil {
					opt.HTTP.Headers = map[string][]string{}
				}
				opt.HTTP.Headers.Set("x-test-header", "do-not-match")
				opt.Check = CheckDeny
				overrideCheck(&opt)
				src.CallOrFail(t, opt)
			})
		})
	})
}

func TestL7JWT(t *testing.T) {
	// Workaround https://github.com/istio/istio/issues/43239
	framework.NewTest(t).Run(func(t framework.TestContext) {
		applyDrainingWorkaround(t)
		runTestContext(t, func(t framework.TestContext, src echo.Instance, dst echo.Instance, opt echo.CallOptions) {
			if opt.Scheme != scheme.HTTP {
				return
			}
			// Ensure we don't get stuck on old connections with old RBAC rules. This causes 45s test times
			// due to draining.
			opt.NewConnectionPerRequest = true
			if src.Config().IsUncaptured() {
				// TODO: fix this and remove this skip
				t.Skip("https://github.com/istio/istio/issues/43238")
			}

			if !dst.Config().HasAnyWaypointProxy() {
				t.Skip("L7 JWT is only for waypoints")
			}

			switch {
			case dst.Config().HasWorkloadAddressedWaypointProxy() && !dst.Config().HasServiceAddressedWaypointProxy():
				// send traffic to the workload instead of the service so it will redirect to the WL waypoint
				opt.Address = dst.MustWorkloads().Addresses()[0]
				opt.Port = echo.Port{ServicePort: ports.All().MustForName(opt.Port.Name).WorkloadPort}
				if src == dst {
					t.Skip("self call is not captured, L7 features will not work")
				}
			}

			t.ConfigIstio().New().EvalFile(apps.Namespace.Name(), map[string]any{
				param.Namespace.String(): apps.Namespace.Name(),
				"Services":               apps.ServiceAddressedWaypoint,
				"To":                     dst,
			}, "testdata/requestauthn/waypoint-jwt.yaml.tmpl").ApplyOrFail(t)

			t.NewSubTest("deny without token").Run(func(t framework.TestContext) {
				opt := opt.DeepCopy()
				opt.HTTP.Path = "/"
				opt.Check = check.Status(http.StatusForbidden)
				src.CallOrFail(t, opt)
			})

			t.NewSubTest("allow with sub-1 token").Run(func(t framework.TestContext) {
				opt := opt.DeepCopy()
				opt.HTTP.Path = "/"
				opt.HTTP.Headers = headers.New().
					WithAuthz(jwt.TokenIssuer1).
					Build()
				opt.Check = check.OK()
			})

			t.NewSubTest("deny with sub-3 token due to ignored RequestAuthentication").Run(func(t framework.TestContext) {
				opt := opt.DeepCopy()
				opt.HTTP.Path = "/"
				opt.HTTP.Headers = headers.New().
					WithAuthz(jwt.TokenIssuer3).
					Build()
				opt.Check = check.Status(http.StatusUnauthorized)
				src.CallOrFail(t, opt)
			})

			t.NewSubTest("deny with sub-2 token").Run(func(t framework.TestContext) {
				opt := opt.DeepCopy()
				opt.HTTP.Path = "/"
				opt.HTTP.Headers = headers.New().
					WithAuthz(jwt.TokenIssuer2).
					Build()
				opt.Check = check.Status(http.StatusForbidden)
				src.CallOrFail(t, opt)
			})

			t.NewSubTest("deny with expired token").Run(func(t framework.TestContext) {
				opt := opt.DeepCopy()
				opt.HTTP.Path = "/"
				opt.HTTP.Headers = headers.New().
					WithAuthz(jwt.TokenExpired).
					Build()
				opt.Check = check.Status(http.StatusUnauthorized)
				src.CallOrFail(t, opt)
			})

			t.NewSubTest("allow healthz").Run(func(t framework.TestContext) {
				opt := opt.DeepCopy()
				opt.HTTP.Path = "/healthz"
				opt.Check = check.OK()
				src.CallOrFail(t, opt)
			})
		})
	})
}

func applyDrainingWorkaround(t framework.TestContext) {
	// Workaround https://github.com/istio/istio/issues/43239
	t.ConfigIstio().YAML(apps.Namespace.Name(), `apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: single-request
spec:
  host: '*.svc.cluster.local'
  trafficPolicy:
    connectionPool:
      http:
        maxRequestsPerConnection: 1`).ApplyOrFail(t)
}

// Relies on the suite running in a cluster with a CNI which enforces K8s netpol but presently has no check
func TestK8sNetPol(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			t.Skip("https://github.com/istio/istio/issues/49301")
			systemNM := istio.ClaimSystemNamespaceOrFail(t, t)

			// configure a NetPol which will only allow HBONE traffic in the test app namespace
			// we should figure out what our recommendation for NetPol will be and have this reflect it
			t.ConfigIstio().File(apps.Namespace.Name(), "testdata/only-hbone.yaml").ApplyOrFail(t)

			Always := func(echo.Instance, echo.CallOptions) bool {
				return true
			}
			Never := func(echo.Instance, echo.CallOptions) bool {
				return false
			}
			SameNetwork := func(from echo.Instance, to echo.Target) echo.Instances {
				return match.Network(from.Config().Cluster.NetworkName()).GetMatches(to.Instances())
			}
			SupportsHBone := func(from echo.Instance, opts echo.CallOptions) bool {
				if !from.Config().IsUncaptured() && !opts.To.Config().IsUncaptured() {
					return true
				}
				if !from.Config().IsUncaptured() && opts.To.Config().HasSidecar() {
					return true
				}
				if from.Config().HasSidecar() && !opts.To.Config().IsUncaptured() {
					return true
				}
				if from.Config().HasSidecar() && opts.To.Config().HasSidecar() {
					return true
				}
				return false
			}
			_ = Never
			_ = SameNetwork
			testCases := []reachability.TestCase{
				{
					ConfigFile:    "beta-mtls-on.yaml",
					Namespace:     systemNM,
					Include:       Always,
					ExpectSuccess: SupportsHBone,
					// we do not expect HBONE traffic to have mutated user traffic
					// presently ExpectMTLS is checking that headers were added to user traffic
					ExpectMTLS: Never,
				},
				{
					ConfigFile:    "beta-mtls-permissive.yaml",
					Namespace:     systemNM,
					Include:       Always,
					ExpectSuccess: SupportsHBone,
					// we do not expect HBONE traffic to have mutated user traffic
					// presently ExpectMTLS is checking that headers were added to user traffic
					ExpectMTLS: Never,
				},
				{
					ConfigFile:    "beta-mtls-off.yaml",
					Namespace:     systemNM,
					Include:       Always,
					ExpectSuccess: SupportsHBone,
					// we do not expect HBONE traffic to have mutated user traffic
					// presently ExpectMTLS is checking that headers were added to user traffic
					ExpectMTLS: Never,
				},
			}
			RunReachability(testCases, t)
		})
}

func TestMTLS(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			t.Skip("https://github.com/istio/istio/issues/42696")
			systemNM := istio.ClaimSystemNamespaceOrFail(t, t)
			// mtlsOnExpect defines our expectations for when mTLS is expected when its enabled
			mtlsOnExpect := func(from echo.Instance, opts echo.CallOptions) bool {
				if from.Config().IsNaked() || opts.To.Config().IsNaked() {
					// If one of the two endpoints is naked, we don't send mTLS
					return false
				}
				if opts.To.Config().IsHeadless() && opts.To.Instances().Contains(from) {
					// pod calling its own pod IP will not be intercepted
					return false
				}
				return true
			}
			Always := func(echo.Instance, echo.CallOptions) bool {
				return true
			}
			Never := func(echo.Instance, echo.CallOptions) bool {
				return false
			}
			SameNetwork := func(from echo.Instance, to echo.Target) echo.Instances {
				return match.Network(from.Config().Cluster.NetworkName()).GetMatches(to.Instances())
			}
			_ = Never
			_ = SameNetwork
			testCases := []reachability.TestCase{
				{
					ConfigFile: "beta-mtls-on.yaml",
					Namespace:  systemNM,
					Include:    Always,
					ExpectSuccess: func(from echo.Instance, opts echo.CallOptions) bool {
						if from.Config().HasProxyCapabilities() != opts.To.Config().HasProxyCapabilities() {
							if from.Config().HasProxyCapabilities() && !from.Config().HasAnyWaypointProxy() {
								if from.Config().HasSidecar() && !opts.To.Config().HasProxyCapabilities() {
									// Sidecar respects it ISTIO_MUTUAL, will only send mTLS
									return false
								}
								return true
							}
							if !from.Config().HasProxyCapabilities() && opts.To.Config().HasAnyWaypointProxy() {
								// TODO: support hairpin
								return true
							}
							if !from.Config().HasProxyCapabilities() && !opts.To.Config().HasSidecar() {
								// TODO: https://github.com/istio/istio/issues/42696
								return true
							}
							return false
						}
						if !from.Config().HasProxyCapabilities() && opts.To.Config().HasSidecar() {
							return false
						}
						return true
					},
					ExpectMTLS: mtlsOnExpect,
				},
				{
					ConfigFile: "beta-mtls-permissive.yaml",
					Namespace:  systemNM,
					Include: func(_ echo.Instance, opts echo.CallOptions) bool {
						// Exclude calls to naked since we are applying ISTIO_MUTUAL
						return !opts.To.Config().IsNaked()
					},
					ExpectSuccess: func(from echo.Instance, opts echo.CallOptions) bool {
						if (from.Config().HasAnyWaypointProxy() || from.Config().HasSidecar()) && !opts.To.Config().HasProxyCapabilities() {
							return false
						}
						return true
					},
					ExpectMTLS: mtlsOnExpect,
				},
				{
					ConfigFile:    "beta-mtls-off.yaml",
					Namespace:     systemNM,
					Include:       Always,
					ExpectSuccess: Always,
					ExpectMTLS:    Never,
					// Without TLS we can't perform SNI routing required for multi-network
					ExpectDestinations: SameNetwork,
				},
				{
					ConfigFile:    "plaintext-to-permissive.yaml",
					Namespace:     systemNM,
					Include:       Always,
					ExpectSuccess: Always,
					ExpectMTLS:    Never,
					// Since we are only sending plaintext and Without TLS
					// we can't perform SNI routing required for multi-network
					ExpectDestinations: SameNetwork,
				},
				{
					ConfigFile: "beta-mtls-automtls.yaml",
					Namespace:  apps.Namespace,
					Include:    Always,
					ExpectSuccess: func(from echo.Instance, opts echo.CallOptions) bool {
						if !from.Config().HasProxyCapabilities() && !opts.To.Config().HasSidecar() {
							// TODO: https://github.com/istio/istio/issues/42696
							return true
						}
						// autoMtls doesn't work for client that doesn't have proxy, unless target doesn't
						// have proxy neither.
						if !from.Config().HasProxyCapabilities() {
							return !opts.To.Config().HasProxyCapabilities()
						}
						return true
					},
					ExpectMTLS: mtlsOnExpect,
				},
				{
					ConfigFile: "no-peer-authn.yaml",
					Namespace:  systemNM,
					Include: func(_ echo.Instance, opts echo.CallOptions) bool {
						// Exclude calls to naked since we are applying ISTIO_MUTUAL
						return !opts.To.Config().IsNaked()
					},
					ExpectSuccess: func(from echo.Instance, opts echo.CallOptions) bool {
						if from.Config().HasSidecar() && !opts.To.Config().HasProxyCapabilities() {
							// Sidecar respects it
							return false
						}
						if from.Config().HasAnyWaypointProxy() && !opts.To.Config().HasProxyCapabilities() {
							// Waypoint respects it
							return false
						}
						return true
					},
					ExpectMTLS: mtlsOnExpect,
				},
				{
					ConfigFile: "global-plaintext.yaml",
					Namespace:  systemNM,
					ExpectDestinations: func(from echo.Instance, to echo.Target) echo.Instances {
						// Without TLS we can't perform SNI routing required for multi-network
						return match.Network(from.Config().Cluster.NetworkName()).GetMatches(to.Instances())
					},
					ExpectSuccess: Always,
					ExpectMTLS:    Never,
				},
				{
					ConfigFile: "automtls-passthrough.yaml",
					Namespace:  systemNM,
					Include: func(_ echo.Instance, opts echo.CallOptions) bool {
						// VM passthrough doesn't work. We will send traffic to the ClusterIP of
						// the VM service, which will have 0 Endpoints. If we generated
						// EndpointSlice's for VMs this might work.
						return !opts.To.Config().IsVM()
					},
					ExpectSuccess: func(from echo.Instance, opts echo.CallOptions) bool {
						// nolint: gosimple
						if from.Config().HasAnyWaypointProxy() {
							if opts.To.Config().HasServiceAddressedWaypointProxy() {
								return true
							}
							// TODO: https://github.com/istio/istio/issues/43242
							return false
						}
						return true
					},
					ExpectMTLS: func(from echo.Instance, opts echo.CallOptions) bool {
						return mtlsOnExpect(from, opts)
					},

					ExpectDestinations: func(from echo.Instance, to echo.Target) echo.Instances {
						// Since we are doing passthrough, only single cluster is relevant here, as we
						// are bypassing any Istio cluster load balancing
						return match.Cluster(from.Config().Cluster).GetMatches(to.Instances())
					},
				},
			}
			RunReachability(testCases, t)
		})
}

func TestOutboundPolicyAllowAny(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			skipOnNativeZtunnel(t, "TODO? not sure why this is broken")
			svcs := apps.All
			for _, svc := range svcs {
				if svc.Config().IsUncaptured() || svc.Config().HasSidecar() {
					continue
				}
				t.NewSubTestf("ALLOW_ANY %v to external service", svc.Config().Service).Run(func(t framework.TestContext) {
					// TODO use Sidecar to simulate external service (see tests/integration/pilot/mirror_test.go)
					svc.CallOrFail(t, echo.CallOptions{
						Address: "httpbin.org",
						Port:    echo.Port{Name: "http", ServicePort: 80},
						Scheme:  scheme.HTTP,
						HTTP: echo.HTTP{
							Path: "/headers",
						},
						Check: check.OK(),
					})
				})
			}
		})
}

func TestServiceEntryDNS(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			skipOnNativeZtunnel(t, "ServiceEntry not supported")
			svcs := apps.All
			for _, svc := range svcs {
				if svc.Config().IsUncaptured() || svc.Config().HasSidecar() {
					continue
				}
				if err := t.ConfigIstio().YAML(svc.NamespaceName(), `apiVersion: networking.istio.io/v1beta1
kind: ServiceEntry
metadata:
  name: externalservice-httpbin
spec:
  exportTo:
  - .
  hosts:
  - httpbin.org
  ports:
  - name: http
    number: 80
    protocol: HTTP
  resolution: DNS`).Apply(apply.NoCleanup); err != nil {
					t.Fatal(err)
				}
				t.NewSubTestf("%v to ServiceEntry", svc.Config().Service).Run(func(t framework.TestContext) {
					// TODO use Sidecar to simulate external service (see tests/integration/pilot/mirror_test.go)
					svc.CallOrFail(t, echo.CallOptions{
						Address: "httpbin.org",
						Port:    echo.Port{Name: "http", ServicePort: 80},
						Scheme:  scheme.HTTP,
						HTTP: echo.HTTP{
							Path: "/headers",
						},
						Check: check.OK(),
					})
				})
			}
		})
}

func TestServiceEntryInlinedWorkloadEntry(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			testCases := []struct {
				location   v1alpha3.ServiceEntry_Location
				resolution v1alpha3.ServiceEntry_Resolution
				to         echo.Instances
			}{
				{
					location:   v1alpha3.ServiceEntry_MESH_INTERNAL,
					resolution: v1alpha3.ServiceEntry_STATIC,
					to:         apps.Mesh,
				},
				{
					location:   v1alpha3.ServiceEntry_MESH_EXTERNAL,
					resolution: v1alpha3.ServiceEntry_STATIC,
					to:         apps.MeshExternal,
				},
				// TODO dns cases
			}

			// Configure a gateway with one app as the destination to be accessible through the ingress
			t.ConfigIstio().Eval(apps.Namespace.Name(), map[string]string{
				"Destination": apps.Captured[0].Config().Service,
			}, `apiVersion: networking.istio.io/v1alpha3
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
    hosts: ["*"]
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: route
spec:
  gateways:
  - gateway
  hosts:
  - "*"
  http:
  - route:
    - destination:
        host: "{{.Destination}}"
`).ApplyOrFail(t)

			cfg := config.YAML(`
{{ $to := .To }}
apiVersion: networking.istio.io/v1beta1
kind: ServiceEntry
metadata:
  name: test-se
spec:
  hosts:
  - serviceentry.istio.io # not used
  addresses:
  - 111.111.222.222
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: {{.Resolution}}
  location: {{.Location}}
  endpoints:
  # we send directly to a Pod IP here. This is essentially headless
  - address: {{.IngressIp}} # TODO won't work with DNS resolution tests
    ports:
      http: {{.IngressHttpPort}}`).
				WithParams(param.Params{}.SetWellKnown(param.Namespace, apps.Namespace))

			ips, ports := istio.DefaultIngressOrFail(t, t).HTTPAddresses()
			for _, tc := range testCases {
				tc := tc
				for i, ip := range ips {
					t.NewSubTestf("%s %s %s", tc.location, tc.resolution, ip).Run(func(t framework.TestContext) {
						echotest.
							New(t, apps.All).
							// TODO eventually we can do this for uncaptured -> l7
							FromMatch(match.Not(match.ServiceName(echo.NamespacedName{
								Name:      "uncaptured",
								Namespace: apps.Namespace,
							}))).
							Config(cfg.WithParams(param.Params{
								"Resolution":      tc.resolution.String(),
								"Location":        tc.location.String(),
								"IngressIp":       ip,
								"IngressHttpPort": ports[i],
							})).
							Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
								// TODO validate L7 processing/some headers indicating we reach the svc we wanted
								from.CallOrFail(t, echo.CallOptions{
									Address: "111.111.222.222",
									Port:    to.PortForName("http"),
								})
							})
					})
				}
			}
		})
}

func TestServiceEntrySelectsWorkloadEntry(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			testCases := []struct {
				location   v1alpha3.ServiceEntry_Location
				resolution v1alpha3.ServiceEntry_Resolution
				to         echo.Instances
			}{
				{
					location:   v1alpha3.ServiceEntry_MESH_INTERNAL,
					resolution: v1alpha3.ServiceEntry_STATIC,
					to:         apps.Mesh,
				},
				{
					location:   v1alpha3.ServiceEntry_MESH_EXTERNAL,
					resolution: v1alpha3.ServiceEntry_STATIC,
					to:         apps.MeshExternal,
				},
				// TODO dns cases
			}

			// Configure a gateway with one app as the destination to be accessible through the ingress
			t.ConfigIstio().Eval(apps.Namespace.Name(), map[string]string{
				"Destination": apps.Captured[0].Config().Service,
			}, `apiVersion: networking.istio.io/v1alpha3
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
    hosts: ["*"]
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: route
spec:
  gateways:
  - gateway
  hosts:
  - "*"
  http:
  - route:
    - destination:
        host: "{{.Destination}}"
`).ApplyOrFail(t)

			cfg := config.YAML(`
{{ $to := .To }}
apiVersion: networking.istio.io/v1beta1
kind: WorkloadEntry
metadata:
  name: test-we
spec:
  address: {{.IngressIp}}
  ports:
    http: {{.IngressHttpPort}}
  labels:
    app: selected
---
apiVersion: networking.istio.io/v1beta1
kind: ServiceEntry
metadata:
  name: test-se
spec:
  hosts:
  - serviceentry.istio.io # not used
  addresses:
  - 111.111.222.222
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: {{.Resolution}}
  location: {{.Location}}
  workloadSelector:
    labels:
      app: selected`).
				WithParams(param.Params{}.SetWellKnown(param.Namespace, apps.Namespace))

			ips, ports := istio.DefaultIngressOrFail(t, t).HTTPAddresses()
			for _, tc := range testCases {
				tc := tc
				for i, ip := range ips {
					t.NewSubTestf("%s %s %s", tc.location, tc.resolution, ip).Run(func(t framework.TestContext) {
						echotest.
							New(t, apps.All).
							// TODO eventually we can do this for uncaptured -> l7
							FromMatch(match.Not(match.ServiceName(echo.NamespacedName{
								Name:      "uncaptured",
								Namespace: apps.Namespace,
							}))).
							Config(cfg.WithParams(param.Params{
								"Resolution":      tc.resolution.String(),
								"Location":        tc.location.String(),
								"IngressIp":       ip,
								"IngressHttpPort": ports[i],
							})).
							Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
								// TODO validate L7 processing/some headers indicating we reach the svc we wanted
								from.CallOrFail(t, echo.CallOptions{
									Address: "111.111.222.222",
									Port:    to.PortForName("http"),
								})
							})
					})
				}

			}
		})
}

func TestServiceEntrySelectsUncapturedPod(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			testCases := []struct {
				location   v1alpha3.ServiceEntry_Location
				resolution v1alpha3.ServiceEntry_Resolution
				to         echo.Instances
			}{
				{
					location:   v1alpha3.ServiceEntry_MESH_INTERNAL,
					resolution: v1alpha3.ServiceEntry_STATIC,
					to:         apps.Mesh,
				},
				{
					location:   v1alpha3.ServiceEntry_MESH_EXTERNAL,
					resolution: v1alpha3.ServiceEntry_STATIC,
					to:         apps.MeshExternal,
				},
				// TODO dns cases
			}

			cfg := config.YAML(`
{{ $to := .To }}
apiVersion: networking.istio.io/v1beta1
kind: ServiceEntry
metadata:
  name: test-se
spec:
  hosts:
  - serviceentry.istio.io
  addresses:
  - 111.111.222.222
  ports:
  - number: 80
    name: http
    protocol: HTTP
    targetPort: 8080
  resolution: {{.Resolution}}
  location: {{.Location}}
  workloadSelector:
    labels:
      app: uncaptured`). // cannot select pods captured in ambient mesh; IPs are unique per network
				WithParams(param.Params{}.SetWellKnown(param.Namespace, apps.Namespace))

			for _, tc := range testCases {
				tc := tc
				t.NewSubTestf("%s %s", tc.location, tc.resolution).Run(func(t framework.TestContext) {
					echotest.
						New(t, apps.All).
						// TODO eventually we can do this for uncaptured -> l7
						FromMatch(match.Not(match.ServiceName(echo.NamespacedName{
							Name:      "uncaptured",
							Namespace: apps.Namespace,
						}))).
						ToMatch(match.ServiceName(echo.NamespacedName{
							Name:      "uncaptured",
							Namespace: apps.Namespace,
						})).
						Config(cfg.WithParams(param.Params{
							"Resolution": tc.resolution.String(),
							"Location":   tc.location.String(),
						})).
						Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
							from.CallOrFail(t, echo.CallOptions{
								Address: "serviceentry.istio.io", // host here is important to test ztunnel DNS resolution
								Port:    to.PortForName("http"),
								// sample response:
								//
								// ServiceVersion=v1
								// ServicePort=8080
								// Host=serviceentry.istio.io
								// URL=/any/path
								// Cluster=cluster-0
								// IstioVersion=
								// Method=GET
								// Proto=HTTP/1.1
								// IP=10.244.2.20
								// Alpn=
								// RequestHeader=Accept:*/*
								// RequestHeader=User-Agent:curl/7.81.0
								// Hostname=uncaptured-v1-868c9b59b5-rxvfq
								Check: check.BodyContains(`Hostname=uncaptured-v`), // can hit v1 or v2
							})
						})
				})
			}
		})
}

// Ambient ServiceEntry support for auto assigned vips is lacking for now, but planned.
// for more, see https://github.com/istio/istio/pull/45621#discussion_r1254970579
func TestServiceEntryDNSWithAutoAssign(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			t.Skip("this will work once we resolve https://github.com/istio/ztunnel/issues/582")
			yaml := `apiVersion: networking.istio.io/v1beta1
kind: ServiceEntry
metadata:
  name: test-service-entry
spec:
  hosts:
  - serviceentry.istio.io
  ports:
  - name: http
    number: 80
    protocol: HTTP
    targetPort: 8080
  location: MESH_EXTERNAL
  resolution: STATIC # not honored for now; everything is static
  workloadSelector:
    labels:
      app: uncaptured` // cannot select pods captured in ambient mesh; IPs are unique per network
			svcs := apps.All
			for _, svc := range svcs {
				if svc.Config().IsUncaptured() || svc.Config().HasSidecar() {
					// TODO(kdorosh) skip if waypoint? waypoints should not need to resolve service entry hostnames
					continue
				}
				if err := t.ConfigIstio().YAML(svc.NamespaceName(), yaml).Apply(apply.NoCleanup); err != nil {
					t.Fatal(err)
				}
				t.NewSubTestf("%v to uncaptured-v1 via ServiceEntry", svc.Config().Service).Run(func(t framework.TestContext) {
					svc.CallOrFail(t, echo.CallOptions{
						Address: "serviceentry.istio.io",
						Port:    echo.Port{Name: "http", ServicePort: 80},
						Scheme:  scheme.HTTP,
						HTTP: echo.HTTP{
							Path: "/any/path",
						},
						// sample response:
						//
						// ServiceVersion=v1
						// ServicePort=8080
						// Host=serviceentry.istio.io
						// URL=/any/path
						// Cluster=cluster-0
						// IstioVersion=
						// Method=GET
						// Proto=HTTP/1.1
						// IP=10.244.2.20
						// Alpn=
						// RequestHeader=Accept:*/*
						// RequestHeader=User-Agent:curl/7.81.0
						// Hostname=uncaptured-v1-868c9b59b5-rxvfq
						Check: check.BodyContains(`Hostname=uncaptured-v`), // can hit v1 or v2
					})
				})

				if err := t.ConfigIstio().YAML(svc.NamespaceName(), yaml).Delete(); err != nil {
					t.Fatal(err)
				}

				t.NewSubTestf("%v to uncaptured via ServiceEntry -- cleanup", svc.Config().Service).Run(func(t framework.TestContext) {
					svc.CallOrFail(t, echo.CallOptions{
						Address: "serviceentry.istio.io",
						Port:    echo.Port{Name: "http", ServicePort: 80},
						Scheme:  scheme.HTTP,
						HTTP: echo.HTTP{
							Path: "/any/path",
						},
						Check: check.NotOK(),
					})
				})
			}
		})
}

// Run runs the given reachability test cases with the context.
func RunReachability(testCases []reachability.TestCase, t framework.TestContext) {
	runTest := func(t framework.TestContext, f func(t framework.TestContext, src echo.Instance, dst echo.Instance, opt echo.CallOptions)) {
		svcs := apps.All
		for _, src := range svcs {
			src := src
			t.NewSubTestf("from %v", src.Config().Service).RunParallel(func(t framework.TestContext) {
				for _, dst := range svcs {
					dst := dst
					t.NewSubTestf("to %v", dst.Config().Service).RunParallel(func(t framework.TestContext) {
						for _, opt := range callOptions {
							opt := opt
							t.NewSubTestf("%v", opt.Scheme).RunParallel(func(t framework.TestContext) {
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
	}
	for _, c := range testCases {
		// Create a copy to avoid races, as tests are run in parallel
		c := c
		testName := strings.TrimSuffix(c.ConfigFile, filepath.Ext(c.ConfigFile))
		t.NewSubTest(testName).Run(func(t framework.TestContext) {
			// Apply the policy.
			cfg := t.ConfigIstio().File(c.Namespace.Name(), filepath.Join("testdata", c.ConfigFile))
			retry.UntilSuccessOrFail(t, func() error {
				t.Logf("[%s] [%v] Apply config %s", testName, time.Now(), c.ConfigFile)
				// TODO(https://github.com/istio/istio/issues/20460) We shouldn't need a retry loop
				return cfg.Apply(apply.Wait)
			})
			runTest(t, func(t framework.TestContext, src echo.Instance, dst echo.Instance, opt echo.CallOptions) {
				expectSuccess := c.ExpectSuccess(src, opt)
				expectMTLS := c.ExpectMTLS(src, opt)

				var tpe string
				if expectSuccess {
					tpe = "positive"
					opt.Check = check.And(
						check.OK(),
						check.ReachedTargetClusters(t))
					if expectMTLS {
						opt.Check = check.And(opt.Check, check.MTLSForHTTP())
					}
				} else {
					tpe = "negative"
					opt.Check = check.NotOK()
				}
				t.Logf("expected result: %v", tpe)

				include := c.Include
				if include == nil {
					include = func(_ echo.Instance, _ echo.CallOptions) bool { return true }
				}
				if !include(src, opt) {
					t.Skip("excluded")
				}
				src.CallOrFail(t, opt)
			})
		})
	}
}

func TestIngress(t *testing.T) {
	runIngressTest(t, func(t framework.TestContext, src ingress.Instance, dst echo.Instance, opt echo.CallOptions) {
		if opt.Scheme != scheme.HTTP {
			return
		}

		// TODO implement waypoint enforcement mechanism
		// Ingress currently never sends to Waypoints
		// We cannot bypass the waypoint, so this fails.
		// if dst.Config().HasAnyWaypointProxy() {
		// 	opt.Check = check.Error()
		// }

		t.ConfigIstio().Eval(apps.Namespace.Name(), map[string]string{
			"Destination": dst.Config().Service,
		}, `apiVersion: networking.istio.io/v1alpha3
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
    hosts: ["*"]
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: route
spec:
  gateways:
  - gateway
  hosts:
  - "*"
  http:
  - route:
    - destination:
        host: "{{.Destination}}"
`).ApplyOrFail(t)
		src.CallOrFail(t, opt)
	})
}

var CheckDeny = check.Or(
	check.ErrorContains("rpc error: code = PermissionDenied"), // gRPC
	check.ErrorContains("EOF"),                                // TCP envoy
	check.ErrorContains("read: connection reset by peer"),     // TCP ztunnel
	check.NoErrorAndStatus(http.StatusForbidden),              // HTTP
	check.NoErrorAndStatus(http.StatusServiceUnavailable),     // HTTP client, TCP server
)

func runTest(t *testing.T, f func(t framework.TestContext, src echo.Instance, dst echo.Instance, opt echo.CallOptions)) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		runTestContext(t, f)
	})
}

func runTestContext(t framework.TestContext, f func(t framework.TestContext, src echo.Instance, dst echo.Instance, opt echo.CallOptions)) {
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
}

func runIngressTest(t *testing.T, f func(t framework.TestContext, src ingress.Instance, dst echo.Instance, opt echo.CallOptions)) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		svcs := apps.All
		for _, dst := range svcs {
			t.NewSubTestf("to %v", dst.Config().Service).Run(func(t framework.TestContext) {
				dst := dst
				opt := echo.CallOptions{
					Port:    echo.Port{Name: "http"},
					Scheme:  scheme.HTTP,
					Count:   5,
					Timeout: time.Second * 2,
					Check:   check.OK(),
					To:      dst,
				}
				f(t, istio.DefaultIngressOrFail(t, t), dst, opt)
			})
		}
	})
}

// skipOnNativeZtunnel used to skip only when on rust based ztunnel; now this is the only option so it always skips
// TODO: fix all these cases and remove
func skipOnNativeZtunnel(tc framework.TestContext, reason string) {
	tc.Skipf("Not currently supported: %v", reason)
}

func TestL7Telemetry(t *testing.T) {
	framework.NewTest(t).
		Run(func(tc framework.TestContext) {
			// ensure that some traffic from each captured workload is
			// sent to each waypoint proxy. This will likely have happened in
			// the other tests (without the teardown), but we want to make
			// sure that some traffic is seen. This test will not validate
			// exact traffic counts, but rather focus on validating that
			// the telemetry is being created and collected properly.
			for _, src := range apps.Captured {
				for _, dst := range apps.ServiceAddressedWaypoint {
					tc.NewSubTestf("from %q to %q", src.Config().Service, dst.Config().Service).Run(func(stc framework.TestContext) {
						localDst := dst
						localSrc := src
						opt := echo.CallOptions{
							Port:    echo.Port{Name: "http"},
							Scheme:  scheme.HTTP,
							Count:   5,
							Timeout: time.Second,
							Check:   check.OK(),
							To:      localDst,
						}
						// allow for delay between prometheus pulls from target pod
						// pulls should happen every 15s, so timeout if not found within 30s

						query := buildQuery(localSrc, localDst)
						stc.Logf("prometheus query: %#v", query)
						err := retry.Until(func() bool {
							stc.Logf("sending call from %q to %q", deployName(localSrc), localDst.Config().Service)
							localSrc.CallOrFail(stc, opt)
							reqs, err := prom.QuerySum(localSrc.Config().Cluster, query)
							if err != nil {
								stc.Logf("could not query for traffic from %q to %q: %v", deployName(localSrc), localDst.Config().Service, err)
								return false
							}
							if reqs == 0.0 {
								stc.Logf("found zero-valued sum for traffic from %q to %q: %v", deployName(localSrc), localDst.Config().Service, err)
								return false
							}
							return true
						}, retry.Timeout(30*time.Second), retry.BackoffDelay(1*time.Second))
						if err != nil {
							util.PromDiff(t, prom, localSrc.Config().Cluster, query)
							stc.Errorf("could not validate L7 telemetry for %q to %q: %v", deployName(localSrc), localDst.Config().Service, err)
						}
					})
				}
			}
		})
}

func TestL4Telemetry(t *testing.T) {
	framework.NewTest(t).
		Run(func(tc framework.TestContext) {
			// ensure that some traffic from each captured workload is
			// sent to each waypoint proxy. This will likely have happened in
			// the other tests (without the teardown), but we want to make
			// sure that some traffic is seen. This test will not validate
			// exact traffic counts, but rather focus on validating that
			// the telemetry is being created and collected properly.
			for _, src := range apps.Captured {
				for _, dst := range apps.Captured {
					tc.NewSubTestf("from %q to %q", src.Config().Service, dst.Config().Service).Run(func(stc framework.TestContext) {
						localDst := dst
						localSrc := src
						opt := echo.CallOptions{
							Port:    echo.Port{Name: "tcp"},
							Scheme:  scheme.TCP,
							Count:   5,
							Timeout: time.Second,
							Check:   check.OK(),
							To:      localDst,
						}
						// allow for delay between prometheus pulls from target pod
						// pulls should happen every 15s, so timeout if not found within 30s

						query := buildL4Query(localSrc, localDst)
						stc.Logf("prometheus query: %#v", query)
						err := retry.Until(func() bool {
							stc.Logf("sending call from %q to %q", deployName(localSrc), localDst.Config().Service)
							localSrc.CallOrFail(stc, opt)
							reqs, err := prom.QuerySum(localSrc.Config().Cluster, query)
							if err != nil {
								stc.Logf("could not query for traffic from %q to %q: %v", deployName(localSrc), localDst.Config().Service, err)
								return false
							}
							if reqs == 0.0 {
								stc.Logf("found zero-valued sum for traffic from %q to %q: %v", deployName(localSrc), localDst.Config().Service, err)
								return false
							}
							return true
						}, retry.Timeout(15*time.Second), retry.BackoffDelay(1*time.Second))
						if err != nil {
							util.PromDiff(t, prom, localSrc.Config().Cluster, query)
							stc.Errorf("could not validate L4 telemetry for %q to %q: %v", deployName(localSrc), localDst.Config().Service, err)
						}
					})
				}
			}
		})
}

func buildQuery(src, dst echo.Instance) prometheus.Query {
	query := prometheus.Query{}

	srcns := src.NamespaceName()
	destns := dst.NamespaceName()

	labels := map[string]string{
		"reporter":                       "waypoint",
		"request_protocol":               "http",
		"response_code":                  "200",
		"response_flags":                 "-",
		"connection_security_policy":     "mutual_tls",
		"destination_canonical_service":  dst.ServiceName(),
		"destination_canonical_revision": dst.Config().Version,
		"destination_service":            fmt.Sprintf("%s.%s.svc.cluster.local", dst.Config().Service, destns),
		"destination_principal":          fmt.Sprintf("spiffe://cluster.local/ns/%v/sa/%v", destns, dst.Config().AccountName()),
		"destination_service_name":       dst.Config().Service,
		"destination_workload":           deployName(dst),
		"destination_workload_namespace": destns,
		"destination_service_namespace":  destns,
		"source_canonical_service":       src.ServiceName(),
		"source_canonical_revision":      src.Config().Version,
		"source_principal":               "spiffe://" + src.Config().ServiceAccountName(),
		"source_workload":                deployName(src),
		"source_workload_namespace":      srcns,
	}

	query.Metric = "istio_requests_total"
	query.Labels = labels

	return query
}

func buildL4Query(src, dst echo.Instance) prometheus.Query {
	query := prometheus.Query{}

	srcns := src.NamespaceName()
	destns := dst.NamespaceName()

	labels := map[string]string{
		"reporter":                       "destination",
		"connection_security_policy":     "mutual_tls",
		"destination_canonical_service":  dst.ServiceName(),
		"destination_canonical_revision": dst.Config().Version,
		"destination_service":            fmt.Sprintf("%s.%s.svc.cluster.local", dst.Config().Service, destns),
		"destination_service_name":       dst.Config().Service,
		"destination_service_namespace":  destns,
		"destination_principal":          "spiffe://" + dst.Config().ServiceAccountName(),
		"destination_version":            dst.Config().Version,
		"destination_workload":           deployName(dst),
		"destination_workload_namespace": destns,
		"source_canonical_service":       src.ServiceName(),
		"source_canonical_revision":      src.Config().Version,
		"source_principal":               "spiffe://" + src.Config().ServiceAccountName(),
		"source_version":                 src.Config().Version,
		"source_workload":                deployName(src),
		"source_workload_namespace":      srcns,
	}

	query.Metric = "istio_tcp_connections_opened_total"
	query.Labels = labels

	return query
}

func deployName(inst echo.Instance) string {
	return inst.ServiceName() + "-" + inst.Config().Version
}

func TestMetadataServer(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		ver, _ := t.Clusters().Default().GetKubernetesVersion()
		if !strings.Contains(ver.GitVersion, "-gke") {
			t.Skip("requires GKE cluster")
		}
		svcs := apps.All
		for _, src := range svcs {
			src := src
			t.NewSubTestf("from %v", src.Config().Service).Run(func(t framework.TestContext) {
				// curl -H "Metadata-Flavor: Google" 169.254.169.254/computeMetadata/v1/instance/service-accounts/default/identity
				opts := echo.CallOptions{
					Address: "169.254.169.254",
					Port:    echo.Port{ServicePort: 80},
					Scheme:  scheme.HTTP,
					HTTP: echo.HTTP{
						// TODO: detect which platform?
						Headers: headers.New().With("Metadata-Flavor", "Google").Build(),
						Path:    "/computeMetadata/v1/instance/service-accounts/default/identity",
					},
					// Test that we see our own identity -- not the ztunnel (istio-system/ztunnel).
					// TODO: if the test SA actually had workload identity enabled the result is probably different
					Check: check.BodyContains(fmt.Sprintf(`Your Kubernetes service account (%s/%s)`, src.NamespaceName(), src.Config().AccountName())),
				}
				src.CallOrFail(t, opts)
			})
		}
	})
}

func TestAPIServer(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		svcs := apps.All
		token, err := t.Clusters().Default().Kube().CoreV1().ServiceAccounts(apps.Namespace.Name()).CreateToken(context.Background(), "default",
			&authenticationv1.TokenRequest{
				Spec: authenticationv1.TokenRequestSpec{
					Audiences:         []string{"kubernetes.default.svc"},
					ExpirationSeconds: ptr.Of(int64(600)),
				},
			}, metav1.CreateOptions{})
		assert.NoError(t, err)

		for _, src := range svcs {
			src := src
			t.NewSubTestf("from %v", src.Config().Service).Run(func(t framework.TestContext) {
				opts := echo.CallOptions{
					Address: "kubernetes.default.svc",
					Port:    echo.Port{ServicePort: 443},
					Scheme:  scheme.HTTPS,
					HTTP: echo.HTTP{
						Headers: headers.New().With("Authorization", "Bearer "+token.Status.Token).Build(),
						Path:    "/",
					},
					// Test that we see our own identity -- not the ztunnel (istio-system/ztunnel).
					Check: check.BodyContains(fmt.Sprintf(`system:serviceaccount:%v:default`, apps.Namespace.Name())),
				}
				src.CallOrFail(t, opts)
			})
		}
	})
}

func TestDirect(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		t.NewSubTest("waypoint").Run(func(t framework.TestContext) {
			c := common.NewCaller()
			cert, err := istio.CreateCertificate(t, i, apps.Captured.ServiceName(), apps.Namespace.Name())
			if err != nil {
				t.Fatal(err)
			}
			// this is real odd but we're going to assume for now that we've just got the one waypoint I guess?
			hbwl := echo.HBONE{
				Address:            apps.WaypointProxies[apps.WorkloadAddressedWaypoint.Config().WorkloadWaypointProxy].Inbound(),
				Headers:            nil,
				Cert:               string(cert.ClientCert),
				Key:                string(cert.Key),
				CaCert:             string(cert.RootCert),
				InsecureSkipVerify: true,
			}
			hbsvc := echo.HBONE{
				Address:            apps.WaypointProxies[apps.ServiceAddressedWaypoint.Config().ServiceWaypointProxy].Inbound(),
				Headers:            nil,
				Cert:               string(cert.ClientCert),
				Key:                string(cert.Key),
				CaCert:             string(cert.RootCert),
				InsecureSkipVerify: true,
			}
			run := func(name string, options echo.CallOptions) {
				t.NewSubTest(name).Run(func(t framework.TestContext) {
					_, err := c.CallEcho(nil, options)
					if err != nil {
						t.Fatal(err)
					}
				})
			}
			run("named destination", echo.CallOptions{
				To:    apps.WorkloadAddressedWaypoint, // TODO: not sure how this is actually addressed?
				Count: 1,
				Port:  echo.Port{Name: ports.HTTP.Name},
				HBONE: hbwl,
				// This is not supported now, discussion in https://github.com/istio/istio/issues/43241
				Check: check.Error(),
			})
			run("VIP destination", echo.CallOptions{
				To:      apps.ServiceAddressedWaypoint,
				Count:   1,
				Address: apps.ServiceAddressedWaypoint[0].Address(),
				Port:    echo.Port{Name: ports.HTTP.Name},
				HBONE:   hbsvc,
				Check:   check.OK(),
			})
			run("VIP destination, unknown port", echo.CallOptions{
				To:      apps.ServiceAddressedWaypoint,
				Count:   1,
				Address: apps.ServiceAddressedWaypoint[0].Address(),
				Port:    echo.Port{ServicePort: 12345},
				Scheme:  scheme.HTTP,
				HBONE:   hbsvc,
				// TODO: VIP:* should error sooner for undeclared ports
				Check: check.Error(),
			})
			run("Pod IP destination", echo.CallOptions{
				To:      apps.WorkloadAddressedWaypoint,
				Count:   1,
				Address: apps.WorkloadAddressedWaypoint[0].WorkloadsOrFail(t)[0].Address(),
				Port:    echo.Port{ServicePort: ports.HTTP.WorkloadPort},
				Scheme:  scheme.HTTP,
				HBONE:   hbwl,
				Check:   check.OK(),
			})
			run("Unserved VIP destination", echo.CallOptions{
				To:      apps.Captured,
				Count:   1,
				Address: apps.Captured[0].Address(),
				Port:    echo.Port{ServicePort: ports.HTTP.ServicePort},
				Scheme:  scheme.HTTP,
				HBONE:   hbsvc,
				Check:   check.Error(),
			})
			run("Unserved pod destination", echo.CallOptions{
				To:      apps.Captured,
				Count:   1,
				Address: apps.Captured[0].WorkloadsOrFail(t)[0].Address(),
				Port:    echo.Port{ServicePort: ports.HTTP.ServicePort},
				Scheme:  scheme.HTTP,
				HBONE:   hbwl,
				Check:   check.Error(),
			})
			run("Waypoint destination", echo.CallOptions{
				To:      apps.ServiceAddressedWaypoint,
				Count:   1,
				Address: apps.WaypointProxies[apps.ServiceAddressedWaypoint.Config().ServiceWaypointProxy].PodIP(),
				Port:    echo.Port{ServicePort: 15000},
				Scheme:  scheme.HTTP,
				HBONE:   hbsvc,
				Check:   check.Error(),
			})
		})
	})
}
