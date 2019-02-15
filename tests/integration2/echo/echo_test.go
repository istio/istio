//  Copyright 2019 Istio Authors
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

package echo

import (
	"os"
	"testing"

	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/ids"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/api/components"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
)

// TODO(sven): Add additional testing of the echo component, this is just the basics.
func TestEcho(t *testing.T) {
	ctx := framework.GetContext(t)

	// Echo is only supported on native environment right now, skip if we can't load that.
	ctx.RequireOrSkip(t, lifecycle.Test, &descriptors.NativeEnvironment)

	reqA := component.NewDescriptor(ids.Echo, "a")
	reqA.Configuration = components.EchoConfig{Service: "a.echo", Version: "v1"}

	reqB := component.NewDescriptor(ids.Echo, "b")
	reqB.Configuration = components.EchoConfig{Service: "b.echo", Version: "v2"}
	reqB.Requires = append(reqB.Requires, reqA)

	ctx.RequireOrFail(t, lifecycle.Test, reqB)

	echoA := components.GetEcho(reqA, ctx, t)
	echoB := components.GetEcho(reqB, ctx, t)

	// Verify the configuration was set appropriately.
	if echoA.Config().Service != "a.echo" {
		t.Fatalf("expected 'a.echo' but echoA service was %s", echoA.Config().Service)
	}
	if echoB.Config().Service != "b.echo" {
		t.Fatalf("expected 'b.echo' but echoB service was %s", echoB.Config().Service)
	}

	be := echoB.EndpointsForProtocol(model.ProtocolHTTP)[0]
	result := echoA.CallOrFail(be, components.EchoCallOptions{}, t)[0]

	if !result.IsOK() {
		t.Fatalf("HTTP Request unsuccessful: %s", result.Body)
	}
}

// To opt-in to the test framework, implement a TestMain, and call test.Run.
func TestMain(m *testing.M) {
	rt, _ := framework.Run("echo_test", m)
	os.Exit(rt)
}
