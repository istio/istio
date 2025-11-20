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
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/label"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	testlabel "istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/security/crl/util"
)

// TestZtunnelCRL verifies that ztunnel respects CRL revocation
// The test verifies that:
//  1. Initial call succeeds before CRL update
//  2. After certificate revocation, deploy NEW apps that get certs from revoked CA
//  3. Calls between new apps fail due to revoked certificate
func TestZtunnelCRL(t *testing.T) {
	framework.NewTest(t).
		Label(testlabel.CustomSetup).
		Run(func(t framework.TestContext) {
			// test pre-revocation apps (should succeed)
			opts := echo.CallOptions{
				To: server,
				Port: echo.Port{
					Name: "http",
				},
				Scheme:                  scheme.HTTP,
				NewConnectionPerRequest: true,
				Count:                   1,
			}

			t.Logf("testing initial call before CRL update")
			client.CallOrFail(t, opts)
			t.Logf("initial call succeeded")

			// revoke the intermediate certificate
			util.RevokeIntermediate(t, certBundle)

			// wait for CRL file to be synced to ztunnel pods (ConfigMap volume propagation)
			waitForZtunnelCRLFile(t, certBundle)

			// deploy NEW apps and test (should fail)
			// create new namespaces for post-revocation apps
			revokedClientNS := namespace.NewOrFail(t, namespace.Config{
				Prefix: "ambient-client-revoked",
				Inject: false,
				Labels: map[string]string{
					label.IoIstioDataplaneMode.Name: "ambient",
				},
			})
			revokedServerNS := namespace.NewOrFail(t, namespace.Config{
				Prefix: "ambient-server-revoked",
				Inject: false,
				Labels: map[string]string{
					label.IoIstioDataplaneMode.Name: "ambient",
				},
			})

			// deploy revoked apps
			revokedClient, revokedServer := deployRevokedApps(t, revokedClientNS, revokedServerNS)

			// test should fail due to revoked cert
			revokedOpts := echo.CallOptions{
				To: revokedServer,
				Port: echo.Port{
					Name: "http",
				},
				Scheme:                  scheme.HTTP,
				NewConnectionPerRequest: true,
				Count:                   1,
				Check:                   check.Error(),
			}
			t.Logf("testing call with revoked apps, expecting failure")
			retry.UntilSuccessOrFail(t, func() error {
				revokedClient.CallOrFail(t, revokedOpts)
				return nil
			})
			t.Logf("call correctly failed with revoked apps")
		})
}

// waitForZtunnelCRLFile waits for the CRL file content inside ztunnel pods to match the expected CRL.
// this is needed because Kubernetes ConfigMap volume updates are eventually consistent.
func waitForZtunnelCRLFile(t framework.TestContext, bundle *util.Bundle) {
	t.Helper()
	t.Logf("waiting for CRL file to sync in ztunnel pods...")

	systemNS, err := istio.ClaimSystemNamespace(t)
	if err != nil {
		t.Fatalf("failed to get system namespace: %v", err)
	}

	expectedCRL := strings.TrimSpace(string(bundle.CRLPEM()))

	retry.UntilSuccessOrFail(t, func() error {
		for _, c := range t.AllClusters() {
			pods, err := c.Kube().CoreV1().Pods(systemNS.Name()).List(
				context.TODO(),
				metav1.ListOptions{LabelSelector: "app=ztunnel"},
			)
			if err != nil {
				return fmt.Errorf("failed to list ztunnel pods: %w", err)
			}

			for _, pod := range pods.Items {
				stdout, stderr, err := c.PodExec(
					pod.Name,
					systemNS.Name(),
					"istio-proxy",
					"cat /var/run/secrets/istio/crl/ca-crl.pem",
				)
				if err != nil {
					return fmt.Errorf("failed to read CRL file in %s: %v, stderr: %s", pod.Name, err, stderr)
				}

				actualCRL := strings.TrimSpace(stdout)
				if actualCRL != expectedCRL {
					return fmt.Errorf("CRL file in %s not yet updated (expected len=%d, got len=%d)",
						pod.Name, len(expectedCRL), len(actualCRL))
				}
			}
		}
		return nil
	}, retry.Timeout(3*time.Minute), retry.Delay(5*time.Second))

	t.Logf("CRL file synced to all ztunnel pods")
}

// deployRevokedApps deploys new echo apps in the given namespaces
// these apps will get certificates from the now-revoked CA chain
func deployRevokedApps(t framework.TestContext, clientNS, serverNS namespace.Instance) (echo.Instance, echo.Instance) {
	t.Helper()

	var revokedClient, revokedServer echo.Instance

	deployment.New(t).
		With(&revokedClient, echo.Config{
			Service:        "ambient-client-revoked",
			Namespace:      clientNS,
			ServiceAccount: true,
			Ports: []echo.Port{{
				Name:         "http",
				Protocol:     protocol.HTTP,
				WorkloadPort: 8080,
			}},
		}).
		With(&revokedServer, echo.Config{
			Service:        "ambient-server-revoked",
			Namespace:      serverNS,
			ServiceAccount: true,
			Ports: []echo.Port{{
				Name:         "http",
				Protocol:     protocol.HTTP,
				WorkloadPort: 8080,
			}},
		}).
		BuildOrFail(t)

	return revokedClient, revokedServer
}
