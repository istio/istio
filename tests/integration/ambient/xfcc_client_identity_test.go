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
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	echot "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/util/retry"
)

const xfccClientIdentityAnnotation = "ambient.istio.io/xfcc-include-client-identity"

// TestWaypointXFCCClientIdentity verifies that when a waypoint Gateway is annotated with
// ambient.istio.io/xfcc-include-client-identity=true, the waypoint synthesizes an
// x-forwarded-client-cert header populated from the ztunnel-provided source workload
// SPIFFE identity. Without the annotation XFCC does not contain the client URI.
func TestWaypointXFCCClientIdentity(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		runTestToServiceWaypoint(t, func(t framework.TestContext, src echo.Instance, dst echo.Target, opt echo.CallOptions) {
			if opt.Scheme != scheme.HTTP {
				return
			}
			waypointName := dst.Config().ServiceWaypointProxy
			if waypointName == "" {
				return
			}
			ns := apps.Namespace.Name()

			addAnnotation := []byte(fmt.Sprintf(
				`{"metadata":{"annotations":{%q:"true"}}}`, xfccClientIdentityAnnotation))
			removeAnnotation := []byte(fmt.Sprintf(
				`{"metadata":{"annotations":{%q:null}}}`, xfccClientIdentityAnnotation))

			for _, c := range t.AllClusters() {
				if _, err := c.GatewayAPI().GatewayV1().Gateways(ns).Patch(
					context.TODO(), waypointName, types.MergePatchType, addAnnotation, metav1.PatchOptions{}); err != nil {
					t.Fatalf("failed annotating waypoint %s: %v", waypointName, err)
				}
				t.Cleanup(func() {
					_, err := c.GatewayAPI().GatewayV1().Gateways(ns).Patch(
						context.TODO(), waypointName, types.MergePatchType, removeAnnotation, metav1.PatchOptions{})
					if err != nil {
						t.Logf("failed removing xfcc annotation from waypoint %s: %v", waypointName, err)
					}
				})
			}

			expectedURI := "spiffe://" + src.Config().SpiffeIdentity()
			// Retry with larger timeout so the waypoint pod has time to roll out with
			// the new annotation and reconnect to istiod.
			opt.Count = 5
			opt.Retry.Options = []retry.Option{
				retry.Timeout(90 * time.Second),
				retry.Delay(2 * time.Second),
			}
			opt.Check = check.And(
				check.OK(),
				check.Each(func(r echot.Response) error {
					xfcc := firstXFCC(r)
					if xfcc == "" {
						return fmt.Errorf("no X-Forwarded-Client-Cert header in response: %v", r.RequestHeaders)
					}
					if !containsXFCCField(xfcc, "URI", expectedURI) {
						return fmt.Errorf("XFCC missing URI=%s; got %q", expectedURI, xfcc)
					}
					return nil
				}),
			)
			src.CallOrFail(t, opt)
		})
	})
}

func firstXFCC(r echot.Response) string {
	if v := r.RequestHeaders.Get("X-Forwarded-Client-Cert"); v != "" {
		return v
	}
	// gRPC lowercases header names; the echo server records request headers verbatim.
	return r.RequestHeaders.Get("x-forwarded-client-cert")
}

// containsXFCCField returns true when the XFCC header contains a field with the
// given key and exact value. XFCC entries are comma-separated, fields within an
// entry are semicolon-separated, and values may be quoted.
func containsXFCCField(xfcc, key, want string) bool {
	for _, entry := range strings.Split(xfcc, ",") {
		for _, field := range strings.Split(entry, ";") {
			k, v, ok := strings.Cut(strings.TrimSpace(field), "=")
			if !ok {
				continue
			}
			if strings.EqualFold(k, key) && strings.Trim(v, `"`) == want {
				return true
			}
		}
	}
	return false
}
