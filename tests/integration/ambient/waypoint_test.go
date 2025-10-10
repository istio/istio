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

package ambient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8s "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

func TestWaypointStatus(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			t.NewSubTest("gateway class").Run(func(t framework.TestContext) {
				client := t.Clusters().Default().GatewayAPI().GatewayV1beta1().GatewayClasses()

				check := func() error {
					gwc, _ := client.Get(context.Background(), constants.WaypointGatewayClassName, metav1.GetOptions{})
					if gwc == nil {
						return fmt.Errorf("failed to find GatewayClass %v", constants.WaypointGatewayClassName)
					}
					cond := kstatus.GetCondition(gwc.Status.Conditions, string(k8s.GatewayClassConditionStatusAccepted))
					if cond.Status != metav1.ConditionTrue {
						return fmt.Errorf("failed to find accepted condition: %+v", cond)
					}
					if cond.ObservedGeneration != gwc.Generation {
						return fmt.Errorf("stale GWC generation: %+v", cond)
					}
					return nil
				}
				retry.UntilSuccessOrFail(t, check)

				// Wipe out the status
				gwc, _ := client.Get(context.Background(), constants.WaypointGatewayClassName, metav1.GetOptions{})
				gwc.Status.Conditions = nil
				client.Update(context.Background(), gwc, metav1.UpdateOptions{})
				// It should be added back
				retry.UntilSuccessOrFail(t, check)
			})
			t.NewSubTest("service").Run(func(t framework.TestContext) {
				retry.UntilSuccessOrFail(t, func() error {
					wp, err := t.Clusters().Default().Kube().CoreV1().
						Services(apps.Namespace.Name()).Get(context.Background(), ServiceAddressedWaypoint, metav1.GetOptions{})
					if err != nil {
						return err
					}
					cond := GetCondition(wp.Status.Conditions, string(model.WaypointBound))
					if cond == nil {
						return fmt.Errorf("condition not found on service, had %v", wp.Status.Conditions)
					}
					if cond.Status != metav1.ConditionTrue {
						return fmt.Errorf("cond not true, had %v", wp.Status.Conditions)
					}
					return nil
				})
			})
		})
}

func TestWaypoint(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			nsConfig := namespace.NewOrFail(t, namespace.Config{
				Prefix: "waypoint",
				Inject: false,
				Labels: map[string]string{
					label.IoIstioDataplaneMode.Name: "ambient",
				},
			})

			istioctl.NewOrFail(t, istioctl.Config{}).InvokeOrFail(t, []string{
				"waypoint",
				"apply",
				"--namespace",
				nsConfig.Name(),
				"--wait",
			})

			nameSet := []string{"", "w1", "w2"}
			for _, name := range nameSet {
				istioctl.NewOrFail(t, istioctl.Config{}).InvokeOrFail(t, []string{
					"waypoint",
					"apply",
					"--namespace",
					nsConfig.Name(),
					"--name",
					name,
					"--wait",
				})
			}

			istioctl.NewOrFail(t, istioctl.Config{}).InvokeOrFail(t, []string{
				"waypoint",
				"apply",
				"--namespace",
				nsConfig.Name(),
				"--name",
				"w3",
				"--enroll-namespace",
				"true",
				"--wait",
			})
			nameSet = append(nameSet, "w3")

			output, _ := istioctl.NewOrFail(t, istioctl.Config{}).InvokeOrFail(t, []string{
				"waypoint",
				"list",
				"--namespace",
				nsConfig.Name(),
			})
			for _, name := range nameSet {
				if !strings.Contains(output, name) {
					t.Fatalf("expect to find %s in output: %s", name, output)
				}
			}

			output, _ = istioctl.NewOrFail(t, istioctl.Config{}).InvokeOrFail(t, []string{
				"waypoint",
				"list",
				"-A",
			})
			for _, name := range nameSet {
				if !strings.Contains(output, name) {
					t.Fatalf("expect to find %s in output: %s", name, output)
				}
			}

			istioctl.NewOrFail(t, istioctl.Config{}).InvokeOrFail(t, []string{
				"waypoint",
				"-n",
				nsConfig.Name(),
				"delete",
				"w1",
				"w2",
			})
			retry.UntilSuccessOrFail(t, func() error {
				for _, name := range []string{"w1", "w2"} {
					if err := checkWaypointIsReady(t, nsConfig.Name(), name); err != nil {
						if !errors.Is(err, kubetest.ErrNoPodsFetched) {
							return fmt.Errorf("failed to check gateway status: %v", err)
						}
					} else {
						return fmt.Errorf("failed to delete multiple gateways: %s not cleaned up", name)
					}
				}
				return nil
			}, retry.Timeout(15*time.Second), retry.BackoffDelay(time.Millisecond*100))

			// delete all waypoints in namespace, so w3 should be deleted
			istioctl.NewOrFail(t, istioctl.Config{}).InvokeOrFail(t, []string{
				"waypoint",
				"-n",
				nsConfig.Name(),
				"delete",
				"--all",
			})
			retry.UntilSuccessOrFail(t, func() error {
				if err := checkWaypointIsReady(t, nsConfig.Name(), "w3"); err != nil {
					if errors.Is(err, kubetest.ErrNoPodsFetched) {
						return nil
					}
					return fmt.Errorf("failed to check gateway status: %v", err)
				}
				return fmt.Errorf("failed to clean up gateway in namespace: %s", nsConfig.Name())
			}, retry.Timeout(15*time.Second), retry.BackoffDelay(time.Millisecond*100))
		})
}

