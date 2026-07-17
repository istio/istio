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
	"fmt"
	"strconv"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/annotation"
	"istio.io/api/label"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/ambient"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	canaryPrimaryWP = "weighted-primary"
	canaryCanaryWP  = "weighted-canary"
)

// acceptAny lets a call succeed regardless of HTTP status; the tests classify the responses
// themselves rather than failing on a non-2xx.
var acceptAny = func(echo.CallResult, error) error { return nil }

// TestWeightedWaypointTrafficShift exercises the weighted waypoint set (a service naming a primary
// waypoint plus a weighted canary waypoint) on the in-mesh path. Both waypoints are provisioned by
// the same control plane revision, so this validates the traffic-shifting mechanism and that each
// waypoint is programmed to serve the service, not a cross-revision canary (that stays covered by
// the manual e2e script since it exercises no additional istiod/ztunnel code path).
//
// ztunnel samples a waypoint per connection, so the client sends with NewConnectionPerRequest to
// re-sample each request. The two waypoints are otherwise configured identically (routes/policies
// attach to the Service and fan out to both), so we tell them apart by service-account identity.
// Each direction is measured under an ALLOW policy admitting only one waypoint's SA (enforced at
// the destination ztunnel, which sees the forwarding waypoint's SA as the source principal), and we
// count ONLY successful (200) responses as that waypoint serving. A warming or broken waypoint
// simply yields fewer successes, so an assertion can never pass on failed traffic.
func TestWeightedWaypointTrafficShift(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		newCanaryWaypoints(t)
		src := apps.Captured[0]
		dst := apps.Captured
		call := func() (echo.CallResult, error) {
			return src.Call(echo.CallOptions{
				To: dst, Port: ports.HTTP, Count: 40, NewConnectionPerRequest: true, Check: acceptAny,
			})
		}

		runWeightShiftSubtests(t, call)

		// Mesh-only: dropping the canary label makes the weighted set disappear, so ztunnel falls
		// back to the single primary waypoint.
		t.NewSubTest("no-canary-label-single-waypoint").Run(func(t framework.TestContext) {
			setWeightedWaypoint(t, canaryCanaryWP, 100)
			patchCapturedService(t, fmt.Sprintf(`{"metadata":{"labels":{"%s":null}}}`,
				label.IoIstioUseWaypointCanary.Name))
			// Collapsing the weighted set back to a single waypoint reprograms more than an in-place
			// weight change, so allow extra time for the canary to drain before asserting.
			servedThrough(t, canaryPrimaryWP, call, wantAll, retry.Timeout(2*time.Minute))
			servedThrough(t, canaryCanaryWP, call, wantNone, retry.Timeout(2*time.Minute))
		})
	})
}

// TestWeightedWaypointCanaryNotReady verifies that a canary reference to a waypoint that does not
// exist degrades gracefully: istiod reports the error on the binding status and falls back to the
// primary waypoint, so traffic keeps flowing rather than blackholing.
func TestWeightedWaypointCanaryNotReady(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		newServiceWaypoint(t, canaryPrimaryWP)
		src := apps.Captured[0]
		dst := apps.Captured

		// Canary points at a waypoint that was never created. Every request should still succeed via
		// the primary waypoint.
		setWeightedWaypoint(t, "does-not-exist", 50)
		call := func() (echo.CallResult, error) {
			return src.Call(echo.CallOptions{
				To: dst, Port: ports.HTTP, Count: 40, NewConnectionPerRequest: true, Check: acceptAny,
			})
		}
		servedThrough(t, canaryPrimaryWP, call, wantAll)
	})
}

