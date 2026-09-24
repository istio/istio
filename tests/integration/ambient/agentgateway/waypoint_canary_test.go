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

package agentgateway

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/annotation"
	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/crd"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

const canaryPrimaryWaypointName = "istio-waypoint-primary"

// acceptAny lets a call succeed regardless of HTTP status; the test classifies the responses itself
// rather than failing on a non-2xx.
var acceptAny = func(echo.CallResult, error) error { return nil }

// TestWeightedWaypointCanaryToAgentgateway shifts connections from an istio (Envoy) waypoint to an
// agentgateway waypoint using the weighted canary labels. Both waypoints front the same service
// under one control plane, so this covers the canary across waypoint implementations: the weighted
// set is resolved by name regardless of GatewayClass, and ztunnel samples one of the two waypoints
// per connection.
//
// The waypoints are told apart by service-account identity, the same way the istio-only canary
// tests do it. Each waypoint originates HBONE to the backend under its own SA, so an ALLOW policy
// admitting a single SA - enforced at the destination ztunnel, which sees the forwarding waypoint
// as the source principal - isolates the traffic that waypoint served. Only 200s count as served:
// denials of the other waypoint and warming 503s never do, so an assertion cannot pass on failed
// traffic.
//
// This also covers config propagation to the canary: agentgateway answers 404 for a service it has
// no route for, so the canary serving traffic at all means the Service-attached route reached a
// waypoint that only the canary label points at.
func TestWeightedWaypointCanaryToAgentgateway(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			if !t.Settings().Agentgateway {
				t.Skip("Only run agentgateway tests when explicitly enabled")
			}
			crd.DeployGatewayAPIOrSkip(t)
			testNs, client, server := setupSmallTrafficTest(t)

			newCanaryWaypoint(t, testNs, canaryPrimaryWaypointName, constants.WaypointGatewayClassName)
			newCanaryWaypoint(t, testNs, agentgatewayWaypointName, constants.AgentgatewayWaypointClassName)

			// agentgateway has no implicit passthrough: a waypoint fronting a service it has no route
			// for answers 404. The route attaches to the Service, so it must reach both waypoints -
			// which is what makes a successful response proof that the serving waypoint was
			// programmed with the service, not just reachable.
			t.ConfigIstio().YAML(testNs.Name(), fmt.Sprintf(`
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: canary-httproute
  namespace: %s
spec:
  parentRefs:
    - name: %s
      kind: Service
      group: ""
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /
      backendRefs:
        - name: %s
          port: 80
`, testNs.Name(), server.ServiceName(), server.ServiceName())).ApplyOrFail(t)

			// ztunnel samples a waypoint per connection, so re-sample on every request.
			call := func() (echo.CallResult, error) {
				return client.Call(echo.CallOptions{
					To:                      server,
					Address:                 fmt.Sprintf("%s.%s.svc.cluster.local", server.ServiceName(), server.NamespaceName()),
					Port:                    server.PortForName("http"),
					Count:                   40,
					NewConnectionPerRequest: true,
					Check:                   acceptAny,
				})
			}

			// Bind both waypoints once and only move the weight from there. Clearing the labels
			// between weights would tear the binding down and rebuild it each time, which istiod
			// reports by dropping and re-adding the service's WaypointBound condition - a shift is
			// then measured against a binding that is still settling.
			bindWeightedWaypoints(t, server, agentgatewayWaypointName)

			for _, tc := range []struct {
				name            string
				weight          int
				canary, primary func(served, total int) error
			}{
				{"weight-0-all-istio-waypoint", 0, wantNone, wantAll},
				{"weight-25-mostly-istio-waypoint", 25, wantMinority, wantMajority},
				{"weight-75-mostly-agentgateway-waypoint", 75, wantMajority, wantMinority},
				{"weight-100-all-agentgateway-waypoint", 100, wantAll, wantNone},
			} {
				t.NewSubTest(tc.name).Run(func(t framework.TestContext) {
					setCanaryWeight(t, server, tc.weight)
					servedThrough(t, server, agentgatewayWaypointName, call, tc.canary)
					servedThrough(t, server, canaryPrimaryWaypointName, call, tc.primary)
				})
			}
		})
}

// newCanaryWaypoint provisions a service waypoint of the given GatewayClass and waits until ready.
func newCanaryWaypoint(t framework.TestContext, ns namespace.Instance, name, class string) {
	t.ConfigIstio().YAML(ns.Name(), fmt.Sprintf(`
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: %s
  namespace: %s
  labels:
    istio.io/waypoint-for: service
  annotations:
    networking.istio.io/service-type: ClusterIP
spec:
  gatewayClassName: %s
  listeners:
  - allowedRoutes:
      namespaces:
        from: All
    name: mesh
    port: 15008
    protocol: HBONE
`, name, ns.Name(), class)).ApplyOrFail(t)

	retry.UntilSuccessOrFail(t, func() error {
		return checkWaypointIsReady(t, ns.Name(), name)
	}, retry.Timeout(2*time.Minute))
}

