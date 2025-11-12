//go:build integ

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

package pilot

import (
	"fmt"
	"net"
	"testing"

	"istio.io/api/annotation"
	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/slices"
	echot "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/assert"
)

func getEchoConfigs() []echo.Config {
	return []echo.Config{
		{
			Service: "client",
			Ports:   ports.All(),
		},
		{
			Service:        "echo-dual",
			Ports:          ports.All(),
			IPFamilyPolicy: "PreferDualStack",
		},
		{
			Service:        "echo-v4",
			Ports:          ports.All(),
			IPFamilyPolicy: "PreferDualStack",
			BindFamily:     "IPv4",
		},
		{
			Service:        "echo-v4only",
			Ports:          ports.All(),
			IPFamilyPolicy: "SingleStack",
			BindFamily:     "IPv4",
			IPFamilies:     "IPv4",
		},
		{
			Service:        "echo-v6only",
			Ports:          ports.All(),
			IPFamilyPolicy: "SingleStack",
			BindFamily:     "IPv6",
			IPFamilies:     "IPv6",
		},
		{
			Service:        "echo-v6",
			Ports:          ports.All(),
			IPFamilyPolicy: "PreferDualStack",
			BindFamily:     "IPv6",
		},
		{
			Service:        "echo-v4-naked",
			Ports:          ports.All(),
			IPFamilyPolicy: "PreferDualStack",
			BindFamily:     "IPv4",
			Subsets:        []echo.SubsetConfig{{Annotations: map[string]string{annotation.SidecarInject.Name: "false"}}},
		},
		{
			Service:        "echo-v6-naked",
			Ports:          ports.All(),
			IPFamilyPolicy: "PreferDualStack",
			BindFamily:     "IPv6",
			Subsets:        []echo.SubsetConfig{{Annotations: map[string]string{annotation.SidecarInject.Name: "false"}}},
		},
	}
}

func TestDualStackEndpointLoadBalancer(t *testing.T) {
	framework.NewTest(t).
		RequiresDualStack().
		Run(func(t framework.TestContext) {
			echoDS := namespace.NewOrFail(t, namespace.Config{
				Prefix: "echo-ds",
				Inject: true,
			})
			echos := deployment.New(t)
			echos.WithClusters(t.Clusters()...)
			for _, config := range getEchoConfigs() {
				config.Namespace = echoDS
				echos.WithConfig(config)
			}
			echosDeployment := echos.BuildOrFail(t)
			fromMatch := match.ServiceName(echo.NamespacedName{
				Name:      "client",
				Namespace: echoDS,
			})
			toMatch := match.Not(fromMatch)

			echotest.New(t, echosDeployment).
				FromMatch(fromMatch).
				ToMatch(toMatch).
				Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
					for _, toInstance := range to.Instances() {
						for _, testFamily := range []int{4, 6} {
							t.NewSubTestf("%sVia%d", to.ServiceName(), testFamily).Run(func(t framework.TestContext) {
								defaultFamily := 6 // This decide the first entry of the happy-eyeballs algorithm
								address := ""
								for i, addr := range toInstance.Addresses() {
									if net.ParseIP(addr).To4() != nil {
										if i == 0 {
											defaultFamily = 4
										}
										if testFamily == 4 {
											address = addr
											break
										}
									} else if testFamily == 6 {
										address = addr
									}
								}

								opts := echo.CallOptions{
									Port: echo.Port{
										Name: "http-instance",
									},
									To:      toInstance,
									Address: address,
									Check:   check.OK(),
								}
								opts.HTTP.Headers = headers.New().
									WithHost(fmt.Sprintf("%s.%s.svc.cluster.local", to.ServiceName(), to.NamespaceName())).
									Build()

								// Expect failure if request will reach family echo is not bound to
								bindFamily := to.Config().BindFamily
								if bindFamily != "" {
									if !to.Config().IsNaked() && ((bindFamily == "IPv4" && defaultFamily == 6) || (bindFamily == "IPv6" && defaultFamily == 4)) {
										opts.Check = check.NotOK()
									}
								}
								res := from.CallOrFail(t, opts)
								responses := slices.Filter(res.Responses, func(response echot.Response) bool {
									return response.Code == "200"
								})
								if len(responses) > 0 {
									response := responses[0]
									ipBodyFamily := 6
									if net.ParseIP(response.RawBody["IP"]).To4() != nil {
										ipBodyFamily = 4
									}
									if to.Config().IsNaked() {
										expectedIP := testFamily
										if to.Config().BindFamily == "IPv4" {
											expectedIP = 4
										} else if to.Config().BindFamily == "IPv6" {
											expectedIP = 6
										}
										assert.Equal(t, ipBodyFamily, expectedIP)
									} else {
										assert.Equal(t, ipBodyFamily, defaultFamily)
									}
								}
							})
						}
					}
				})
		})
}