// TestWeightedWaypointPolicyPropagation verifies that a service-scoped L7 policy attached to a
// service with a weighted canary is enforced at the canary waypoint. With the canary at weight 100
// all traffic is served by the canary, so if it were not programmed with the service's policy the
// requests would be allowed (200) instead of denied (403).
func TestWeightedWaypointPolicyPropagation(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		ns := apps.Namespace.Name()
		newCanaryWaypoints(t)
		src := apps.Captured[0]
		dst := apps.Captured

		// All traffic goes to the canary waypoint.
		setWeightedWaypoint(t, canaryCanaryWP, 100)

		// Service-scoped L7 DENY, enforced at the waypoint. Must be programmed onto the canary too.
		t.ConfigIstio().Eval(ns, map[string]string{"Service": Captured}, `apiVersion: security.istio.io/v1
kind: AuthorizationPolicy
metadata:
  name: deny-service-l7
spec:
  targetRefs:
  - kind: Service
    group: ""
    name: {{.Service}}
  action: DENY
  rules:
  - to:
    - operation:
        methods: ["GET"]
`).ApplyOrFail(t, apply.CleanupConditionally)

		// A GET must be denied by the canary waypoint (403). 503 means the canary is still warming;
		// 200 means the service policy did not reach the canary waypoint.
		retry.UntilSuccessOrFail(t, func() error {
			res, err := src.Call(echo.CallOptions{
				To: dst, Port: ports.HTTP, Count: 40, NewConnectionPerRequest: true, Check: acceptAny,
			})
			if err != nil {
				return err
			}
			if len(res.Responses) == 0 {
				return fmt.Errorf("no responses")
			}
			for _, r := range res.Responses {
				switch r.Code {
				case "403":
				case "200":
					return fmt.Errorf("request bypassed the service policy (canary waypoint not configured); codes=%v", codes(res))
				default:
					return fmt.Errorf("canary waypoint not ready (code %s); codes=%v", r.Code, codes(res))
				}
			}
			return nil
		}, retry.Timeout(90*time.Second), retry.Delay(time.Second))
	})
}

// TestWeightedWaypointIngressTrafficShift covers the north-south path: a service with
// ingress-use-waypoint plus a weighted canary. This is a distinct mechanism from the in-mesh path -
// istiod rewrites the ingress gateway's endpoints to the weighted waypoint set (findServiceWaypoint),
// so the gateway does a per-request weighted L7 split across the two waypoints rather than ztunnel
// sampling per connection. Identity is observed the same way: the gateway originates HBONE to the
// chosen waypoint, so the destination sees that waypoint's SA.
func TestWeightedWaypointIngressTrafficShift(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		if t.Settings().AmbientMultiNetwork {
			t.Skip("https://github.com/istio/istio/issues/54245")
		}
		ns := apps.Namespace.Name()
		newCanaryWaypoints(t)

		// Ingress gateway routing to Captured.
		t.ConfigIstio().Eval(ns, map[string]string{
			"Destination": apps.Captured.ServiceName(),
		}, `apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: canary-ingress
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
  name: canary-ingress-route
spec:
  gateways:
  - canary-ingress
  hosts:
  - "*"
  http:
  - route:
    - destination:
        host: "{{.Destination}}"
`).ApplyOrFail(t)
		ingress := istio.DefaultIngressOrFail(t, t)

		// Route ingress traffic through the weighted waypoint set.
		SetIngressUseWaypoint(t, apps.Captured.ServiceName(), ns)

		call := func() (echo.CallResult, error) {
			return ingress.Call(echo.CallOptions{
				Port:   echo.Port{Protocol: protocol.HTTP, ServicePort: 80},
				Scheme: scheme.HTTP,
				Count:  40,
				Check:  acceptAny,
			})
		}
		runWeightShiftSubtests(t, call)
	})
}

// runWeightShiftSubtests drives the shared weight=0/50/100 shift assertions against call, which is
// whatever produces the traffic for the path under test (an in-mesh client or the ingress gateway).
// Each subtest checks both waypoints' successfully-served share.
func runWeightShiftSubtests(t framework.TestContext, call func() (echo.CallResult, error)) {
	for _, tc := range []struct {
		name            string
		weight          int
		canary, primary func(served, total int) error
	}{
		{"weight-0-all-primary", 0, wantNone, wantAll},
		{"weight-50-splits-both-waypoints", 50, wantSome, wantSome},
		{"weight-100-all-canary", 100, wantAll, wantNone},
	} {
		t.NewSubTest(tc.name).Run(func(t framework.TestContext) {
			setWeightedWaypoint(t, canaryCanaryWP, tc.weight)
			servedThrough(t, canaryCanaryWP, call, tc.canary)
			servedThrough(t, canaryPrimaryWP, call, tc.primary)
		})
	}
}