// bindWeightedWaypoints labels the server service with the primary and canary waypoints at weight 0
// and waits for the binding to be accepted. The labels stay put for the rest of the test so the
// binding is established once; only the weight moves after this. Cleanup clears all three.
func bindWeightedWaypoints(t framework.TestContext, server echo.Instance, canary string) {
	set := fmt.Sprintf(`{"metadata":{"labels":{"%s":%q,"%s":%q},"annotations":{"%s":"0"}}}`,
		label.IoIstioUseWaypoint.Name, canaryPrimaryWaypointName,
		label.IoIstioUseWaypointCanary.Name, canary,
		annotation.IoIstioUseWaypointCanaryWeight.Name)
	if err := patchServerService(t, server, set); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		reset := fmt.Sprintf(`{"metadata":{"labels":{"%s":null,"%s":null},"annotations":{"%s":null}}}`,
			label.IoIstioUseWaypoint.Name, label.IoIstioUseWaypointCanary.Name,
			annotation.IoIstioUseWaypointCanaryWeight.Name)
		if err := patchServerService(t, server, reset); err != nil {
			scopes.Framework.Errorf("failed clearing weighted waypoint for %s: %v", server.ServiceName(), err)
		}
	})

	// An unresolvable canary is reported on the binding status and falls back to the primary, so
	// confirm the binding was accepted before measuring any split.
	retry.UntilSuccessOrFail(t, func() error {
		svc, err := t.Clusters().Default().Kube().CoreV1().Services(server.NamespaceName()).Get(
			context.TODO(), server.ServiceName(), metav1.GetOptions{})
		if err != nil {
			return err
		}
		for _, cond := range svc.Status.Conditions {
			if cond.Type == string(model.WaypointBound) {
				if cond.Status == metav1.ConditionTrue {
					return nil
				}
				return fmt.Errorf("waypoint binding not accepted: %s", cond.Message)
			}
		}
		return fmt.Errorf("waypoint condition not found on service (conditions: %v)", svc.Status.Conditions)
	}, retry.Timeout(1*time.Minute))
}

// setCanaryWeight moves the canary weight on the already-bound service.
func setCanaryWeight(t framework.TestContext, server echo.Instance, weight int) {
	patch := fmt.Sprintf(`{"metadata":{"annotations":{"%s":%q}}}`,
		annotation.IoIstioUseWaypointCanaryWeight.Name, strconv.Itoa(weight))
	if err := patchServerService(t, server, patch); err != nil {
		t.Fatal(err)
	}
}

// servedThrough admits only waypoint's SA with an ALLOW policy (enforced at the destination ztunnel)
// and retries call until the count of successful (200) responses - the requests that waypoint served
// - satisfies want.
func servedThrough(
	t framework.TestContext,
	server echo.Instance,
	waypoint string,
	call func() (echo.CallResult, error),
	want func(served, total int) error,
) {
	ns := server.NamespaceName()
	cfg := t.ConfigIstio().YAML(ns, fmt.Sprintf(`
apiVersion: security.istio.io/v1
kind: AuthorizationPolicy
metadata:
  name: allow-only-waypoint
spec:
  selector:
    matchLabels:
      app: %s
  action: ALLOW
  rules:
  - from:
    - source:
        principals: ["cluster.local/ns/%s/sa/%s"]
`, server.ServiceName(), ns, waypoint))
	cfg.ApplyOrFail(t)
	defer func() {
		if err := cfg.Delete(); err != nil {
			scopes.Framework.Errorf("failed deleting allow-only-waypoint policy: %v", err)
		}
	}()

	retry.UntilSuccessOrFail(t, func() error {
		res, err := call()
		if err != nil {
			return err
		}
		served, total := 0, len(res.Responses)
		for _, r := range res.Responses {
			if r.Code == "200" {
				served++
			}
		}
		return want(served, total)
	}, retry.Timeout(90*time.Second), retry.Delay(time.Second))
}

// wantAll: the admitted waypoint served every request (it fronts the service and got all traffic).
func wantAll(served, total int) error {
	if total == 0 || served != total {
		return fmt.Errorf("want all served, got %d/%d", served, total)
	}
	return nil
}

// wantNone: the admitted waypoint served nothing (it received no traffic at this weight).
func wantNone(served, total int) error {
	if total == 0 {
		return fmt.Errorf("no responses")
	}
	if served != 0 {
		return fmt.Errorf("want none served, got %d/%d", served, total)
	}
	return nil
}

// wantMinority: the admitted waypoint served part of the traffic, but less than half - it is the
// lower-weighted side of the split. Checking which side holds the majority, rather than just that
// both served something, is what catches the weights landing on the wrong waypoint.
//
// The margin is wide enough not to flake: at 40 connections and a 25/75 split, landing 20 or more
// on the 25% side is about a 3.5 sigma event.
func wantMinority(served, total int) error {
	if served == 0 || served*2 >= total {
		return fmt.Errorf("want a minority share, got %d/%d", served, total)
	}
	return nil
}

// wantMajority: the admitted waypoint served more than half the traffic - the higher-weighted side.
func wantMajority(served, total int) error {
	if total == 0 || served*2 <= total {
		return fmt.Errorf("want a majority share, got %d/%d", served, total)
	}
	return nil
}

func patchServerService(t framework.TestContext, server echo.Instance, patch string) error {
	for _, c := range t.Clusters() {
		if _, err := c.Kube().CoreV1().Services(server.NamespaceName()).Patch(
			context.TODO(), server.ServiceName(), types.MergePatchType, []byte(patch), metav1.PatchOptions{}); err != nil {
			return err
		}
	}
	return nil
}
