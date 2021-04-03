// +build integ
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

package stackdriver

import (
	"context"
	"testing"

	"golang.org/x/sync/errgroup"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/stackdriver"
	telemetrypkg "istio.io/istio/pkg/test/framework/components/telemetry"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/telemetry"
)

const (
	tcpServerConnectionCount = "testdata/server_tcp_connection_count.json.tmpl"
	tcpClientConnectionCount = "testdata/client_tcp_connection_count.json.tmpl"
	tcpServerLogEntry        = "testdata/tcp_server_access_log.json.tmpl"
)

// TestTCPStackdriverMonitoring verifies that stackdriver TCP filter works.
func TestTCPStackdriverMonitoring(t *testing.T) {
	framework.NewTest(t).
		Features("observability.telemetry.stackdriver").
		Run(func(ctx framework.TestContext) {
			g, _ := errgroup.WithContext(context.Background())
			for _, cltInstance := range clt {
				cltInstance := cltInstance
				g.Go(func() error {
					err := retry.UntilSuccess(func() error {
						_, err := cltInstance.Call(echo.CallOptions{
							Target:   srv[0],
							PortName: "tcp",
							Count:    telemetry.RequestCountMultipler * len(srv),
						})
						if err != nil {
							return err
						}
						t.Logf("Validating Telemetry for Cluster %v", cltInstance.Config().Cluster)
						clName := cltInstance.Config().Cluster.Name()
						trustDomain := telemetry.GetTrustDomain(cltInstance.Config().Cluster, ist.Settings().SystemNamespace)
						if err := validateMetrics(t, tcpServerConnectionCount, tcpClientConnectionCount, clName, trustDomain); err != nil {
							return err
						}
						if err := validateLogs(t, tcpServerLogEntry, clName, trustDomain, stackdriver.ServerAccessLog); err != nil {
							return err
						}

						return nil
					}, retry.Delay(telemetrypkg.RetryDelay), retry.Timeout(telemetrypkg.RetryTimeout))
					if err != nil {
						return err
					}
					return nil
				})
			}
			if err := g.Wait(); err != nil {
				t.Fatalf("test failed: %v", err)
			}
		})
}
