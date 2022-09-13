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

// Package otelcollector allows testing a variety of tracing solutions by
// employing an OpenTelemetry collector that exposes receiver endpoints for
// various protocols and forwards the spans to a Zipkin backend (for further
// querying and inspection).
package otelcollector

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/opentelemetry"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/telemetry/tracing"
)

// TestProxyTracingOpenCensusMeshConfig exercises the trace generation features of Istio, based on
// the Envoy Trace driver for OpenCensusAgent.
func TestProxyTracingOpenCensusMeshConfig(t *testing.T) {
	framework.NewTest(t).
		Features("observability.telemetry.tracing.server").
		Run(func(t framework.TestContext) {
			appNsInst := tracing.GetAppNamespace()
			// TODO fix tracing tests in multi-network https://github.com/istio/istio/issues/28890
			for _, cluster := range t.Clusters().ByNetwork()[t.Clusters().Default().NetworkName()] {
				cluster := cluster
				t.NewSubTest(cluster.StableName()).Run(func(ctx framework.TestContext) {
					retry.UntilSuccessOrFail(t, func() error {
						err := tracing.SendTraffic(ctx, nil, cluster)
						if err != nil {
							return fmt.Errorf("cannot send traffic from cluster %s: %v", cluster.Name(), err)
						}

						traces, err := tracing.GetZipkinInstance().QueryTraces(300,
							fmt.Sprintf("server.%s.svc.cluster.local:80/*", appNsInst.Name()), "")
						if err != nil {
							return fmt.Errorf("cannot get traces from zipkin: %v", err)
						}
						if !tracing.VerifyEchoTraces(ctx, appNsInst.Name(), cluster.Name(), traces) {
							return errors.New("cannot find expected traces")
						}
						return nil
					}, retry.Delay(3*time.Second), retry.Timeout(80*time.Second))
				})
			}
		})
}

// TestProxyTracingOpenTelemetryProvider validates that Telemetry API configuration
// referencing an OpenTelemetry provider will generate traces appropriately.
// NOTE: This test relies on the priority of Telemetry API over MeshConfig tracing
// configuration. In the future, these two approaches should likely be separated
// into two distinct test suites.
func TestProxyTracingOpenTelemetryProvider(t *testing.T) {
	framework.NewTest(t).
		Features("observability.telemetry.tracing.server").
		Run(func(t framework.TestContext) {
			appNsInst := tracing.GetAppNamespace()

			// apply Telemetry resource with OTel provider

			config := `apiVersion: telemetry.istio.io/v1alpha1
kind: Telemetry
metadata:
  name: logs
spec:
  tracing:
  - providers:
    - name: test-otel
    randomSamplingPercentage: 100.0
`
			t.ConfigIstio().YAML(appNsInst.Name(), config).ApplyOrFail(t)

			// TODO fix tracing tests in multi-network https://github.com/istio/istio/issues/28890
			for _, cluster := range t.Clusters().ByNetwork()[t.Clusters().Default().NetworkName()] {
				cluster := cluster
				t.NewSubTest(cluster.StableName()).Run(func(ctx framework.TestContext) {
					retry.UntilSuccessOrFail(t, func() error {
						err := tracing.SendTraffic(ctx, nil, cluster)
						if err != nil {
							return fmt.Errorf("cannot send traffic from cluster %s: %v", cluster.Name(), err)
						}

						// the OTel collector exports to Zipkin
						traces, err := tracing.GetZipkinInstance().QueryTraces(300,
							fmt.Sprintf("server.%s.svc.cluster.local:80/*", appNsInst.Name()), "")
						if err != nil {
							return fmt.Errorf("cannot get traces from zipkin: %v", err)
						}
						if !tracing.VerifyEchoTraces(ctx, appNsInst.Name(), cluster.Name(), traces) {
							return errors.New("cannot find expected traces")
						}
						return nil
					}, retry.Delay(3*time.Second), retry.Timeout(80*time.Second))
				})
			}

			t.ConfigIstio().YAML(appNsInst.Name(), config).DeleteOrFail(t)
		})
}

func TestMain(m *testing.M) {
	framework.NewSuite(m).
		Label(label.CustomSetup).
		Setup(istio.Setup(tracing.GetIstioInstance(), setupConfig)).
		Setup(tracing.TestSetup).
		Setup(testSetup).
		Run()
}

// TODO: convert test to Telemetry API for both scenarios
func setupConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.ControlPlaneValues = `
meshConfig:
  enableTracing: true
  defaultConfig:
    tracing:
      openCensusAgent:
        address: "dns:opentelemetry-collector.istio-system.svc:55678"
        context: [B3]
  extensionProviders:
  - name: test-otel
    opentelemetry:
      service: opentelemetry-collector.istio-system.svc.cluster.local
      port: 4317
`
	cfg.Values["pilot.traceSampling"] = "100.0"
	cfg.Values["global.proxy.tracer"] = "openCensusAgent"
}

func testSetup(ctx resource.Context) (err error) {
	addr, _ := tracing.GetIngressInstance().HTTPAddress()
	_, err = opentelemetry.New(ctx, opentelemetry.Config{IngressAddr: addr})
	return
}
