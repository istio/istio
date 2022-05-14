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

package metadataexchange

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	kubeApiCore "k8s.io/api/core/v1"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/telemetry"
)

var (
	ist         istio.Instance
	echoNsInst  namespace.Instance
	srv         echo.Instance
	clt         echo.Instance
	sinkPodName string
)

type Metadata struct {
	LogType        string
	UpstreamPeer   string
	DownstreamPeer string
}

func TestHTTPMetadataExchangeBetweenSidecars(t *testing.T) {
	framework.NewTest(t).
		Features("observability.telemetry.tracing.server").
		Run(func(t framework.TestContext) {
			resetAlsSinkServer(t)
			err := retry.UntilSuccess(func() error {
				if err := sendTraffic(clt, http.Header{}, false); err != nil {
					return err
				}

				if err := validateMetadataExchangeAmongSidecars(t, "httpMx"); err != nil {
					return err
				}
				t.Logf("http metadata-exchange validated")

				return nil
			}, retry.Delay(framework.TelemetryRetryDelay), retry.Timeout(framework.TelemetryRetryTimeout))
			if err != nil {
				t.Fatalf("test failed: %v", err)
			}
		})
}

func TestTCPMetadataExchangeBetweenSidecars(t *testing.T) {
	framework.NewTest(t).
		Features("observability.telemetry.tracing.server").
		Run(func(t framework.TestContext) {
			resetAlsSinkServer(t)
			err := retry.UntilSuccess(func() error {
				if err := sendTraffic(clt, http.Header{}, true); err != nil {
					return err
				}

				if err := validateMetadataExchangeAmongSidecars(t, "tcpMx"); err != nil {
					return err
				}
				t.Logf("tcp metadata-exchange validated")

				return nil
			}, retry.Delay(framework.TelemetryRetryDelay), retry.Timeout(framework.TelemetryRetryTimeout))
			if err != nil {
				t.Fatalf("test failed: %v", err)
			}
		})
}

func TestTCPMetadataExchangeBetweenSidecarAndGateway(t *testing.T) {
	framework.NewTest(t).
		Features("observability.telemetry.metadataexchange").
		Run(func(t framework.TestContext) {
			resetAlsSinkServer(t)
			err := retry.UntilSuccess(func() error {
				requestURL := "curl --insecure -s -o /dev/null -w '%{http_code}' https://edition.cnn.com/politics"
				if err := sendTrafficFromSidecarToGateway(t, requestURL); err != nil {
					return err
				}

				if err := validateMxBetweenSidecarAndGateway(t, "tcpMx"); err != nil {
					return err
				}
				t.Logf("tcp metadata-exchange validated")

				return nil
			}, retry.Delay(framework.TelemetryRetryDelay), retry.Timeout(framework.TelemetryRetryTimeout))
			if err != nil {
				t.Fatalf("test failed: %v", err)
			}
		})
}

func TestHTTPMetadataExchangeBetweenSidecarAndGateway(t *testing.T) {
	framework.NewTest(t).
		Features("observability.telemetry.tracing.server").
		Run(func(t framework.TestContext) {
			resetAlsSinkServer(t)
			err := retry.UntilSuccess(func() error {
				requestURL := "curl -sSL -o /dev/null -w  '%{http_code}' http://edition.cnn.com/politics "
				if err := sendTrafficFromSidecarToGateway(t, requestURL); err != nil {
					return err
				}

				if err := validateMxBetweenSidecarAndGateway(t, "httpMx"); err != nil {
					return err
				}
				t.Logf("http metadata-exchange validated")

				return nil
			}, retry.Delay(framework.TelemetryRetryDelay), retry.Timeout(framework.TelemetryRetryTimeout))
			if err != nil {
				t.Fatalf("test failed: %v", err)
			}
		})
}

func TestMain(m *testing.M) {
	// nolint: staticcheck
	framework.
		NewSuite(m).
		Label(label.CustomSetup).
		Setup(istio.Setup(&ist, setupMeshConfig)).
		Setup(testSetup).
		Run()
}

func setupMeshConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}

	// All sidecars and gateways will send access logs along with upstream and downstream,
	// gathered through metadata exchange to this als-sink server
	cfg.ControlPlaneValues = `
meshConfig:
  enableEnvoyAccessLogService: true
  defaultConfig:
    envoyAccessLogService:
      address: als-sink.istio-system.svc:9001
`
}

