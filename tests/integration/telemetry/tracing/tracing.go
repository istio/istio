//go:build integ

// Copyright Istio Authors. All Rights Reserved.
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

package tracing

import (
	"fmt"
	"strings"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/zipkin"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	client, server echo.Instances
	ist            istio.Instance
	ingInst        ingress.Instance
	zipkinInst     zipkin.Instance
	appNsInst      namespace.Instance
)

const (
	TraceHeader = "x-client-trace-id"
)

func GetIstioInstance() *istio.Instance {
	return &ist
}

// GetAppNamespace gets echo app namespace instance.
func GetAppNamespace() namespace.Instance {
	return appNsInst
}

func GetIngressInstance() ingress.Instance {
	return ingInst
}

func GetZipkinInstance() zipkin.Instance {
	return zipkinInst
}

func TestSetup(ctx resource.Context) (err error) {
	appNsInst, err = namespace.New(ctx, namespace.Config{
		Prefix: "echo",
		Inject: true,
	})
	if err != nil {
		return err
	}
	builder := deployment.New(ctx)
	for _, c := range ctx.Clusters() {
		clName := c.Name()
		builder = builder.
			WithConfig(echo.Config{
				Service:   fmt.Sprintf("client-%s", clName),
				Namespace: appNsInst,
				Cluster:   c,
				Ports:     nil,
				Subsets:   []echo.SubsetConfig{{}},
			}).
			WithConfig(echo.Config{
				Service:   "server",
				Namespace: appNsInst,
				Cluster:   c,
				Subsets:   []echo.SubsetConfig{{}},
				Ports: []echo.Port{
					{
						Name:         "http",
						Protocol:     protocol.HTTP,
						WorkloadPort: 8090,
					},
					{
						Name:     "tcp",
						Protocol: protocol.TCP,
						// We use a port > 1024 to not require root
						WorkloadPort: 9000,
					},
				},
			})
	}
	echos, err := builder.Build()
	if err != nil {
		return err
	}

	servicePrefix := func(prefix string) match.Matcher {
		return func(i echo.Instance) bool {
			return strings.HasPrefix(i.Config().Service, prefix)
		}
	}
	client = servicePrefix("client").GetMatches(echos)
	server = match.ServiceName(echo.NamespacedName{Name: "server", Namespace: appNsInst}).GetMatches(echos)
	ingInst = ist.IngressFor(ctx.Clusters().Default())
	addrs, _ := ingInst.HTTPAddresses()
	zipkinInst, err = zipkin.New(ctx, zipkin.Config{Cluster: ctx.Clusters().Default(), IngressAddr: addrs[0]})
	if err != nil {
		return err
	}
	return nil
}

func VerifyEchoTraces(t framework.TestContext, namespace, clName string, traces []zipkin.Trace) bool {
	t.Helper()
	wtr := WantTraceRoot(namespace, clName)
	for _, trace := range traces {
		// compare each candidate trace with the wanted trace
		for _, s := range trace.Spans {
			// find the root span of candidate trace and do recursive comparison
			if s.ParentSpanID == "" && CompareTrace(t, s, wtr) {
				return true
			}
		}
	}

	return false
}

func VerifyOtelEchoTraces(t framework.TestContext, namespace, clName string, traces []zipkin.Trace) bool {
	t.Helper()
	wtr := WantOtelTraceRoot(namespace, clName)
	for _, trace := range traces {
		// compare each candidate trace with the wanted trace
		for _, s := range trace.Spans {
			// find the root span of candidate trace and do recursive comparison
			if s.ParentSpanID == "" && CompareTrace(t, s, wtr) {
				return true
			}
		}
	}

	return false
}

func WantOtelTraceRoot(namespace, clName string) (root zipkin.Span) {
	serverSpan := zipkin.Span{
		Name:        fmt.Sprintf("server.%s.svc.cluster.local:80/*", namespace),
		ServiceName: fmt.Sprintf("server.%s", namespace),
	}

	root = zipkin.Span{
		Name:        fmt.Sprintf("server.%s.svc.cluster.local:80/*", namespace),
		ServiceName: fmt.Sprintf("client-%s.%s", clName, namespace),
		ChildSpans:  []*zipkin.Span{&serverSpan},
	}
	return root
}

