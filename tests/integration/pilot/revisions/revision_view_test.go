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
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"istio.io/istio/istioctl/cmd"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istioctl"
)

var (
	manifestPath = filepath.Join(env.IstioSrc, "manifests")
)

type revisionResource struct {
	ns   namespace.Instance
	echo echo.Instance
}

func TestRevisionCommand(t *testing.T) {
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Features("installation.istioctl.revision_centric_view").
		Run(func(ctx framework.TestContext) {
			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})

			revisions := []string{"default", "stable", "canary"}
			revResourceMap := map[string]*revisionResource{}
			builder := echoboot.NewBuilder(ctx)
			for _, rev := range revisions {
				effectiveRev := rev
				if rev == "default" {
					effectiveRev = ""
				}
				revResourceMap[rev] = &revisionResource{
					ns: namespace.NewOrFail(t, ctx, namespace.Config{
						Prefix:   fmt.Sprintf("istioctl-rev-%s", rev),
						Inject:   true,
						Revision: effectiveRev,
					}),
				}
				builder.With(&revResourceMap[rev].echo,
					echo.Config{Namespace: revResourceMap[rev].ns})
			}

			testCases := []struct {
				name     string
				testFunc func(framework.TestContext, istioctl.Instance, map[string]*revisionResource)
			}{
				{
					name:     "list revisions",
					testFunc: testRevisionListing,
				},
				{
					name:     "describe valid revision",
					testFunc: testRevisionDescription,
				},
				{
					name:     "describe non-existent revision",
					testFunc: testNonExistentRevisionDescription,
				},
				{
					name:     "describe invalid revision",
					testFunc: testInvalidRevisionFormat,
				},
				{
					name:     "invalid output format",
					testFunc: testInvalidOutputFormat,
				},
			}
			for _, testCase := range testCases {
				ctx.NewSubTest(testCase.name).Run(func(sctx framework.TestContext) {
					testCase.testFunc(sctx, istioCtl, revResourceMap)
				})
			}
		})
}

func testRevisionListing(ctx framework.TestContext, istioCtl istioctl.Instance, revResources map[string]*revisionResource) {
	listVerboseCmd := []string{"x", "revision", "list", "-d", manifestPath, "-v", "-o", "json"}
	stdout, _, err := istioCtl.Invoke(listVerboseCmd)
	if err != nil {
		ctx.Fatalf("unexpected error while invoking istioctl command %v: %v", listVerboseCmd, err)
	}
	var revDescriptions map[string]*cmd.RevisionDescription
	if err = json.Unmarshal([]byte(stdout), &revDescriptions); err != nil {
		ctx.Fatalf("error while unmarshaling JSON output: %v", err)
	}
	// TODO(su225): complete this
}

func testRevisionDescription(ctx framework.TestContext, istioCtl istioctl.Instance, revResources map[string]*revisionResource) {
	stableDescr, err := getDescriptionForRevision(istioCtl, "stable")
	if err != nil || stableDescr == nil {
		ctx.Fatalf("failed to retrieve description for stable: %v", err)
	}
	// TODO(su225): complete this
}

func getDescriptionForRevision(istioCtl istioctl.Instance, revision string) (*cmd.RevisionDescription, error) {
	describeCmd := []string{"x", "revision", "describe", revision, "-d", manifestPath, "-v", "-o", "json"}
	descr, _, err := istioCtl.Invoke(describeCmd)
	if err != nil {
		return nil, err
	}
	var revDescription cmd.RevisionDescription
	if err = json.Unmarshal([]byte(descr), &revDescription); err != nil {
		return nil, fmt.Errorf("error while unmarshaling revision description JSON for"+
			" revision=%s : %v", revision, err)
	}
	return &revDescription, nil
}

func testNonExistentRevisionDescription(ctx framework.TestContext, istioCtl istioctl.Instance, _ map[string]*revisionResource) {
	describeArgs := []string{"x", "revision", "describe", "ghost", "-o", "json", "-v"}
	_, _, err := istioCtl.Invoke(describeArgs)
	if err == nil {
		ctx.Fatalf("expected error for non-existent revision 'ghost', but didn't get it")
	}
}

func testInvalidRevisionFormat(ctx framework.TestContext, istioCtl istioctl.Instance, _ map[string]*revisionResource) {
	describeArgs := []string{"x", "revision", "describe", "$%#@abc", "-o", "json", "-v"}
	_, _, err := istioCtl.Invoke(describeArgs)
	if err == nil || !strings.Contains(err.Error(), "invalid revision format") {
		ctx.Fatalf("expected error message saying revision format is invalid, but got %v", err)
	}
}

func testInvalidOutputFormat(ctx framework.TestContext, istioCtl istioctl.Instance, _ map[string]*revisionResource) {
	type invalidFormatTest struct {
		name    string
		command []string
	}
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
}
