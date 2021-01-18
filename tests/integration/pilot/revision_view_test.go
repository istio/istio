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

package pilot

import (
	"encoding/json"
	"istio.io/istio/istioctl/cmd"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

func TestRevisionListing(t *testing.T) {
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Features("installation.istioctl.revision_centric_view").
		Run(func(ctx framework.TestContext) {
			skipIfK8sVersionUnsupported(ctx)

			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})

			// purge all istio resources before starting
			istioCtl.InvokeOrFail(ctx, []string{"experimental", "uninstall", "--purge", "-y"})

			// keep track of applied configurations and clean up after the test
			configs := make(map[string]string)
			ctx.WhenDone(func() error {
				var errs *multierror.Error
				for _, config := range configs {
					multierror.Append(errs, ctx.Config().DeleteYAML("istio-system", config))
				}
				return errs.ErrorOrNil()
			})

			defaultVersion := "1.7.6"
			canaryVersion := "1.8.0"

			installRevisionOrFail(ctx, defaultVersion, configs)
			installRevisionOrFail(ctx, canaryVersion, configs)
			defaultRevNs := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "istioctl-rev-default",
				Inject: true,
			})
			canaryRevNs := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix:   "istioctl-rev-canary",
				Inject:   false,
				Revision: "canary",
			})
			builder := echoboot.NewBuilder(ctx)

			var defaultEchoInstance echo.Instance
			var canaryEchoInstance echo.Instance
			builder.With(&defaultEchoInstance, echo.Config{Namespace: defaultRevNs}).
				With(&canaryEchoInstance, echo.Config{Namespace: canaryRevNs}).
				BuildOrFail(ctx)
			testRevisionListing(ctx, istioCtl, defaultRevNs, canaryRevNs)
		})
}

func testRevisionListing(ctx framework.TestContext, istioCtl istioctl.Instance,
	defaultRevNs namespace.Instance, canaryRevNs namespace.Instance) {
	listCmd := []string{
		"experimental", "revision", "list",
		"-d", operator.ManifestPath,
		"-o", "json",
	}
	jsonOut, _, err := istioCtl.InvokeOrFail(ctx, listCmd)
	if err != nil {
		ctx.Fatalf("revision listing should have been successful, "+
			"but got an error instead: %v", err)
	}
	var descriptions map[string]*cmd.RevisionDescription
	err = json.Unmarshal([]byte(jsonOut), descriptions)
	if err != nil {
		ctx.Fatalf("error while unmarshaling revision list output: %v", err)
	}
	expectedRevisions := []string{"default", "revision"}
	for rev, rd := range descriptions {
		found := false
		for _, r := range expectedRevisions {
			if r == rev {
				found = true
				break
			}
		}
		if !found {
			ctx.Fatalf("unexpected revision found: %s", rev)
		}
		if len(rd.IstioOperatorCRs) != 1 {
			ctx.Fatalf("expected a single IstioOperator CR for %s, found %d",
				rev, len(rd.IstioOperatorCRs))
		}
		ctx.Logf("installed IstioOperator CR (profile=%s) components: %v\n",
			rd.IstioOperatorCRs[0].Profile, rd.IstioOperatorCRs[0].Components)
		if len(rd.ControlPlanePods) == 0 {
			ctx.Fatalf("no control plane pods listed for revision %s", rev)
		}
		if len(rd.Webhooks) == 0 {
			ctx.Fatalf("no mutating webhook listed for revision %s", rev)
		}
	}
}

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
				{name: "describe", command: []string{"x", "revision", "describe", "-o", "mystery"}},
			} {
				ctx.NewSubTest(t.name).Run(func(sctx framework.TestContext) {
					_, _, fErr := istioCtl.Invoke(listCmd)
					if fErr == nil {
						ctx.Fatalf("expected error due to invalid format, but got nil")
					}
				})
			}
		})
}

func TestValidRevisionDescription(t *testing.T) {

}

func TestNonExistentRevisionDescription(t *testing.T) {
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Features("installation.istioctl.revision_centric_view").
		Run(func(ctx framework.TestContext) {

		})
}
