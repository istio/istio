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
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/pkg/test/util/yml"
)

type TrafficCall struct {
	name string
	call func(t test.Failer, options echo.CallOptions) echo.CallResult
	opts echo.CallOptions
}

type skip struct {
	skip   bool
	reason string
}

type TrafficTestCase struct {
	name string
	// config can optionally be templated using the params src, dst (each are []echo.Instance)
	config string

	// Multiple calls. Cannot be used with call/opts
	children []TrafficCall

	// Single call. Cannot be used with children or workloadAgnostic tests.
	call func(t test.Failer, options echo.CallOptions) echo.CallResult
	// opts specifies the echo call options. When using RunForApps, the To will be set dynamically.
	opts echo.CallOptions
	// setupOpts allows modifying options based on sources/destinations
	setupOpts func(src echo.Caller, opts *echo.CallOptions)
	// check is used to build validators dynamically when using RunForApps based on the active/src dest pair
	check     func(src echo.Caller, opts *echo.CallOptions) echo.Checker
	checkForN func(src echo.Caller, dst echo.Services, opts *echo.CallOptions) echo.Checker

	// setting cases to skipped is better than not adding them - gives visibility to what needs to be fixed
	skip skip

	// workloadAgnostic is a temporary setting to trigger using RunForApps
	// TODO remove this and force everything to be workoad agnostic
	workloadAgnostic bool

	// toN causes the test to be run for N destinations. The call will be made from instances of the first deployment
	// in each subset in each cluster. See echotes.T's RunToN for more details.
	toN int
	// viaIngress makes the ingress gateway the caller for tests
	viaIngress bool
	// sourceMatchers allows adding additional filtering for workload agnostic cases to test using fewer clients
	sourceMatchers []match.Matcher
	// targetMatchers allows adding additional filtering for workload agnostic cases to test using fewer targets
	targetMatchers []match.Matcher
	// comboFilters allows conditionally filtering based on pairs of apps
	comboFilters []echotest.CombinationFilter
	// vars given to the config template
	templateVars func(src echo.Callers, dest echo.Instances) map[string]any

	// minIstioVersion allows conditionally skipping based on required version
	minIstioVersion string

	// If set, a datapath with no L7 proxies can run this test
	RequiresL4 bool
	// If set, will apply config to all clusters, not just the local one
	globalConfig bool
}

