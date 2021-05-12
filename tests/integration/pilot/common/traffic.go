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
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
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
	name string
	// config can optionally be templated using the params src, dst (each are []echo.Instance)
	config string

	// Multiple calls. Cannot be used with call/opts
	children []TrafficCall

	// Single call. Cannot be used with children or workloadAgnostic tests.
	call func(t test.Failer, options echo.CallOptions, retryOptions ...retry.Option) echoclient.ParsedResponses
	// opts specifies the echo call options. When using RunForApps, the Target will be set dynamically.
	opts echo.CallOptions
	// setupOpts allows modifying options based on sources/destinations
	setupOpts func(src echo.Instance, dest echo.Services, opts *echo.CallOptions)
	// validate is used to build validators dynamically when using RunForApps based on the active/src dest pair
	validate func(src echo.Instance, dst echo.Services) echo.Validator

	// setting cases to skipped is better than not adding them - gives visibility to what needs to be fixed
	skip bool

	// workloadAgnostic is a temporary setting to trigger using RunForApps
	// TODO remove this and force everything to be workoad agnostic
	workloadAgnostic bool

	// toN causes the test to be run for N destinations. The call will be made from instances of the first deployment
	// in each subset in each cluster. See echotes.T's RunToN for more details.
	toN int
	// sourceFilters allows adding additional filtering for workload agnostic cases to test using fewer clients
	sourceFilters []echotest.Filter
	// targetFilters allows adding additional filtering for workload agnostic cases to test using fewer targets
	targetFilters []echotest.Filter
	// comboFilters allows conditionally filtering based on pairs of apps
	comboFilters []echotest.CombinationFilter
	// vars given to the config template
	templateVars map[string]interface{}
}

func (c TrafficTestCase) RunForApps(t framework.TestContext, apps echo.Instances, namespace string) {
	if c.opts.Target != nil {
		t.Fatal("TrafficTestCase.RunForApps: opts.Target must not be specified")
	}
	if c.call != nil {
		t.Fatal("TrafficTestCase.RunForApps: call must not be specified")
	}
	// just check if any of the required fields are set
	optsSpecified := c.opts.Port != nil || c.opts.PortName != "" || c.opts.Scheme != ""
	if optsSpecified && len(c.children) > 0 {
		t.Fatal("TrafficTestCase: must not specify both opts and children")
	}

	job := func(t framework.TestContext) {
		echoT := echotest.New(t, apps).
			SetupForServicePair(func(t framework.TestContext, src echo.Instances, dsts echo.Services) error {
				tmplData := map[string]interface{}{
					"src":    src,
					"srcSvc": src[0].Config().Service,
					// tests that use simple Run only need the first
					"dst":    dsts[0],
					"dstSvc": dsts[0][0].Config().Service,
					// tests that use RunForN need all destination deployments
					"dsts":    dsts,
					"dstSvcs": dsts.Services(),
				}
				for k, v := range c.templateVars {
					tmplData[k] = v
				}
				cfg := yml.MustApplyNamespace(t, tmpl.MustEvaluate(c.config, tmplData), namespace)
				return t.Config().ApplyYAML("", cfg)
			}).
			WithDefaultFilters().
			From(c.sourceFilters...).
			To(c.targetFilters...).
			ConditionallyTo(c.comboFilters...)

		doTest := func(t framework.TestContext, src echo.Instance, dsts echo.Services) {
			if c.skip {
				t.SkipNow()
			}
			buildOpts := func(options echo.CallOptions) echo.CallOptions {
				opts := options
				opts.Target = dsts[0][0]
				if c.validate != nil {
					opts.Validator = c.validate(src, dsts)
				}
				if opts.Count == 0 {
					opts.Count = callsPerCluster * len(dsts) * len(dsts[0])
				}
				if c.setupOpts != nil {
					c.setupOpts(src, dsts, &opts)
				}
				return opts
			}
			if optsSpecified {
				src.CallWithRetryOrFail(t, buildOpts(c.opts), retryOptions...)
			}
			for _, child := range c.children {
				t.NewSubTest(child.name).Run(func(t framework.TestContext) {
					src.CallWithRetryOrFail(t, buildOpts(child.opts), retryOptions...)
				})
			}
		}

		if c.toN > 0 {
			echoT.RunToN(c.toN, func(t framework.TestContext, src echo.Instance, dsts echo.Services) {
				doTest(t, src, dsts)
			})
		} else {
			echoT.Run(func(t framework.TestContext, src echo.Instance, dst echo.Instances) {
				doTest(t, src, echo.Services{dst})
			})
		}
	}

	if c.name != "" {
		t.NewSubTest(c.name).Run(job)
	} else {
		job(t)
	}
}

func (c TrafficTestCase) Run(t framework.TestContext, namespace string) {
	job := func(t framework.TestContext) {
		if c.skip {
			t.SkipNow()
		}
		if len(c.config) > 0 {
			cfg := yml.MustApplyNamespace(t, c.config, namespace)
			t.Config().ApplyYAMLOrFail(t, "", cfg)
		}

		if c.call != nil && len(c.children) > 0 {
			t.Fatal("TrafficTestCase: must not specify both call and children")
		}

		if c.call != nil {
			// Call the function with a few custom retry options.
			c.call(t, c.opts, retryOptions...)
		}

		for _, child := range c.children {
			t.NewSubTest(child.name).Run(func(t framework.TestContext) {
				child.call(t, child.opts, retryOptions...)
			})
		}
	}
	if c.name != "" {
		t.NewSubTest(c.name).Run(job)
	} else {
		job(t)
	}
}

func RunAllTrafficTests(t framework.TestContext, apps *EchoDeployments) {
	cases := map[string][]TrafficTestCase{}
	cases["virtualservice"] = virtualServiceCases(t.Settings().SkipVM)
	cases["sniffing"] = protocolSniffingCases()
	cases["selfcall"] = selfCallsCases()
	cases["serverfirst"] = serverFirstTestCases(apps)
	cases["gateway"] = gatewayCases(apps)
	cases["autopassthrough"] = autoPassthroughCases(apps)
	cases["loop"] = trafficLoopCases(apps)
	cases["tls-origination"] = tlsOriginationCases(apps)
	cases["instanceip"] = instanceIPTests(apps)
	cases["services"] = serviceCases(apps)
	if !t.Settings().SkipVM {
		cases["vm"] = VMTestCases(apps.VM, apps)
	}
	cases["dns"] = DNSTestCases(apps)
	for name, tts := range cases {
		t.NewSubTest(name).Run(func(t framework.TestContext) {
			for _, tt := range tts {
				if tt.workloadAgnostic {
					tt.RunForApps(t, apps.All, apps.Namespace.Name())
				} else {
					tt.Run(t, apps.Namespace.Name())
				}
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
