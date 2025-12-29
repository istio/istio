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

package zipkin

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/telemetry/tracing"
)

// TestServerTracing exercises the trace generation features of Istio, based on the Envoy Trace driver for zipkin.
// The test verifies that all expected spans (a client span and a server span for each service call in the sample bookinfo app)
// are generated and that they are all a part of the same distributed trace with correct hierarchy and name.
//
// More information on distributed tracing can be found here: https://istio.io/docs/tasks/telemetry/distributed-tracing/zipkin/
func TestServerTracing(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			appNsInst := tracing.GetAppNamespace()
			for _, cluster := range t.Clusters().ByNetwork()[t.Clusters().Default().NetworkName()] {
				t.NewSubTest(cluster.StableName()).Run(func(t framework.TestContext) {
					retry.UntilSuccessOrFail(t, func() error {
						err := tracing.SendTraffic(t, nil, cluster)
						if err != nil {
							return fmt.Errorf("cannot send traffic from cluster %s: %v", cluster.Name(), err)
						}
						hostDomain := ""
						if t.Settings().OpenShift {
							ingressAddr, _ := tracing.GetIngressInstance().HTTPAddresses()
							hostDomain = ingressAddr[0]
						}
						traces, err := tracing.GetZipkinInstance().QueryTraces(300,
							fmt.Sprintf("server.%s.svc.cluster.local:80/*", appNsInst.Name()), "", hostDomain)
						if err != nil {
							return fmt.Errorf("cannot get traces from zipkin: %v", err)
						}
						if !tracing.VerifyEchoTraces(t, appNsInst.Name(), cluster.Name(), traces) {
							return errors.New("cannot find expected traces")
						}
						return nil
					}, retry.Delay(3*time.Second), retry.Timeout(150*time.Second))
				})
			}
		})
}
