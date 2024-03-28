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
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8s "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

func TestWaypointStatus(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.ambient").
		Run(func(t framework.TestContext) {
			client := t.Clusters().Kube().Default().GatewayAPI().GatewayV1beta1().GatewayClasses()

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
}

func TestWaypoint(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.ambient").
		Run(func(t framework.TestContext) {
			nsConfig := namespace.NewOrFail(t, t, namespace.Config{
				Prefix: "waypoint",
				Inject: false,
				Labels: map[string]string{
					constants.DataplaneMode: "ambient",
				},
			})

			istioctl.NewOrFail(t, t, istioctl.Config{}).InvokeOrFail(t, []string{
				"x",
				"waypoint",
				"apply",
				"--namespace",
				nsConfig.Name(),
				"--wait",
			})

			saSet := []string{"sa1", "sa2", "sa3"}
			for _, sa := range saSet {
				istioctl.NewOrFail(t, t, istioctl.Config{}).InvokeOrFail(t, []string{
					"x",
					"waypoint",
					"apply",
					"--namespace",
					nsConfig.Name(),
					"--service-account",
					sa,
					"--wait",
				})
			}

			output, _ := istioctl.NewOrFail(t, t, istioctl.Config{}).InvokeOrFail(t, []string{
				"x",
				"waypoint",
				"list",
				"--namespace",
				nsConfig.Name(),
			})
			for _, sa := range saSet {
				if !strings.Contains(output, sa) {
					t.Fatalf("expect to find %s in output: %s", sa, output)
				}
			}

			output, _ = istioctl.NewOrFail(t, t, istioctl.Config{}).InvokeOrFail(t, []string{
				"x",
				"waypoint",
				"list",
				"-A",
			})
			for _, sa := range saSet {
				if !strings.Contains(output, sa) {
					t.Fatalf("expect to find %s in output: %s", sa, output)
				}
			}

			istioctl.NewOrFail(t, t, istioctl.Config{}).InvokeOrFail(t, []string{
				"x",
				"waypoint",
				"-n",
				nsConfig.Name(),
				"delete",
			})
			retry.UntilSuccessOrFail(t, func() error {
				if err := checkWaypointIsReady(t, nsConfig.Name(), "namespace"); err != nil {
					if errors.Is(err, kubetest.ErrNoPodsFetched) {
						return nil
					}
					return fmt.Errorf("failed to check gateway status: %v", err)
				}
				return fmt.Errorf("failed to clean up gateway in namespace: %s", nsConfig.Name())
			}, retry.Timeout(15*time.Second), retry.BackoffDelay(time.Millisecond*100))

			istioctl.NewOrFail(t, t, istioctl.Config{}).InvokeOrFail(t, []string{
				"x",
				"waypoint",
				"-n",
				nsConfig.Name(),
				"delete",
				"sa1",
				"sa2",
			})
			retry.UntilSuccessOrFail(t, func() error {
				for _, sa := range []string{"sa1", "sa2"} {
					if err := checkWaypointIsReady(t, nsConfig.Name(), sa); err != nil {
						if !errors.Is(err, kubetest.ErrNoPodsFetched) {
							return fmt.Errorf("failed to check gateway status: %v", err)
						}
					} else {
						return fmt.Errorf("failed to delete multiple gateways: %s not cleaned up", sa)
					}
				}
				return nil
			}, retry.Timeout(15*time.Second), retry.BackoffDelay(time.Millisecond*100))

			// delete all waypoints in namespace, so sa3 should be deleted
			istioctl.NewOrFail(t, t, istioctl.Config{}).InvokeOrFail(t, []string{
				"x",
				"waypoint",
				"-n",
				nsConfig.Name(),
				"delete",
				"--all",
			})
			retry.UntilSuccessOrFail(t, func() error {
				if err := checkWaypointIsReady(t, nsConfig.Name(), "sa3"); err != nil {
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
	fetch := kubetest.NewPodFetch(t.AllClusters()[0], ns, constants.GatewayNameLabel+"="+name)
	_, err := kubetest.CheckPodsAreReady(fetch)
	return err
}

func TestSimpleHTTPSandwich(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.ambient").
		Run(func(t framework.TestContext) {
			config := `
apiVersion: gateway.networking.k8s.io/v1beta1
kind: Gateway
metadata:
  name: {{.Service}}-gateway
  namespace: {{.Namespace}}
  annotations:
    networking.istio.io/address-type: IPAddress
    networking.istio.io/service-type: ClusterIP
    ambient.istio.io/redirection: enabled
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
  - name: {{.Service}}-gateway
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

				// Update use-waypoint for Captured service
			for _, c := range t.Clusters().Kube() {
				client := c.Kube().CoreV1().Services(apps.Namespace.Name())
				setWaypoint := func(waypoint string) error {
					annotation := []byte(fmt.Sprintf(`{"metadata":{"annotations":{"%s":"%s"}}}`,
						constants.AmbientUseWaypoint, waypoint))
					_, err := client.Patch(context.TODO(), Captured, types.MergePatchType, annotation, metav1.PatchOptions{})
					return err
				}

				if err := setWaypoint("captured-gateway"); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := setWaypoint(""); err != nil {
						scopes.Framework.Errorf("failed resetting waypoint for %s", Captured)
					}
				})

			}

			// ensure HTTP traffic works with all hostname variants
			for _, src := range apps.All {
				src := src
				if !hboneClient(src) {
					// TODO if we hairpinning, don't skip here
					continue
				}
				t.NewSubTestf("from %s", src.ServiceName()).Run(func(t framework.TestContext) {
					if src.Config().HasSidecar() {
						t.Skip("TODO: sidecars don't properly handle use-waypoint")
					}
					for _, host := range apps.Captured.Config().HostnameVariants() {
						host := host
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
