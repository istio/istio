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
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sync/errgroup"

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
			t.ConfigIstio().EvalFile(EchoNsInst.Name(), nil, filepath.Join(env.IstioSrc, accessLogPolicyEnvoyFilter)).ApplyOrFail(t)
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
						if err := ValidateTraces(t); err != nil {
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
	cfg.Values["global.proxy.tracer"] = "stackdriver"
	cfg.Values["pilot.traceSampling"] = "100"
	cfg.Values["pilot.env.STACKDRIVER_AUDIT_LOG"] = "true"
	cfg.Values["global.proxy.componentLogLevel"] = "misc:warning,rbac:debug,wasm:debug"

	// conditionally use a fake metadata server for testing off of GCP
	if GCEInst != nil {
		cfg.ControlPlaneValues = strings.Join([]string{cfg.ControlPlaneValues, FakeGCEMetadataServerValues, GCEInst.Address()}, "")
	}
}
