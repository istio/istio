// Copyright 2019 Istio Authors
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

package echo

import (
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/environment"
)

// TODO(sven): Add additional testing of the echo component, this is just the basics.
func TestEcho(t *testing.T) {
	ctx := framework.NewContext(t)

	// Echo is only supported on native environment right now, skip if we can't load that.
	ctx.RequireOrSkip(t, environment.Native)

	echoA := echo.NewOrFail(ctx, t, echo.Config{
		Service: "a.echo",
		Version: "v1",
	})
	echoB := echo.NewOrFail(ctx, t, echo.Config{
		Service: "b.echo",
		Version: "v2",
		Ports: model.PortList{
			{
				Name:     "http",
				Protocol: model.ProtocolHTTP,
			},
		}})

	// Verify the configuration was set appropriately.
	if echoA.Config().Service != "a.echo" {
		t.Fatalf("expected 'a.echo' but echoA service was %s", echoA.Config().Service)
	}
	if echoB.Config().Service != "b.echo" {
		t.Fatalf("expected 'b.echo' but echoB service was %s", echoB.Config().Service)
	}

	be := echoB.EndpointsForProtocol(model.ProtocolHTTP)[0]
	result := echoA.CallOrFail(be, echo.CallOptions{}, t)[0]

	if !result.IsOK() {
		t.Fatalf("HTTP Request unsuccessful: %s", result.Body)
	}
}

func TestMain(m *testing.M) {
	framework.Main("echo_test", m)
}
