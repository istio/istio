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
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/onsi/gomega"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/file"
)

const (
	describeSvcAOutput = `Service: a\..*
   Port: grpc 7070/GRPC targets pod port 7070
   Port: http 80/HTTP targets pod port 8090
7070 DestinationRule: a\..* for "a"
   Matching subsets: v1
   No Traffic Policy
7070 VirtualService: a\..*
   when headers are end-user=jason
7070 RBAC policies: ns\[.*\]-policy\[integ-test\]-rule\[0\]
80 DestinationRule: a\..* for "a"
   Matching subsets: v1
   No Traffic Policy
80 VirtualService: a\..*
   when headers are end-user=jason
`

	describePodAOutput = `Pod: .*
   Pod Ports: 7070 \(app\), 8090 \(app\), 8080 \(app\), 3333 \(app\), 15090 \(istio-proxy\)
--------------------
Service: a\..*
   Port: grpc 7070\/GRPC targets pod port 7070
   Port: http 80\/HTTP targets pod port 8090
7070 DestinationRule: a\..* for "a"
   Matching subsets: v1
   No Traffic Policy
7070 VirtualService: a\..*
   when headers are end-user=jason
7070 RBAC policies: ns\[.*\]-policy\[integ-test\]-rule\[0\]
80 DestinationRule: a\..* for "a"
   Matching subsets: v1
   No Traffic Policy
80 VirtualService: a\..*
   when headers are end-user=jason
`

	addToMeshPodAOutput = `deployment .* updated successfully with Istio sidecar injected.
Next Step: Add related labels to the deployment to align with Istio's requirement: https://istio.io/docs/setup/kubernetes/additional-setup/requirements/
`
	removeFromMeshPodAOutput = `deployment .* updated successfully with Istio sidecar un-injected.`
)

func TestWait(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "default",
				Inject: true,
			})
			ctx.ApplyConfigOrFail(t, ns.Name(), `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: reviews
spec:
  gateways: [missing-gw]
  hosts:
  - reviews
  http:
  - route:
    - destination: 
        host: reviews
`)
			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})
			istioCtl.InvokeOrFail(t, []string{"x", "wait", "VirtualService", "reviews." + ns.Name()})
		})
}

// This test requires `--istio.test.env=kube` because it tests istioctl doing PodExec
// TestVersion does "istioctl version --remote=true" to verify the CLI understands the data plane version data
func TestVersion(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			cfg := i.Settings()

			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})

			args := []string{"version", "--remote=true", fmt.Sprintf("--istioNamespace=%s", cfg.SystemNamespace)}

			output, _ := istioCtl.InvokeOrFail(t, args)

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
				regexp.MustCompile(`istiod version: [a-z0-9\-]*`),
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
		RunParallel(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(ctx, ctx, namespace.Config{
				Prefix: "istioctl-describe",
				Inject: true,
			})

			deployment := file.AsStringOrFail(t, "../istioctl/testdata/a.yaml")
			ctx.ApplyConfigOrFail(t, ns.Name(), deployment)

			var a echo.Instance
			echoboot.NewBuilderOrFail(ctx, ctx).
				With(&a, echoConfig(ns, "a")).
				BuildOrFail(ctx)

			if err := a.WaitUntilCallable(a); err != nil {
				t.Fatal(err)
			}
			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})

			podID, err := getPodID(a)
			if err != nil {
				ctx.Fatalf("Could not get Pod ID: %v", err)
			}

			var output string
			var args []string
			g := gomega.NewGomegaWithT(t)

			// When this test passed the namespace through --namespace it was flakey
			// because istioctl uses a global variable for namespace, and this test may
			// run in parallel.
			args = []string{"--namespace=dummy",
				"x", "describe", "pod", fmt.Sprintf("%s.%s", podID, ns.Name())}
			output, _ = istioCtl.InvokeOrFail(t, args)
			g.Expect(output).To(gomega.MatchRegexp(describePodAOutput))

			args = []string{"--namespace=dummy",
				"x", "describe", "svc", fmt.Sprintf("a.%s", ns.Name())}
			output, _ = istioCtl.InvokeOrFail(t, args)
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
		RunParallel(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "istioctl-add-to-mesh",
				Inject: true,
			})

			var a echo.Instance
			echoboot.NewBuilderOrFail(ctx, ctx).
				With(&a, echoConfig(ns, "a")).
				BuildOrFail(ctx)

			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})

			var output string
			var args []string
			g := gomega.NewGomegaWithT(t)

			// able to remove from mesh when the deployment is auto injected
			args = []string{fmt.Sprintf("--namespace=%s", ns.Name()),
				"x", "remove-from-mesh", "service", "a"}
			output, _ = istioCtl.InvokeOrFail(t, args)
			g.Expect(output).To(gomega.MatchRegexp(removeFromMeshPodAOutput))

			// remove from mesh should be clean
			// users can add it back to mesh successfully
			if err := a.WaitUntilCallable(a); err != nil {
				t.Fatal(err)
			}

			args = []string{fmt.Sprintf("--namespace=%s", ns.Name()),
				"x", "add-to-mesh", "service", "a"}
			output, _ = istioCtl.InvokeOrFail(t, args)
			g.Expect(output).To(gomega.MatchRegexp(addToMeshPodAOutput))
		})
}

