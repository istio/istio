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

package istioctl

import (
	"regexp"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	i   istio.Instance
	env *kube.Environment
)

// This test requires `--istio.test.env=kube` because it tests istioctl doing PodExec
func TestMain(m *testing.M) {
	framework.
		NewSuite("istioctl_integration_test", m).

		// Deploy Istio
		SetupOnEnv(environment.Kube, istio.Setup(&i, nil)).
		SetupOnEnv(environment.Kube, func(ctx resource.Context) error {
			env = ctx.Environment().(*kube.Environment)
			return nil
		}).
		Run()
}

// TestVersion does "istioctl version --remote=true" to verify the CLI understands the data plane version data
func TestVersion(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			g := galley.NewOrFail(t, ctx, galley.Config{})
			_ = pilot.NewOrFail(t, ctx, pilot.Config{Galley: g})

			istioCtl := istioctl.NewOrFail(t, ctx, istioctl.Config{})

			args := []string{"version", "--remote=true"}

			output := istioCtl.InvokeOrFail(t, args)

			// istioctl will return a single "control plane version" if all control plane versions match
			controlPlaneRegex := regexp.MustCompile(`control plane version: [a-z0-9\-]*`)
			if controlPlaneRegex.MatchString(output) {
				return
			}

			t.Logf("Did not find control plane version. This may mean components have different versions.")

			// At this point, we expect the version for each component
			expectedRegexps := []*regexp.Regexp{
				regexp.MustCompile(`citadel version: [a-z0-9\-]*`),
				regexp.MustCompile(`client version: [a-z0-9\-]*`),
				regexp.MustCompile(`egressgateway version: [a-z0-9\-]*`),
				regexp.MustCompile(`ingressgateway version: [a-z0-9\-]*`),
				regexp.MustCompile(`pilot version: [a-z0-9\-]*`),
				regexp.MustCompile(`galley version: [a-z0-9\-]*`),
				regexp.MustCompile(`policy version: [a-z0-9\-]*`),
				regexp.MustCompile(`sidecar-injector version: [a-z0-9\-]*`),
				regexp.MustCompile(`telemetry version: [a-z0-9\-]*`),
			}
			for _, regexp := range expectedRegexps {
				if !regexp.MatchString(output) {
					t.Fatalf("Output didn't match for 'istioctl %s'\n got %v\nwant: %v",
						strings.Join(args, " "), output, regexp)
				}
			}
		})
}
