//go:build integ
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
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sync/errgroup"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/stackdriver"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/telemetry"
)

// TestStackdriverMonitoring verifies that stackdriver WASM filter exports metrics with expected labels.
func TestStackdriverMonitoring(t *testing.T) {
	framework.NewTest(t).
		Features("observability.telemetry.stackdriver").
		Run(func(t framework.TestContext) {
			g, _ := errgroup.WithContext(context.Background())
			for _, cltInstance := range Clt {
				cltInstance := cltInstance
				g.Go(func() error {
					err := retry.UntilSuccess(func() error {
						if err := SendTraffic(cltInstance, http.Header{}, false); err != nil {
							return err
						}
						clName := cltInstance.Config().Cluster.Name()
						trustDomain := telemetry.GetTrustDomain(cltInstance.Config().Cluster, Ist.Settings().SystemNamespace)
						scopes.Framework.Infof("Validating for cluster %s", clName)

						// Validate cluster names in telemetry below once https://github.com/istio/istio/issues/28125 is fixed.
						if err := ValidateMetrics(t, filepath.Join(env.IstioSrc, serverRequestCount), filepath.Join(env.IstioSrc, clientRequestCount),
							clName, trustDomain); err != nil {
							return err
						}
						t.Logf("Metrics validated")
						if err := ValidateLogs(t, filepath.Join(env.IstioSrc, serverLogEntry), clName, trustDomain, stackdriver.ServerAccessLog); err != nil {
							return err
						}
						t.Logf("logs validated")
						if err := validateTraces(t); err != nil {
							return err
						}
						t.Logf("Traces validated")

						return nil
					}, retry.Delay(framework.TelemetryRetryDelay), retry.Timeout(framework.TelemetryRetryTimeout))
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

func TestMain(m *testing.M) {
	framework.NewSuite(m).
		Label(label.CustomSetup).
		Label(label.IPv4). // We get detected as on GCE, since our tests run there, but don't have connectivity
		Setup(ConditionallySetupMetadataServer).
		Setup(istio.Setup(&Ist, setupConfig)).
		Setup(TestSetup).
		Run()
}

func setupConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.ControlPlaneValues = `
meshConfig:
  enableTracing: true
`
	// enable stackdriver filter
	cfg.Values["telemetry.v2.stackdriver.enabled"] = "true"
	cfg.Values["telemetry.v2.stackdriver.logging"] = "true"
	cfg.Values["telemetry.v2.stackdriver.topology"] = "true"
	cfg.Values["telemetry.v2.stackdriver.configOverride.enable_audit_log"] = "true"
	cfg.Values["global.proxy.tracer"] = "stackdriver"
	cfg.Values["pilot.traceSampling"] = "100"
	cfg.Values["telemetry.v2.accessLogPolicy.enabled"] = "true"
	cfg.Values["telemetry.v2.accessLogPolicy.logWindowDuration"] = "1s"
	cfg.Values["global.proxy.componentLogLevel"] = "rbac:debug,wasm:debug"

	// conditionally use a fake metadata server for testing off of GCP
	if GCEInst != nil {
		cfg.ControlPlaneValues = strings.Join([]string{cfg.ControlPlaneValues, FakeGCEMetadataServerValues, GCEInst.Address()}, "")
	}
}

func validateTraces(t test.Failer) error {
	t.Helper()

	// we are looking for a trace that looks something like:
	//
	// project_id:"test-project"
	// trace_id:"99bc9a02417c12c4877e19a4172ae11a"
	// spans:{
	//   span_id:440543054939690778
	//   name:"srv.istio-echo-1-92573.svc.cluster.local:80/*"
	//   start_time:{seconds:1594418699  nanos:648039133}
	//   end_time:{seconds:1594418699  nanos:669864006}
	//   parent_span_id:18050098903530484457
	// }
	//
	// we only need to validate the span value in the labels and project_id for
	// the purposes of this test at the moment.
	//
	// future improvements include adding canonical service info, etc. in the
	// span.

	wantSpanName := fmt.Sprintf("srv.%s.svc.cluster.local:80/*", EchoNsInst.Name())
	traces, err := SDInst.ListTraces(EchoNsInst.Name(), "")
	if err != nil {
		return fmt.Errorf("traces: could not retrieve traces from Stackdriver: %v", err)
	}
	for _, trace := range traces {
		t.Logf("trace: %v\n", trace)
		for _, span := range trace.Spans {
			if span.Name == wantSpanName {
				return nil
			}
		}
	}
	return errors.New("traces: could not find expected trace")
}
