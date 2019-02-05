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

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/components"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/ids"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
)

// TODO(sven): Add additional testing of the echo component, this is just the basics.
func TestEcho(t *testing.T) {
	ctx := framework.GetContext(t)

	// Echo is only supported on native environment right now, skip if we can't load that.
	ctx.RequireOrSkip(t, lifecycle.Test, &descriptors.NativeEnvironment)

	reqA := component.NewNamedRequirement("a", &ids.Echo)
	reqB := component.NewNamedRequirement("b", &ids.Echo)
	ctx.RequireOrFail(t, lifecycle.Test, reqA, reqB)

	echoA := components.GetEcho("a", ctx, t)
	echoB := components.GetEcho("b", ctx, t)

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
