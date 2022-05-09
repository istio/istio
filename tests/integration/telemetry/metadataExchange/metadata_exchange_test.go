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

package metadataExchange

import (
	"encoding/json"
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
		Run(func(t framework.TestContext) {
			resetAlsSinkServer(t)
			err := retry.UntilSuccess(func() error {
				if err := sendTraffic(clt, http.Header{}, false); err != nil {
					return err
				}

				if err := validateMetadataExchange(t, "httpMx"); err != nil {
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
		Run(func(t framework.TestContext) {
			resetAlsSinkServer(t)
			err := retry.UntilSuccess(func() error {
				if err := sendTraffic(clt, http.Header{}, true); err != nil {
					return err
				}

				if err := validateMetadataExchange(t, "tcpMx"); err != nil {
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

	err = ctx.ConfigIstio().File("istio-system", filepath.Join(env.IstioSrc, "samples/als-sink/sink.yaml")).Apply()
	if err != nil {
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
	//  All server instance have same names, so setting target as srv[0].
	// Sending the number of total request same as number of servers, so that load balancing gets a chance to send request to all the clusters.
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
	// an HTTP request with forced tracing
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

func resetAlsSinkServer(t framework.TestContext) {
	_, _, _ = t.Clusters().Default().PodExec(
		sinkPodName,
		"istio-system",
		"als-sink",
		"curl localhost:16000/reset")
}

func validateMetadataExchange(t framework.TestContext, expectedALSType string) error {
	t.Helper()

	// get metadata exchange mappings from als-sink admin server endpoint, /metadata
	// verify client and server reported each other's details in filterstate to als-sink
	out, _, err := t.Clusters().Default().PodExec(
		sinkPodName,
		"istio-system",
		"als-sink",
		"curl localhost:16000/metadata")
	if err != nil {
		t.Fatalf("couldn't curl als-sink: %v", err)
	}
	var metadataMappings map[string][]Metadata = make(map[string][]Metadata)
	json.Unmarshal([]byte(out), &metadataMappings)
	for nodeid, metadata := range metadataMappings {
		if expectedALSType != metadata[0].LogType {
			t.Fatalf("Expected: %s, Got: %s", expectedALSType, metadata[0].LogType)
		}

		t.Logf("node id: %v, metadata: %v", nodeid, metadata[0])
		if strings.Contains(nodeid, clt.WorkloadsOrFail(t)[0].PodName()) {
			if !strings.Contains(metadata[0].UpstreamPeer, srv.WorkloadsOrFail(t)[0].PodName()) {
				t.Fatalf("client did not receive server id as the upstream peer. Expected %s, Got: %s",
					srv.WorkloadsOrFail(t)[0].PodName(), metadata[0].UpstreamPeer)
			}
		}
		if strings.Contains(nodeid, srv.WorkloadsOrFail(t)[0].PodName()) {
			if !strings.Contains(metadata[0].DownstreamPeer, clt.WorkloadsOrFail(t)[0].PodName()) {
				t.Fatalf("server did not receive client id as the downstream peer. Expected: %s, Got: %s",
					clt.WorkloadsOrFail(t)[0].PodName(), metadata[0].DownstreamPeer)
			}
		}
	}
	return nil
}
