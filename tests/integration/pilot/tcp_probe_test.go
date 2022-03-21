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

func TestTcpProbe(t *testing.T) {
	framework.NewTest(t).
		Features("usability.observability.tcp-probe").
		Run(func(t framework.TestContext) {
			ns := namespace.NewOrFail(t, t, namespace.Config{Prefix: "tcp-probe", Inject: true})
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
		})
}

func runTCPProbeDeployment(ctx framework.TestContext, ns namespace.Instance, //nolint:interfacer
	name string, rewrite bool, wantSuccess bool, openPort bool) {
	ctx.Helper()

	var tcpProbe echo.Instance
	cfg := echo.Config{
		Namespace:        ns,
		Service:          name,
		ReadinessTCPPort: "1234",
		Subsets: []echo.SubsetConfig{
			{
				Annotations: echo.NewAnnotations().SetBool(echo.SidecarRewriteAppHTTPProbers, rewrite),
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

	// Negative test, we expect the tcp readiness check fails, so set a timeout duration.
	if !wantSuccess {
		cfg.ReadinessTimeout = time.Second * 15
	}
	_, err := deployment.New(ctx).
		With(&tcpProbe, cfg).
		Build()
	gotSuccess := err == nil
	if gotSuccess != wantSuccess {
		ctx.Errorf("tcpProbe app %v, got error %v, want success = %v", name, err, wantSuccess)
	}
}
