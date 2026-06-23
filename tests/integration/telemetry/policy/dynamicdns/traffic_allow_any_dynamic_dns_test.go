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

package dynamics

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"istio.io/api/annotation"
	"istio.io/istio/istioctl/pkg/util"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
)

const sidecarScope = `
apiVersion: networking.istio.io/v1
kind: Sidecar
metadata:
  name: restrict-egress
spec:
  egress:
  - hosts:
    - "{{.ImportNamespace}}/*"
    - "istio-system/*"
`

// TestAllowAnyDynamicDNSPolicy verifies traffic routing under mesh mode ALLOW_ANY_DYNAMIC_DNS:
//   - HTTP to an unknown destination is forwarded via AllowAnyDynamicDNSCluster.
//   - HTTPS (non-HTTP / unknown protocol) still falls through to PassthroughCluster.
//
// Asserts on Envoy admin stats from the client proxy to avoid coupling to the istio-proxy
// stats filter labeling.
func TestAllowAnyDynamicDNSPolicy(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		appsNS := namespace.NewOrFail(t, namespace.Config{Prefix: "app", Inject: true})
		serviceNS := namespace.NewOrFail(t, namespace.Config{Prefix: "service", Inject: true})

		args := map[string]string{"ImportNamespace": serviceNS.Name()}
		t.ConfigIstio().Eval(appsNS.Name(), args, sidecarScope).ApplyOrFail(t)

		var client, dest echo.Instance
		deployment.New(t).
			With(&client, echo.Config{
				Service:   "client",
				Namespace: appsNS,
				// Istio-proxy strips most cluster.* counters by default; opt these in so /stats
				// exposes upstream_rq_total for the clusters we assert on.
				Subsets: []echo.SubsetConfig{{
					Annotations: map[string]string{
						annotation.SidecarStatsInclusionPrefixes.Name: "cluster.AllowAnyDynamicDNSCluster,cluster.PassthroughCluster,cluster.BlackHoleCluster",
					},
				}},
			}).
			With(&dest, echo.Config{
				Service:   "destination",
				Namespace: appsNS,
				Subsets:   []echo.SubsetConfig{{}},
				Ports: []echo.Port{
					{
						Name:         "http",
						Protocol:     protocol.HTTP,
						ServicePort:  80,
						WorkloadPort: 8080,
					},
					{
						Name:         "https",
						Protocol:     protocol.HTTPS,
						ServicePort:  443,
						WorkloadPort: 8443,
						TLS:          true,
					},
				},
				TLSSettings: &common.TLSSettings{
					ClientCert: mustReadCert(t, "cert.crt"),
					Key:        mustReadCert(t, "cert.key"),
				},
			}).BuildOrFail(t)

		t.NewSubTest("http-via-dfp").Run(func(t framework.TestContext) {
			baseline := clusterUpstreamRqTotal(t, client, "AllowAnyDynamicDNSCluster")
			passthroughBaseline := clusterUpstreamRqTotal(t, client, "PassthroughCluster")

			client.CallOrFail(t, echo.CallOptions{
				To:    dest,
				Count: 1,
				Port:  echo.Port{Name: "http"},
				Check: check.OK(),
			})

			retry.UntilSuccessOrFail(t, func() error {
				got := clusterUpstreamRqTotal(t, client, "AllowAnyDynamicDNSCluster")
				if got <= baseline {
					return fmt.Errorf("AllowAnyDynamicDNSCluster.upstream_rq_total did not increase: baseline=%d got=%d", baseline, got)
				}
				return nil
			}, retry.Delay(time.Second), retry.Timeout(20*time.Second))

			if got := clusterUpstreamRqTotal(t, client, "PassthroughCluster"); got != passthroughBaseline {
				t.Fatalf("HTTP request unexpectedly hit PassthroughCluster: baseline=%d got=%d", passthroughBaseline, got)
			}
		})

		t.NewSubTest("https-via-passthrough").Run(func(t framework.TestContext) {
			baseline := clusterUpstreamRqTotal(t, client, "PassthroughCluster")
			dfpBaseline := clusterUpstreamRqTotal(t, client, "AllowAnyDynamicDNSCluster")

			client.CallOrFail(t, echo.CallOptions{
				To:    dest,
				Count: 1,
				Port:  echo.Port{Name: "https"},
				Check: check.OK(),
			})

			retry.UntilSuccessOrFail(t, func() error {
				got := clusterUpstreamRqTotal(t, client, "PassthroughCluster")
				if got <= baseline {
					return fmt.Errorf("PassthroughCluster.upstream_rq_total did not increase: baseline=%d got=%d", baseline, got)
				}
				return nil
			}, retry.Delay(time.Second), retry.Timeout(20*time.Second))

			if got := clusterUpstreamRqTotal(t, client, "AllowAnyDynamicDNSCluster"); got != dfpBaseline {
				t.Fatalf("HTTPS request unexpectedly hit AllowAnyDynamicDNSCluster: baseline=%d got=%d", dfpBaseline, got)
			}
		})
	})
}

