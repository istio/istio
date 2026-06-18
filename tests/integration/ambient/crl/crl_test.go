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

package crl

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/istio"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/retry"
)

// TestZtunnelCrlOutbound verifies CRL enforcement on outbound connections in a multi-cluster, multi-network setup.
//
// The source ztunnel validates the remote destination workload's certificate chain.
// By revoking the remote cluster's IA in local cluster's root CRL, local cluster's ztunnel rejects all
// connections to remote cluster's workloads whose cert chain includes the revoked IA.
//
// Test steps:
//  1. Cross-cluster call local client → remote server succeeds (baseline)
//  2. Revoke remote cluster's IA in local cluster's root CRL
//  3. Wait for CRL propagation to local cluster's istio-ca-crl ConfigMap
//  4. Restart local cluster's ztunnel so it loads the updated CRL at startup and drops existing connections
//  5. Cross-cluster call fails: source ztunnel rejects server cert because remote cluster's IA cert is revoked
//  6. Verify istio_crl_policy_rejections_total{reporter="source"} metric incremented on local cluster
//  7. Reset local/client cluster to clean CRL state
func TestZtunnelCrlOutbound(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			defer resetCrlState(t, clientCluster)

			verifyConnectionTo(t, server)
			t.Logf("baseline call succeeded")

			// Revoke the server cluster's IA in the client cluster's root CRL so that the
			// client cluster's ztunnel rejects outbound connections to the server cluster.
			certBundle.RevokeRemoteIntermediate(t, clientCluster, serverCluster)
			certBundle.WaitForCRLPropagation(t, clientCluster)
			restartZtunnelPodAndWait(t, clientCluster)

			t.Logf("waiting for source ztunnel to reject outbound connection to remote server with revoked IA")
			verifyConnectionToRejected(t, server, clientCluster, "source")
		})
}

// TestZtunnelCrlInbound verifies CRL enforcement on inbound connections in a multi-cluster, multi-network setup.
//
// The remote destination ztunnel validates the source workload's certificate chain.
// By revoking local cluster's IA in the remote cluster's root CRL, remote cluster's destination ztunnel rejects
// all inbound connections from local cluster's workloads whose cert chain includes the revoked IA.
//
// Test steps:
//  1. Cross-cluster call local client → remote server succeeds (baseline)
//  2. Revoke local cluster's IA in remote cluster's root CRL
//  3. Wait for CRL propagation to remote cluster's istio-ca-crl ConfigMap
//  4. Restart remote cluster's ztunnel so it loads the updated CRL at startup and drops existing connections
//  5. Cross-cluster call fails: destination ztunnel rejects client cert because local cluster's IA cert is revoked
//  6. Verify istio_crl_policy_rejections_total{reporter="destination"} metric incremented on remote cluster
//  7. Reset remote/server cluster to clean CRL state
func TestZtunnelCrlInbound(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			defer resetCrlState(t, serverCluster)

			verifyConnectionTo(t, server)
			t.Logf("baseline call succeeded")

			// Revoke the client cluster's IA in the server cluster's root CRL so that the
			// server cluster's ztunnel rejects inbound connections from the client cluster.
			certBundle.RevokeRemoteIntermediate(t, serverCluster, clientCluster)
			certBundle.WaitForCRLPropagation(t, serverCluster)
			restartZtunnelPodAndWait(t, serverCluster)

			t.Logf("waiting for destination ztunnel to reject inbound connection from client with revoked IA")
			verifyConnectionToRejected(t, server, serverCluster, "destination")
		})
}

// resetCrlState resets a cluster's CRL to empty state, waits for ConfigMap propagation, then restarts all ztunnel instances.
func resetCrlState(t framework.TestContext, cl cluster.Cluster) {
	t.Helper()
	certBundle.ResetCRL(t, cl)
	restartZtunnelPodAndWait(t, cl)
	t.Logf("CRL reset complete on %s", cl.Name())
}

// restartZtunnelPodAndWait deletes all ztunnel pods on the given cluster so the DS recreates them,
// causing ztunnel to start fresh with the CRL loaded from the current istio-ca-crl ConfigMap volume and to drop existing connections.
func restartZtunnelPodAndWait(t framework.TestContext, targetCluster cluster.Cluster) {
	t.Helper()
	clusterName := targetCluster.Name()
	istioCfg := istio.DefaultConfigOrFail(t, t)
	ztunnelNS := istioCfg.ZtunnelNamespace

	t.Logf("restarting ztunnel pods on %s to load CRL from ConfigMap and drop existing connections", clusterName)
	if err := targetCluster.Kube().CoreV1().Pods(ztunnelNS).DeleteCollection(
		context.TODO(),
		metav1.DeleteOptions{},
		metav1.ListOptions{LabelSelector: "app=ztunnel"},
	); err != nil {
		t.Fatalf("failed to delete ztunnel pods on %s: %v", clusterName, err)
	}

	if _, err := kubetest.WaitUntilPodsAreReady(
		kubetest.NewPodFetch(targetCluster, ztunnelNS, "app=ztunnel"),
	); err != nil {
		t.Fatalf("ztunnel pods failed to become ready on %s after restart: %v", clusterName, err)
	}
	t.Logf("ztunnel pods restarted and ready on %s", clusterName)
}

