//go:build integ
// +build integ

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

package pilot

import (
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

func TestGRPCProbe(t *testing.T) {
	framework.NewTest(t).
		Features("usability.observability.grpc-probe").
		Run(func(t framework.TestContext) {
			if !t.Clusters().Default().MinKubeVersion(23) {
				t.Skip("gRPC probe not supported")
			}

			ns := namespace.NewOrFail(t, t, namespace.Config{Prefix: "grpc-probe", Inject: true})
			// apply strict mtls
			t.ConfigKube().YAML(ns.Name(), `
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: grpc-probe-mtls
spec:
  mtls:
    mode: STRICT`).ApplyOrFail(t)

			for _, testCase := range []struct {
				name     string
				rewrite  bool
				ready    bool
				openPort bool
			}{
				{name: "rewrite-ready", rewrite: true, ready: true, openPort: true},
			} {
				t.NewSubTest(testCase.name).Run(func(t framework.TestContext) {
					runGRPCProbeDeployment(t, ns, testCase.name, testCase.rewrite, testCase.ready, testCase.openPort)
				})
			}
		})
}

func runGRPCProbeDeployment(ctx framework.TestContext, ns namespace.Instance, //nolint:interfacer
	name string, rewrite bool, wantReady bool, openPort bool,
) {
	ctx.Helper()

	var grpcProbe echo.Instance
	cfg := echo.Config{
		Namespace:         ns,
		Service:           name,
		ReadinessGRPCPort: "1234",
		Subsets: []echo.SubsetConfig{
			{
				Annotations: echo.NewAnnotations().SetBool(echo.SidecarRewriteAppHTTPProbers, rewrite),
			},
		},
	}

	if openPort {
		cfg.Ports = []echo.Port{{
			Name:         "readiness-grpc-port",
			Protocol:     protocol.GRPC,
			ServicePort:  1234,
			WorkloadPort: 1234,
		}}
	}

	// Negative test, we expect the grpc readiness check fails, so set a timeout duration.
	if !wantReady {
		cfg.ReadinessTimeout = time.Second * 15
	}
	_, err := deployment.New(ctx).
		With(&grpcProbe, cfg).
		Build()
	gotReady := err == nil
	if gotReady != wantReady {
		ctx.Errorf("grpcProbe app %v, got error %v, want ready = %v", name, err, wantReady)
	}
}