func checkWaypointIsReady(t framework.TestContext, ns, name string) error {
	fetch := kubetest.NewPodFetch(t.AllClusters()[0], ns, label.IoK8sNetworkingGatewayGatewayName.Name+"="+name)
	_, err := kubetest.CheckPodsAreReady(fetch)
	return err
}

func TestSimpleHTTPSandwich(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			config := `
apiVersion: networking.istio.io/v1beta1
kind: ProxyConfig
metadata:
  name: disable-hbone
spec:
  selector:
    matchLabels:
      gateway.networking.k8s.io/gateway-name: simple-http-waypoint
  environmentVariables:
    ISTIO_META_DISABLE_HBONE_SEND: "true"
---
apiVersion: gateway.networking.k8s.io/v1beta1
kind: Gateway
metadata:
  name: simple-http-waypoint
  namespace: {{.Namespace}}
  labels:
    istio.io/dataplane-mode: ambient
  annotations:
    networking.istio.io/service-type: ClusterIP
spec:
  gatewayClassName: istio
  listeners:
  - name: {{.Service}}-fqdn
    hostname: {{.Service}}.{{.Namespace}}.svc.cluster.local
    port: {{.Port}}
    protocol: HTTP
    allowedRoutes:
      namespaces:
        from: Same
  - name: {{.Service}}-svc
    hostname: {{.Service}}.{{.Namespace}}.svc
    port: {{.Port}}
    protocol: HTTP
    allowedRoutes:
      namespaces:
        from: Same
  - name: {{.Service}}-namespace
    hostname: {{.Service}}.{{.Namespace}}
    port: {{.Port}}
    protocol: HTTP
    allowedRoutes:
      namespaces:
        from: Same
  - name: {{.Service}}-short
    hostname: {{.Service}}
    port: {{.Port}}
    protocol: HTTP
    allowedRoutes:
      namespaces:
        from: Same
  # HACK:zTunnel currently expects the HBONE port to always be on the Waypoint's Service
  # This will be fixed in future PRs to both istio and zTunnel.
  - name: fake-hbone-port
    port: 15008
    protocol: TCP
---
apiVersion: gateway.networking.k8s.io/v1beta1
kind: HTTPRoute
metadata:
  name: {{.Service}}-httproute
spec:
  parentRefs:
  - name: simple-http-waypoint
  hostnames:
  - {{.Service}}.{{.Namespace}}.svc.cluster.local
  - {{.Service}}.{{.Namespace}}.svc
  - {{.Service}}.{{.Namespace}}
  - {{.Service}}
  rules:
  - matches:
    - path:
        type: PathPrefix
        value: /
    filters:
    - type: ResponseHeaderModifier
      responseHeaderModifier:
        add:
        - name: traversed-waypoint
          value: {{.Service}}-gateway
    backendRefs:
    - name: {{.Service}}
      port: {{.Port}}
      `

			t.ConfigKube().
				New().
				Eval(
					apps.Namespace.Name(),
					map[string]any{
						"Service":   Captured,
						"Namespace": apps.Namespace.Name(),
						"Port":      apps.Captured.PortForName("http").ServicePort,
					},
					config).
				ApplyOrFail(t, apply.CleanupConditionally)

			retry.UntilSuccessOrFail(t, func() error {
				return checkWaypointIsReady(t, apps.Namespace.Name(), "simple-http-waypoint")
			}, retry.Timeout(2*time.Minute))

			// Update use-waypoint for Captured service
			SetWaypoint(t, Captured, "simple-http-waypoint")

			// ensure HTTP traffic works with all hostname variants
			for _, src := range apps.All {
				if !hboneClient(src) {
					// TODO if we hairpinning, don't skip here
					continue
				}
				t.NewSubTestf("from %s", src.ServiceName()).Run(func(t framework.TestContext) {
					if src.Config().HasSidecar() {
						t.Skip("TODO: sidecars don't properly handle use-waypoint")
					}
					for _, host := range apps.Captured.Config().HostnameVariants() {
						t.NewSubTestf("to %s", host).Run(func(t framework.TestContext) {
							src.CallOrFail(t, echo.CallOptions{
								To:      apps.Captured,
								Address: host,
								Port:    echo.Port{Name: "http"},
								Scheme:  scheme.HTTP,
								Count:   10,
								Check: check.And(
									check.OK(),
									check.ResponseHeader("traversed-waypoint", "captured-gateway"),
								),
							})
						})
					}
					apps.Captured.ServiceName()
				})
			}
		})
}

