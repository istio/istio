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

// Package otelcollector allows testing a variety of tracing solutions by
// employing an OpenTelemetry collector that exposes receiver endpoints for
// various protocols and forwards the spans to a Zipkin backend (for further
// querying and inspection).
package otelcollector

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/opentelemetry"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/telemetry/tracing"
)

var (
	//go:embed testdata/otel-tracing.yaml
	otelTracingCfg string

	//go:embed testdata/otel-tracing-http.yaml
	otelTracingHTTPCfg string

	//go:embed testdata/otel-tracing-res-detectors.yaml
	otelTracingResDetectorsCfg string

	//go:embed testdata/otel-grpc-with-initial-metadata.yaml
	otelTracingGRPCWithInitialMetadataCfg string

	//go:embed testdata/otel-tracing-with-auth.yaml
	otelTracingWithAuth string

	//go:embed testdata/echo-gateway.yaml
	echoGateway string

	//go:embed testdata/echo-gateway-tracing.yaml
	echoGatewayTracing string
)

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
		{
			name:            "grpc exporter with auth",
			customAttribute: "provider=otel-with-auth",
			cfgFile:         otelTracingWithAuth,
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
								}, retry.Delay(3*time.Second), retry.Timeout(150*time.Second))
							})
						}
					})
			}
		})
}

func TestGatewayTracing(t *testing.T) {
	framework.NewTest(t).Run(func(ctx framework.TestContext) {
		appNsInst := tracing.GetAppNamespace()
		istioInst := *tracing.GetIstioInstance()
		ctx.ConfigIstio().YAML(istioInst.Settings().SystemNamespace, echoGatewayTracing).ApplyOrFail(ctx)
		ctx.ConfigIstio().YAML(appNsInst.Name(), echoGateway).ApplyOrFail(ctx)

		// TODO fix tracing tests in multi-network https://github.com/istio/istio/issues/28890
		nt := ctx.Clusters().Default().NetworkName()
		for _, cluster := range ctx.Clusters() {
			if cluster.NetworkName() != nt {
				t.Skip()
			}
			ctx.NewSubTest(cluster.StableName()).Run(func(ctx framework.TestContext) {
				retry.UntilSuccessOrFail(ctx, func() error {
					reqPath := "/echo-server"
					err := tracing.SendIngressTraffic(ctx, reqPath, nil, cluster)
					if err != nil {
						return fmt.Errorf("cannot send traffic from cluster %s: %v", cluster.Name(), err)
					}

					hostDomain := ""
					if ctx.Settings().OpenShift {
						ingressAddr, _ := tracing.GetIngressInstance().HTTPAddresses()
						hostDomain = ingressAddr[0]
					}

					// the OTel collector exports to Zipkin
					traces, err := tracing.GetZipkinInstance().QueryTraces(300, "", "provider=otel-ingress", hostDomain)
					if err != nil {
						return fmt.Errorf("cannot get traces from zipkin: %v", err)
					}
					if !tracing.VerifyOtelIngressTraces(ctx, appNsInst.Name(), reqPath, traces) {
						t.Logf("got gateway traces %v from %s", traces, cluster)
						return errors.New("cannot find expected traces")
					}
					return nil
				}, retry.Delay(3*time.Second), retry.Timeout(150*time.Second))
			})
		}
	})
}

func TestMain(m *testing.M) {
	framework.NewSuite(m).
		Label(label.CustomSetup).
		Setup(istio.Setup(tracing.GetIstioInstance(), setupConfig, setupOtelCredentials)).
		Setup(tracing.TestSetup).
		Setup(testSetup).
		Run()
}

// setupOtelCredentials creates a secret in the istio-system namespace
// which will be mounted into Istiod and used by the OTel tracer provider.
func setupOtelCredentials(ctx resource.Context) error {
	systemNs, err := istio.ClaimSystemNamespace(ctx)
	if err != nil {
		return err
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "otel-credentials",
			Namespace: systemNs.Name(),
		},
		Data: map[string][]byte{
			"bearer-token": []byte("Bearer somerandomtoken"),
		},
	}
	for _, cluster := range ctx.AllClusters().Primaries() {
		if _, err := cluster.Kube().CoreV1().Secrets(systemNs.Name()).Create(context.TODO(), secret, metav1.CreateOptions{}); err != nil {
			if apierrors.IsAlreadyExists(err) {
				if _, err := cluster.Kube().CoreV1().Secrets(systemNs.Name()).Update(context.TODO(), secret, metav1.UpdateOptions{}); err != nil {
					return err
				}
			} else {
				return err
			}
		}
	}
	return nil
}

// TODO: convert test to Telemetry API for both scenarios
func setupConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.ControlPlaneValues = `
values:
  pilot:
    env:
      PILOT_SPAWN_UPSTREAM_SPAN_FOR_GATEWAY: true
    envVarFrom:
    - name: "OTEL_GRPC_AUTHORIZATION"
      valueFrom:
        secretKeyRef:
          name: otel-credentials
          key: bearer-token
          optional: true
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
  - name: test-otel-grpc-auth
    opentelemetry:
      service: opentelemetry-collector.istio-system.svc.cluster.local
      port: 5317
      grpc:
        initialMetadata:
        - name: "Authorization"
          envName: "OTEL_GRPC_AUTHORIZATION"
`
}

func testSetup(ctx resource.Context) (err error) {
	addrs, _ := tracing.GetIngressInstance().HTTPAddresses()
	_, err = opentelemetry.New(ctx, opentelemetry.Config{IngressAddr: addrs[0]})
	return err
}
