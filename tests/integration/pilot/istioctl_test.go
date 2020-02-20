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

package pilot

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/onsi/gomega"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/util/file"
)

const (
	describeSvcAOutput = `Service: a.*
   Port: grpc 7070/GRPC targets pod port 7070
   Port: http 80/HTTP targets pod port 8090
7070 DestinationRule: a for "a"
   Matching subsets: v1
   No Traffic Policy
7070 Pod is PERMISSIVE, clients configured automatically
7070 VirtualService: a.*
   when headers are end-user=jason
80 DestinationRule: a.* for "a"
   Matching subsets: v1
   No Traffic Policy
80 Pod is PERMISSIVE, clients configured automatically
80 VirtualService: a.*
   when headers are end-user=jason
`

	describePodAOutput = `Pod: .*
   Pod Ports: 7070 \(app\), 8090 \(app\), 8080 \(app\), 3333 \(app\), 15090 \(istio-proxy\)
--------------------
Service: a.*
   Port: grpc 7070\/GRPC targets pod port 7070
   Port: http 80\/HTTP targets pod port 8090
7070 DestinationRule: a for "a"
   Matching subsets: v1
   No Traffic Policy
7070 Pod is PERMISSIVE, clients configured automatically
7070 VirtualService: a.*
   when headers are end-user=jason
80 DestinationRule: a.* for "a"
   Matching subsets: v1
   No Traffic Policy
80 Pod is PERMISSIVE, clients configured automatically
80 VirtualService: a.*
   when headers are end-user=jason
`

	addToMeshPodAOutput = `deployment .* updated successfully with Istio sidecar injected.
Next Step: Add related labels to the deployment to align with Istio's requirement: https://istio.io/docs/setup/kubernetes/additional-setup/requirements/
`
	removeFromMeshPodAOutput = `deployment .* updated successfully with Istio sidecar un-injected.`
)

// This test requires `--istio.test.env=kube` because it tests istioctl doing PodExec
// TestVersion does "istioctl version --remote=true" to verify the CLI understands the data plane version data
func TestVersion(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			g := galley.NewOrFail(t, ctx, galley.Config{})
			_ = pilot.NewOrFail(t, ctx, pilot.Config{Galley: g})
			cfg := i.Settings()

			istioCtl := istioctl.NewOrFail(t, ctx, istioctl.Config{})

			args := []string{"version", "--remote=true", fmt.Sprintf("--istioNamespace=%s", cfg.SystemNamespace)}

			output := istioCtl.InvokeOrFail(t, args)

			// istioctl will return a single "control plane version" if all control plane versions match
			controlPlaneRegex := regexp.MustCompile(`control plane version: [a-z0-9\-]*`)
			if controlPlaneRegex.MatchString(output) {
				return
			}

			ctx.Logf("Did not find control plane version. This may mean components have different versions.")

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
					ctx.Fatalf("Output didn't match for 'istioctl %s'\n got %v\nwant: %v",
						strings.Join(args, " "), output, regexp)
				}
			}
		})
}

func TestDescribe(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		RunParallel(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(ctx, ctx, namespace.Config{
				Prefix: "istioctl-describe",
				Inject: true,
			})

			deployment := file.AsStringOrFail(t, "../istioctl/testdata/a.yaml")
			g.ApplyConfigOrFail(t, ns, deployment)

			var a echo.Instance
			echoboot.NewBuilderOrFail(ctx, ctx).
				With(&a, echoConfig(ns, "a")).
				BuildOrFail(ctx)

			if err := a.WaitUntilCallable(a); err != nil {
				t.Fatal(err)
			}
			istioCtl := istioctl.NewOrFail(t, ctx, istioctl.Config{})

			podID, err := getPodID(a)
			if err != nil {
				ctx.Fatalf("Could not get Pod ID: %v", err)
			}

			var output string
			var args []string
			g := gomega.NewGomegaWithT(t)

			args = []string{fmt.Sprintf("--namespace=%s", ns.Name()),
				"x", "describe", "pod", podID}
			output = istioCtl.InvokeOrFail(t, args)
			g.Expect(output).To(gomega.MatchRegexp(describePodAOutput))

			args = []string{fmt.Sprintf("--namespace=%s", ns.Name()),
				"x", "describe", "svc", "a"}
			output = istioCtl.InvokeOrFail(t, args)
			g.Expect(output).To(gomega.MatchRegexp(describeSvcAOutput))
		})
}

func getPodID(i echo.Instance) (string, error) {
	wls, err := i.Workloads()
	if err != nil {
		return "", nil
	}

	for _, wl := range wls {
		hostname := strings.Split(wl.Sidecar().NodeID(), "~")[2]
		podID := strings.Split(hostname, ".")[0]
		return podID, nil
	}

	return "", fmt.Errorf("no workloads")
}

func TestAddToAndRemoveFromMesh(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		RunParallel(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "istioctl-add-to-mesh",
				Inject: true,
			})

			var a echo.Instance
			echoboot.NewBuilderOrFail(ctx, ctx).
				With(&a, echoConfig(ns, "a")).
				BuildOrFail(ctx)

			if err := a.WaitUntilCallable(a); err != nil {
				t.Fatal(err)
			}
			istioCtl := istioctl.NewOrFail(t, ctx, istioctl.Config{})

			var output string
			var args []string
			g := gomega.NewGomegaWithT(t)

			// able to remove from mesh when the deployment is auto injected
			args = []string{fmt.Sprintf("--namespace=%s", ns.Name()),
				"x", "remove-from-mesh", "service", "a"}
			output = istioCtl.InvokeOrFail(t, args)
			g.Expect(output).To(gomega.MatchRegexp(removeFromMeshPodAOutput))

			// remove from mesh should be clean
			// users can add it back to mesh successfully
			if err := a.WaitUntilCallable(a); err != nil {
				t.Fatal(err)
			}

			args = []string{fmt.Sprintf("--namespace=%s", ns.Name()),
				"x", "add-to-mesh", "service", "a"}
			output = istioCtl.InvokeOrFail(t, args)
			g.Expect(output).To(gomega.MatchRegexp(addToMeshPodAOutput))
		})
}