func testSetup(ctx resource.Context) (err error) {
	echoNsInst, err = namespace.New(ctx, namespace.Config{
		Prefix: "istio-echo",
		Inject: true,
	})
	if err != nil {
		return
	}

	// Deploy als-sink server to cache metadata from mesh nodes
	if err = ctx.ConfigIstio().File("istio-system",
		filepath.Join(env.IstioSrc, "samples/als-sink/sink.yaml")).Apply(); err != nil {
		return
	}

	// Deploying client and server services, both within the mesh i,e with sidecars.
	// For testing sidecar to sidecar, traffic will be sent from `clt` instance to `srv` instance
	_, err = deployment.New(ctx).
		With(&clt, echo.Config{
			Service:        "clt",
			Namespace:      echoNsInst,
			ServiceAccount: true,
		}).
		With(&srv, echo.Config{
			Service:   "srv",
			Namespace: echoNsInst,
			Ports: []echo.Port{
				{
					Name:     "http",
					Protocol: protocol.HTTP,
					// We use a port > 1024 to not require root
					WorkloadPort: 8888,
				},
				{
					Name:     "tcp",
					Protocol: protocol.TCP,
					// We use a port > 1024 to not require root
					WorkloadPort: 9000,
				},
			},
			ServiceAccount: true,
		}).
		Build()
	if err != nil {
		return
	}

	// Following resources are being deployed to test sidecar->gateway communication. With following resources,
	// routing is being setup from sidecar to external site, edition.cnn.com, via egress gateway.
	// clt(https:443) -> sidecar(tls:443) -> istio-mtls -> (TLS:443)egress-gateway(tcp:443) -> cnn.com
	// clt(http:80) -> sidecar(http:80) -> istio-mtls -> (HTTPS:80)egress-gateway(http:80) -> cnn.com
	if err = ctx.ConfigIstio().File(echoNsInst.Name(),
		filepath.Join(env.IstioSrc,
			"tests/integration/telemetry/metadataexchange/testdata/cnn-service-entry.yaml")).Apply(); err != nil {
		return
	}
	if err = ctx.ConfigIstio().File(echoNsInst.Name(),
		filepath.Join(env.IstioSrc,
			"tests/integration/telemetry/metadataexchange/testdata/istio-mtls-dest-rule.yaml")).Apply(); err != nil {
		return
	}
	if err = ctx.ConfigIstio().File(echoNsInst.Name(),
		filepath.Join(env.IstioSrc,
			"tests/integration/telemetry/metadataexchange/testdata/istio-mtls-gateway.yaml")).Apply(); err != nil {
		return
	}
	if err = ctx.ConfigIstio().File(echoNsInst.Name(),
		filepath.Join(env.IstioSrc,
			"tests/integration/telemetry/metadataexchange/testdata/istio-mtls-vs.yaml")).Apply(); err != nil {
		return
	}

	// Wait for als-sink service to be up.
	fetchFn := kube.NewPodFetch(ctx.Clusters().Default(), "istio-system", "app=als-sink")
	var sinkPods []kubeApiCore.Pod
	if sinkPods, err = kube.WaitUntilPodsAreReady(fetchFn); err != nil {
		return
	}
	sinkPodName = sinkPods[0].Name
	return nil
}

// send both a grpc and http requests (http with forced tracing).
func sendTraffic(cltInstance echo.Instance, headers http.Header, onlyTCP bool) error {
	callCount := telemetry.RequestCountMultipler * srv.MustWorkloads().Len()
	if onlyTCP {
		_, err := cltInstance.Call(echo.CallOptions{
			To: srv,
			Port: echo.Port{
				Name: "tcp",
			},
			Count: callCount,
			Retry: echo.Retry{
				NoRetry: true,
			},
		})
		return err
	}
	httpOpts := echo.CallOptions{
		To: srv,
		Port: echo.Port{
			Name: "http",
		},
		HTTP: echo.HTTP{
			Headers: headers,
		},
		Count: callCount,
		Retry: echo.Retry{
			NoRetry: true,
		},
	}
	if _, err := cltInstance.Call(httpOpts); err != nil {
		return err
	}
	return nil
}

func sendTrafficFromSidecarToGateway(t framework.TestContext, testRequestCmd string) error {
	clientPodName := clt.WorkloadsOrFail(t)[0].PodName()
	out, _, err := t.Clusters().Default().PodExec(
		clientPodName,
		echoNsInst.Name(),
		"app",
		testRequestCmd)
	if err != nil {
		return fmt.Errorf("failed to execute command in %s pod: %v: %s", clientPodName, err, out)
	}
	if strings.Contains(out, "200") {
		return nil
	}
	return fmt.Errorf("test request failed because of unexpected response code: %s", out)
}

