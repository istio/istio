package pilot

import (
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

func TestDNSAutoAllocation(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			ns := namespace.NewOrFail(t, t, namespace.Config{
				Prefix: "dns-auto-allocation",
				Inject: true,
			})
			cfg := `apiVersion: networking.istio.io/v1beta1
kind: ProxyConfig
metadata:
  name: enable-dns-auto-allocation
spec:
  environmentVariables:
    ISTIO_META_DNS_CAPTURE: "true"
    ISTIO_META_DNS_AUTO_ALLOCATE: "true"
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: fake-local
spec:
  hosts:
  - "fake.local"
  resolution: DNS
  ports:
  - number: 80
    name: http
    protocol: HTTP
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: route-to-b
spec:
  hosts:
  - fake.local
  http:
  - route:
    - destination:
        host: b.{{.echoNamespace}}.svc.cluster.local
`
			t.ConfigIstio().Eval(ns.Name(), map[string]string{"echoNamespace": apps.Namespace.Name()}, cfg).ApplyOrFail(t)
			instances := deployment.New(t, t.AllClusters().Configs()...).WithConfig(echo.Config{Namespace: ns, Service: "a"}).BuildOrFail(t)

			_ = instances[0].CallOrFail(t, echo.CallOptions{
				Address: "fake.local",
				Port: echo.Port{
					Name:        "http",
					ServicePort: 80,
					Protocol:    protocol.HTTP,
				},
				Check: check.OK(),
			})
		})
}