func SetWaypoint(t framework.TestContext, svc string, waypoint string) {
	setWaypointInternal(t, svc, apps.Namespace.Name(), waypoint, true)
}

func SetWaypointServiceEntry(t framework.TestContext, se, namespace string, waypoint string) {
	setWaypointInternal(t, se, namespace, waypoint, false)
}

func setWaypointInternal(t framework.TestContext, name, ns string, waypoint string, service bool) {
	for _, c := range t.Clusters() {
		setWaypoint := func(waypoint string) error {
			if waypoint == "" {
				waypoint = "null"
			} else {
				waypoint = fmt.Sprintf("%q", waypoint)
			}
			label := []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":%s}}}`,
				label.IoIstioUseWaypoint.Name, waypoint))
			if service {
				_, err := c.Kube().CoreV1().Services(ns).Patch(context.TODO(), name, types.MergePatchType, label, metav1.PatchOptions{})
				return err
			}
			_, err := c.Istio().NetworkingV1().ServiceEntries(ns).Patch(context.TODO(), name, types.MergePatchType, label, metav1.PatchOptions{})
			return err
		}

		if err := setWaypoint(waypoint); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := setWaypoint(""); err != nil {
				scopes.Framework.Errorf("failed resetting waypoint for %s", name)
			}
		})
	}
}

func TestWaypointDNS(t *testing.T) {
	runTest := func(t framework.TestContext, c echo.Checker) {
		for _, src := range apps.All {
			if !hboneClient(src) {
				continue
			}
			t.NewSubTestf("from %s", src.ServiceName()).Run(func(t framework.TestContext) {
				if src.Config().HasSidecar() {
					t.Skip("TODO: sidecars don't properly handle use-waypoint")
				}
				v4, v6 := getSupportedIPFamilies(t)
				if v4 {
					t.NewSubTest("v4").Run(func(t framework.TestContext) {
						src.CallOrFail(t, echo.CallOptions{
							To:            apps.MockExternal,
							Address:       apps.MockExternal.Config().DefaultHostHeader,
							ForceIPFamily: echo.ForceIPFamilyV4,
							Port:          echo.Port{Name: "http"},
							Scheme:        scheme.HTTP,
							Count:         1,
							Check:         check.And(c, check.DestinationIPv4(), check.SourceIPv4()),
						})
					})
				}
				if v6 {
					t.NewSubTest("v6").Run(func(t framework.TestContext) {
						src.CallOrFail(t, echo.CallOptions{
							To:            apps.MockExternal,
							Address:       apps.MockExternal.Config().DefaultHostHeader,
							ForceIPFamily: echo.ForceIPFamilyV6,
							Port:          echo.Port{Name: "http"},
							Scheme:        scheme.HTTP,
							Count:         1,
							// Depending on the environment, the destination may or may not actually get a destination IPv6 address.
							// With waypoint: we always send to IPv4 on the waypoint if it has an IPv4 address (https://github.com/istio/istio/issues/52318)
							// Without waypoint: Ztunnel DNS currently prefers IPv4, so it will always win if there is an IPv4 address.
							// (https://github.com/istio/ztunnel/issues/1225)
							Check: check.And(c, check.SourceIPv6()),
						})
					})
				}
			})
		}
	}
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			t.NewSubTest("without waypoint").Run(func(t framework.TestContext) {
				runTest(t, check.OK())
			})
			t.NewSubTest("with waypoint").Run(func(t framework.TestContext) {
				// Update use-waypoint for Captured service
				SetWaypointServiceEntry(t, "external-service", apps.Namespace.Name(), "waypoint")
				runTest(t, check.And(check.OK(), IsL7()))
			})
		})
}

type externalSubsetService struct {
	Name      string
	SubsetKey string
	Hostname  string
	ClusterIP string
}

// servicesForSubsets is a helper function to create a kubernetes service for all subsets of a service
func servicesForSubsets(t framework.TestContext, instance echo.Instance) []externalSubsetService {
	ns := instance.Config().Namespace.Name()
	services := []externalSubsetService{}
	for _, subset := range instance.Config().Subsets {
		svc := corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      instance.Config().Service + "-" + subset.Version,
				Namespace: ns,
				Labels: map[string]string{
					"app":     instance.Config().Service,
					"version": subset.Version,
				},
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{
					"app":     instance.Config().Service,
					"version": subset.Version,
				},
				Ports: []corev1.ServicePort{
					{
						Name: "http",
						Port: 8080,
					},
				},
			},
		}

		s, err := t.Clusters().Default().Kube().CoreV1().Services(ns).Create(context.TODO(), &svc, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("failed to create service %s: %v", svc.Name, err)
		}
		t.Cleanup(func() {
			t.Clusters().Default().Kube().CoreV1().Services(ns).Delete(context.TODO(), s.Name, metav1.DeleteOptions{})
		})
		services = append(services, externalSubsetService{
			Name:      s.Name,
			SubsetKey: subset.Version,
			Hostname:  instance.WorkloadsOrFail(t)[0].PodName(),
			ClusterIP: s.Spec.ClusterIP,
		})
	}
	return services
}

