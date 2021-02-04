// +build integ
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
	"fmt"
	"time"

	"istio.io/istio/pkg/test"
	echoclient "istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/yml"
)

// callsPerCluster is used to ensure cross-cluster load balancing has a chance to work
const callsPerCluster = 5

// Slow down retries to allow for delayed_close_timeout. Also require 3 successive successes.
var retryOptions = []retry.Option{retry.Delay(1000 * time.Millisecond), retry.Converge(3)}

type TrafficCall struct {
	name string
	call func(t test.Failer, options echo.CallOptions, retryOptions ...retry.Option) echoclient.ParsedResponses
	opts echo.CallOptions
}

type TrafficTestCase struct {
	name   string
	config string

	// Multiple calls. Cannot be used with call/opts
	children []TrafficCall

	// Single call. Cannot be used with children.
	call func(t test.Failer, options echo.CallOptions, retryOptions ...retry.Option) echoclient.ParsedResponses
	opts echo.CallOptions

	// setting cases to skipped is better than not adding them - gives visibility to what needs to be fixed
	skip bool
}

func (c TrafficTestCase) Run(ctx framework.TestContext, namespace string) {
	job := func(ctx framework.TestContext) {
		if c.skip {
			ctx.SkipNow()
		}
		if len(c.config) > 0 {
			cfg := yml.MustApplyNamespace(ctx, c.config, namespace)
			ctx.Config().ApplyYAMLOrFail(ctx, "", cfg)
		}

		if c.call != nil && len(c.children) > 0 {
			ctx.Fatal("TrafficTestCase: must not specify both call and children")
		}

		if c.call != nil {
			// Call the function with a few custom retry options.
			c.call(ctx, c.opts, retryOptions...)
		}

		for _, child := range c.children {
			ctx.NewSubTest(child.name).Run(func(ctx framework.TestContext) {
				child.call(ctx, child.opts, retryOptions...)
			})
		}
	}
	if c.name != "" {
		ctx.NewSubTest(c.name).Run(job)
	} else {
		job(ctx)
	}
}

func RunAllTrafficTests(ctx framework.TestContext, apps *EchoDeployments) {
	cases := map[string][]TrafficTestCase{}
	cases["virtualservice"] = virtualServiceCases(apps)
	cases["sniffing"] = protocolSniffingCases(apps)
	cases["selfcall"] = selfCallsCases(apps)
	cases["serverfirst"] = serverFirstTestCases(apps)
	cases["gateway"] = gatewayCases(apps)
	cases["loop"] = trafficLoopCases(apps)
	cases["tls-origination"] = tlsOriginationCases(apps)
	cases["instanceip"] = instanceIPTests(apps)
	cases["services"] = serviceCases(apps)
	if !ctx.Settings().SkipVM {
		cases["vm"] = VMTestCases(apps.VM, apps)
	}
	cases["dns"] = DNSTestCases(apps)
	for name, tts := range cases {
		ctx.NewSubTest(name).Run(func(ctx framework.TestContext) {
			for _, tt := range tts {
				tt.Run(ctx, apps.Namespace.Name())
			}
		})
	}
}

func ExpectString(got, expected, help string) error {
	if got != expected {
		return fmt.Errorf("got unexpected %v: got %q, wanted %q", help, got, expected)
	}
	return nil
}

func AlmostEquals(a, b, precision int) bool {
	upper := a + precision
	lower := a - precision
	if b < lower || b > upper {
		return false
	}
	return true
}