func (c TrafficTestCase) RunForApps(t framework.TestContext, apps echo.Instances, namespace string) {
	if c.skip.skip {
		t.Skip(c.skip.reason)
	}
	if c.minIstioVersion != "" {
		skipMV := !t.Settings().Revisions.AtLeast(resource.IstioVersion(c.minIstioVersion))
		if skipMV {
			t.SkipNow()
		}
	}
	if c.opts.To != nil {
		t.Fatal("TrafficTestCase.RunForApps: opts.To must not be specified")
	}
	if c.call != nil {
		t.Fatal("TrafficTestCase.RunForApps: call must not be specified")
	}
	// just check if any of the required fields are set
	optsSpecified := c.opts.Port.Name != "" || c.opts.Port.Protocol != "" || c.opts.Scheme != ""
	if optsSpecified && len(c.children) > 0 {
		t.Fatal("TrafficTestCase: must not specify both opts and children")
	}
	if !optsSpecified && len(c.children) == 0 {
		t.Fatal("TrafficTestCase: must specify either opts or children")
	}

	if !c.RequiresL4 {
		c.comboFilters = append(c.comboFilters, echotest.HasL7)
	}

	job := func(t framework.TestContext) {
		echoT := echotest.New(t, apps).
			SetupForServicePair(func(t framework.TestContext, src echo.Callers, dsts echo.Services) error {
				tmplData := map[string]any{
					// tests that use simple Run only need the first
					"dst":    dsts[0],
					"dstSvc": dsts[0][0].Config().Service,
					// tests that use RunForN need all destination deployments
					"dsts":    dsts,
					"dstSvcs": dsts.NamespacedNames().Names(),
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
				scope := t.ConfigIstio()
				if c.globalConfig {
					scope = t.ConfigKube()
				}
				return scope.YAML("", cfg).Apply()
			}).
			FromMatch(match.And(c.sourceMatchers...)).
			// TODO mainly testing proxyless features as a client for now
			ToMatch(match.And(append(c.targetMatchers, match.NotProxylessGRPC)...)).
			WithDefaultFilters(1, c.toN).
			ConditionallyTo(c.comboFilters...)

		doTest := func(t framework.TestContext, from echo.Caller, to echo.Services) {
			buildOpts := func(options echo.CallOptions) echo.CallOptions {
				opts := options
				opts.To = to[0]
				if c.check != nil {
					opts.Check = c.check(from, &opts)
				}
				if c.checkForN != nil {
					opts.Check = c.checkForN(from, to, &opts)
				}
				if c.setupOpts != nil {
					c.setupOpts(from, &opts)
				}
				// If unset, assume they want to just check the request succeeds
				if opts.Check == nil {
					opts.Check = check.OK()
				}
				return opts
			}
			if optsSpecified {
				from.CallOrFail(t, buildOpts(c.opts))
			}
			for _, child := range c.children {
				t.NewSubTest(child.name).Run(func(t framework.TestContext) {
					from.CallOrFail(t, buildOpts(child.opts))
				})
			}
		}

		if c.toN > 0 {
			echoT.RunToN(c.toN, func(t framework.TestContext, src echo.Instance, dsts echo.Services) {
				doTest(t, src, dsts)
			})
		} else if c.viaIngress {
			echoT.RunViaIngress(func(t framework.TestContext, from ingress.Instance, to echo.Target) {
				doTest(t, from, echo.Services{to.Instances()})
			})
		} else {
			echoT.Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
				doTest(t, from, echo.Services{to.Instances()})
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
		if c.skip.skip {
			t.Skip(c.skip.reason)
		}
		if c.minIstioVersion != "" {
			skipMV := !t.Settings().Revisions.AtLeast(resource.IstioVersion(c.minIstioVersion))
			if skipMV {
				t.SkipNow()
			}
		}
		// we only apply to config clusters
		if len(c.config) > 0 {
			tmplData := map[string]any{}
			if c.templateVars != nil {
				// we don't have echo instances so just pass nil
				for k, v := range c.templateVars(nil, nil) {
					tmplData[k] = v
				}
			}
			cfg := yml.MustApplyNamespace(t, tmpl.MustEvaluate(c.config, tmplData), namespace)
			scope := t.ConfigIstio()
			if c.globalConfig {
				scope = t.ConfigKube()
			}
			scope.YAML("", cfg).ApplyOrFail(t)
		}

		if c.call != nil && len(c.children) > 0 {
			t.Fatal("TrafficTestCase: must not specify both call and children")
		}

		if c.call != nil {
			c.call(t, c.opts)
		}

		for _, child := range c.children {
			t.NewSubTest(child.name).Run(func(t framework.TestContext) {
				child.call(t, child.opts)
			})
		}
	}
	if c.name != "" {
		t.NewSubTest(c.name).Run(job)
	} else {
		job(t)
	}
}

func skipAmbient(t framework.TestContext, reason string) skip {
	return skip{skip: t.Settings().Ambient, reason: reason}
}

func RunAllTrafficTests(t framework.TestContext, i istio.Instance, apps deployment.SingleNamespaceView) {
	RunCase := func(name string, f func(t TrafficContext)) {
		t.NewSubTest(name).Run(func(t framework.TestContext) {
			f(TrafficContext{TestContext: t, Apps: apps, Istio: i})
		})
	}
	RunSkipAmbient := func(name string, f func(t TrafficContext), reason string) {
		t.NewSubTest(name).Run(func(t framework.TestContext) {
			if t.Settings().Ambient {
				t.Skipf("ambient skipped: %v", reason)
			} else {
				f(TrafficContext{TestContext: t, Apps: apps, Istio: i})
			}
		})
	}
	RunCase("jwt-claim-route", jwtClaimRoute)
	RunCase("virtualservice", virtualServiceCases)
	RunCase("sniffing", protocolSniffingCases)
	RunCase("selfcall", selfCallsCases)
	RunSkipAmbient("serverfirst", serverFirstTestCases, "Expected success cases time out")
	RunCase("gateway", gatewayCases)
	RunCase("autopassthrough", autoPassthroughCases)
	RunSkipAmbient("loop", trafficLoopCases, "does not error (waypoint -> waypoint)")
	RunSkipAmbient("tls-origination", tlsOriginationCases, "not workload agnostic")
	RunSkipAmbient("instanceip", instanceIPTests, "not supported")
	RunCase("services", serviceCases)
	RunSkipAmbient("externalname", externalNameCases, "Relies on X-Forwarded-Client-Cert in checker")
	RunSkipAmbient("host", hostCases, "Relies on X-Forwarded-Client-Cert in checker")
	RunSkipAmbient("envoyfilter", envoyFilterCases, "not supported")
	RunCase("consistent-hash", consistentHashCases)
	RunCase("use-client-protocol", useClientProtocolCases)
	RunCase("destinationrule", destinationRuleCases)
	RunCase("vm", VMTestCases(apps.VM))
	RunSkipAmbient("dns", DNSTestCases, "https://github.com/istio/istio/issues/48614")
	RunCase("externalservice", TestExternalService)
	RunCase("upstreamproxy", TestUpstreamProxyProtocol)
	RunCase("service-entry-vips-resolution-none", testServiceEntryWithMultipleVIPsAndResolutionNone)
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

type TrafficContext struct {
	framework.TestContext
	Apps  deployment.SingleNamespaceView
	Istio istio.Instance

	// sourceFilters defines default filters for all cases
	sourceMatchers []match.Matcher
	// targetFilters defines default filters for all cases
	targetMatchers []match.Matcher
	// comboFilters defines default filters for all cases
	comboFilters []echotest.CombinationFilter
}

func (t *TrafficContext) SetDefaultSourceMatchers(f ...match.Matcher) {
	t.sourceMatchers = f
}

func (t *TrafficContext) SetDefaultTargetMatchers(f ...match.Matcher) {
	t.targetMatchers = f
}

func (t *TrafficContext) SetDefaultComboFilter(f ...echotest.CombinationFilter) {
	t.comboFilters = f
}

func (t TrafficContext) RunTraffic(tt TrafficTestCase) {
	if tt.sourceMatchers == nil {
		tt.sourceMatchers = t.sourceMatchers
	}
	if tt.targetMatchers == nil {
		tt.targetMatchers = t.targetMatchers
	}
	if tt.comboFilters == nil {
		tt.comboFilters = t.comboFilters
	}
	if tt.workloadAgnostic {
		tt.RunForApps(t, t.Apps.All.Instances(), t.Apps.Namespace.Name())
	} else {
		tt.Run(t, t.Apps.Namespace.Name())
	}
}