// servedThrough admits only waypoint's SA with an ALLOW policy (enforced at the destination ztunnel)
// and retries call until the count of successful (200) responses - the requests that waypoint served
// - satisfies want. Only 200s count: denials of the other waypoint, warming 503s, and transport
// errors are never counted as success, so a broken or warming dataplane cannot pass an assertion.
func servedThrough(
	t framework.TestContext,
	waypoint string,
	call func() (echo.CallResult, error),
	want func(served, total int) error,
	retryOpts ...retry.Option,
) {
	ns := apps.Namespace.Name()
	cfg := t.ConfigIstio().Eval(ns, map[string]string{
		"Namespace": ns,
		"Waypoint":  waypoint,
	}, `apiVersion: security.istio.io/v1
kind: AuthorizationPolicy
metadata:
  name: allow-only-waypoint
spec:
  selector:
    matchLabels:
      app: captured
  action: ALLOW
  rules:
  - from:
    - source:
        principals: ["cluster.local/ns/{{.Namespace}}/sa/{{.Waypoint}}"]
`)
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
	}, append([]retry.Option{retry.Timeout(90 * time.Second), retry.Delay(time.Second)}, retryOpts...)...)
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

// wantSome: the admitted waypoint served part of the traffic and the other served the rest.
func wantSome(served, total int) error {
	if served == 0 || served >= total {
		return fmt.Errorf("want a partial split, got %d/%d", served, total)
	}
	return nil
}

// newCanaryWaypoints provisions the primary and canary service waypoints.
func newCanaryWaypoints(t framework.TestContext) {
	newServiceWaypoint(t, canaryPrimaryWP)
	newServiceWaypoint(t, canaryCanaryWP)
}

// newServiceWaypoint provisions a single service waypoint and waits until it is ready.
func newServiceWaypoint(t framework.TestContext, name string) {
	if _, err := ambient.NewWaypointProxyWithTrafficType(t, apps.Namespace, name, constants.ServiceTraffic); err != nil {
		t.Fatalf("failed creating waypoint %s: %v", name, err)
	}
	retry.UntilSuccessOrFail(t, func() error {
		return checkWaypointIsReady(t, apps.Namespace.Name(), name)
	}, retry.Timeout(2*time.Minute))
}

// setWeightedWaypoint labels the Captured service with the primary and the given canary waypoint
// plus a canary weight, registering cleanup that clears all three.
func setWeightedWaypoint(t framework.TestContext, canary string, weight int) {
	set := fmt.Sprintf(`{"metadata":{"labels":{"%s":%q,"%s":%q},"annotations":{"%s":%q}}}`,
		label.IoIstioUseWaypoint.Name, canaryPrimaryWP,
		label.IoIstioUseWaypointCanary.Name, canary,
		annotation.IoIstioUseWaypointCanaryWeight.Name, strconv.Itoa(weight))
	patchCapturedService(t, set)
	t.Cleanup(func() {
		reset := fmt.Sprintf(`{"metadata":{"labels":{"%s":null,"%s":null},"annotations":{"%s":null}}}`,
			label.IoIstioUseWaypoint.Name, label.IoIstioUseWaypointCanary.Name,
			annotation.IoIstioUseWaypointCanaryWeight.Name)
		if err := patchCapturedServiceE(t, reset); err != nil {
			scopes.Framework.Errorf("failed clearing weighted waypoint for %s: %v", Captured, err)
		}
	})
}

func patchCapturedService(t framework.TestContext, patch string) {
	if err := patchCapturedServiceE(t, patch); err != nil {
		t.Fatal(err)
	}
}

func patchCapturedServiceE(t framework.TestContext, patch string) error {
	for _, c := range t.Clusters() {
		if _, err := c.Kube().CoreV1().Services(apps.Namespace.Name()).Patch(
			context.TODO(), Captured, types.MergePatchType, []byte(patch), metav1.PatchOptions{}); err != nil {
			return err
		}
	}
	return nil
}

func codes(res echo.CallResult) []string {
	out := make([]string, len(res.Responses))
	for i, r := range res.Responses {
		out[i] = r.Code
	}
	return out
}
