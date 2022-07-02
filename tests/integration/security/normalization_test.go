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

package security

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
)

func supportedPercentEncode(i int) bool {
	special := map[int]struct{}{
		0x2d: {}, // -
		0x2e: {}, // .
		0x2f: {}, // /
		0x5c: {}, // \
		0x5f: {}, // _
		0x7e: {}, // ~
	}
	if _, found := special[i]; found {
		return true
	}
	if 0x30 <= i && i <= 0x39 {
		// 0-9
		return true
	}
	if 0x41 <= i && i <= 0x5a {
		// A-Z
		return true
	}
	if 0x61 <= i && i <= 0x7a {
		// a-z
		return true
	}
	return false
}

func TestNormalization(t *testing.T) {
	type expect struct {
		in, out string
	}
	var percentEncodedCases []expect
	for i := 1; i <= 0xff; i++ {
		input := fmt.Sprintf("/admin%%%.2x", i)
		output := input
		if supportedPercentEncode(i) {
			var err error
			output, err = url.PathUnescape(input)
			switch i {
			case 0x5c:
				output = strings.ReplaceAll(output, `\`, `/`)
			case 0x7e:
				output = strings.ReplaceAll(output, `%7e`, `~`)
			}
			if err != nil {
				t.Errorf("failed to unescape percent encoded path %s: %v", input, err)
			}
		}
		percentEncodedCases = append(percentEncodedCases, expect{in: input, out: output})
	}
	framework.NewTest(t).
		Features("security.normalization").
		Run(func(t framework.TestContext) {
			cases := []struct {
				name         string
				ntype        meshconfig.MeshConfig_ProxyPathNormalization_NormalizationType
				expectations []expect
			}{
				{
					"None",
					meshconfig.MeshConfig_ProxyPathNormalization_NONE,
					[]expect{
						{"/", "/"},
						{"/app#foo", "/app"},
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
					"Base",
					meshconfig.MeshConfig_ProxyPathNormalization_BASE,
					[]expect{
						{"/", "/"},
						{"/app#foo", "/app"},
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
					"MergeSlashes",
					meshconfig.MeshConfig_ProxyPathNormalization_MERGE_SLASHES,
					[]expect{
						{"/", "/"},
						{"/app#foo", "/app"},
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
					"DecodeAndMergeSlashes",
					meshconfig.MeshConfig_ProxyPathNormalization_DECODE_AND_MERGE_SLASHES,
					[]expect{
						{"/", "/"},
						{"/app#foo", "/app"},
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
						{`/%c0%2e%c0%2e/admin`, `/%c0.%c0./admin`},
						{`/%c0%2fadmin`, `/%c0/admin`},
					},
				},
				{
					"NotNormalized",
					meshconfig.MeshConfig_ProxyPathNormalization_DECODE_AND_MERGE_SLASHES,
					[]expect{
						{`/0x2e0x2e/admin`, `/0x2e0x2e/admin`},
						{`/0x2e0x2e0x2fadmin`, `/0x2e0x2e0x2fadmin`},
						{`/0x2e0x2e0x5cadmin`, `/0x2e0x2e0x5cadmin`},
						{`/0x2f0x2fadmin`, `/0x2f0x2fadmin`},
						{`/0x5c0x5cadmin`, `/0x5c0x5cadmin`},
						{`/%25c0%25ae%25c1%259cadmin`, `/%25c0%25ae%25c1%259cadmin`},
						{`/%25c0%25ae%25c0%25ae/admin`, `/%25c0%25ae%25c0%25ae/admin`},
						{`/%25c0%25afadmin`, `/%25c0%25afadmin`},
						{`/%25c1%259cadmin`, `/%25c1%259cadmin`},
						{`/%252e%252e/admin`, `/%252e%252e/admin`},
						{`/%252e%252e%252fadmin`, `/%252e%252e%252fadmin`},
						{`/%c1%9cadmin`, `/%c1%9cadmin`},
						{`/%c0%ae%c0%ae/admin`, `/%c0%ae%c0%ae/admin`},
						{`/%c0%afadmin`, `/%c0%afadmin`},
						{`/.../admin`, `/.../admin`},
						{`/..../admin`, `/..../admin`},
						{`/..;/admin`, `/..;/admin`},
						{`/;/admin`, `/;/admin`},
						{`/admin;a=b`, `/admin;a=b`},
						{`/admin;a=b/xyz`, `/admin;a=b/xyz`},
						{`/admin,a=b/xyz`, `/admin,a=b/xyz`},
						{`/Admin`, `/Admin`},
						{`/ADMIN`, `/ADMIN`},
					},
				},
				{
					// Test percent encode cases from %01 to %ff. (%00 is covered in the invalid group below).
					name:         "PercentEncoded",
					ntype:        meshconfig.MeshConfig_ProxyPathNormalization_DECODE_AND_MERGE_SLASHES,
					expectations: percentEncodedCases,
				},
				{
					"Invalid",
					meshconfig.MeshConfig_ProxyPathNormalization_DECODE_AND_MERGE_SLASHES,
					[]expect{
						{`/admin%00`, `400`},
					},
				},
			}
			for _, tt := range cases {
				t.NewSubTest(tt.name).Run(func(t framework.TestContext) {
					ist.PatchMeshConfigOrFail(t, t, fmt.Sprintf(`
pathNormalization:
  normalization: %v`, tt.ntype.String()))
					for _, c := range apps.A {
						for _, tt := range tt.expectations {
							t.NewSubTest(tt.in).Run(func(t framework.TestContext) {
								checker := check.URL(tt.out)
								if tt.out == "400" {
									checker = check.Status(http.StatusBadRequest)
								}
								c.CallOrFail(t, echo.CallOptions{
									To:    apps.B,
									Count: 1,
									HTTP: echo.HTTP{
										Path: tt.in,
									},
									Port: echo.Port{
										Name: "http",
									},
									Check: checker,
								})
							})
						}
					}
				})
			}
		})
}
