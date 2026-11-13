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
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/label"
	echoproto "istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	// sourceIPRequests is the number of requests each goroutine of a source pod sends.
	sourceIPRequests = 5
	// sourceIPParallelism is the number of goroutines sending requests concurrently from each source pod.
	sourceIPParallelism = 3
)

func quotedList(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, v := range values {
		quoted = append(quoted, strconv.Quote(v))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// callFromPod sends requests from one pod, concurrently and each on a new connection, so that ztunnel multiplexes
// them as CONNECT streams over its pooled HBONE connections, and checks that every response has the wanted code.
func callFromPod(w echo.Workload, url string, xff string, want int) error {
	var hdrs []*echoproto.Header
	if xff != "" {
		hdrs = append(hdrs, &echoproto.Header{Key: "X-Forwarded-For", Value: xff})
	}
	errs := make(chan error, sourceIPParallelism)
	var wg sync.WaitGroup
	for range sourceIPParallelism {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			resps, err := w.ForwardEcho(ctx, &echoproto.ForwardEchoRequest{
				Url:                     url,
				Count:                   sourceIPRequests,
				Headers:                 hdrs,
				TimeoutMicros:           (5 * time.Second).Microseconds(),
				NewConnectionPerRequest: true,
			})
			if err != nil {
				errs <- fmt.Errorf("pod %s: %v", w.PodName(), err)
				return
			}
			if len(resps) != sourceIPRequests {
				errs <- fmt.Errorf("pod %s: got %d responses, want %d", w.PodName(), len(resps), sourceIPRequests)
				return
			}
			for _, r := range resps {
				if r.Code != strconv.Itoa(want) {
					errs <- fmt.Errorf("pod %s: got code %s, want %d", w.PodName(), r.Code, want)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	return <-errs
}

// connectTerminateStats returns the total CONNECT streams and HBONE connections the waypoint pods have accepted. The
// counters are only reported if the waypoint's stats matcher includes them, which Istio's default does not; found
// reports whether every waypoint pod reported both of them.
func connectTerminateStats(t framework.TestContext, waypoint string) (streams, conns int, found bool) {
	c := t.Clusters().Default()
	pods, err := c.Kube().CoreV1().Pods(apps.Namespace.Name()).List(context.Background(), metav1.ListOptions{
		LabelSelector: label.IoK8sNetworkingGatewayGatewayName.Name + "=" + waypoint,
	})
	if err != nil || len(pods.Items) == 0 {
		t.Fatalf("failed to list pods of waypoint %s: %v", waypoint, err)
	}
	found = true
	for _, pod := range pods.Items {
		hasStreams, hasConns := false, false
		out, _, err := c.PodExec(pod.Name, pod.Namespace, "istio-proxy", "pilot-agent request GET stats?filter=http.connect_terminate.downstream_")
		if err != nil {
			t.Fatalf("failed to read stats of %s: %v", pod.Name, err)
		}
		for _, line := range strings.Split(out, "\n") {
			name, value, ok := strings.Cut(line, ": ")
			if !ok {
				continue
			}
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				continue
			}
			switch strings.TrimSpace(name) {
			case "http.connect_terminate.downstream_rq_total":
				streams += n
				hasStreams = true
			case "http.connect_terminate.downstream_cx_total":
				conns += n
				hasConns = true
			}
		}
		found = found && hasStreams && hasConns
	}
	return streams, conns, found
}

// TestAuthorizationWaypointSourceIP checks that a waypoint tells apart two pods of the same workload, with the same
// service account and distinct IPs, by ipBlocks and notIpBlocks, while their requests are concurrent and multiplexed
// over pooled HBONE connections, and that X-Forwarded-For does not affect ipBlocks. If the source IP of one pod were
// applied to the requests of the other, the per-pod results below would differ. See
// https://github.com/istio/istio/issues/62522.
func TestAuthorizationWaypointSourceIP(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		skipIfGatewayAPIUnsupported(t)
		if t.Clusters().IsMulticluster() {
			// Cross-network traffic reaches the waypoint from the east-west gateway, which is then the peer.
			t.Skip("requires a single cluster")
		}
		src := apps.Captured[0]
		dst := apps.ServiceAddressedWaypoint[0]
		workloads := src.WorkloadsOrFail(t)
		if len(workloads) < 2 {
			t.Fatalf("expected at least 2 pods of %s, got %d", src.ServiceName(), len(workloads))
		}
		selected, other := workloads[0], workloads[1]

		// The two pods must have the same service account and distinct IPs, so that only their IPs tell them apart.
		accounts := map[string]bool{}
		for _, w := range []echo.Workload{selected, other} {
			pod, err := w.Cluster().Kube().CoreV1().Pods(apps.Namespace.Name()).Get(context.Background(), w.PodName(), metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			accounts[pod.Spec.ServiceAccountName] = true
		}
		if len(accounts) != 1 {
			t.Fatalf("pods %s and %s must share a service account, got %v", selected.PodName(), other.PodName(), accounts)
		}
		for _, a := range selected.Addresses() {
			for _, b := range other.Addresses() {
				if a == b {
					t.Fatalf("pods %s and %s share address %s", selected.PodName(), other.PodName(), a)
				}
			}
		}

		url := fmt.Sprintf("http://%s:%d/", dst.Config().ClusterLocalFQDN(), dst.Config().Ports.MustForName("http").ServicePort)
		// All addresses of the pod, as the HBONE connection uses the address family of the waypoint.
		selectedIPs := quotedList(selected.Addresses())
		cases := []struct {
			name       string
			action     string
			source     string
			xff        string
			wantSelect int
			wantOther  int
		}{
			{"deny ipBlocks", "DENY", "ipBlocks: " + selectedIPs, "", http.StatusForbidden, http.StatusOK},
			{"deny notIpBlocks", "DENY", "notIpBlocks: " + selectedIPs, "", http.StatusOK, http.StatusForbidden},
			{"allow ipBlocks", "ALLOW", "ipBlocks: " + selectedIPs, "", http.StatusOK, http.StatusForbidden},
			{"allow notIpBlocks", "ALLOW", "notIpBlocks: " + selectedIPs, "", http.StatusForbidden, http.StatusOK},
			// The other pod claims the selected IP in X-Forwarded-For, which must not change ipBlocks.
			{"allow ipBlocks ignores X-Forwarded-For", "ALLOW", "ipBlocks: " + selectedIPs, selected.Address(), http.StatusOK, http.StatusForbidden},
			{"deny ipBlocks ignores X-Forwarded-For", "DENY", "ipBlocks: " + selectedIPs, other.Address(), http.StatusForbidden, http.StatusOK},
			{"deny all IPs", "DENY", `ipBlocks: ["0.0.0.0/0", "::/0"]`, "", http.StatusForbidden, http.StatusForbidden},
		}
		for _, tc := range cases {
			t.NewSubTest(tc.name).Run(func(t framework.TestContext) {
				t.ConfigIstio().Eval(apps.Namespace.Name(), map[string]string{
					"Destination": dst.Config().Service,
					"Action":      tc.action,
					"Source":      tc.source,
				}, `
apiVersion: security.istio.io/v1
kind: AuthorizationPolicy
metadata:
  name: source-ip
spec:
  targetRefs:
  - kind: Service
    group: ""
    name: "{{ .Destination }}"
  action: {{ .Action }}
  rules:
  - from:
    - source:
        {{ .Source }}
`).ApplyOrFail(t)

				streamsBefore, connsBefore, statsBefore := connectTerminateStats(t, dst.Config().ServiceWaypointProxy)
				// Retry until the policy is in effect, sending from both pods at the same time every attempt.
				retry.UntilSuccessOrFail(t, func() error {
					var wg sync.WaitGroup
					var selectedErr, otherErr error
					wg.Add(2)
					go func() { defer wg.Done(); selectedErr = callFromPod(selected, url, tc.xff, tc.wantSelect) }()
					go func() { defer wg.Done(); otherErr = callFromPod(other, url, tc.xff, tc.wantOther) }()
					wg.Wait()
					if selectedErr != nil {
						return selectedErr
					}
					return otherErr
				}, retry.Timeout(2*time.Minute), retry.Delay(time.Second))
				streams, conns, statsAfter := connectTerminateStats(t, dst.Config().ServiceWaypointProxy)
				if !statsBefore || !statsAfter {
					// Only the per-pod IP policy results above are verified, which catch an IP of one pod applied to the
					// other; HBONE connection reuse is not confirmed.
					t.Logf("IP policy regression coverage only: waypoint does not report both http.connect_terminate " +
						"counters, so HBONE connection reuse is NOT verified")
					return
				}
				streams, conns = streams-streamsBefore, conns-connsBefore
				t.Logf("waypoint accepted %d CONNECT streams on %d new HBONE connections", streams, conns)
				// Each request opened its own connection, so its own CONNECT stream. Fewer new HBONE connections
				// than streams means the streams were multiplexed over pooled connections.
				if streams < 2*sourceIPParallelism*sourceIPRequests || conns >= streams {
					t.Fatalf("expected CONNECT streams to share HBONE connections, got %d streams on %d connections", streams, conns)
				}
				t.Logf("HBONE connection reuse verified")
			})
		}
	})
}
