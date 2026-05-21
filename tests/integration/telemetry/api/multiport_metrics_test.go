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

package api

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/util/retry"
)

// multiPortPrimaryConfigMap serves "primary_metric_total" on port 8080 at /metrics.
// busybox httpd maps /www/<filename> → GET /<filename>, so key "metrics" is deliberate.
const multiPortPrimaryConfigMap = `
apiVersion: v1
kind: ConfigMap
metadata:
  name: multiport-primary-metrics
data:
  metrics: |
    # HELP primary_metric_total Test metric from container 1 (port 8080).
    # TYPE primary_metric_total counter
    primary_metric_total{container="primary"} 1
`

// multiPortSecondaryConfigMap serves "secondary_metric_total" on port 9100 at /metrics.
const multiPortSecondaryConfigMap = `
apiVersion: v1
kind: ConfigMap
metadata:
  name: multiport-secondary-metrics
data:
  metrics: |
    # HELP secondary_metric_total Test metric from container 2 (port 9100).
    # TYPE secondary_metric_total counter
    secondary_metric_total{container="secondary"} 42
`

// multiPortDeployment runs two busybox httpd containers on distinct ports.
// The prometheus.istio.io/scrape-targets annotation triggers multi-port fan-out
// in the pilot-agent; the webhook rewrites prometheus.io/* to point to :15020.
const multiPortDeployment = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: multiport-metrics-app
spec:
  replicas: 1
  selector:
    matchLabels:
      app: multiport-metrics-app
  template:
    metadata:
      labels:
        app: multiport-metrics-app
      annotations:
        prometheus.istio.io/scrape-targets: "8080:/metrics,9100:/metrics"
    spec:
      containers:
      - name: primary
        image: busybox:1.28
        command: ["httpd", "-f", "-p", "8080", "-h", "/www"]
        ports:
        - containerPort: 8080
        volumeMounts:
        - name: primary-metrics
          mountPath: /www
      - name: secondary
        image: busybox:1.28
        command: ["httpd", "-f", "-p", "9100", "-h", "/www"]
        ports:
        - containerPort: 9100
        volumeMounts:
        - name: secondary-metrics
          mountPath: /www
      volumes:
      - name: primary-metrics
        configMap:
          name: multiport-primary-metrics
      - name: secondary-metrics
        configMap:
          name: multiport-secondary-metrics
`

// multiPortFailingDeployment has the same annotation but port 9100 is never
// bound, so the pilot-agent gets connection-refused and increments AppScrapeErrors.
const multiPortFailingDeployment = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: multiport-failing-app
spec:
  replicas: 1
  selector:
    matchLabels:
      app: multiport-failing-app
  template:
    metadata:
      labels:
        app: multiport-failing-app
      annotations:
        prometheus.istio.io/scrape-targets: "8080:/metrics,9100:/metrics"
    spec:
      containers:
      - name: primary
        image: busybox:1.28
        command: ["httpd", "-f", "-p", "8080", "-h", "/www"]
        ports:
        - containerPort: 8080
        volumeMounts:
        - name: primary-metrics
          mountPath: /www
      - name: noop
        image: busybox:1.28
        command: ["sleep", "infinity"]
      volumes:
      - name: primary-metrics
        configMap:
          name: multiport-primary-metrics
`

// TestMultiPortMetricsMerge verifies that when a pod carries the
// prometheus.istio.io/scrape-targets annotation with two ports, Prometheus
// receives metrics from both containers via the pilot-agent's merged :15020
// endpoint, even under STRICT mTLS where direct pod scraping is blocked.
func TestMultiPortMetricsMerge(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			ns := apps.Namespace.Name()

			t.ConfigIstio().YAML(ist.Settings().SystemNamespace, strictMtlsPeerAuthenticationConfig).ApplyOrFail(t)
			t.ConfigIstio().YAML(ns,
				multiPortPrimaryConfigMap+"\n---"+
					multiPortSecondaryConfigMap+"\n---"+
					multiPortDeployment,
			).ApplyOrFail(t)

			cluster := t.Clusters().Default()

			// Phase 1: direct pod-exec assertion against :15020/stats/prometheus. Hitting the
			// agent endpoint directly distinguishes "the agent didn't merge" (a fast failure,
			// observable here) from "Prometheus hasn't scraped yet" (a slow path observed in
			// phase 2). The merged-metrics endpoint is reachable from any container in the pod
			// because they share the network namespace; the primary busybox container has wget
			// natively, so we exec there.
			retry.UntilSuccessOrFail(t, func() error {
				pods, err := cluster.PodsForSelector(context.TODO(), ns, "app=multiport-metrics-app")
				if err != nil {
					return fmt.Errorf("listing multiport-metrics-app pods: %w", err)
				}
				if len(pods.Items) == 0 {
					return fmt.Errorf("no multiport-metrics-app pods yet")
				}
				stdout, _, err := cluster.PodExec(pods.Items[0].Name, ns, "primary",
					"wget -qO- http://localhost:15020/stats/prometheus")
				if err != nil {
					return fmt.Errorf("PodExec wget against :15020/stats/prometheus failed: %w", err)
				}
				if !strings.Contains(stdout, "primary_metric_total") {
					return fmt.Errorf("merged response missing primary_metric_total; got:\n%s", stdout)
				}
				if !strings.Contains(stdout, "secondary_metric_total") {
					return fmt.Errorf("merged response missing secondary_metric_total; got:\n%s", stdout)
				}
				return nil
			}, retry.Delay(framework.TelemetryRetryDelay), retry.Timeout(framework.TelemetryRetryTimeout))

			// Phase 2: full pipeline via Prometheus. Validates the scrape-to-Prometheus path
			// once the agent-side behavior is confirmed.
			retry.UntilSuccessOrFail(t, func() error {
				v, err := promInst.RawQuery(cluster, fmt.Sprintf(`primary_metric_total{namespace=%q}`, ns))
				if err != nil {
					return fmt.Errorf("querying primary_metric_total: %w", err)
				}
				if _, err := prometheus.Sum(v); err != nil {
					return fmt.Errorf("primary_metric_total absent in namespace %s: %w", ns, err)
				}

				v, err = promInst.RawQuery(cluster, fmt.Sprintf(`secondary_metric_total{namespace=%q}`, ns))
				if err != nil {
					return fmt.Errorf("querying secondary_metric_total: %w", err)
				}
				if _, err := prometheus.Sum(v); err != nil {
					return fmt.Errorf("secondary_metric_total absent in namespace %s: %w", ns, err)
				}

				return nil
			}, retry.Delay(framework.TelemetryRetryDelay), retry.Timeout(framework.TelemetryRetryTimeout))
		})
}

