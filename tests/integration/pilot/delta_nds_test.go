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
	"time"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/sets"
)

// TestDeltaNDS exercises the incremental (delta) Name Discovery Service end-to-end. It deploys an
// echo workload whose agent advertises the DELTA_NDS capability (via ISTIO_META_DELTA_NDS) and has
// DNS capture enabled, then drives the delta flows that the unit tests cover in isolation:
//   - initial name table delivery (a host resolves),
//   - incremental add (a second host added after sync resolves, WITHOUT losing the first — proving
//     the agent accumulates deltas rather than replacing the table),
//   - incremental removal (deleting a host stops its resolution while the other survives).
func TestDeltaNDS(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			ns := namespace.NewOrFail(t, namespace.Config{
				Prefix: "delta-nds",
				Inject: true,
			})

			// The agent only advertises the DELTA_NDS capability (and captures DNS) when these
			// env vars are present at injection time, so apply the ProxyConfig before deploying.
			proxyCfg := `apiVersion: networking.istio.io/v1beta1
kind: ProxyConfig
metadata:
  name: enable-delta-nds
spec:
  selector:
    matchLabels:
      app: delta
  environmentVariables:
    ISTIO_META_DNS_CAPTURE: "true"
    ISTIO_META_DELTA_NDS: "true"
`
			t.ConfigIstio().YAML(ns.Name(), proxyCfg).ApplyOrFail(t)

			client := deployment.New(t, t.AllClusters().Configs()...).
				WithConfig(echo.Config{Namespace: ns, Service: "delta"}).
				BuildOrFail(t)[0]

			// A STATIC ServiceEntry publishes its `addresses` as the resolved IPs for `host` in the
			// NDS name table, so a captured DNS query for `host` returns exactly those addresses.
			makeSE := func(name, host, addr string) string {
				return fmt.Sprintf(`apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: %s
spec:
  hosts:
  - "%s"
  addresses:
  - "%s"
  resolution: STATIC
  endpoints:
  - address: "10.0.0.1"
  ports:
  - number: 80
    name: http
    protocol: HTTP
`, name, host, addr)
			}

			const (
				hostA = "fake-a.delta.local"
				hostB = "fake-b.delta.local"
				ipA   = "240.0.0.1"
				ipB   = "240.0.0.2"
			)

			resolvesTo := func(host string, want ...string) {
				t.Helper()
				retry.UntilSuccessOrFail(t, func() error {
					_, err := client.Call(echo.CallOptions{
						Scheme:  scheme.DNS,
						Count:   1,
						Address: host + "?&protocol=udp",
						Check: func(result echo.CallResult, _ error) error {
							if len(result.Responses) == 0 {
								return fmt.Errorf("no DNS responses for %s", host)
							}
							for _, r := range result.Responses {
								if got := sets.New(r.Body()...); !got.Equals(sets.New(want...)) {
									return fmt.Errorf("dns %s: wanted %v, got %v", host, want, r.Body())
								}
							}
							return nil
						},
					})
					return err
				}, retry.Timeout(60*time.Second))
			}

			doesNotResolve := func(host string) {
				t.Helper()
				retry.UntilSuccessOrFail(t, func() error {
					// With the host absent from the name table the agent forwards upstream, which
					// NXDOMAINs for this fake .local name, so the call errors.
					_, err := client.Call(echo.CallOptions{
						Scheme:  scheme.DNS,
						Count:   1,
						Address: host + "?&protocol=udp",
						Check:   check.Error(),
					})
					return err
				}, retry.Timeout(60*time.Second))
			}

			// Phase 1: initial name table delivery — hostA resolves once NDS is synced.
			seA := t.ConfigIstio().YAML(ns.Name(), makeSE("fake-a", hostA, ipA))
			seA.ApplyOrFail(t)
			resolvesTo(hostA, ipA)

			// Phase 2: incremental add — hostB (added after the initial sync) resolves, and hostA
			// must still resolve. If deltas replaced the table instead of accumulating, hostA would
			// vanish here.
			seB := t.ConfigIstio().YAML(ns.Name(), makeSE("fake-b", hostB, ipB))
			seB.ApplyOrFail(t)
			resolvesTo(hostB, ipB)
			resolvesTo(hostA, ipA)

			// Phase 3: incremental removal — deleting hostB stops its resolution (delivered as a
			// RemovedResources delta) while hostA survives.
			seB.DeleteOrFail(t)
			doesNotResolve(hostB)
			resolvesTo(hostA, ipA)
		})
}
