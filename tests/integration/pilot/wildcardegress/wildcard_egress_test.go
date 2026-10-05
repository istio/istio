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

package wildcardegress

import (
	"net/netip"
	"testing"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
)

// routeViaEgressGateway sends TLS traffic for the wildcard host from sidecars to the egress gateway,
// which passes it through to a wildcard DYNAMIC_DNS ServiceEntry resolved by Envoy's dynamic forward proxy.
const routeViaEgressGateway = `
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: wildcard-external
spec:
  hosts:
  - "{{.WildcardHost}}"
  ports:
  - number: 443
    name: tls
    protocol: TLS
  location: MESH_EXTERNAL
  resolution: DYNAMIC_DNS
---
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: wildcard-egress
spec:
  selector:
    istio: {{.EgressLabel}}
  servers:
  - port:
      number: 443
      name: tls
      protocol: TLS
    hosts:
    - "{{.WildcardHost}}"
    tls:
      mode: PASSTHROUGH
---
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: wildcard-via-egress-gateway
spec:
  hosts:
  - "{{.WildcardHost}}"
  gateways:
  - mesh
  - wildcard-egress
  tls:
  - match:
    - gateways: [mesh]
      port: 443
      sniHosts: ["{{.WildcardHost}}"]
    route:
    - destination:
        host: {{.EgressService}}.{{.EgressNamespace}}.svc.cluster.local
        port:
          number: 443
  - match:
    - gateways: [wildcard-egress]
      port: 443
      sniHosts: ["{{.WildcardHost}}"]
    route:
    - destination:
        host: "{{.WildcardHost}}"
        port:
          number: 443
`

// denySNIAtEgressGateway rejects the host at the egress gateway, which proves the traffic goes through it.
const denySNIAtEgressGateway = `
apiVersion: security.istio.io/v1
kind: AuthorizationPolicy
metadata:
  name: deny-wildcard-sni
spec:
  selector:
    matchLabels:
      istio: {{.EgressLabel}}
  action: DENY
  rules:
  - when:
    - key: connection.sni
      values: ["{{.Host}}"]
`

func TestEgressGatewayWildcardDynamicDNS(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			client := match.ServiceName(echo.NamespacedName{Name: "client", Namespace: apps.Namespace}).GetMatches(apps.All.Instances())
			if len(client) == 0 {
				t.Fatal("client echo not found")
			}
			if !hasIPv4(t, client) {
				t.Skip("TODO: skipping test as wildcard DNS doesn't support resolving to IPv6 address")
			}

			host := apps.External.All.Config().ClusterLocalFQDN()
			settings := i.Settings()
			args := map[string]string{
				"WildcardHost":    "*." + apps.External.Namespace.Name() + ".svc.cluster.local",
				"Host":            host,
				"EgressLabel":     settings.EgressGatewayIstioLabel,
				"EgressService":   settings.EgressGatewayServiceName,
				"EgressNamespace": settings.EgressGatewayServiceNamespace,
			}
			t.ConfigIstio().Eval(apps.Namespace.Name(), args, routeViaEgressGateway).ApplyOrFail(t)

			call := func(c echo.Checker) echo.CallOptions {
				return echo.CallOptions{
					Address: host,
					Port:    echo.Port{ServicePort: 443},
					Scheme:  scheme.HTTPS,
					TLS: echo.TLS{
						// The external echo serves a certificate for a different name.
						InsecureSkipVerify: true,
						ServerName:         host,
					},
					Count: 1,
					Check: c,
				}
			}

			// Config propagation is not awaited (apply.Wait is a no-op), and the client can also reach the
			// external service directly. The deny step is therefore a barrier: it can only fail the call once the
			// sidecar routes through the egress gateway, so the success that follows must have used the gateway.
			deny := t.ConfigIstio().Eval(settings.EgressGatewayServiceNamespace, args, denySNIAtEgressGateway)
			deny.ApplyOrFail(t, apply.NoCleanup)
			t.NewSubTest("denied by AuthorizationPolicy at egress gateway").Run(func(t framework.TestContext) {
				for _, c := range client {
					c.CallOrFail(t, call(check.Error()))
				}
			})
			t.NewSubTest("tls passthrough to wildcard host via egress gateway").Run(func(t framework.TestContext) {
				deny.DeleteOrFail(t)
				for _, c := range client {
					c.CallOrFail(t, call(check.OK()))
				}
			})
		})
}

