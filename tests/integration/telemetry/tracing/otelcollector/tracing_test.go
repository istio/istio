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
	_ "embed"
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

//go:embed testdata/otel-tracing.yaml
var otelTracingCfg string

//go:embed testdata/otel-tracing-http.yaml
var otelTracingHTTPCfg string

//go:embed testdata/otel-tracing-res-detectors.yaml
var otelTracingResDetectorsCfg string

//go:embed testdata/test-otel-grpc-with-initial-metadata.yaml
var otelTracingGRPCWithInitialMetadataCfg string

// TestProxyTracingOpenTelemetryProvider validates that Telemetry API configuration
// referencing an OpenTelemetry provider will generate traces appropriately.
// NOTE: This test relies on the priority of Telemetry API over MeshConfig tracing
// configuration. In the future, these two approaches should likely be separated
// into two distinct test suites.
func TestProxyTracingOpenTelemetryProvider(t *testing.T) {
	testcases := []struct {
		name            string
		customAttribute string
		cfgFile         string
	}{
		{
			name:            "grpc exporter",
			customAttribute: "provider=otel",
			cfgFile:         otelTracingCfg,
		},
		{
			name:            "http exporter",
			customAttribute: "provider=otel-http",
			cfgFile:         otelTracingHTTPCfg,
		},
		{
			name:            "resource detectors",
			customAttribute: "provider=otel-grpc-with-res-detectors",
			cfgFile:         otelTracingResDetectorsCfg,
		},
		{
			name:            "grpc exporter with initial metadata",
			customAttribute: "provider=test-otel-grpc-with-initial-metadata",
			cfgFile:         otelTracingGRPCWithInitialMetadataCfg,
		},
	}

	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			appNsInst := tracing.GetAppNamespace()

			for _, tc := range testcases {
				ctx.NewSubTest(tc.name).
					Run(func(ctx framework.TestContext) {
						// apply Telemetry resource with OTel provider
						ctx.ConfigIstio().YAML(appNsInst.Name(), tc.cfgFile).ApplyOrFail(ctx)

						// TODO fix tracing tests in multi-network https://github.com/istio/istio/issues/28890
						for _, cluster := range ctx.Clusters().ByNetwork()[ctx.Clusters().Default().NetworkName()] {
							ctx.NewSubTest(cluster.StableName()).Run(func(ctx framework.TestContext) {
								retry.UntilSuccessOrFail(ctx, func() error {
									err := tracing.SendTraffic(ctx, nil, cluster)
									if err != nil {
										return fmt.Errorf("cannot send traffic from cluster %s: %v", cluster.Name(), err)
									}
									hostDomain := ""
									if ctx.Settings().OpenShift {
										ingressAddr, _ := tracing.GetIngressInstance().HTTPAddresses()
										hostDomain = ingressAddr[0]
									}

									// the OTel collector exports to Zipkin
									traces, err := tracing.GetZipkinInstance().QueryTraces(300, "", tc.customAttribute, hostDomain)
									t.Logf("got traces %v from %s", traces, cluster)
									if err != nil {
										return fmt.Errorf("cannot get traces from zipkin: %v", err)
									}
									if !tracing.VerifyOtelEchoTraces(ctx, appNsInst.Name(), cluster.Name(), traces) {
										return errors.New("cannot find expected traces")
									}
									return nil
								}, retry.Delay(3*time.Second), retry.Timeout(80*time.Second))
							})
						}
					})
			}
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
  extensionProviders:
  - name: test-otel
    opentelemetry:
      service: opentelemetry-collector.istio-system.svc.cluster.local
      port: 4317
  - name: test-otel-http
    opentelemetry:
      service: opentelemetry-collector.istio-system.svc.cluster.local
      port: 4318
      http:
        path: "v1/traces"
        timeout: 10s
        headers:
          - name: "some-header"
            value: "some-value"
  - name: test-otel-res-detectors
    opentelemetry:
      service: opentelemetry-collector.istio-system.svc.cluster.local
      port: 4317
      resource_detectors:
        environment: {}
        dynatrace: {}
  - name: test-otel-grpc-with-initial-metadata
    opentelemetry:
      service: opentelemetry-collector.istio-system.svc.cluster.local
      port: 4317
      grpc:
        timeout: 3s
        initialMetadata:
        - name: "Authentication"
          value: "token-xxxxx"
`
}

func testSetup(ctx resource.Context) (err error) {
	addrs, _ := tracing.GetIngressInstance().HTTPAddresses()
	_, err = opentelemetry.New(ctx, opentelemetry.Config{IngressAddr: addrs[0]})
	return
}