func TestWaypointAsEgressGateway(t *testing.T) {
	runTest := func(t framework.TestContext, name string, config string, skipMultiClusterReason string, opts ...echo.CallOptions) {
		t.NewSubTest(name).Run(func(t framework.TestContext) {
			if skipMultiClusterReason != "" && t.Settings().AmbientMultiNetwork {
				t.Skip(skipMultiClusterReason)
			}
			if config != "" {
				t.ConfigIstio().YAML(apps.Namespace.Name(), config).ApplyOrFail(t)
			}
			for _, src := range apps.All {
				if !hboneClient(src) {
					continue
				}
				t.NewSubTestf("from %s", src.ServiceName()).Run(func(t framework.TestContext) {
					if src.Config().HasSidecar() {
						t.Skip("TODO: sidecars don't properly handle use-waypoint")
					}
					for _, o := range opts {
						src.CallOrFail(t, o)
					}
				})
			}
		})
	}
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			egressNamespace, err := namespace.Claim(t, namespace.Config{
				Prefix: "egress",
				Inject: false,
			})
			assert.NoError(t, err)
			waypointSpec := `apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: egress-gateway
spec:
  gatewayClassName: istio-waypoint
  listeners:
  - name: mesh
    port: 15008
    protocol: HBONE
    allowedRoutes:
      namespaces:
        from: Selector
        selector:
          matchLabels:
            kubernetes.io/metadata.name: "{{.}}"
`
			t.ConfigIstio().
				Eval(egressNamespace.Name(), apps.Namespace.Name(), waypointSpec).
				ApplyOrFail(t, apply.CleanupConditionally)

			external := apps.MockExternal.Instances()[0]

			if len(external.WorkloadsOrFail(t)) < 1 {
				t.Skip("not enough external service instances")
			}

			subsetServices := servicesForSubsets(t, external)
			externalIPs := []string{}
			for _, s := range subsetServices {
				externalIPs = append(externalIPs, s.ClusterIP)
			}

			resolutionNoneServiceEntry := `apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: external-resolution-none
  labels:
    istio.io/use-waypoint: egress-gateway
    istio.io/use-waypoint-namespace: {{.EgressNamespace}}
spec:
  hosts:
  - fake-passthrough.example.com
  ports:
  - name: http
    number: 8080
    protocol: HTTP
  resolution: NONE
  location: MESH_EXTERNAL
  addresses:
{{ range $ip := .IPs }}
  - "{{$ip}}"
{{ end }}
`
			t.ConfigIstio().
				Eval(apps.Namespace.Name(), map[string]any{
					"EgressNamespace": egressNamespace.Name(),
					"IPs":             externalIPs,
				}, resolutionNoneServiceEntry).
				ApplyOrFail(t, apply.CleanupConditionally)

			hostHeader := http.Header{}
			hostHeader.Set("Host", "fake-passthrough.example.com")

			// Test that we send traffic through the waypoint successfully
			for i, sss := range subsetServices {
				if i != 0 {
					// Presently, only the first IP will be used by the waypoint proxy.
					continue
				}
				// The setup for this testing would be tricky in multi-network because the VIPs being used
				// are not in the mesh, but they are in a cluster.
				// This means each cluster's VIPs would need to be unique.
				// That isn't useful for testing though because it's just turning the multi-cluster
				// tests into multiple single-cluster tests.
				// Arguably, egress gateways should never be accessed across a cluster-boundary,
				// so perhaps the skips need not be removed as even in multi-cluster testing we expect egress
				// to behave as though it is single-cluster.
				testName := fmt.Sprintf("resolution none %s", sss.ClusterIP)
				runTest(t, testName, "",
					"relies on unmeshed ClusterIPs as a simulated external service IP",
					echo.CallOptions{
						Address: sss.ClusterIP,
						HTTP:    echo.HTTP{Headers: hostHeader},
						Port:    echo.Port{ServicePort: 8080},
						Scheme:  scheme.HTTP,
						Count:   1,
						Check:   check.And(check.OK(), IsL7(), check.Hostname(sss.Hostname)),
					})
			}

			service := `apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: external
  labels:
    istio.io/use-waypoint: egress-gateway
    istio.io/use-waypoint-namespace: {{.EgressNamespace}}
spec:
  hosts:
  - fake-egress.example.com
  ports:
  - name: http
    number: 80
    protocol: HTTP
  - name: https
    number: 443
    protocol: HTTPS
  - name: http-for-tls
    number: 8080
    protocol: HTTP
    targetPort: 443
  location: MESH_EXTERNAL
  resolution: DNS
  endpoints:
  - address: external.{{.ExternalNamespace}}.svc.cluster.local`
			// ServiceEntry in app namespace, points to waypoint in EgressNamespace. Backend is in ExternalNamespace
			t.ConfigIstio().
				Eval(apps.Namespace.Name(), map[string]string{
					"ExternalNamespace": apps.ExternalNamespace.Name(),
					"EgressNamespace":   egressNamespace.Name(),
				}, service).
				ApplyOrFail(t)

			// We can send a simple request
			runTest(t, "basic", "", "", echo.CallOptions{
				Address: "fake-egress.example.com",
				Port:    echo.Port{ServicePort: 80},
				Scheme:  scheme.HTTP,
				Count:   1,
				Check:   check.And(check.OK(), IsL7()),
			})

			// Test we can do TLS origination, by utilizing ServiceEntry target port
			tlsOrigination := `apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: "tls-origination"
spec:
  host: "fake-egress.example.com"
  trafficPolicy:
    tls:
      mode: SIMPLE
      insecureSkipVerify: true`

			t.ConfigIstio().
				Eval(apps.Namespace.Name(), map[string]string{}, tlsOrigination).
				ApplyOrFail(t)
			time.Sleep(60 * time.Minute)
			runTest(t, "http origination targetPort", tlsOrigination, "", echo.CallOptions{
				Address: "fake-egress.example.com",
				Port:    echo.Port{ServicePort: 8080},
				Scheme:  scheme.HTTP,
				Count:   1,
				Check:   check.And(check.OK(), IsL7(), check.Alpn("http/1.1")),
			})

			tlsOriginationRedirect := tlsOrigination + `
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: route-port
spec:
  parentRefs:
  - kind: ServiceEntry
    group: networking.istio.io
    name: external
  rules:
  - backendRefs:
    - kind: Hostname
      group: networking.istio.io
      name: fake-egress.example.com
      port: 443
`
			runTest(t, "http origination route", tlsOriginationRedirect, "", echo.CallOptions{
				Address: "fake-egress.example.com",
				Port:    echo.Port{ServicePort: 80},
				Scheme:  scheme.HTTP,
				Count:   1,
				Check:   check.And(check.OK(), IsL7(), check.Alpn("http/1.1")),
			})

			authz := `apiVersion: security.istio.io/v1
kind: AuthorizationPolicy
metadata:
  name: only-get
spec:
  targetRefs:
  - kind: ServiceEntry
    group: networking.istio.io
    name: external
  action: ALLOW
  rules:
  - to:
    - operation:
        methods: ["GET"]
`
			runTest(
				t,
				"authz on service allow",
				authz,
				"",
				// Check blocked requests are denied
				echo.CallOptions{
					Address: "fake-egress.example.com",
					Port:    echo.Port{ServicePort: 80},
					HTTP:    echo.HTTP{Method: "POST"},
					Scheme:  scheme.HTTP,
					Count:   1,
					Check:   check.Status(403),
				},
				// And allowed ones are not
				echo.CallOptions{
					Address: "fake-egress.example.com",
					Port:    echo.Port{ServicePort: 80},
					Scheme:  scheme.HTTP,
					Count:   1,
					Check:   check.And(check.OK(), IsL7()),
				},
			)
		})
}

