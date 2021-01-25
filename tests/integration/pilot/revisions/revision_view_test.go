// +build integ
//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package revisions

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istioctl"
)

func TestInvalidFormat(t *testing.T) {
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Features("installation.istioctl.revision_centric_view").
		Run(func(ctx framework.TestContext) {
			type invalidFormatTest struct {
				name    string
				command []string
			}
			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})
			for _, t := range []invalidFormatTest{
				{name: "list", command: []string{"x", "revision", "list", "-o", "mystery"}},
				{name: "describe", command: []string{"x", "revision", "describe", "default", "-o", "mystery"}},
			} {
				ctx.NewSubTest(t.name).Run(func(sctx framework.TestContext) {
					_, _, fErr := istioCtl.Invoke(t.command)
					if fErr == nil {
						ctx.Fatalf("expected error due to invalid format, but got nil")
					}
				})
			}
		})
}

func TestNonExistentRevisionDescription(t *testing.T) {
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Features("installation.istioctl.revision_centric_view").
		Run(func(ctx framework.TestContext) {
			istioCtl := istioctl.NewOrFail(t, ctx, istioctl.Config{})
			describeArgs := []string{"x", "revision", "describe", "ghost", "-o", "json", "-v"}
			_, _, err := istioCtl.Invoke(describeArgs)
			if err == nil {
				t.Fatalf("expected error for non-existent revision 'ghost', but didn't get it")
			}
		})
}