func TestProxyConfig(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(ctx, ctx, namespace.Config{
				Prefix: "istioctl-pc",
				Inject: true,
			})

			var a echo.Instance
			echoboot.NewBuilderOrFail(ctx, ctx).
				With(&a, echoConfig(ns, "a")).
				BuildOrFail(ctx)

			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})

			podID, err := getPodID(a)
			if err != nil {
				ctx.Fatalf("Could not get Pod ID: %v", err)
			}

			var output string
			var args []string
			g := gomega.NewGomegaWithT(t)

			args = []string{"--namespace=dummy",
				"pc", "bootstrap", fmt.Sprintf("%s.%s", podID, ns.Name())}
			output, _ = istioCtl.InvokeOrFail(t, args)
			jsonOutput := jsonUnmarshallOrFail(t, strings.Join(args, " "), output)
			g.Expect(jsonOutput).To(gomega.HaveKey("bootstrap"))

			args = []string{"--namespace=dummy",
				"pc", "cluster", fmt.Sprintf("%s.%s", podID, ns.Name()), "-o", "json"}
			output, _ = istioCtl.InvokeOrFail(t, args)
			jsonOutput = jsonUnmarshallOrFail(t, strings.Join(args, " "), output)
			g.Expect(jsonOutput).To(gomega.Not(gomega.BeEmpty()))

			args = []string{"--namespace=dummy",
				"pc", "endpoint", fmt.Sprintf("%s.%s", podID, ns.Name()), "-o", "json"}
			output, _ = istioCtl.InvokeOrFail(t, args)
			jsonOutput = jsonUnmarshallOrFail(t, strings.Join(args, " "), output)
			g.Expect(jsonOutput).To(gomega.Not(gomega.BeEmpty()))

			args = []string{"--namespace=dummy",
				"pc", "listener", fmt.Sprintf("%s.%s", podID, ns.Name()), "-o", "json"}
			output, _ = istioCtl.InvokeOrFail(t, args)
			jsonOutput = jsonUnmarshallOrFail(t, strings.Join(args, " "), output)
			g.Expect(jsonOutput).To(gomega.Not(gomega.BeEmpty()))

			args = []string{"--namespace=dummy",
				"pc", "route", fmt.Sprintf("%s.%s", podID, ns.Name()), "-o", "json"}
			output, _ = istioCtl.InvokeOrFail(t, args)
			jsonOutput = jsonUnmarshallOrFail(t, strings.Join(args, " "), output)
			g.Expect(jsonOutput).To(gomega.Not(gomega.BeEmpty()))

			args = []string{"--namespace=dummy",
				"pc", "secret", fmt.Sprintf("%s.%s", podID, ns.Name()), "-o", "json"}
			output, _ = istioCtl.InvokeOrFail(t, args)
			jsonOutput = jsonUnmarshallOrFail(t, strings.Join(args, " "), output)
			g.Expect(jsonOutput).To(gomega.HaveKey("dynamicActiveSecrets"))
		})
}

func jsonUnmarshallOrFail(t *testing.T, context, s string) interface{} {
	t.Helper()
	var val interface{}

	// this is guarded by prettyPrint
	if err := json.Unmarshal([]byte(s), &val); err != nil {
		t.Fatalf("Could not unmarshal %s response %s", context, s)
	}
	return val
}

func TestProxyStatus(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(ctx, ctx, namespace.Config{
				Prefix: "istioctl-ps",
				Inject: true,
			})

			var a echo.Instance
			echoboot.NewBuilderOrFail(ctx, ctx).
				With(&a, echoConfig(ns, "a")).
				BuildOrFail(ctx)

			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})

			podID, err := getPodID(a)
			if err != nil {
				ctx.Fatalf("Could not get Pod ID: %v", err)
			}

			var output string
			var args []string
			g := gomega.NewGomegaWithT(t)

			args = []string{"proxy-status"}
			output, _ = istioCtl.InvokeOrFail(t, args)
			// Just verify pod A is known to Pilot; implicitly this verifies that
			// the printing code printed it.
			g.Expect(output).To(gomega.ContainSubstring(fmt.Sprintf("%s.%s", podID, ns.Name())))

			args = []string{
				"proxy-status", fmt.Sprintf("%s.%s", podID, ns.Name())}
			output, _ = istioCtl.InvokeOrFail(t, args)
			g.Expect(output).To(gomega.ContainSubstring("Clusters Match"))
			g.Expect(output).To(gomega.ContainSubstring("Listeners Match"))
			g.Expect(output).To(gomega.ContainSubstring("Routes Match"))
		})
}

func TestAuthZCheck(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(ctx, ctx, namespace.Config{
				Prefix: "istioctl-authz",
				Inject: true,
			})

			authPol := file.AsStringOrFail(t, "../istioctl/testdata/authz-a.yaml")
			ctx.ApplyConfigOrFail(t, ns.Name(), authPol)

			var a echo.Instance
			echoboot.NewBuilderOrFail(ctx, ctx).
				With(&a, echoConfig(ns, "a")).
				BuildOrFail(ctx)

			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})

			podID, err := getPodID(a)
			if err != nil {
				ctx.Fatalf("Could not get Pod ID: %v", err)
			}

			var output string
			var args []string
			g := gomega.NewGomegaWithT(t)

			args = []string{"experimental", "authz", "check",
				fmt.Sprintf("%s.%s", podID, ns.Name())}
			output, _ = istioCtl.InvokeOrFail(t, args)
			// Verify the output includes a policy "integ-test", which is the policy
			// loaded above from authz-a.yaml
			g.Expect(output).To(gomega.MatchRegexp("noneSDS: default.*\\[integ-test\\]"))
		})
}
