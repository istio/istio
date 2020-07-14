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

package server

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/util/retry"
	util "istio.io/istio/tests/integration/mixer"
	"istio.io/istio/tests/integration/telemetry/tracing"
)

// TestProxyTracing exercises the trace generation features of Istio, based on the Envoy Trace driver for zipkin.
// The test verifies that all expected spans (a client span and a server span for each service call in the sample bookinfo app)
// are generated and that they are all a part of the same distributed trace with correct hierarchy and name.
//
// More information on distributed tracing can be found here: https://istio.io/docs/tasks/telemetry/distributed-tracing/zipkin/
func TestProxyTracing(t *testing.T) {
	framework.NewTest(t).
		Features("observability.telemetry.tracing.server").
		Run(func(ctx framework.TestContext) {
			bookinfoNsInst := tracing.GetBookinfoNamespaceInstance()

			retry.UntilSuccessOrFail(t, func() error {
				util.SendTraffic(tracing.GetIngressInstance(), t, "Sending traffic", "", "", 1)
				traces, err := tracing.GetZipkinInstance().QueryTraces(100,
					fmt.Sprintf("productpage.%s.svc.cluster.local:9080/productpage", bookinfoNsInst.Name()), "")
				if err != nil {
					return fmt.Errorf("cannot get traces from zipkin: %v", err)
				}
				if !tracing.VerifyBookinfoTraces(t, bookinfoNsInst.Name(), traces) {
					return errors.New("cannot find expected traces")
				}
				return nil
			}, retry.Delay(3*time.Second), retry.Timeout(80*time.Second))
		})
}

func TestMain(m *testing.M) {
	framework.NewSuite(m).
		RequireSingleCluster().
		Label(label.CustomSetup).
		Setup(istio.Setup(tracing.GetIstioInstance(), setupConfig)).
		Setup(tracing.TestSetup).
		Run()
}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.Values["tracing.provider"] = "zipkin"
	cfg.Values["meshConfig.enableTracing"] = "true"
	cfg.Values["meshConfig.disablePolicyChecks"] = "true"
	cfg.Values["pilot.traceSampling"] = "100.0"
}
