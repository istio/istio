//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package egressgatewayorigination

import (
	"fmt"
	"path"
	"reflect"
	"testing"
	"time"

	"io/ioutil"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/common/response"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/resource/environment"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/structpath"
)

func mustReadCert(t *testing.T, f string) string {
	b, err := ioutil.ReadFile(path.Join(env.IstioSrc, "tests/testdata/certs", f))
	if err != nil {
		t.Fatalf("failed to read %v: %v", f, err)
	}
	return string(b)
}

const (
	// paths to test configs
	simpleTLSDestinationRuleConfig  = "testdata/destination-rule-tls-origination.yaml"
	disableTLSDestinationRuleConfig = "testdata/destination-rule-no-tls-origination.yaml"
)

// TestEgressGatewayTls brings up an cluster and will ensure that the TLS origination at
// egress gateway allows secure communication between the egress gateway and external workload.
// This test brings up an egress gateway to originate TLS connection. The test will ensure that requests
// are routed securely through the egress gateway and that the TLS origination happens at the gateway.
func TestEgressGatewayTls(t *testing.T) {
	framework.NewTest(t).
		Features("security.egress.tls").
		Run(func(ctx framework.TestContext) {
			ctx.RequireOrSkip(environment.Kube)

			client, server, appNamespace := setupEcho(t, ctx)

			testCases := map[string]struct {
				destinationRulePath string
				response            []string
				portName            string
			}{
				"SIMPLE TLS origination from egress gateway succeeds": {
					destinationRulePath: simpleTLSDestinationRuleConfig,
					response:            []string{response.StatusCodeOK},
					portName:            "https",
				},
				"No TLS origination from egress gateway returns 503 response": {
					destinationRulePath: disableTLSDestinationRuleConfig,
					response:            []string{response.StatusCodeUnavailable},
					portName:            "https",
				},
			}

			for name, tc := range testCases {
				t.Run(name, func(t *testing.T) {
					ctx.ApplyConfigOrFail(ctx, appNamespace.Name(), file.AsStringOrFail(ctx, tc.destinationRulePath))
					defer ctx.DeleteConfigOrFail(ctx, appNamespace.Name(), file.AsStringOrFail(ctx, tc.destinationRulePath))

					retry.UntilSuccessOrFail(t, func() error {
						resp, err := client.Call(echo.CallOptions{
							Target:   server,
							PortName: tc.portName,
						})
						if err != nil {
							return fmt.Errorf("request failed: %v", err)
						}
						codes := make([]string, 0, len(resp))
						for _, r := range resp {
							codes = append(codes, r.Code)
						}
						if !reflect.DeepEqual(codes, tc.response) {
							return fmt.Errorf("got codes %q, expected %q", codes, tc.response)
						}
						return nil
					}, retry.Delay(time.Second), retry.Timeout(20*time.Second))
				})
			}
		})
}

const (
	// paths to setup configs
	simpleTLSGatewayConfig = "testdata/gateway-tls-origination.yaml"
	sidecarScopeConfig     = "testdata/sidecar-scope.yaml"
)

func setupEcho(t *testing.T, ctx framework.TestContext) (echo.Instance, echo.Instance, namespace.Instance) {
	p := pilot.NewOrFail(t, ctx, pilot.Config{})

	appsNamespace := namespace.NewOrFail(t, ctx, namespace.Config{
		Prefix: "app",
		Inject: true,
	})
	serviceNamespace := namespace.NewOrFail(t, ctx, namespace.Config{
		Prefix: "service",
		Inject: true,
	})

	var client, server echo.Instance
	echoboot.NewBuilderOrFail(t, ctx).
		With(&client, echo.Config{
			Service:   "client",
			Namespace: appsNamespace,
			Pilot:     p,
			Subsets:   []echo.SubsetConfig{{}},
		}).
		With(&server, echo.Config{
			Service:   "destination",
			Namespace: appsNamespace,
			Ports: []echo.Port{
				{
					// Plain HTTP port
					Name:         "http",
					Protocol:     protocol.HTTP,
					ServicePort:  80,
					InstancePort: 8080,
				},
				{
					// HTTPS port
					Name:        "https",
					Protocol:    protocol.HTTPS,
					ServicePort: 443,
					TLS:         true,
				},
			},
			Pilot: p,
			// Set up TLS certs on the server. This will make the server listen with these credentials.
			TLSSettings: &common.TLSSettings{
				// Echo has these test certs baked into the docker image
				RootCert:   mustReadCert(t, "cacert.pem"),
				ClientCert: mustReadCert(t, "cert.crt"),
				Key:        mustReadCert(t, "cert.key"),
			},
			// Do not inject, as we are testing non-Istio TLS here
			Subsets: []echo.SubsetConfig{{
				Version:     "v1",
				Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
			}},
		}).
		BuildOrFail(t)

	// External traffic should work even if we have service entries on the same ports
	// Only Service namespace is known so that app namespace "appears" to be outside the mesh
	if err := ctx.ApplyConfig(appsNamespace.Name(), file.AsStringOrFail(ctx, sidecarScopeConfig)); err != nil {
		ctx.Fatalf("failed to apply configuration file %s; err: %v", sidecarScopeConfig, err)
	}
	// Apply Egress Gateway for service namespace to handle external traffic
	if err := ctx.ApplyConfig(serviceNamespace.Name(), file.AsStringOrFail(ctx, simpleTLSGatewayConfig)); err != nil {
		ctx.Fatalf("failed to apply configuration file %s; err: %v", simpleTLSGatewayConfig, err)
	}

	if err := WaitUntilNotCallable(client, server); err != nil {
		t.Fatalf("failed to apply sidecar, %v", err)
	}

	return client, server, appsNamespace
}

func clusterName(target echo.Instance, port echo.Port) string {
	cfg := target.Config()
	return fmt.Sprintf("outbound|%d||%s.%s.svc.%s", port.ServicePort, cfg.Service, cfg.Namespace.Name(), cfg.Domain)
}

// Wait for the destination to NOT be callable by the client. This allows us to simulate external traffic.
// This essentially just waits for the Sidecar to be applied, without sleeping.
func WaitUntilNotCallable(c echo.Instance, dest echo.Instance) error {
	accept := func(cfg *envoyAdmin.ConfigDump) (bool, error) {
		validator := structpath.ForProto(cfg)
		for _, port := range dest.Config().Ports {
			clusterName := clusterName(dest, port)
			// Ensure that we have an outbound configuration for the target port.
			err := validator.NotExists("{.configs[*].dynamicActiveClusters[?(@.cluster.Name == '%s')]}", clusterName).Check()
			if err != nil {
				return false, err
			}
		}

		return true, nil
	}

	workloads, _ := c.Workloads()
	// Wait for the outbound config to be received by each workload from Pilot.
	for _, w := range workloads {
		if w.Sidecar() != nil {
			if err := w.Sidecar().WaitForConfig(accept, retry.Timeout(time.Second*10)); err != nil {
				return err
			}
		}
	}

	return nil
}
