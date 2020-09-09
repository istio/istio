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

	echoclient "istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/util/retry"
)

// callsPerCluster is used to ensure cross-cluster load balancing has a chance to work
const callsPerCluster = 10

type TrafficTestCase struct {
	name   string
	config string

	// Multiple calls. Cannot be used with call/validator
	calls []TrafficCall

	// Single call
	call      func() (echoclient.ParsedResponses, error)
	validator func(echoclient.ParsedResponses) error

	// if enabled, we will assert the request fails, rather than the request succeeds
	expectFailure bool
}

type TrafficCall struct {
	call          func() (echoclient.ParsedResponses, error)
	validator     func(echoclient.ParsedResponses) error
	expectFailure bool
}

func ExecuteTrafficTest(ctx framework.TestContext, tt TrafficTestCase, namespace string) {
	ctx.NewSubTest(tt.name).Run(func(ctx framework.TestContext) {
		if len(tt.config) > 0 {
			ctx.Config().ApplyYAMLOrFail(ctx, namespace, tt.config)
			ctx.WhenDone(func() error {
				return ctx.Config().DeleteYAML(namespace, tt.config)
			})
		}
		if tt.call != nil {
			if tt.calls != nil {
				ctx.Fatalf("defined calls and calls; may only define on or the other")
			}
			tt.calls = []TrafficCall{{tt.call, tt.validator, tt.expectFailure}}
		}
		for i, c := range tt.calls {
			name := fmt.Sprintf("%s/%d", tt.name, i)
			retry.UntilSuccessOrFail(ctx, func() error {
				r, err := c.call()
				if !c.expectFailure && err != nil {
					ctx.Logf("call for %v failed, retrying: %v", name, err)
					return err
				} else if c.expectFailure && err == nil {
					e := fmt.Errorf("call for %v did not fail, retrying", name)
					ctx.Log(e)
					return e
				}

				err = c.validator(r)
				if !c.expectFailure && err != nil {
					ctx.Logf("validation for call for %v failed, retrying: %v", name, err)
					return err
				} else if c.expectFailure && err == nil {
					e := fmt.Errorf("validation for %v did not fail, retrying", name)
					ctx.Log(e)
					return e
				}
				return nil
			}, retry.Delay(time.Millisecond*100), retry.Timeout(time.Second*10), retry.Converge(3))
		}
	})
}

func RunTrafficTest(ctx framework.TestContext, apps *EchoDeployments) {
	cases := map[string][]TrafficTestCase{}
	cases["virtualservice"] = virtualServiceCases(apps)
	cases["sniffing"] = protocolSniffingCases(apps)
	cases["serverfirst"] = serverFirstTestCases(apps)
	cases["vm"] = VMTestCases(apps.VM, apps)
	for n, tts := range cases {
		ctx.NewSubTest(n).Run(func(ctx framework.TestContext) {
			for _, tt := range tts {
				ExecuteTrafficTest(ctx, tt, apps.Namespace.Name())
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