func TestIngressToWaypoint(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		// Apply a deny-all waypoint policy. This allows us to test the traffic traverses the waypoint
		t.ConfigIstio().Eval(apps.Namespace.Name(), map[string]string{
			"Waypoint": apps.ServiceAddressedWaypoint.Config().ServiceWaypointProxy,
		}, `
apiVersion: security.istio.io/v1
kind: AuthorizationPolicy
metadata:
  name: deny-all-waypoint
spec:
  targetRefs:
  - kind: Gateway
    group: gateway.networking.k8s.io
    name: {{.Waypoint}}
`).ApplyOrFail(t)
		t.NewSubTest("sidecar-service").Run(func(t framework.TestContext) {
			if t.Settings().AmbientMultiNetwork {
				t.Skip("https://github.com/istio/istio/issues/54245")
			}
			for _, src := range apps.Sidecar {
				for _, dst := range apps.ServiceAddressedWaypoint {
					for _, opt := range basicCalls {
						t.NewSubTestf("%v", opt.Scheme).Run(func(t framework.TestContext) {
							opt = opt.DeepCopy()
							opt.To = dst
							// Sidecar does not currently traverse waypoint, so we expect to bypass it and get success
							opt.Check = check.OK()
							src.CallOrFail(t, opt)
						})
					}
				}
			}
		})
		t.NewSubTest("sidecar-workload").Run(func(t framework.TestContext) {
			if t.Settings().AmbientMultiNetwork {
				t.Skip("https://github.com/istio/istio/issues/54245")
			}
			for _, src := range apps.Sidecar {
				for _, dst := range apps.WorkloadAddressedWaypoint {
					for _, dstWl := range dst.WorkloadsOrFail(t) {
						for _, opt := range basicCalls {
							t.NewSubTestf("%v-%v", opt.Scheme, dstWl.Address()).Run(func(t framework.TestContext) {
								opt = opt.DeepCopy()
								opt.Address = dstWl.Address()
								opt.Port = echo.Port{ServicePort: ports.All().MustForName(opt.Port.Name).WorkloadPort}
								// Sidecar does not currently traverse waypoint, so we expect to bypass it and get success
								opt.Check = check.OK()
								src.CallOrFail(t, opt)
							})
						}
					}
				}
			}
		})
		t.NewSubTest("ingress-service").Run(func(t framework.TestContext) {
			if t.Settings().AmbientMultiNetwork {
				t.Skip("https://github.com/istio/istio/issues/54245")
			}
			t.ConfigIstio().Eval(apps.Namespace.Name(), map[string]string{
				"Destination": apps.ServiceAddressedWaypoint.ServiceName(),
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
			ingress := istio.DefaultIngressOrFail(t, t)
			t.NewSubTest("endpoint routing").Run(func(t framework.TestContext) {
				ingress.CallOrFail(t, echo.CallOptions{
					Port: echo.Port{
						Protocol:    protocol.HTTP,
						ServicePort: 80,
					},
					Scheme: scheme.HTTP,
					Check:  check.OK(),
				})
			})
			t.NewSubTest("service routing").Run(func(t framework.TestContext) {
				SetIngressUseWaypoint(t, apps.ServiceAddressedWaypoint.ServiceName(), apps.ServiceAddressedWaypoint.NamespaceName())
				ingress.CallOrFail(t, echo.CallOptions{
					Port: echo.Port{
						Protocol:    protocol.HTTP,
						ServicePort: 80,
					},
					Scheme: scheme.HTTP,
					Check:  CheckDeny,
				})
			})
		})
		t.NewSubTest("ingress-workload").Run(func(t framework.TestContext) {
			t.Skip("not implemented")
			t.ConfigIstio().Eval(apps.Namespace.Name(), map[string]string{
				"Destination": apps.WorkloadAddressedWaypoint.ServiceName(),
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
			ingress := istio.DefaultIngressOrFail(t, t)
			t.NewSubTest("endpoint routing").Run(func(t framework.TestContext) {
				ingress.CallOrFail(t, echo.CallOptions{
					Port: echo.Port{
						Protocol:    protocol.HTTP,
						ServicePort: 80,
					},
					Scheme: scheme.HTTP,
					Check:  CheckDeny,
				})
			})
			t.NewSubTest("service routing").Run(func(t framework.TestContext) {
				// This will be ignored entirely if there is only workload waypoint, so this behaves the same as endpoint routing.
				SetIngressUseWaypoint(t, apps.WorkloadAddressedWaypoint.ServiceName(), apps.WorkloadAddressedWaypoint.NamespaceName())
				ingress.CallOrFail(t, echo.CallOptions{
					Port: echo.Port{
						Protocol:    protocol.HTTP,
						ServicePort: 80,
					},
					Scheme: scheme.HTTP,
					Check:  CheckDeny,
				})
			})
		})
	})
}

func TestTCPRoute(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		t.ConfigIstio().YAML(apps.Namespace.Name(), `apiVersion: gateway.networking.k8s.io/v1alpha2
kind: TCPRoute
metadata:
  name: tcproute
spec:
  parentRefs:
    - group: ""
      kind: Service
      name: service-addressed-waypoint
  rules:
    - backendRefs:
        - name: captured
          port: 9090
          weight: 3
        - name: uncaptured
          port: 9090
          weight: 1
        - name: service-addressed-waypoint
          port: 9093
          weight: 1
`).ApplyOrFail(t)
		apps.Captured[0].CallOrFail(t, echo.CallOptions{
			To:    apps.ServiceAddressedWaypoint,
			Port:  ports.TCP,
			Count: 40,
			Check: check.And(check.OK(), func(result echo.CallResult, err error) error {
				gotCaptured, gotUncaptured, gotWaypoint := 0, 0, 0
				for _, r := range result.Responses {
					if strings.HasPrefix(r.Hostname, "captured-") && r.Port == "19090" {
						gotCaptured++
					}
					if strings.HasPrefix(r.Hostname, "uncaptured-") && r.Port == "19090" {
						gotUncaptured++
					}
					if strings.HasPrefix(r.Hostname, "service-addressed-waypoint-") && r.Port == "16061" {
						gotWaypoint++
					}
				}
				if gotCaptured == 0 || gotUncaptured == 0 || gotWaypoint == 0 {
					return fmt.Errorf("didn't hit all expected backends (%v, %v, %v)", gotCaptured, gotUncaptured, gotWaypoint)
				}
				if gotCaptured < gotUncaptured || gotCaptured < gotWaypoint {
					return fmt.Errorf("captured has the highest weight so it should get the most requests (%v, %v, %v)",
						gotCaptured, gotUncaptured, gotWaypoint)
				}
				return nil
			}),
		})
	})
}

