// +build integ
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

package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"
	"testing"
	"time"

	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"golang.org/x/sync/errgroup"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/pkg/log"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/yml"
)

var (
	dashboards = []struct {
		configmap      string
		name           string
		excluded       []string
		requirePrimary bool
	}{
		{
			"istio-grafana-dashboards",
			"pilot-dashboard.json",
			[]string{
				"pilot_xds_push_errors",
				"pilot_total_xds_internal_errors",
				"pilot_xds_push_context_errors",
				`pilot_xds_pushes{type!~"lds|cds|rds|eds"}`,
				// We do not push credentials in this test
				`pilot_xds_pushes{type="sds"}`,
				"_timeout",
				"_rejects",
				// We do not simulate injection errors
				"sidecar_injection_failure_total",
				// In default install, we have no proxy
				"istio-proxy",
				// https://github.com/istio/istio/issues/22674 this causes flaky tests
				"galley_validation_passed",
				// cAdvisor does not expose this metrics, and we don't have kubelet in kind
				"container_fs_usage_bytes",
			},
			// Pilot is installed only on Primary cluster, hence validate for primary clusters only.
			true,
		},
		{
			"istio-services-grafana-dashboards",
			"istio-mesh-dashboard.json",
			[]string{
				"galley_",
				"istio_tcp_",
				"max(pilot_k8s_cfg_events{",
			},
			false,
		},
		{
			"istio-services-grafana-dashboards",
			"istio-service-dashboard.json",
			[]string{
				"istio_tcp_",
			},
			false,
		},
		{
			"istio-services-grafana-dashboards",
			"istio-workload-dashboard.json",
			[]string{
				"istio_tcp_",
			},
			false,
		},
		{
			"istio-grafana-dashboards",
			"istio-performance-dashboard.json",
			[]string{
				// TODO add these back: https://github.com/istio/istio/issues/20175
				`istio-telemetry`,
				`istio-policy`,
				// cAdvisor does not expose this metrics, and we don't have kubelet in kind
				"container_fs_usage_bytes",
			},
			true,
		},
		{
			"istio-services-grafana-dashboards",
			"istio-extension-dashboard.json",
			[]string{
				"avg(envoy_wasm_vm_v8_",
			},
			false,
		},
	}
)

func TestDashboard(t *testing.T) {
	framework.NewTest(t).
		Features("observability.telemetry.dashboard").
		Run(func(ctx framework.TestContext) {

			p := prometheus.NewOrFail(ctx, ctx, prometheus.Config{})
			setupDashboardTest(ctx)
			waitForMetrics(ctx, p)
			for _, d := range dashboards {
				d := d
				ctx.NewSubTest(d.name).RunParallel(func(t framework.TestContext) {
					for _, cl := range ctx.Clusters() {
						if !cl.IsPrimary() && d.requirePrimary {
							// Skip verification of dashboards that won't be present on non primary(remote) clusters.
							continue
						}
						t.Logf("Verifying %s for cluster %s", d.name, cl.Name())
						cm, err := cl.CoreV1().ConfigMaps(i.Settings().TelemetryNamespace).Get(
							context.TODO(), d.configmap, kubeApiMeta.GetOptions{})
						if err != nil {
							t.Fatalf("Failed to find dashboard %v: %v", d.configmap, err)
						}

						config, f := cm.Data[d.name]
						if !f {
							t.Fatalf("Failed to find expected dashboard: %v", d.name)
						}

						queries, err := extractQueries(config)
						if err != nil {
							t.Fatalf("Failed to extract queries: %v", err)
						}

						for _, query := range queries {
							if err := checkMetric(cl, p, query, d.excluded); err != nil {
								t.Errorf("Check query failed for cluster %s: %v", cl.Name(), err)
							}
						}
					}
				})
			}
		})
}

var (
	// Some templates use replacement variables. Instead, replace those with wildcard
	replacer = strings.NewReplacer(
		"$dstns", ".*",
		"$dstwl", ".*",
		"$service", ".*",
		"$srcns", ".*",
		"$srcwl", ".*",
		"$namespace", ".*",
		"$workload", ".*",
		"$dstsvc", ".*",
		"$adapter", ".*",
		// Just allow all mTLS settings rather than trying to send mtls and plaintext
		`connection_security_policy="unknown"`, `connection_security_policy=~".*"`,
		`connection_security_policy="mutual_tls"`, `connection_security_policy=~".*"`,
		`connection_security_policy!="mutual_tls"`, `connection_security_policy=~".*"`,
		// Test runs in istio-system
		`destination_workload_namespace!="istio-system"`, `destination_workload_namespace=~".*"`,
		`source_workload_namespace!="istio-system"`, `source_workload_namespace=~".*"`,
	)
)

func checkMetric(cl resource.Cluster, p prometheus.Instance, query string, excluded []string) error {
	query = replacer.Replace(query)
	value, _, err := p.APIForCluster(cl).QueryRange(context.Background(), query, promv1.Range{
		Start: time.Now().Add(-time.Minute),
		End:   time.Now(),
		Step:  time.Second,
	})
	if err != nil {
		return fmt.Errorf("failure executing query (%s): %v", query, err)
	}
	if value == nil {
		return fmt.Errorf("returned value should not be nil for '%s'", query)
	}
	numSamples := 0
	switch v := value.(type) {
	case model.Vector:
		numSamples = v.Len()
	case model.Matrix:
		numSamples = v.Len()
	case *model.Scalar:
		numSamples = 1
	default:
		return fmt.Errorf("unknown metric value type: %T", v)
	}
	if includeQuery(query, excluded) {
		if numSamples == 0 {
			return fmt.Errorf("expected a metric value for '%s', found no samples: %#v", query, value)
		}
	} else {
		if numSamples != 0 {
			scopes.Framework.Infof("Filtered out metric '%v', but got samples: %v", query, value)
		}
	}
	return nil
}

