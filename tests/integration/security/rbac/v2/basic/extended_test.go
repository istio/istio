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

package basic

import (
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/integration/security/rbac/util"
	"istio.io/istio/tests/integration/security/util/connection"
)

const (
	extendedRbacV2RulesTmpl = "testdata/istio-extended-rbac-v2-rules.yaml.tmpl"
)

// TestRBACV2Extended tests extended features of RBAC v2 such as global namespace and inline role def.
func TestRBACV2Extended(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, "rbacv2-extended-test", true)
			ports := []echo.Port{
				{
					Name:        "http",
					Protocol:    model.ProtocolHTTP,
					ServicePort: 80,
				},
				{
					Name:        "tcp",
					Protocol:    model.ProtocolTCP,
					ServicePort: 90,
				},
			}

			var a, b, c echo.Instance
			echoboot.NewBuilderOrFail(t, ctx).
				With(&a, echo.Config{
					Service:        "a",
					Namespace:      ns,
					ServiceAccount: true,
					Ports:          ports,
					Galley:         g,
					Pilot:          p,
				}).
				With(&b, echo.Config{
					Service:        "b",
					Namespace:      ns,
					Ports:          ports,
					ServiceAccount: true,
					Galley:         g,
					Pilot:          p,
				}).
				With(&c, echo.Config{
					Service:        "c",
					Namespace:      ns,
					Ports:          ports,
					ServiceAccount: true,
					Galley:         g,
					Pilot:          p,
				}).
				BuildOrFail(t)

			cases := []util.TestCase{
				{
					Request: connection.Checker{
						From: b,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/some-path",
						},
					},
					ExpectAllowed: isMtlsEnabled,
				},
				{
					Request: connection.Checker{
						From: b,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/bad-path/black-hole",
						},
					},
					ExpectAllowed: false,
				},
				{
					Request: connection.Checker{
						From: b,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "tcp",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAllowed: isMtlsEnabled,
				},
				{
					Request: connection.Checker{
						From: c,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/",
						},
					},
					ExpectAllowed: false,
				},
				{
					Request: connection.Checker{
						From: c,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/good-path",
						},
					},
					ExpectAllowed: isMtlsEnabled,
				},
				{
					Request: connection.Checker{
						From: c,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "tcp",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAllowed: false,
				},

				{
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   b,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/xyz",
						},
					},
					ExpectAllowed: true,
				},
				{
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   b,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/secret",
						},
					},
					ExpectAllowed: false,
				},
				{
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   b,
							PortName: "tcp",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAllowed: true,
				},
				{
					Request: connection.Checker{
						From: c,
						Options: echo.CallOptions{
							Target:   b,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/",
						},
					},
					ExpectAllowed: true,
				},
				{
					Request: connection.Checker{
						From: c,
						Options: echo.CallOptions{
							Target:   b,
							PortName: "tcp",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAllowed: true,
				},
			}

			rootNamespace := model.DefaultMeshConfig().RootNamespace
			namespaceTmpl := map[string]string{
				"Namespace":     ns.Name(),
				"RootNamespace": rootNamespace,
			}
			policies := tmpl.EvaluateAllOrFail(t, namespaceTmpl,
				file.AsStringOrFail(t, rbacClusterConfigTmpl),
				file.AsStringOrFail(t, extendedRbacV2RulesTmpl))

			// Pass in nil for namespace to apply the policies for all namespaces.
			g.ApplyConfigOrFail(t, nil, policies...)
			rootNs := namespace.ClaimOrFail(t, ctx, rootNamespace)
			defer func() { _ = g.DeleteConfig(ns, policies...) }()
			defer func() { _ = g.DeleteConfig(rootNs, policies...) }()

			// Sleep 60 seconds for the policy to take effect.
			// TODO(pitlv2109): Check to make sure policies have been created instead.
			time.Sleep(60 * time.Second)

			for _, tc := range cases {
				testName := fmt.Sprintf("%s->%s:%s%s[%v]",
					tc.Request.From.Config().Service,
					tc.Request.Options.Target.Config().Service,
					tc.Request.Options.PortName,
					tc.Request.Options.Path,
					tc.ExpectAllowed)
				t.Run(testName, func(t *testing.T) {
					retry.UntilSuccessOrFail(t, tc.CheckRBACRequest, retry.Delay(time.Second), retry.Timeout(10*time.Second))
				})
			}
		})
}