func TestTLSRoute(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		t.ConfigIstio().YAML(apps.Namespace.Name(), `apiVersion: gateway.networking.k8s.io/v1alpha2
kind: TLSRoute
metadata:
  name: tlsroute
spec:
  parentRefs:
    - group: ""
      kind: Service
      name: service-addressed-waypoint
  rules:
    - backendRefs:
        - name: captured
          port: 9090
          weight: 3
        - name: uncaptured
          port: 9090
          weight: 1
        - name: service-addressed-waypoint
          port: 9093
          weight: 1
`).ApplyOrFail(t)
		apps.Captured[0].CallOrFail(t, echo.CallOptions{
			To:    apps.ServiceAddressedWaypoint,
			Port:  ports.TCP,
			Count: 40,
			Check: check.And(check.OK(), func(result echo.CallResult, err error) error {
				gotCaptured, gotUncaptured, gotWaypoint := 0, 0, 0
				for _, r := range result.Responses {
					if strings.HasPrefix(r.Hostname, "captured-") && r.Port == "19090" {
						gotCaptured++
					}
					if strings.HasPrefix(r.Hostname, "uncaptured-") && r.Port == "19090" {
						gotUncaptured++
					}
					if strings.HasPrefix(r.Hostname, "service-addressed-waypoint-") && r.Port == "16061" {
						gotWaypoint++
					}
				}
				if gotCaptured == 0 || gotUncaptured == 0 || gotWaypoint == 0 {
					return fmt.Errorf("didn't hit all expected backends (%v, %v, %v)", gotCaptured, gotUncaptured, gotWaypoint)
				}
				if gotCaptured < gotUncaptured || gotCaptured < gotWaypoint {
					return fmt.Errorf("captured has the highest weight so it should get the most requests (%v, %v, %v)",
						gotCaptured, gotUncaptured, gotWaypoint)
				}
				return nil
			}),
		})
	})
}

