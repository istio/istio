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

package pilot

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
)

func TestClusterTrustBundleTrafficBasic(t *testing.T) {
	framework.NewTest(t).Run(func(ctx framework.TestContext) {
		ns := namespace.NewOrFail(ctx, namespace.Config{
			Prefix: "ctb-traffic",
			Inject: true,
		})
		values := map[string]string{
			"pilot.env.ENABLE_CLUSTER_TRUST_BUNDLE_API": "true",
		}

		// Apply the ClusterTrustBundle first
		ctx.ConfigIstio().EvalFile(ns.Name(), values, "testdata/clustertrustbundle-injection.yaml")

		// Deploy httpbin and sleep in the test namespace
		ctx.ConfigIstio().EvalFile(ns.Name(), values, "../../../../samples/httpbin/httpbin.yaml")
		ctx.ConfigIstio().EvalFile(ns.Name(), values, "../../../../samples/sleep/sleep.yaml")

		cluster := ctx.Clusters().Default()

		// Wait for sleep pod to be ready
		retry.UntilSuccessOrFail(t, func() error {
			pods, err := cluster.PodsForSelector(context.Background(), ns.Name(), "app=sleep")
			if err != nil || len(pods.Items) == 0 {
				return fmt.Errorf("no sleep pods: %v", err)
			}
			if pods.Items[0].Status.Phase != corev1.PodRunning {
				return fmt.Errorf("sleep pod not running: %v", pods.Items[0].Status.Phase)
			}
			return nil
		}, retry.Timeout(2*time.Minute), retry.Delay(2*time.Second))

		// Wait for httpbin pod to be ready
		retry.UntilSuccessOrFail(t, func() error {
			pods, err := cluster.PodsForSelector(context.Background(), ns.Name(), "app=httpbin")
			if err != nil || len(pods.Items) == 0 {
				return fmt.Errorf("no httpbin pods: %v", err)
			}
			if pods.Items[0].Status.Phase != corev1.PodRunning {
				return fmt.Errorf("httpbin pod not running: %v", pods.Items[0].Status.Phase)
			}
			return nil
		}, retry.Timeout(2*time.Minute), retry.Delay(2*time.Second))

		// Send a request from sleep to httpbin
		stdout, stderr, err := cluster.PodExec(
			ns.Name(),
			"app=sleep",
			"sleep",
			"curl -sS http://httpbin:8000/get",
		)
		if err != nil {
			t.Fatalf("failed to exec curl from sleep to httpbin: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}
		if !containsString(stdout, "\"url\": \"/get\"") {
			t.Fatalf("unexpected response from httpbin: %s", stdout)
		}
	})
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func containsString(s, substr string) bool {
	return (len(s) >= len(substr)) && (s == substr || (len(s) > len(substr) && (containsString(s[1:], substr) || containsString(s[:len(s)-1], substr))))
}
