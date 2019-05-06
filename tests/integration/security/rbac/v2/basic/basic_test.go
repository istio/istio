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
	rbacClusterConfigTmpl = "testdata/istio-clusterrbacconfig.yaml.tmpl"
	rbacV2RulesTmpl       = "testdata/istio-rbac-v2-rules.yaml.tmpl"
)

func TestRBACV2(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, "rbacv1", true)
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
			a := echoboot.NewOrFail(t, ctx, echo.Config{
				Service:   "a",
				Namespace: ns,
				Sidecar:   true,
				Ports:     ports,
				Galley:    g,
				Pilot:     p,
			})
			b := echoboot.NewOrFail(t, ctx, echo.Config{
				Service:        "b",
				Namespace:      ns,
				Ports:          ports,
				Sidecar:        true,
				ServiceAccount: true,
				Galley:         g,
				Pilot:          p,
			})
			c := echoboot.NewOrFail(t, ctx, echo.Config{
				Service:        "c",
				Namespace:      ns,
				Ports:          ports,
				Sidecar:        true,
				ServiceAccount: true,
				Galley:         g,
				Pilot:          p,
			})
			d := echoboot.NewOrFail(t, ctx, echo.Config{
				Service:        "d",
				Namespace:      ns,
				Ports:          ports,
				Sidecar:        true,
				ServiceAccount: true,
				Galley:         g,
				Pilot:          p,
			})

			cases := []util.TestCase{
				// Port 80 is where HTTP is served, 90 is where TCP is served. When an HTTP request is at port
				// 90, this means it is a TCP request. The test framework uses HTTP to mimic TCP calls in this case.
				{
					Request: connection.Checker{
						From: b,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/xyz",
						},
					},
					ExpectAllowed: false,
					RejectionCode: util.DenyHTTPRespCode,
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
					ExpectAllowed: false,
					RejectionCode: util.DenyHTTPRespCode,
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
					RejectionCode: util.DenyHTTPRespCode,
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
					RejectionCode: util.DenyHTTPRespCode,
				},
				{
					Request: connection.Checker{
						From: d,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/",
						},
					},
					ExpectAllowed: false,
					RejectionCode: util.DenyHTTPRespCode,
				},
				{
					Request: connection.Checker{
						From: d,
						Options: echo.CallOptions{
							Target:   a,
							PortName: "tcp",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAllowed: false,
					RejectionCode: util.DenyHTTPRespCode,
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
					ExpectAllowed: isMtlsEnabled,
					RejectionCode: util.DenyHTTPRespCode,
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
					RejectionCode: util.DenyHTTPRespCode,
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
					ExpectAllowed: isMtlsEnabled,
					RejectionCode: util.DenyHTTPRespCode,
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
					ExpectAllowed: isMtlsEnabled,
					RejectionCode: util.DenyHTTPRespCode,
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
					ExpectAllowed: isMtlsEnabled,
					RejectionCode: util.DenyHTTPRespCode,
				},
				{
					Request: connection.Checker{
						From: d,
						Options: echo.CallOptions{
							Target:   b,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/",
						},
					},
					ExpectAllowed: isMtlsEnabled,
					RejectionCode: util.DenyHTTPRespCode,
				},
				{
					Request: connection.Checker{
						From: d,
						Options: echo.CallOptions{
							Target:   b,
							PortName: "tcp",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAllowed: isMtlsEnabled,
					RejectionCode: util.DenyHTTPRespCode,
				},

				{
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   c,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/",
						},
					},
					ExpectAllowed: false,
					RejectionCode: util.DenyHTTPRespCode,
				},
				{
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   c,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/secrets/admin",
						},
					},
					ExpectAllowed: false,
					RejectionCode: util.DenyHTTPRespCode,
				},
				{
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   c,
							PortName: "tcp",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAllowed: false,
					RejectionCode: util.DenyHTTPRespCode,
				},
				{
					Request: connection.Checker{
						From: b,
						Options: echo.CallOptions{
							Target:   c,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/",
						},
					},
					ExpectAllowed: false,
					RejectionCode: util.DenyHTTPRespCode,
				},
				{
					Request: connection.Checker{
						From: b,
						Options: echo.CallOptions{
							Target:   c,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/credentials/admin",
						},
					},
					ExpectAllowed: false,
					RejectionCode: util.DenyHTTPRespCode,
				},
				{
					Request: connection.Checker{
						From: b,
						Options: echo.CallOptions{
							Target:   c,
							PortName: "tcp",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAllowed: false,
					RejectionCode: util.DenyHTTPRespCode,
				},
				{
					Request: connection.Checker{
						From: d,
						Options: echo.CallOptions{
							Target:   c,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/",
						},
					},
					ExpectAllowed: isMtlsEnabled,
					RejectionCode: util.DenyHTTPRespCode},
				{
					Request: connection.Checker{
						From: d,
						Options: echo.CallOptions{
							Target:   c,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/any_path/admin",
						},
					},
					ExpectAllowed: false,
					RejectionCode: util.DenyHTTPRespCode,
				},
				{
					Request: connection.Checker{
						From: d,
						Options: echo.CallOptions{
							Target:   c,
							PortName: "tcp",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAllowed: false,
					RejectionCode: util.DenyHTTPRespCode,
				},
				{
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   d,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/xyz",
						},
					},
					ExpectAllowed: true,
					RejectionCode: util.DenyHTTPRespCode,
				},
				{
					Request: connection.Checker{
						From: a,
						Options: echo.CallOptions{
							Target:   d,
							PortName: "tcp",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAllowed: false,
					RejectionCode: util.DenyHTTPRespCode,
				},
				{
					Request: connection.Checker{
						From: b,
						Options: echo.CallOptions{
							Target:   d,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/",
						},
					},
					ExpectAllowed: true,
					RejectionCode: util.DenyHTTPRespCode,
				},
				{
					Request: connection.Checker{
						From: b,
						Options: echo.CallOptions{
							Target:   d,
							PortName: "tcp",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAllowed: false,
					RejectionCode: util.DenyHTTPRespCode,
				},
				{
					Request: connection.Checker{
						From: c,
						Options: echo.CallOptions{
							Target:   d,
							PortName: "http",
							Scheme:   scheme.HTTP,
							Path:     "/any_path",
						},
					},
					ExpectAllowed: true,
					RejectionCode: util.DenyHTTPRespCode,
				},
				{
					Request: connection.Checker{
						From: c,
						Options: echo.CallOptions{
							Target:   d,
							PortName: "tcp",
							Scheme:   scheme.HTTP,
						},
					},
					ExpectAllowed: false,
					RejectionCode: util.DenyHTTPRespCode,
				},
			}

			args := map[string]string{
				"Namespace": ns.Name(),
			}
			policies := tmpl.EvaluateAllOrFail(t, args,
				file.AsString(t, rbacClusterConfigTmpl),
				file.AsString(t, rbacV2RulesTmpl))

			g.ApplyConfigOrFail(t, ns, policies...)
			defer g.DeleteConfigOrFail(t, ns, policies...)

			// Sleep 60 seconds for the policy to take effect.
			// TODO(pitlv2109: Check to make sure policies have been created instead.
			time.Sleep(60 * time.Second)

			for _, tc := range cases {
				testName := fmt.Sprintf("%s->%s:%s/%s",
					tc.Request.From.Config().Service,
					tc.Request.Options.Target.Config().Service,
					tc.Request.Options.PortName,
					tc.Request.Options.Path)
				t.Run(testName, func(t *testing.T) {
					retry.UntilSuccessOrFail(t, tc.Check, retry.Delay(time.Second), retry.Timeout(10*time.Second))
				})
			}
		})
}