func resetAlsSinkServer(t framework.TestContext) {
	_, _, _ = t.Clusters().Default().PodExec(
		sinkPodName,
		"istio-system",
		"als-sink",
		"curl localhost:16000/reset")
}

func validateMetadataExchangeAmongSidecars(t framework.TestContext, expectedALSType string) error {
	// get metadata exchange mappings from als-sink admin server endpoint, /metadata
	// Following is the example output:
	// {
	// 	"httpMx-client": {
	// 		"LogType": "httpMx",
	// nolint: lll
	// 		"UpstreamPeer": "[type.googleapis.com/google.protobuf.BytesValue]:{value:\"sidecar~10.244.0.124~srv-v1-668779db5f-7cjsb.istio-echo-1-28132~istio-echo-1-28132.svc.cluster.local\"}",
	// 		"DownstreamPeer": "\u003cnil\u003e"
	// 	  },
	// 	  "httpMx-server": {
	// 		"LogType": "httpMx",
	// 		"UpstreamPeer": "\u003cnil\u003e",
	// nolint: lll
	// 		"DownstreamPeer": "[type.googleapis.com/google.protobuf.BytesValue]:{value:\"sidecar~10.244.0.125~clt-v1-5c579976bf-jxtlm.istio-echo-1-28132~istio-echo-1-28132.svc.cluster.local\"}"
	// 	  }
	// }
	out, _, err := t.Clusters().Default().PodExec(
		sinkPodName,
		"istio-system",
		"als-sink",
		"curl localhost:16000/metadata")
	if err != nil {
		return fmt.Errorf("couldn't curl als-sink: %v", err)
	}
	metadataMappings := map[string]Metadata{}
	json.Unmarshal([]byte(out), &metadataMappings)

	var metadata Metadata
	var ok bool
	if metadata, ok = metadataMappings[expectedALSType+"-"+"server"]; !ok {
		return fmt.Errorf("als-sink has not received access logs from server")
	}
	if metadata.LogType != expectedALSType {
		return fmt.Errorf("Access logs are not from the expected traffic type. expected: %s got: %s",
			expectedALSType, metadata.LogType)
	}
	if !strings.Contains(metadata.DownstreamPeer, clt.WorkloadsOrFail(t)[0].PodName()) {
		return fmt.Errorf("server did not receive client id as the downstream peer. Expected: %s, Got: %s",
			clt.WorkloadsOrFail(t)[0].PodName(), metadata.DownstreamPeer)
	}

	if metadata, ok := metadataMappings[expectedALSType+"-"+"client"]; !ok {
		return fmt.Errorf("als-sink has not received access logs from client")
	} else if metadata.LogType != expectedALSType {
		return fmt.Errorf("Access logs are not from the expected traffic type. expected: %s got: %s",
			expectedALSType, metadata.LogType)
	}

	return nil
}

func validateMxBetweenSidecarAndGateway(t framework.TestContext, expectedALSType string) error {
	// get metadata exchange mappings from als-sink admin server endpoint, /metadata
	// verify client and server reported each other's details in filterstate to als-sink
	out, _, err := t.Clusters().Default().PodExec(
		sinkPodName,
		"istio-system",
		"als-sink",
		"curl localhost:16000/metadata")
	if err != nil {
		return fmt.Errorf("couldn't curl als-sink: %v", err)
	}
	metadataMappings := map[string]Metadata{}
	json.Unmarshal([]byte(out), &metadataMappings)
	var metadata Metadata
	var ok bool
	if metadata, ok = metadataMappings[expectedALSType+"-"+"server"]; !ok {
		return fmt.Errorf("als-sink has not received access logs from server")
	}
	if metadata.LogType != expectedALSType {
		return fmt.Errorf("Access logs are not from the expected traffic type. expected: %s got: %s",
			expectedALSType, metadata.LogType)
	}
	if !strings.Contains(metadata.DownstreamPeer, clt.WorkloadsOrFail(t)[0].PodName()) {
		return fmt.Errorf("server did not receive client id as the downstream peer. Expected: %s, Got: %s",
			clt.WorkloadsOrFail(t)[0].PodName(), metadata.DownstreamPeer)
	}

	if metadata, ok := metadataMappings[expectedALSType+"-"+"client"]; !ok {
		return fmt.Errorf("als-sink has not received access logs from client")
	} else if metadata.LogType != expectedALSType {
		return fmt.Errorf("Access logs are not from the expected traffic type. expected: %s got: %s",
			expectedALSType, metadata.LogType)
	}
	return nil
}
