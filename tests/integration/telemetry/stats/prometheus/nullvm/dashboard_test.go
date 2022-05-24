//go:build integ
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

package nullvm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/yml"
	common "istio.io/istio/tests/integration/telemetry/stats/prometheus"
	"istio.io/pkg/log"
)

var dashboards = []struct {
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
			//"galley_validation_passed",
			"galley_validation_failed",
			// cAdvisor does not expose this metrics, and we don't have kubelet in kind
			"container_fs_usage_bytes",
			// flakes: https://github.com/istio/istio/issues/29871
			"container_memory_working_set_bytes",
			"container_cpu_usage_seconds_total",
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
			// cAdvisor does not expose this metrics, and we don't have kubelet in kind
			"container_fs_usage_bytes",
			// flakes: https://github.com/istio/istio/issues/29871
			"container_memory_working_set_bytes",
			"container_cpu_usage_seconds_total",
		},
		true,
	},
	{
		"istio-services-grafana-dashboards",
		"istio-extension-dashboard.json",
		[]string{
			"avg(envoy_wasm_envoy_wasm_runtime_v8_",
			// flakes: https://github.com/istio/istio/issues/29871
			"container_memory_working_set_bytes",
			"container_cpu_usage_seconds_total",
		},
		false,
	},
}

func TestDashboard(t *testing.T) {
	c, cancel := context.WithCancel(context.Background())
	defer cancel()
	framework.NewTest(t).
		Features("observability.telemetry.dashboard").
		Run(func(t framework.TestContext) {
			p := common.GetPromInstance()

			t.ConfigIstio().YAML(common.GetAppNamespace().Name(), fmt.Sprintf(gatewayConfig, common.GetAppNamespace().Name())).
				ApplyOrFail(t)

			// Apply just the grafana dashboards
			cfg, err := os.ReadFile(filepath.Join(env.IstioSrc, "samples/addons/grafana.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			t.ConfigKube().YAML("istio-system", yml.SplitYamlByKind(string(cfg))["ConfigMap"]).ApplyOrFail(t)

			// We will send a bunch of requests until the test exits. This ensures we are continuously
			// getting new metrics ingested. If we just send a bunch at once, Prometheus may scrape them
			// all in a single scrape which can lead to `rate()` not behaving correctly.
			go setupDashboardTest(c.Done())
			for _, d := range dashboards {
				d := d
				t.NewSubTest(d.name).Run(func(t framework.TestContext) {
					for _, cl := range t.Clusters() {
						if !cl.IsPrimary() && d.requirePrimary {
							// Skip verification of dashboards that won't be present on non primary(remote) clusters.
							continue
						}
						t.Logf("Verifying %s for cluster %s", d.name, cl.Name())
						cm, err := cl.Kube().CoreV1().ConfigMaps((*common.GetIstioInstance()).Settings().TelemetryNamespace).Get(
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
							retry.UntilSuccessOrFail(t, func() error {
								return checkMetric(cl, p, query, d.excluded)
							}, retry.Timeout(time.Minute))
						}
					}
				})
			}
		})
}

// Some templates use replacement variables. Instead, replace those with wildcard
var replacer = strings.NewReplacer(
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

func checkMetric(cl cluster.Cluster, p prometheus.Instance, query string, excluded []string) error {
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
			scopes.Framework.Infof("Filtered out metric '%v', but got samples: %v", query, numSamples)
		}
	}
	return nil
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
        host: b
        port:
          number: 80
  tcp:
  - match:
    - port: 31400
    route:
    - destination:
        host: b
        port:
          number: 9090
`

func setupDashboardTest(done <-chan struct{}) {
	// Send 200 http requests, 20 tcp requests across goroutines, generating a variety of error codes.
	// Spread out over 20s so rate() queries will behave correctly
	ticker := time.NewTicker(time.Second)
	times := 0
	for {
		select {
		case <-ticker.C:
			times++
			scopes.Framework.Infof("sending traffic %v", times)
			for _, ing := range common.GetIngressInstance() {
				host, port := ing.TCPAddress()
				_, err := ing.Call(echo.CallOptions{
					Port: echo.Port{
						Protocol: protocol.HTTP,
					},
					Count: 10,
					HTTP: echo.HTTP{
						Path:    fmt.Sprintf("/echo-%s?codes=418:10,520:15,200:75", common.GetAppNamespace().Name()),
						Headers: headers.New().WithHost("server").Build(),
					},
					Retry: echo.Retry{
						NoRetry: true,
					},
				})
				if err != nil {
					// Do not fail on errors since there may be initial startup errors
					// These calls are not under tests, the dashboards are, so we can be leniant here
					log.Warnf("requests failed: %v", err)
				}
				_, err = ing.Call(echo.CallOptions{
					Port: echo.Port{
						Protocol:    protocol.TCP,
						ServicePort: port,
					},
					Address: host,
					HTTP: echo.HTTP{
						Path:    fmt.Sprintf("/echo-%s", common.GetAppNamespace().Name()),
						Headers: headers.New().WithHost("server").Build(),
					},
					Retry: echo.Retry{
						NoRetry: true,
					},
				})
				if err != nil {
					// Do not fail on errors since there may be initial startup errors
					// These calls are not under tests, the dashboards are, so we can be leniant here
					log.Warnf("requests failed: %v", err)
				}
			}
		case <-done:
			scopes.Framework.Infof("done sending traffic after %v rounds", times)
			return
		}
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