func TestWaypointAsEgressGatewayForWildcardEntries(t *testing.T) {
	runTest := func(t framework.TestContext, name string, config string, opts ...echo.CallOptions) {
		t.NewSubTest(name).Run(func(t framework.TestContext) {
			if config != "" {
				t.ConfigIstio().YAML(apps.Namespace.Name(), config).ApplyOrFail(t)
			}
			for _, src := range apps.All {
				if !hboneClient(src) {
					continue
				}
				t.NewSubTestf("from %s", src.ServiceName()).Run(func(t framework.TestContext) {
					if src.Config().HasSidecar() {
						t.Skip("TODO: sidecars don't properly handle use-waypoint")
					}
					for _, o := range opts {
						src.CallOrFail(t, o)
					}
				})
			}
		})
	}
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			if _, v6 := getSupportedIPFamilies(t); v6 {
				t.Skip("TODO: skipping test as wildcard DNS doesn't support resolving to IPv6 address")
			}
			egressNamespace, err := namespace.Claim(t, namespace.Config{
				Prefix: "wildcard-egress",
				Inject: false,
			})
			assert.NoError(t, err)
			waypointSpec := `apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: wildcard-egress-gateway
spec:
  gatewayClassName: istio-waypoint
  listeners:
  - name: mesh
    port: 15008
    protocol: HBONE
    allowedRoutes:
      namespaces:
        from: Selector
        selector:
          matchLabels:
            kubernetes.io/metadata.name: "{{.}}"
`
			t.ConfigIstio().
				Eval(egressNamespace.Name(), apps.Namespace.Name(), waypointSpec).
				ApplyOrFail(t, apply.CleanupConditionally)

			service := `apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: external-wildcard
  labels:
    istio.io/use-waypoint: wildcard-egress-gateway
    istio.io/use-waypoint-namespace: {{.EgressNamespace}}
spec:
  hosts:
  - "*.{{.ExternalNamespace}}.svc.cluster.local"
  ports:
  - name: http
    number: 80
    protocol: HTTP
  - name: http-for-tls
    number: 8080
    protocol: HTTP
    targetPort: 443
  location: MESH_EXTERNAL
  resolution: DYNAMIC_DNS`
			// ServiceEntry in app namespace, points to waypoint in EgressNamespace. Backend is in ExternalNamespace
			t.ConfigIstio().
				Eval(apps.Namespace.Name(), map[string]string{
					"ExternalNamespace": apps.ExternalNamespace.Name(),
					"EgressNamespace":   egressNamespace.Name(),
				}, service).
				ApplyOrFail(t)
			// We can send a simple request
			runTest(t, "basic", "", echo.CallOptions{
				Address: fmt.Sprintf("external.%s.svc.cluster.local", apps.ExternalNamespace.Name()),
				Port:    echo.Port{ServicePort: 80},
				Scheme:  scheme.HTTP,
				Count:   1,
				Check:   check.And(check.OK(), IsL7()),
			})

			// Try to use a different Host header than the target host
			invalidHeader := make(http.Header, 1)
			invalidHeader.Add("Host", "external.non-existent.svc.cluster.local")
			runTest(t, "overriding with invalid Host header", "", echo.CallOptions{
				Address: fmt.Sprintf("external.%s.svc.cluster.local", apps.ExternalNamespace.Name()),
				Port:    echo.Port{ServicePort: 80},
				Scheme:  scheme.HTTP,
				Count:   1,
				HTTP: echo.HTTP{
					Headers: invalidHeader,
				},
				// We expect the request to return a 404 since the Host does not match the wildcarded hostname
				Check: check.And(check.Status(404)),
			})

			matchingHeader := make(http.Header, 1)
			matchingHeader.Add("Host", fmt.Sprintf("external.%s.svc.cluster.local", apps.ExternalNamespace.Name()))
			runTest(t, "overriding with matching Host header", "", echo.CallOptions{
				Address: fmt.Sprintf("non-existent.%s.svc.cluster.local", apps.ExternalNamespace.Name()),
				Port:    echo.Port{ServicePort: 80},
				Scheme:  scheme.HTTP,
				Count:   1,
				HTTP: echo.HTTP{
					Headers: matchingHeader,
				},
				// We expect the request to succeed since Host matches the wildcarded hostname even though it is not the original destination host
				Check: check.And(check.OK(), IsL7()),
			})

			// Test we can do TLS origination, by utilizing ServiceEntry target port
			wildacardTLSOrigination := `apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: wildcard-tls-origination
spec:
  host: "*.{{.ExternalNamespace}}.svc.cluster.local"
  trafficPolicy:
    tls:
      mode: SIMPLE
      insecureSkipVerify: true
`
			t.ConfigIstio().
				Eval(apps.Namespace.Name(), map[string]string{
					"ExternalNamespace": apps.ExternalNamespace.Name(),
				}, wildacardTLSOrigination).
				ApplyOrFail(t)

			runTest(t, "http origination targetPort", "", echo.CallOptions{
				Address: fmt.Sprintf("external.%s.svc.cluster.local", apps.ExternalNamespace.Name()),
				Port:    echo.Port{ServicePort: 8080},
				Scheme:  scheme.HTTP,
				Count:   1,
				Check:   check.And(check.OK(), IsL7(), check.Alpn("http/1.1")),
			})

			wildcardTLSOriginationRedirect := wildacardTLSOrigination + `
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: route-port
spec:
  parentRefs:
  - kind: ServiceEntry
    group: networking.istio.io
    name: external
  rules:
  - backendRefs:
    - kind: Hostname
      group: networking.istio.io
      name: "*.{{.ExternalNamespace}}.svc.cluster.local"
      port: 443
`
			t.ConfigIstio().
				Eval(apps.Namespace.Name(), map[string]string{
					"ExternalNamespace": apps.ExternalNamespace.Name(),
				}, wildcardTLSOriginationRedirect).
				ApplyOrFail(t)

			runTest(t, "http origination targetPort", "", echo.CallOptions{
				Address: fmt.Sprintf("external.%s.svc.cluster.local", apps.ExternalNamespace.Name()),
				Port:    echo.Port{ServicePort: 80},
				Scheme:  scheme.HTTP,
				Count:   1,
				Check:   check.And(check.OK(), IsL7(), check.Alpn("http/1.1")),
			})
		})
}

func SetIngressUseWaypoint(t framework.TestContext, name, ns string) {
	for _, c := range t.Clusters() {
		set := func(service bool) error {
			var set string
			if service {
				set = fmt.Sprintf("%q", "true")
			} else {
				set = "null"
			}
			label := []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":%s}}}`,
				"istio.io/ingress-use-waypoint", set))
			_, err := c.Kube().CoreV1().Services(ns).Patch(context.TODO(), name, types.MergePatchType, label, metav1.PatchOptions{})
			return err
		}

		if err := set(true); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := set(false); err != nil {
				scopes.Framework.Errorf("failed resetting service-addressed for %s", name)
			}
		})
	}
}

func GetCondition(conditions []metav1.Condition, condition string) *metav1.Condition {
	for _, cond := range conditions {
		if cond.Type == condition {
			return &cond
		}
	}
	return nil
}