func verifyConnectionTo(t framework.TestContext, app echo.Instance) {
	t.Helper()
	t.Logf("verifying connection to %v", app.ServiceName())
	retry.UntilSuccessOrFail(t, func() error {
		_, err := client.Call(echo.CallOptions{
			To:                      app,
			Port:                    echo.Port{Name: "http"},
			Scheme:                  scheme.HTTP,
			NewConnectionPerRequest: true,
			Count:                   1,
		})
		if err != nil {
			return err
		}
		return nil
	}, retry.Timeout(30*time.Second))
}

func verifyConnectionToRejected(t framework.TestContext, app echo.Instance, cl cluster.Cluster, reporter string) {
	t.Helper()
	retry.UntilSuccessOrFail(t, func() error {
		if _, err := client.Call(echo.CallOptions{
			To:                      app,
			Port:                    echo.Port{Name: "http"},
			Scheme:                  scheme.HTTP,
			NewConnectionPerRequest: true,
			Count:                   1,
			Retry:                   echo.Retry{NoRetry: true},
		}); err == nil {
			return fmt.Errorf("expected connection to fail, but it succeeded")
		}
		return checkCRLRejectionMetric(t, cl, reporter)
	}, retry.Timeout(2*time.Minute), retry.Delay(5*time.Second))
}

// checkCRLRejectionMetric queries all ztunnel pods on the targetCluster and returns an error
// if istio_crl_policy_rejections_total{reporter="<reporter>"} is zero.
func checkCRLRejectionMetric(t framework.TestContext, targetCluster cluster.Cluster, reporter string) error {
	t.Helper()
	istioCfg := istio.DefaultConfigOrFail(t, t)
	ztunnelNS := istioCfg.ZtunnelNamespace
	clusterName := targetCluster.Name()

	ztunnelPods, err := kubetest.NewPodFetch(targetCluster, ztunnelNS, "app=ztunnel")()
	if err != nil {
		return fmt.Errorf("failed to fetch ztunnel pods on %s: %v", clusterName, err)
	}

	var total float64
	for _, pod := range ztunnelPods {
		fwd, err := targetCluster.NewPortForwarder(pod.Name, pod.Namespace, "", 0, 15020)
		if err != nil {
			return fmt.Errorf("failed to create port forwarder to ztunnel pod %s: %v", pod.Name, err)
		}
		if err := fwd.Start(); err != nil {
			fwd.Close()
			return fmt.Errorf("failed to start port forwarder to ztunnel pod %s: %v", pod.Name, err)
		}

		resp, err := http.Get(fmt.Sprintf("http://%s/metrics", fwd.Address()))
		fwd.Close()
		if err != nil {
			return fmt.Errorf("failed to GET /metrics from ztunnel pod %s: %v", pod.Name, err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("failed to read /metrics from ztunnel pod %s: %v", pod.Name, err)
		}

		total += parseCRLRejectionTotal(string(body), reporter)
	}

	if total == 0 {
		return fmt.Errorf("istio_crl_policy_rejections_total{reporter=%q} is 0 on %s ztunnel pods",
			reporter, clusterName)
	}
	t.Logf("istio_crl_policy_rejections_total{reporter=%q} = %g on %s", reporter, total, clusterName)
	return nil
}

// parseCRLRejectionTotal scans Prometheus text-format metrics and returns the value of
// istio_crl_policy_rejections_total for the given reporter label.
func parseCRLRejectionTotal(metricsBody, reporter string) float64 {
	const metricName = "istio_crl_policy_rejections_total{"
	reporterLabel := fmt.Sprintf(`reporter="%s"`, reporter)
	for _, line := range strings.Split(metricsBody, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(trimmed, metricName) {
			continue
		}
		if !strings.Contains(trimmed, reporterLabel) {
			continue
		}
		parts := strings.Fields(trimmed)
		if len(parts) >= 2 {
			if val, err := strconv.ParseFloat(parts[1], 64); err == nil {
				return val
			}
		}
	}
	return 0
}
