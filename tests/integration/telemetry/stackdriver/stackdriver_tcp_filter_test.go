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
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	tcpServerConnectionCount = "testdata/server_tcp_connection_count.json.tmpl"
	tcpClientConnectionCount = "testdata/client_tcp_connection_count.json.tmpl"
	tcpServerLogEntry        = "testdata/tcp_server_access_log.json.tmpl"
)

// TestTCPStackdriverMonitoring verifies that stackdriver TCP filter works.
func TestTCPStackdriverMonitoring(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			retry.UntilSuccessOrFail(t, func() error {
				_, err := clt.Call(echo.CallOptions{
					Target:   srv,
					PortName: "tcp",
					Count:    1,
				})
				if err != nil {
					return err
				}
				if err := validateMetrics(t, tcpServerConnectionCount, tcpClientConnectionCount); err != nil {
					return err
				}
				if err := validateLogs(t, tcpServerLogEntry); err != nil {
					return err
				}
				return nil
			}, retry.Delay(3*time.Second), retry.Timeout(40*time.Second))
		})
}
