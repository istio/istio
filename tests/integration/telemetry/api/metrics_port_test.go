//go:build integ

// Copyright Istio Authors. All Rights Reserved.
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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	// secureMetricsPort is the Envoy-only stats port protected by mTLS (configured in setup_test.go).
	secureMetricsPort = 15091
	// secureMergedMetricsPort is the merged metrics port (Envoy + app + agent) protected by mTLS.
	secureMergedMetricsPort = 15092
	// envoyStatsPort is the plaintext Envoy stats port — bound to localhost by METRICS_LOCALHOST_ACCESS_ONLY.
	envoyStatsPort = 15090
	// mergedMetricsPort is the plaintext merged metrics port — restricted to localhost by METRICS_LOCALHOST_ACCESS_ONLY.
	mergedMetricsPort = 15020
)

// TestSecureMetricsPorts verifies mTLS enforcement on ports 15091/15092
// (ENVOY_SECURE_METRICS_PORT / ENVOY_SECURE_MERGED_METRICS_PORT) and the
// localhost-only restriction on 15090/15020 (METRICS_LOCALHOST_ACCESS_ONLY).
func TestSecureMetricsPorts(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		target := getSecureMetricsTarget(t)

		t.NewSubTest("mtls-scrape-15091").Run(func(t framework.TestContext) {
			testMTLSScrape(t, target, secureMetricsPort)
		})
		t.NewSubTest("mtls-scrape-15092").Run(func(t framework.TestContext) {
			testMTLSScrape(t, target, secureMergedMetricsPort)
		})
		t.NewSubTest("plain-http-rejected-15091").Run(func(t framework.TestContext) {
			testHTTPBlocked(t, target, secureMetricsPort)
		})
		t.NewSubTest("plain-http-rejected-15092").Run(func(t framework.TestContext) {
			testHTTPBlocked(t, target, secureMergedMetricsPort)
		})
		t.NewSubTest("15090-localhost-only").Run(func(t framework.TestContext) {
			testHTTPBlocked(t, target, envoyStatsPort)
		})
		t.NewSubTest("15020-localhost-only").Run(func(t framework.TestContext) {
			testHTTPBlocked(t, target, mergedMetricsPort)
		})
		t.NewSubTest("auto-annotation-secure-port").Run(func(t framework.TestContext) {
			testAutoAnnotation(t, target, "prometheus.istio.io/secure-port", "15092")
		})
	})
}

func getSecureMetricsTarget(t framework.TestContext) echo.Instances {
	target := match.ServiceName(echo.NamespacedName{Name: "secure-metrics-target", Namespace: apps.Namespace}).
		GetMatches(apps.Echos().All.Instances())
	if len(target) == 0 {
		t.Fatalf("secure-metrics-target workload not found")
	}
	return target
}

// testMTLSScrape verifies mockProm can scrape the port using its proxy-provisioned Istio certs.
// Cert files are written to /etc/certs/custom via the OUTPUT_CERTS sidecar option in setup_test.go.
func testMTLSScrape(t framework.TestContext, target echo.Instances, port int) {
	for _, prom := range mockProm {
		retry.UntilSuccessOrFail(t, func() error {
			st := match.Cluster(prom.Config().Cluster).FirstOrFail(t, target)
			_, err := prom.Call(echo.CallOptions{
				ToWorkload: st,
				Scheme:     scheme.HTTPS,
				Port:       echo.Port{ServicePort: port},
				HTTP:       echo.HTTP{Path: "/stats/prometheus"},
				TLS: echo.TLS{
					CertFile:           "/etc/certs/custom/cert-chain.pem",
					KeyFile:            "/etc/certs/custom/key.pem",
					CaCertFile:         "/etc/certs/custom/root-cert.pem",
					InsecureSkipVerify: true,
				},
			})
			return err
		})
	}
}

// testHTTPBlocked verifies plain HTTP to port is not accessible from outside the pod.
// mockProm has outbound interception disabled, so its HTTP goes directly to the target
// without Envoy's transparent mTLS upgrade. Expects a connection error or non-200 response.
func testHTTPBlocked(t framework.TestContext, target echo.Instances, port int) {
	for _, prom := range mockProm {
		st := match.Cluster(prom.Config().Cluster).FirstOrFail(t, target)
		if _, err := prom.Call(echo.CallOptions{
			ToWorkload: st,
			Scheme:     scheme.HTTP,
			Port:       echo.Port{ServicePort: port},
			HTTP:       echo.HTTP{Path: "/stats/prometheus"},
			Check:      check.Or(check.Error(), check.NotOK()),
			Retry:      echo.Retry{NoRetry: true},
		}); err != nil {
			t.Errorf("port %d is accessible via plain HTTP but should not be: %v", port, err)
		}
	}
}

// testAutoAnnotation verifies that the sidecar injector automatically sets the given annotation
// on the pod when ENVOY_SECURE_MERGED_METRICS_PORT is configured via proxyMetadata.
// This exercises applySecurePrometheusAnnotation end-to-end in a real cluster.
func testAutoAnnotation(t framework.TestContext, target echo.Instances, annotation, wantValue string) {
	for _, inst := range target {
		for _, w := range inst.WorkloadsOrFail(t) {
			pod, err := inst.Config().Cluster.Kube().CoreV1().Pods(inst.NamespaceName()).Get(
				context.TODO(), w.PodName(), metav1.GetOptions{},
			)
			if err != nil {
				t.Fatalf("failed to get pod %s: %v", w.PodName(), err)
			}
			got := pod.Annotations[annotation]
			if got != wantValue {
				t.Errorf("pod %s: annotation %s = %q, want %q (should be auto-set by injector from ENVOY_SECURE_MERGED_METRICS_PORT)",
					w.PodName(), annotation, got, wantValue)
			}
		}
	}
}