func VerifyOtelIngressTraces(t framework.TestContext, namespace, path string, traces []zipkin.Trace) bool {
	t.Helper()
	wtr := WantOtelIngressTraceRoot(namespace, path)
	for _, trace := range traces {
		// compare each candidate trace with the wanted trace
		for _, s := range trace.Spans {
			// find the root span of candidate trace and do recursive comparison
			if s.ParentSpanID == "" && CompareTrace(t, s, wtr) {
				return true
			}
		}
	}

	return false
}

func WantOtelIngressTraceRoot(namespace, path string) (root zipkin.Span) {
	return zipkin.Span{
		ServiceName: "istio-ingressgateway.istio-system",
		Name:        fmt.Sprintf("server.%s.svc.cluster.local:80%s", namespace, path),
		ChildSpans: []*zipkin.Span{
			{
				Name:        fmt.Sprintf("router outbound|80||server.%s.svc.cluster.local; egress", namespace),
				ServiceName: "istio-ingressgateway.istio-system",
				ChildSpans: []*zipkin.Span{
					{
						Name:        fmt.Sprintf("server.%s.svc.cluster.local:80%s", namespace, path),
						ServiceName: fmt.Sprintf("server.%s", namespace),
					},
				},
			},
		},
	}
}

// CompareTrace recursively compares the two given spans
func CompareTrace(t framework.TestContext, got, want zipkin.Span) bool {
	t.Helper()
	if got.Name != want.Name || got.ServiceName != want.ServiceName {
		t.Logf("got span %+v, want span %+v", got, want)
		return false
	}
	if len(got.ChildSpans) < len(want.ChildSpans) {
		t.Logf("got %d child spans from, want %d child spans, maybe trace has not be fully reported",
			len(got.ChildSpans), len(want.ChildSpans))
		return false
	} else if len(got.ChildSpans) > len(want.ChildSpans) {
		t.Logf("got %d child spans from, want %d child spans, maybe destination rule has not became effective",
			len(got.ChildSpans), len(want.ChildSpans))
		return false
	}
	for i := range got.ChildSpans {
		if !CompareTrace(t, *got.ChildSpans[i], *want.ChildSpans[i]) {
			return false
		}
	}
	return true
}

// WantTraceRoot constructs the wanted trace and returns the root span of that trace
func WantTraceRoot(namespace, clName string) (root zipkin.Span) {
	serverSpan := zipkin.Span{
		Name:        fmt.Sprintf("server.%s.svc.cluster.local:80/*", namespace),
		ServiceName: fmt.Sprintf("server.%s", namespace),
	}

	root = zipkin.Span{
		Name:        fmt.Sprintf("server.%s.svc.cluster.local:80/*", namespace),
		ServiceName: fmt.Sprintf("client-%s.%s", clName, namespace),
		ChildSpans:  []*zipkin.Span{&serverSpan},
	}
	return root
}

// SendTraffic makes a client call to the "server" service on the http port.
func SendTraffic(t framework.TestContext, headers map[string][]string, cl cluster.Cluster) error {
	t.Helper()
	t.Logf("Sending from %s...", cl.Name())
	for _, cltInstance := range client {
		if cltInstance.Config().Cluster != cl {
			continue
		}

		_, err := cltInstance.Call(echo.CallOptions{
			To: server,
			Port: echo.Port{
				Name: "http",
			},
			HTTP: echo.HTTP{
				Headers: headers,
			},
			Retry: echo.Retry{
				NoRetry: true,
			},
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// SendIngressTraffic makes a call to the "server" service from ingress.
func SendIngressTraffic(t framework.TestContext, path string, headers map[string][]string, cl cluster.Cluster) error {
	t.Helper()
	t.Logf("Sending from %s ingress...", cl.Name())
	ing := GetIngressInstance()
	_, err := ing.Call(echo.CallOptions{
		Port: echo.Port{
			Protocol: protocol.HTTP,
		},
		Count: 10,
		HTTP: echo.HTTP{
			Path:    path,
			Headers: headers,
		},
		Check: check.OK(),
		Retry: echo.Retry{
			NoRetry: true,
		},
	})
	if err != nil {
		return err
	}
	return nil
}
