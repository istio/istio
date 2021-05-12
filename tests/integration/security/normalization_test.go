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

package security

import (
	"fmt"
	"testing"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/istio"
)

func TestNormalization(t *testing.T) {
	framework.NewTest(t).
		Features("traffic.routing").
		Run(func(t framework.TestContext) {
			type expect struct {
				in, out string
			}
			cases := []struct {
				ntype        meshconfig.MeshConfig_ProxyPathNormalization_NormalizationType
				expectations []expect
			}{
				{
					meshconfig.MeshConfig_ProxyPathNormalization_NONE,
					[]expect{
						{"/", "/"},
						{"/app/", "/app/"},
						{"/app/../admin", "/app/../admin"},
						{"/app", "/app"},
						{"/app//", "/app//"},
						{"/app/%2f", "/app/%2f"},
						{"/app%2f/", "/app%2f/"},
						{"/xyz%30..//abc", "/xyz%30..//abc"},
						{"/app/%2E./admin", "/app/%2E./admin"},
						{`/app\admin`, `/app\admin`},
						{`/app/\/\/\admin`, `/app/\/\/\admin`},
						{`/%2Fapp%5cadmin%5Cabc`, `/%2Fapp%5cadmin%5Cabc`},
						{`/%5Capp%2f%5c%2F%2e%2e%2fadmin%5c\abc`, `/%5Capp%2f%5c%2F%2e%2e%2fadmin%5c\abc`},
						{`/app//../admin`, `/app//../admin`},
						{`/app//../../admin`, `/app//../../admin`},
					},
				},
				{
					meshconfig.MeshConfig_ProxyPathNormalization_BASE,
					[]expect{
						{"/", "/"},
						{"/app/", "/app/"},
						{"/app/../admin", "/admin"},
						{"/app", "/app"},
						{"/app//", "/app//"},
						{"/app/%2f", "/app/%2f"},
						{"/app%2f/", "/app%2f/"},
						{"/xyz%30..//abc", "/xyz0..//abc"},
						{"/app/%2E./admin", "/admin"},
						{`/app\admin`, `/app/admin`},
						{`/app/\/\/\admin`, `/app//////admin`},
						{`/%2Fapp%5cadmin%5Cabc`, `/%2Fapp%5cadmin%5Cabc`},
						{`/%5Capp%2f%5c%2F%2e%2e%2fadmin%5c\abc`, `/%5Capp%2f%5c%2F..%2fadmin%5c/abc`},
						{`/app//../admin`, `/app/admin`},
						{`/app//../../admin`, `/admin`},
					},
				},
				{
					meshconfig.MeshConfig_ProxyPathNormalization_MERGE_SLASHES,
					[]expect{
						{"/", "/"},
						{"/app/", "/app/"},
						{"/app/../admin", "/admin"},
						{"/app", "/app"},
						{"/app//", "/app/"},
						{"/app/%2f", "/app/%2f"},
						{"/app%2f/", "/app%2f/"},
						{"/xyz%30..//abc", "/xyz0../abc"},
						{"/app/%2E./admin", "/admin"},
						{`/app\admin`, `/app/admin`},
						{`/app/\/\/\admin`, `/app/admin`},
						{`/%2Fapp%5cadmin%5Cabc`, `/%2Fapp%5cadmin%5Cabc`},
						{`/%5Capp%2f%5c%2F%2e%2e%2fadmin%5c\abc`, `/%5Capp%2f%5c%2F..%2fadmin%5c/abc`},
						{`/app//../admin`, `/app/admin`},
						{`/app//../../admin`, `/admin`},
					},
				},
				{
					meshconfig.MeshConfig_ProxyPathNormalization_DECODE_AND_MERGE_SLASHES,
					[]expect{
						{"/", "/"},
						{"/app/", "/app/"},
						{"/app/../admin", "/admin"},
						{"/app", "/app"},
						{"/app//", "/app/"},
						{"/app/%2f", "/app/"},
						{"/app%2f/", "/app/"},
						{"/xyz%30..//abc", "/xyz0../abc"},
						{"/app/%2E./admin", "/admin"},
						{`/app\admin`, `/app/admin`},
						{`/app/\/\/\admin`, `/app/admin`},
						{`/%2Fapp%5cadmin%5Cabc`, `/app/admin/abc`},
						{`/%5Capp%2f%5c%2F%2e%2e%2fadmin%5c\abc`, `/app/admin/abc`},
						{`/app//../admin`, `/app/admin`},
						{`/app//../../admin`, `/admin`},
					},
				},
			}
			for _, tt := range cases {
				t.NewSubTest(tt.ntype.String()).Run(func(t framework.TestContext) {
					istio.PatchMeshConfig(t, ist.Settings().IstioNamespace, t.Clusters(), fmt.Sprintf(`
pathNormalization:
  normalization: %v`, tt.ntype.String()))
					for _, c := range apps.A {
						for _, tt := range tt.expectations {
							t.NewSubTest(tt.in).Run(func(t framework.TestContext) {
								c.CallWithRetryOrFail(t, echo.CallOptions{
									Target:    apps.B[0],
									Path:      tt.in,
									PortName:  "http",
									Validator: echo.ExpectKey("URL", tt.out),
								})
							})
						}
					}
				})
			}
		})
}
