//go:build integ
// +build integ

//  Copyright Istio Authors
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
	"strings"
	"time"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/yml"
	promMetric "istio.io/istio/tests/integration/telemetry"
)

// Slow down retries to allow for delayed_close_timeout. Also require 3 successive successes.
var retryOptions = []retry.Option{retry.Delay(1000 * time.Millisecond), retry.Converge(3)}

type TrafficCall struct {
	name string
	call func(t test.Failer, options echo.CallOptions, retryOptions ...retry.Option) echo.CallResult
	opts echo.CallOptions
}

type TrafficTestCase struct {
	name string
	// destination app
	destinationApp string
	// config can optionally be templated using the params src, dst (each are []echo.Instance)
	config string

	// Single call. Cannot be used with children.
	call func(t test.Failer, options echo.CallOptions) echo.CallResult
	// opts specifies the echo call options. When using RunForApps, the Target will be set dynamically.
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
			scopes.Framework.Debugf("apply configuration %+s", c.config)
			cfg := yml.MustApplyNamespace(ctx, c.config, namespace)
			ctx.ConfigIstio().YAML(namespace, cfg).ApplyOrFail(ctx, apply.Wait)
			ctx.Cleanup(func() {
				_ = ctx.ConfigIstio().YAML(cfg).Delete()
			})
		}

		if c.call != nil {
			// Call the function with a few custom retry options.
			c.call(ctx, c.opts)
		}

		// Verify that the proxy is not sending http requests
		promCase := prometheus.Query{
			Metric:      "istio_requests_total",
			Aggregation: "sum",
			Labels: map[string]string{
				"destination_app": c.destinationApp,
				"response_code":   "200",
			},
		}
		ValidateMetric(ctx, ctx.Clusters().Default(), prom, promCase, 0)
	}
	if c.name != "" {
		ctx.NewSubTest(c.name).Run(job)
	} else {
		job(ctx)
	}

}

func RunAllTrafficTests(ctx framework.TestContext, apps map[string]*EchoDeployments) {
	clients := setupTrafficTest(ctx)

	cases := httpTraffic(clients, apps)
	ctx.NewSubTest("Reachability_from_echo").
		Run(func(ctx framework.TestContext) {
			for _, tt := range cases {
				tt.Run(ctx, "echoNS")
			}
		})
}

func setupTrafficTest(ctx framework.TestContext) map[string]*EchoDeployments {
	var clientIPv4, clientIPv6 echo.Instance
	testNs := namespace.NewOrFail(ctx, ctx, namespace.Config{
		Prefix: "clients",
		Inject: true,
	})

	ipVersions := []string{
		"IPv4",
		"IPv6",
	}

	echoConfigs := []echo.Config{}

	clients := make(map[string]*EchoDeployments)

	for _, echoIPVersion := range ipVersions {
		lowercaseIPVersion := strings.ToLower(echoIPVersion)
		echoConfigs = append(echoConfigs, echo.Config{
			Service:        "client-" + lowercaseIPVersion,
			Namespace:      testNs,
			Ports:          []echo.Port{},
			IPFamilies:     echoIPVersion,
			IPFamilyPolicy: "SingleStack",
		})
		clients[lowercaseIPVersion] = &EchoDeployments{}
		switch echoIPVersion {
		case "IPv4":
			clients[lowercaseIPVersion].IPFamily = IPv4
		case "IPv6":
			clients[lowercaseIPVersion].IPFamily = IPv6
		}
		clients[lowercaseIPVersion].Namespace = testNs
	}

	deployment.New(ctx).
		With(&clientIPv4, echoConfigs[0]).
		With(&clientIPv6, echoConfigs[1]).
		BuildOrFail(ctx)

	clients["ipv4"].EchoPod = clientIPv4
	clients["ipv6"].EchoPod = clientIPv6

	return clients
}

func httpTraffic(sourceApp map[string]*EchoDeployments, apps map[string]*EchoDeployments) []TrafficTestCase {
	cases := []TrafficTestCase{}

	for ipVersion, client := range sourceApp {
		for _, dest := range apps {
			expectedResponse := check.OK()
			if len(dest.EchoPod.Addresses()) == 1 && dest.IPFamily != client.IPFamily {
				expectedResponse = check.Error()
			}
			cases = append(cases, TrafficTestCase{
				name:           fmt.Sprintf("%s_http_call_to_%s", ipVersion, dest.EchoPod.Config().Service),
				destinationApp: client.ServiceName,
				call:           client.EchoPod.CallOrFail,
				opts: echo.CallOptions{
					To: dest.EchoPod,
					Port: echo.Port{
						Name: "http",
					},
					Check: expectedResponse,
				},
			})
			if len(dest.EchoPod.Addresses()) > 1 {
				cases = append(cases, TrafficTestCase{
					name:           fmt.Sprintf("%s_http_call_to_%s", ipVersion, dest.EchoPod.Addresses()[1]),
					destinationApp: client.ServiceName,
					call:           client.EchoPod.CallOrFail,
					opts: echo.CallOptions{
						To:      dest.EchoPod,
						Address: dest.EchoPod.Addresses()[1],
						Port: echo.Port{
							Name: "http",
						},
						Check: check.OK(),
					},
				})
			}
		}
	}

	return cases
}

func ValidateMetric(t framework.TestContext, cluster cluster.Cluster, prometheus prometheus.Instance, query prometheus.Query, want float64) {
	t.Helper()
	err := retry.UntilSuccess(func() error {
		got, err := prometheus.QuerySum(cluster, query)
		t.Logf("%s: %f", query.Metric, got)
		if err != nil && !strings.Contains(err.Error(), "value not found") {
			return err
		}
		if got != want {
			return fmt.Errorf("bad metric value: got %f, want %f", got, want)
		}
		return nil
	}, retry.Delay(time.Second), retry.Timeout(time.Second*20))
	if err != nil {
		promMetric.PromDiff(t, prometheus, cluster, query)
		t.Fatal(err)
	}
}
