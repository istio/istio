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

package security

import (
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

func TestTcpHealthCheck(t *testing.T) {
	framework.NewTest(t).
		Features("tcp.healthcheck").
		Run(func(t framework.TestContext) {
			ns := namespace.NewOrFail(t, t, namespace.Config{Prefix: "healthcheck", Inject: true})
			for _, testCase := range []struct {
				name    string
				rewrite bool
				success bool
			}{
				{name: "norewrite-success", rewrite: false, success: true},
				{name: "rewrite-fail", rewrite: true, success: false},
			} {
				t.NewSubTest(testCase.name).Run(func(t framework.TestContext) {
					runHealthCheckDeployment(t, ns, testCase.name, testCase.rewrite, testCase.success)
				})
			}
		})
}

func runHealthCheckDeployment(ctx framework.TestContext, ns namespace.Instance, //nolint:interfacer
	name string, rewrite bool, wantSuccess bool) {
	ctx.Helper()

	var healthcheck echo.Instance
	cfg := echo.Config{ //FIXME: liveness probe of app must use tcpSocket and use a port that the app does not open
		Namespace: ns,
		Service:   name,
		Ports: []echo.Port{{
			Name:         "http-8080",
			Protocol:     protocol.HTTP,
			ServicePort:  8080,
			InstancePort: 8080,
		}},
		Subsets: []echo.SubsetConfig{
			{
				Annotations: echo.NewAnnotations().SetBool(echo.SidecarRewriteAppHTTPProbers, rewrite),
			},
		},
	}
	// Negative test, we expect the health check fails, so set a timeout duration.
	if !wantSuccess {
		cfg.ReadinessTimeout = time.Second * 15
	}
	_, err := echoboot.NewBuilder(ctx).
		With(&healthcheck, cfg).
		Build()
	gotSuccess := err == nil
	if gotSuccess != wantSuccess {
		ctx.Errorf("health check app %v, got error %v, want success = %v", name, err, wantSuccess)
	}
}
