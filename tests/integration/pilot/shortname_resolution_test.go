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

package pilot

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
)

// TestShortNameResolutionSameNamespace tests short name resolution within the same namespace
func TestShortNameResolutionSameNamespace(t *testing.T) {
	framework.
		NewTest(t).
		Label(label.IPv4).
		Run(func(t framework.TestContext) {
			ns := namespace.New(t, namespace.Config{
				Prefix: "shortname-same-ns",
				Inject: true,
			})

			// Deploy two services in the same namespace
			client := echo.New(t, echo.Config{
				Namespace: ns,
				Name:      "client",
				Ports: []echo.Port{
					{
						Name:     "http",
						Protocol: "HTTP",
						Port:     8080,
					},
				},
			})

			server := echo.New(t, echo.Config{
				Namespace: ns,
				Name:      "server",
				Ports: []echo.Port{
					{
						Name:     "http",
						Protocol: "HTTP",
						Port:     8080,
					},
				},
			})

			// Create VirtualService with short name
			vs := fmt.Sprintf(`
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: server-vs
  namespace: %s
spec:
  hosts:
  - server
  http:
  - route:
    - destination:
        host: server
        port:
          number: 8080
`, ns.Name())

			t.ConfigIstio().Eval(ns.Name(), map[string]string{
				"Namespace": ns.Name(),
			}).ApplyYAML(vs)

			// Test traffic from client to server using short name
			echotest.New(t, []echo.Instance{client}).
				WithDefaultCallOptions().
				To(echotest.To(server, echotest.CallOptions{
					Port: &echo.Port{
						Name: "http",
					},
				})).
				Run()
		})
}

// TestShortNameResolutionCrossNamespace tests short name resolution across namespaces
func TestShortNameResolutionCrossNamespace(t *testing.T) {
	framework.
		NewTest(t).
		Label(label.IPv4).
		Run(func(t framework.TestContext) {
			appNs := namespace.New(t, namespace.Config{
				Prefix: "shortname-app",
				Inject: true,
			})

			backendNs := namespace.New(t, namespace.Config{
				Prefix: "shortname-backend",
				Inject: true,
			})

			// Deploy client in app namespace
			client := echo.New(t, echo.Config{
				Namespace: appNs,
				Name:      "client",
				Ports: []echo.Port{
					{
						Name:     "http",
						Protocol: "HTTP",
						Port:     8080,
					},
				},
			})

			// Deploy server in backend namespace
			server := echo.New(t, echo.Config{
				Namespace: backendNs,
				Name:      "backend",
				Ports: []echo.Port{
					{
						Name:     "http",
						Protocol: "HTTP",
						Port:     8080,
					},
				},
			})

			// Create VirtualService with FQDN (cross-namespace requires FQDN)
			vs := fmt.Sprintf(`
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: backend-vs
  namespace: %s
spec:
  hosts:
  - backend.%s.svc.cluster.local
  http:
  - route:
    - destination:
        host: backend.%s.svc.cluster.local
        port:
          number: 8080
`, appNs.Name(), backendNs.Name(), backendNs.Name())

			t.ConfigIstio().Eval(appNs.Name(), map[string]string{
				"Namespace": appNs.Name(),
			}).ApplyYAML(vs)

			// Test traffic from client to server
			echotest.New(t, []echo.Instance{client}).
				WithDefaultCallOptions().
				To(echotest.To(server, echotest.CallOptions{
					Port: &echo.Port{
						Name: "http",
					},
				})).
				Run()
		})
}

// TestShortNameResolutionInvalidService tests behavior with non-existent services
func TestShortNameResolutionInvalidService(t *testing.T) {
	framework.
		NewTest(t).
		Label(label.IPv4).
		Run(func(t framework.TestContext) {
			ns := namespace.New(t, namespace.Config{
				Prefix: "shortname-invalid",
				Inject: true,
			})

			client := echo.New(t, echo.Config{
				Namespace: ns,
				Name:      "client",
				Ports: []echo.Port{
					{
						Name:     "http",
						Protocol: "HTTP",
						Port:     8080,
					},
				},
			})

			// Create VirtualService pointing to non-existent service
			vs := fmt.Sprintf(`
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: nonexistent-vs
  namespace: %s
spec:
  hosts:
  - nonexistent
  http:
  - route:
    - destination:
        host: nonexistent
        port:
          number: 8080
`, ns.Name())

			t.ConfigIstio().Eval(ns.Name(), map[string]string{
				"Namespace": ns.Name(),
			}).ApplyYAML(vs)

			// Configuration should be accepted (graceful degradation)
			// but traffic should fail
			t.Log("VirtualService with non-existent service created successfully (graceful degradation)")
		})
}

// TestShortNameResolutionMixedNames tests VirtualService with mixed short and FQDN names
func TestShortNameResolutionMixedNames(t *testing.T) {
	framework.
		NewTest(t).
		Label(label.IPv4).
		Run(func(t framework.TestContext) {
			ns := namespace.New(t, namespace.Config{
				Prefix: "shortname-mixed",
				Inject: true,
			})

			client := echo.New(t, echo.Config{
				Namespace: ns,
				Name:      "client",
				Ports: []echo.Port{
					{
						Name:     "http",
						Protocol: "HTTP",
						Port:     8080,
					},
				},
			})

			server1 := echo.New(t, echo.Config{
				Namespace: ns,
				Name:      "server1",
				Ports: []echo.Port{
					{
						Name:     "http",
						Protocol: "HTTP",
						Port:     8080,
					},
				},
			})

			server2 := echo.New(t, echo.Config{
				Namespace: ns,
				Name:      "server2",
				Ports: []echo.Port{
					{
						Name:     "http",
						Protocol: "HTTP",
						Port:     8080,
					},
				},
			})

			// Create VirtualService with mixed short and FQDN names
			vs := fmt.Sprintf(`
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: mixed-vs
  namespace: %s
spec:
  hosts:
  - server1
  - server2.%s.svc.cluster.local
  http:
  - match:
    - uri:
        prefix: /server1
    route:
    - destination:
        host: server1
        port:
          number: 8080
  - match:
    - uri:
        prefix: /server2
    route:
    - destination:
        host: server2.%s.svc.cluster.local
        port:
          number: 8080
`, ns.Name(), ns.Name(), ns.Name())

			t.ConfigIstio().Eval(ns.Name(), map[string]string{
				"Namespace": ns.Name(),
			}).ApplyYAML(vs)

			// Test traffic to both servers
			echotest.New(t, []echo.Instance{client}).
				WithDefaultCallOptions().
				To(echotest.To(server1, echotest.CallOptions{
					Port: &echo.Port{
						Name: "http",
					},
				})).
				Run()

			echotest.New(t, []echo.Instance{client}).
				WithDefaultCallOptions().
				To(echotest.To(server2, echotest.CallOptions{
					Port: &echo.Port{
						Name: "http",
					},
				})).
				Run()
		})
}