// routeViaEgressGatewayMutualTLS sends TLS traffic for the wildcard host from sidecars to the egress gateway inside
// Istio mTLS. The gateway terminates the mTLS and forwards the application TLS to a wildcard DYNAMIC_DNS
// ServiceEntry. The DestinationRule SNI selects the gateway server.
const routeViaEgressGatewayMutualTLS = `
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: wildcard-external-mtls
spec:
  hosts:
  - "{{.WildcardHost}}"
  ports:
  - number: 443
    name: tls
    protocol: TLS
  location: MESH_EXTERNAL
  resolution: DYNAMIC_DNS
---
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: wildcard-egress-mtls
spec:
  selector:
    istio: {{.EgressLabel}}
  servers:
  - port:
      number: 443
      name: tls-mtls
      protocol: TLS
    hosts:
    - "{{.WildcardHost}}"
    tls:
      mode: ISTIO_MUTUAL
---
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: wildcard-egress-mtls
spec:
  host: {{.EgressService}}.{{.EgressNamespace}}.svc.cluster.local
  subsets:
  - name: wildcard
    trafficPolicy:
      tls:
        mode: ISTIO_MUTUAL
        sni: "{{.GatewaySNI}}"
---
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: wildcard-via-egress-gateway-mtls
spec:
  hosts:
  - "{{.WildcardHost}}"
  gateways:
  - mesh
  - wildcard-egress-mtls
  tls:
  - match:
    - gateways: [mesh]
      port: 443
      sniHosts: ["{{.WildcardHost}}"]
    route:
    - destination:
        host: {{.EgressService}}.{{.EgressNamespace}}.svc.cluster.local
        subset: wildcard
        port:
          number: 443
  tcp:
  - match:
    - gateways: [wildcard-egress-mtls]
      port: 443
    route:
    - destination:
        host: "{{.WildcardHost}}"
        port:
          number: 443
`

// allowPerCallerAtEgressGateway allows each caller only its own host. The other client is only allowed a host
// that it does not call.
const allowPerCallerAtEgressGateway = `
apiVersion: security.istio.io/v1
kind: AuthorizationPolicy
metadata:
  name: allow-per-caller
spec:
  selector:
    matchLabels:
      istio: {{.EgressLabel}}
  action: ALLOW
  rules:
  - from:
    - source:
        principals: ["{{.AllowedPrincipal}}"]
    when:
    - key: connection.sni
      values: ["{{.Host}}"]
  - from:
    - source:
        principals: ["{{.OtherPrincipal}}"]
    when:
    - key: connection.sni
      values: ["other.{{.Host}}"]
`

func TestEgressGatewayMutualTLSWildcardDynamicDNS(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			allowed := match.ServiceName(echo.NamespacedName{Name: "client", Namespace: apps.Namespace}).GetMatches(apps.All.Instances())
			other := match.ServiceName(echo.NamespacedName{Name: "other-client", Namespace: apps.Namespace}).GetMatches(apps.All.Instances())
			if len(allowed) == 0 || len(other) == 0 {
				t.Fatal("client echoes not found")
			}
			if !hasIPv4(t, allowed) {
				t.Skip("TODO: skipping test as wildcard DNS doesn't support resolving to IPv6 address")
			}

			host := apps.External.All.Config().ClusterLocalFQDN()
			wildcardSuffix := apps.External.Namespace.Name() + ".svc.cluster.local"
			settings := i.Settings()
			args := map[string]string{
				"WildcardHost":     "*." + wildcardSuffix,
				"GatewaySNI":       "egress." + wildcardSuffix,
				"Host":             host,
				"EgressLabel":      settings.EgressGatewayIstioLabel,
				"EgressService":    settings.EgressGatewayServiceName,
				"EgressNamespace":  settings.EgressGatewayServiceNamespace,
				"AllowedPrincipal": allowed.Config().SpiffeIdentity(),
				"OtherPrincipal":   other.Config().SpiffeIdentity(),
			}
			t.ConfigIstio().Eval(apps.Namespace.Name(), args, routeViaEgressGatewayMutualTLS).ApplyOrFail(t)

			call := func(c echo.Checker) echo.CallOptions {
				return echo.CallOptions{
					Address: host,
					Port:    echo.Port{ServicePort: 443},
					Scheme:  scheme.HTTPS,
					TLS: echo.TLS{
						// The external echo serves a certificate for a different name.
						InsecureSkipVerify: true,
						ServerName:         host,
					},
					Count: 1,
					Check: c,
				}
			}

			// Config propagation is not awaited (apply.Wait is a no-op), and the clients can also reach the
			// external service directly. The deny step is therefore a barrier: it can only fail the calls once the
			// sidecars route through the egress gateway, so the results that follow must have used the gateway.
			deny := t.ConfigIstio().Eval(settings.EgressGatewayServiceNamespace, args, denySNIAtEgressGateway)
			deny.ApplyOrFail(t, apply.NoCleanup)
			t.NewSubTest("denied by AuthorizationPolicy at egress gateway").Run(func(t framework.TestContext) {
				for _, c := range append(allowed, other...) {
					c.CallOrFail(t, call(check.Error()))
				}
			})

			// The allow policy is applied before the deny policy is removed, so a successful call proves that the
			// gateway enforces the allow policy with the caller identity and the real host.
			t.ConfigIstio().Eval(settings.EgressGatewayServiceNamespace, args, allowPerCallerAtEgressGateway).ApplyOrFail(t)
			deny.DeleteOrFail(t)
			t.NewSubTest("allowed caller reaches wildcard host over mTLS").Run(func(t framework.TestContext) {
				for _, c := range allowed {
					c.CallOrFail(t, call(check.OK()))
				}
			})
			t.NewSubTest("other caller denied for the same host").Run(func(t framework.TestContext) {
				for _, c := range other {
					c.CallOrFail(t, call(check.Error()))
				}
			})
		})
}

func hasIPv4(t framework.TestContext, instances echo.Instances) bool {
	for _, a := range instances.WorkloadsOrFail(t).Addresses() {
		if ip, err := netip.ParseAddr(a); err == nil && ip.Is4() {
			return true
		}
	}
	return false
}
