//go:build integ
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

	"istio.io/istio/pkg/test"
	echoclient "istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/pkg/test/util/yml"
)

// callsPerCluster is used to ensure cross-cluster load balancing has a chance to work
const callsPerCluster = 5

// Require 3 successive successes. Delay can be configured with istio.test.echo.callDelay
var retryOptions = []retry.Option{retry.Converge(3)}

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
	setupOpts func(src echo.Caller, dest echo.Instances, opts *echo.CallOptions)
	// validate is used to build validators dynamically when using RunForApps based on the active/src dest pair
	validate     func(src echo.Caller, dst echo.Instances, opts *echo.CallOptions) echo.Validator
	validateForN func(src echo.Caller, dst echo.Services, opts *echo.CallOptions) echo.Validator

	// setting cases to skipped is better than not adding them - gives visibility to what needs to be fixed
	skip bool

	// workloadAgnostic is a temporary setting to trigger using RunForApps
	// TODO remove this and force everything to be workoad agnostic
	workloadAgnostic bool

	// toN causes the test to be run for N destinations. The call will be made from instances of the first deployment
	// in each subset in each cluster. See echotes.T's RunToN for more details.
	toN int
	// viaIngress makes the ingress gateway the caller for tests
	viaIngress bool
	// sourceFilters allows adding additional filtering for workload agnostic cases to test using fewer clients
	sourceFilters []echotest.Filter
	// targetFilters allows adding additional filtering for workload agnostic cases to test using fewer targets
	targetFilters []echotest.Filter
	// comboFilters allows conditionally filtering based on pairs of apps
	comboFilters []echotest.CombinationFilter
	// vars given to the config template
	templateVars func(src echo.Callers, dest echo.Instances) map[string]interface{}

	// minIstioVersion allows conditionally skipping based on required version
	minIstioVersion string
}

var noProxyless = echotest.Not(echotest.FilterMatch(echo.IsProxylessGRPC()))

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
			SetupForServicePair(func(t framework.TestContext, src echo.Callers, dsts echo.Services) error {
				tmplData := map[string]interface{}{
					// tests that use simple Run only need the first
					"dst":    dsts[0],
					"dstSvc": dsts[0][0].Config().Service,
					// tests that use RunForN need all destination deployments
					"dsts":    dsts,
					"dstSvcs": dsts.Services(),
				}
				if len(src) > 0 {
					tmplData["src"] = src
					if src, ok := src[0].(echo.Instance); ok {
						tmplData["srcSvc"] = src.Config().Service
					}
				}
				if c.templateVars != nil {
					for k, v := range c.templateVars(src, dsts[0]) {
						tmplData[k] = v
					}
				}
				cfg := yml.MustApplyNamespace(t, tmpl.MustEvaluate(c.config, tmplData), namespace)
				// we only apply to config clusters
				return t.Config().ApplyYAML("", cfg)
			}).
			WithDefaultFilters().
			From(c.sourceFilters...).
			// TODO mainly testing proxyless features as a client for now
			To(append(c.targetFilters, noProxyless)...).
			ConditionallyTo(c.comboFilters...)

		doTest := func(t framework.TestContext, src echo.Caller, dsts echo.Services) {
			if c.skip {
				t.SkipNow()
			}
			if c.minIstioVersion != "" {
				skipMV := !t.Settings().Revisions.AtLeast(resource.IstioVersion(c.minIstioVersion))
				if skipMV {
					t.SkipNow()
				}
			}
			buildOpts := func(options echo.CallOptions) echo.CallOptions {
				opts := options
				opts.Target = dsts[0][0]
				if c.validate != nil {
					opts.Validator = c.validate(src, dsts[0], &opts)
				}
				if c.validateForN != nil {
					opts.Validator = c.validateForN(src, dsts, &opts)
				}
				if opts.Count == 0 {
					opts.Count = callsPerCluster * len(dsts) * len(dsts[0])
				}
				if c.setupOpts != nil {
					c.setupOpts(src, dsts[0], &opts)
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
		} else if c.viaIngress {
			echoT.RunViaIngress(func(t framework.TestContext, src ingress.Instance, dst echo.Instances) {
				doTest(t, src, echo.Services{dst})
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
		if c.minIstioVersion != "" {
			skipMV := !t.Settings().Revisions.AtLeast(resource.IstioVersion(c.minIstioVersion))
			if skipMV {
				t.SkipNow()
			}
		}
		if len(c.config) > 0 {
			cfg := yml.MustApplyNamespace(t, c.config, namespace)
			// we only apply to config clusters
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

func RunAllTrafficTests(t framework.TestContext, i istio.Instance, apps *EchoDeployments) {
	cases := map[string][]TrafficTestCase{}
	cases["jwt-claim-route"] = jwtClaimRoute(apps)
	cases["virtualservice"] = virtualServiceCases(t.Settings().SkipVM)
	cases["sniffing"] = protocolSniffingCases()
	cases["selfcall"] = selfCallsCases()
	cases["serverfirst"] = serverFirstTestCases(apps)
	cases["gateway"] = gatewayCases()
	cases["autopassthrough"] = autoPassthroughCases(apps)
	cases["loop"] = trafficLoopCases(apps)
	cases["tls-origination"] = tlsOriginationCases(apps)
	cases["instanceip"] = instanceIPTests(apps)
	cases["services"] = serviceCases(apps)
	if h, err := hostCases(apps); err != nil {
		t.Fatal("failed to setup host cases: %v", err)
	} else {
		cases["host"] = h
	}
	cases["envoyfilter"] = envoyFilterCases(apps)
	if len(t.Clusters().ByNetwork()) == 1 {
		// Consistent hashing does not work for multinetwork. The first request will consistently go to a
		// gateway, but that gateway will tcp_proxy it to a random pod.
		cases["consistent-hash"] = consistentHashCases(apps)
	}
	cases["use-client-protocol"] = useClientProtocolCases(apps)
	cases["destinationrule"] = destinationRuleCases(apps)
	if !t.Settings().SkipVM {
		cases["vm"] = VMTestCases(apps.VM, apps)
	}
	cases["dns"] = DNSTestCases(apps, i.Settings().EnableCNI)
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
