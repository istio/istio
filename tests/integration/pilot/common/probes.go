//go:build integ

// Copyright Istio Authors
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

package common

import (
	"strconv"
	"time"

	"istio.io/api/annotation"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	echodeployment "istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

// RunTcpProbeTests verifies TCP readiness probe behavior with and without
// sidecar.istio.io/rewriteAppHTTPProbers, which rewrites probes via iptables/nftables REDIRECT.
func RunTcpProbeTests(t framework.TestContext) {
	t.Helper()
	ns := namespace.NewOrFail(t, namespace.Config{Prefix: "tcp-probe", Inject: true})
	for _, testCase := range []struct {
		name     string
		rewrite  bool
		success  bool
		openPort bool
	}{
		{name: "norewrite-success", rewrite: false, success: true, openPort: false},
		{name: "rewrite-success", rewrite: true, success: true, openPort: true},
	} {
		t.NewSubTest(testCase.name).Run(func(t framework.TestContext) {
			runTCPProbeDeployment(t, ns, testCase.name, testCase.rewrite, testCase.success, testCase.openPort)
		})
	}
}

func runTCPProbeDeployment(ctx framework.TestContext, ns namespace.Instance, //nolint:interfacer
	name string, rewrite bool, wantSuccess bool, openPort bool,
) {
	ctx.Helper()

	var tcpProbe echo.Instance
	cfg := echo.Config{
		Namespace:        ns,
		Service:          name,
		ReadinessTCPPort: "1234",
		Subsets: []echo.SubsetConfig{
			{
				Annotations: map[string]string{annotation.SidecarRewriteAppHTTPProbers.Name: strconv.FormatBool(rewrite)},
			},
		},
	}

	if openPort {
		cfg.Ports = []echo.Port{{
			Name:         "readiness-tcp-port",
			Protocol:     protocol.TCP,
			ServicePort:  1234,
			WorkloadPort: 1234,
		}}
	}

	if !wantSuccess {
		cfg.ReadinessTimeout = time.Second * 15
	}
	_, err := echodeployment.New(ctx).
		With(&tcpProbe, cfg).
		Build()
	gotSuccess := err == nil
	if gotSuccess != wantSuccess {
		ctx.Errorf("tcpProbe app %v, got error %v, want success = %v", name, err, wantSuccess)
	}
}

// RunGRPCProbeTests verifies gRPC readiness probe behavior under STRICT mTLS with
// sidecar.istio.io/rewriteAppHTTPProbers, which rewrites probes via iptables/nftables REDIRECT.
func RunGRPCProbeTests(t framework.TestContext) {
	t.Helper()
	if !t.Clusters().Default().MinKubeVersion(23) {
		t.Skip("gRPC probe not supported")
	}

	ns := namespace.NewOrFail(t, namespace.Config{Prefix: "grpc-probe", Inject: true})
	t.ConfigKube(t.Clusters().Configs()...).YAML(ns.Name(), `
apiVersion: security.istio.io/v1
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
				Annotations: map[string]string{annotation.SidecarRewriteAppHTTPProbers.Name: strconv.FormatBool(rewrite)},
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

	if !wantReady {
		cfg.ReadinessTimeout = time.Second * 15
	}
	_, err := echodeployment.New(ctx).
		With(&grpcProbe, cfg).
		Build()
	gotReady := err == nil
	if gotReady != wantReady {
		ctx.Errorf("grpcProbe app %v, got error %v, want ready = %v", name, err, wantReady)
	}
}