// nolint: interfacer
func waitForMetrics(t framework.TestContext, instance prometheus.Instance) {
	// These are sentinel metrics that will be used to evaluate if prometheus
	// scraping has occurred and data is available via promQL.
	// We will retry these queries, but not future ones, otherwise failures will take too long
	queries := []string{
		`istio_requests_total`,
		`istio_tcp_received_bytes_total`,
	}

	for _, cl := range t.Clusters() {
		for _, query := range queries {
			err := retry.UntilSuccess(func() error {
				return checkMetric(cl, instance, query, nil)
			})
			// Do not fail here - this is just to let the metrics sync. We will fail on the test if query fails
			if err != nil {
				t.Logf("Sentinel query %v failed: %v", query, err)
			}
		}
	}
}

const gatewayConfig = `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: echo-gateway
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
  - port:
      number: 31400
      name: tcp
      protocol: TCP
    hosts:
    - "*"
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: echo
spec:
  hosts:
  - "*"
  gateways:
  - echo-gateway
  http:
  - match:
    - uri:
        exact: /echo-%s
    route:
    - destination:
        host: server
        port:
          number: 80
  tcp:
  - match:
    - port: 31400
    route:
    - destination:
        host: server
        port:
          number: 7777
`

func setupDashboardTest(t framework.TestContext) {
	ns := namespace.NewOrFail(t, t, namespace.Config{
		Prefix: "dashboard",
		Inject: true,
	})
	t.Config().ApplyYAMLOrFail(t, ns.Name(), fmt.Sprintf(gatewayConfig, ns.Name()))

	// Apply just the grafana dashboards
	cfg, err := ioutil.ReadFile(filepath.Join(env.IstioSrc, "samples/addons/grafana.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	t.Config().ApplyYAMLOrFail(t, "istio-system", yml.SplitYamlByKind(string(cfg))["ConfigMap"])

	for _, cl := range t.Clusters() {
		var instance echo.Instance
		echoboot.
			NewBuilder(t).
			With(&instance, echo.Config{
				Service:   "server",
				Cluster:   cl,
				Namespace: ns,
				Subsets:   []echo.SubsetConfig{{}},
				Ports: []echo.Port{
					{
						Name:     "http",
						Protocol: protocol.HTTP,
						// We use a port > 1024 to not require root
						InstancePort: 8090,
					},
					{
						Name:         "tcp",
						Protocol:     protocol.TCP,
						InstancePort: 7777,
						ServicePort:  7777,
					},
				},
			}).
			BuildOrFail(t)
	}
	// Send 200 http requests, 20 tcp requests across goroutines, generating a variety of error codes.
	// Spread out over 20s so rate() queries will behave correctly
	g, _ := errgroup.WithContext(context.Background())
	ticker := time.NewTicker(time.Second)
	for t := 0; t < 20; t++ {
		<-ticker.C
		g.Go(func() error {
			for _, ing := range ingr {
				tcpAddr := ing.TCPAddress()
				for i := 0; i < 10; i++ {
					_, err := ing.CallEcho(echo.CallOptions{
						Port: &echo.Port{
							Protocol: protocol.HTTP,
						},
						Path: fmt.Sprintf("/echo-%s?codes=418:10,520:15,200:75", ns.Name()),
						Headers: map[string][]string{
							"Host": {"server"},
						},
					})
					if err != nil {
						// Do not fail on errors since there may be initial startup errors
						// These calls are not under tests, the dashboards are, so we can be leniant here
						log.Warnf("requests failed: %v", err)
					}
				}
				_, err := ing.CallEcho(echo.CallOptions{
					Port: &echo.Port{
						Protocol:    protocol.TCP,
						ServicePort: tcpAddr.Port,
					},
					Address: tcpAddr.IP.String(),
					Path:    fmt.Sprintf("/echo-%s", ns.Name()),
					Headers: map[string][]string{
						"Host": {"server"},
					},
				})
				if err != nil {
					// Do not fail on errors since there may be initial startup errors
					// These calls are not under tests, the dashboards are, so we can be leniant here
					log.Warnf("requests failed: %v", err)
				}
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		t.Fatalf("ingress call failed: %v", err)
	}
}

// extractQueries pulls all prometheus queries out of a grafana dashboard
// Rather than importing the entire grafana API just for this test, do some shoddy json parsing
// Equivalent to jq command: '.panels[].targets[]?.expr'
func extractQueries(dash string) ([]string, error) {
	var queries []string
	js := map[string]interface{}{}
	if err := json.Unmarshal([]byte(dash), &js); err != nil {
		return nil, err
	}
	panels, f := js["panels"]
	if !f {
		return nil, fmt.Errorf("failed to find panels in %v", dash)
	}
	panelsList, f := panels.([]interface{})
	if !f {
		return nil, fmt.Errorf("failed to find panelsList in type %T: %v", panels, panels)
	}
	for _, p := range panelsList {
		pm := p.(map[string]interface{})
		targets, f := pm["targets"]
		if !f {
			continue
		}
		targetsList, f := targets.([]interface{})
		if !f {
			return nil, fmt.Errorf("failed to find targetsList in type %T: %v", targets, targets)
		}
		for _, t := range targetsList {
			tm := t.(map[string]interface{})
			expr, f := tm["expr"]
			if !f {
				return nil, fmt.Errorf("failed to find expr in %v", t)
			}
			queries = append(queries, expr.(string))
		}
	}
	return queries, nil
}

func includeQuery(query string, excluded []string) bool {
	for _, f := range excluded {
		if strings.Contains(query, f) {
			return false
		}
	}
	return true
}