// TestMultiPortMetricsMergePartialFailure verifies that when one of the scrape
// targets is unreachable the pilot-agent still serves metrics from the healthy
// target and increments the AppScrapeErrors counter at least once.
func TestMultiPortMetricsMergePartialFailure(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			ns := apps.Namespace.Name()

			t.ConfigIstio().YAML(ist.Settings().SystemNamespace, strictMtlsPeerAuthenticationConfig).ApplyOrFail(t)
			t.ConfigIstio().YAML(ns,
				multiPortPrimaryConfigMap+"\n---"+
					multiPortFailingDeployment,
			).ApplyOrFail(t)

			cluster := t.Clusters().Default()

			// Phase 1: direct pod-exec assertion. Confirms the agent serves primary metrics
			// even when the secondary target is unreachable, without waiting on Prometheus.
			retry.UntilSuccessOrFail(t, func() error {
				pods, err := cluster.PodsForSelector(context.TODO(), ns, "app=multiport-failing-app")
				if err != nil {
					return fmt.Errorf("listing multiport-failing-app pods: %w", err)
				}
				if len(pods.Items) == 0 {
					return fmt.Errorf("no multiport-failing-app pods yet")
				}
				stdout, _, err := cluster.PodExec(pods.Items[0].Name, ns, "primary",
					"wget -qO- http://localhost:15020/stats/prometheus")
				if err != nil {
					return fmt.Errorf("PodExec wget against :15020/stats/prometheus failed: %w", err)
				}
				if !strings.Contains(stdout, "primary_metric_total") {
					return fmt.Errorf("primary_metric_total missing from merged response despite healthy port 8080; got:\n%s", stdout)
				}
				return nil
			}, retry.Delay(framework.TelemetryRetryDelay), retry.Timeout(framework.TelemetryRetryTimeout))

			// Phase 2: full pipeline via Prometheus.
			retry.UntilSuccessOrFail(t, func() error {
				// Primary metrics from port 8080 must still be present.
				v, err := promInst.RawQuery(cluster, fmt.Sprintf(`primary_metric_total{namespace=%q}`, ns))
				if err != nil {
					return fmt.Errorf("querying primary_metric_total: %w", err)
				}
				if _, err := prometheus.Sum(v); err != nil {
					return fmt.Errorf("primary_metric_total absent despite port 8080 being healthy: %w", err)
				}

				// At least one app scrape error must be recorded against THIS deployment.
				// The pod-label filter `pod=~"multiport-failing-app-.*"` scopes the cumulative
				// counter so a stray failure from any other sidecar in the namespace cannot
				// satisfy the >= 1 assertion. AppScrapeErrors counter is exported by
				// pilot-agent as istio_agent_scrape_failures_total{type="application"}.
				v, err = promInst.RawQuery(cluster, fmt.Sprintf(
					`sum(istio_agent_scrape_failures_total{type="application",namespace=%q,pod=~"multiport-failing-app-.*"})`, ns))
				if err != nil {
					return fmt.Errorf("querying istio_agent_scrape_failures_total: %w", err)
				}
				total, err := prometheus.Sum(v)
				if err != nil {
					return fmt.Errorf("istio_agent_scrape_failures_total absent: %w", err)
				}
				if total < 1 {
					return fmt.Errorf("expected >= 1 app scrape error from multiport-failing-app, got %v", total)
				}

				return nil
			}, retry.Delay(framework.TelemetryRetryDelay), retry.Timeout(framework.TelemetryRetryTimeout))
		})
}