// TestAllowAnyDynamicDNSWithTLS verifies that when outboundTrafficPolicy.tls is configured with
// mode SIMPLE, the proxy originates TLS to unknown external destinations via the DFP cluster.
// An external echo instance (no sidecar) serves TLS. The client sends plaintext HTTP; the
// proxy's DFP cluster resolves the host and originates TLS to the external destination.
func TestAllowAnyDynamicDNSWithTLS(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		ist.PatchMeshConfigOrFail(t, `
outboundTrafficPolicy:
  mode: ALLOW_ANY_DYNAMIC_DNS
  tls:
    mode: SIMPLE
    insecureSkipVerify: true
`)
		appsNS := namespace.NewOrFail(t, namespace.Config{Prefix: "app-tls", Inject: true})
		serviceNS := namespace.NewOrFail(t, namespace.Config{Prefix: "service-tls", Inject: true})
		externalNS := namespace.NewOrFail(t, namespace.Config{Prefix: "external-tls", Inject: false})

		args := map[string]string{"ImportNamespace": serviceNS.Name()}
		t.ConfigIstio().Eval(appsNS.Name(), args, sidecarScope).ApplyOrFail(t)

		var client, external echo.Instance
		deployment.New(t).
			With(&client, echo.Config{
				Service:   "client",
				Namespace: appsNS,
				Subsets: []echo.SubsetConfig{{
					Annotations: map[string]string{
						annotation.SidecarStatsInclusionPrefixes.Name: "cluster.AllowAnyDynamicDNSCluster,cluster.PassthroughCluster,cluster.BlackHoleCluster",
					},
				}},
			}).
			With(&external, echo.Config{
				Service:   "external-tls",
				Namespace: externalNS,
				Subsets: []echo.SubsetConfig{{
					Annotations: map[string]string{annotation.SidecarInject.Name: "false"},
				}},
				Ports: []echo.Port{
					{
						Name:         "https",
						Protocol:     protocol.HTTPS,
						ServicePort:  443,
						WorkloadPort: 8443,
						TLS:          true,
					},
				},
				TLSSettings: &common.TLSSettings{
					ClientCert: mustReadCert(t, "cert.crt"),
					Key:        mustReadCert(t, "cert.key"),
				},
			}).BuildOrFail(t)

		t.NewSubTest("http-to-external-with-tls-origination").Run(func(t framework.TestContext) {
			baseline := clusterUpstreamRqTotal(t, client, "AllowAnyDynamicDNSCluster")

			client.CallOrFail(t, echo.CallOptions{
				Address: "external-tls." + externalNS.Name() + ".svc.cluster.local",
				Port: echo.Port{
					ServicePort: 443,
					Protocol:    protocol.HTTP,
				},
				Scheme: scheme.HTTP,
				HTTP: echo.HTTP{
					Headers: headers.New().WithHost("external-tls." + externalNS.Name() + ".svc.cluster.local").Build(),
				},
				Count: 1,
				Check: check.OK(),
			})

			retry.UntilSuccessOrFail(t, func() error {
				got := clusterUpstreamRqTotal(t, client, "AllowAnyDynamicDNSCluster")
				if got <= baseline {
					return fmt.Errorf("AllowAnyDynamicDNSCluster.upstream_rq_total did not increase: baseline=%d got=%d", baseline, got)
				}
				return nil
			}, retry.Delay(time.Second), retry.Timeout(20*time.Second))
		})
	})
}

func mustReadCert(t framework.TestContext, f string) string {
	t.Helper()
	b, err := os.ReadFile(path.Join(env.IstioSrc, "tests/testdata/certs", f))
	if err != nil {
		t.Fatalf("failed to read %v: %v", f, err)
	}
	return string(b)
}

func clusterUpstreamRqTotal(t framework.TestContext, c echo.Instance, clusterName string) int {
	t.Helper()
	wl := c.WorkloadsOrFail(t)[0]
	body, err := wl.Cluster().EnvoyDoWithPort(
		context.TODO(),
		wl.PodName(),
		c.Config().Namespace.Name(),
		"GET",
		"stats?filter=^cluster\\."+clusterName,
		util.DefaultProxyAdminPort,
	)
	if err != nil {
		t.Fatalf("failed to query envoy stats: %v", err)
	}
	// Envoy may emit "cluster.<name>;.upstream_rq_total" (stat-tag form) or the bare form.
	clusterTok := "cluster." + clusterName
	const suffix = ".upstream_rq_total"
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		idx := strings.LastIndex(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		if !strings.HasPrefix(key, clusterTok) || !strings.HasSuffix(key, suffix) {
			continue
		}
		mid := key[len(clusterTok) : len(key)-len(suffix)]
		if mid != "" && mid != ";" {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(line[idx+1:]), "%d", &n); err == nil {
			return n
		}
	}
	return 0
}
