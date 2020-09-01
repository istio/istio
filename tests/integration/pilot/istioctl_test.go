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
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	admin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	"github.com/golang/protobuf/jsonpb"

	"istio.io/istio/pkg/test/framework/components/environment/kube"

	"github.com/onsi/gomega"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	describeSvcAOutput = regexp.MustCompile(`Service: a\..*
   Port: http 80/HTTP targets pod port 80
   Port: grpc 7070/GRPC targets pod port 7070
   Port: tcp 9090/TCP targets pod port 9090
   Port: auto-tcp 9091/UnsupportedProtocol targets pod port 9091
   Port: auto-http 81/UnsupportedProtocol targets pod port 81
   Port: auto-grpc 7071/UnsupportedProtocol targets pod port 7071
80 DestinationRule: a\..* for "a"
   Matching subsets: v1
   No Traffic Policy
80 VirtualService: a\..*
   when headers are end-user=jason
80 RBAC policies: ns\[.*\]-policy\[integ-test\]-rule\[0\]
7070 DestinationRule: a\..* for "a"
   Matching subsets: v1
   No Traffic Policy
7070 VirtualService: a\..*
   when headers are end-user=jason
7070 RBAC policies: ns\[.*\]-policy\[integ-test\]-rule\[0\]
9090 DestinationRule: a\..* for "a"
   Matching subsets: v1
   No Traffic Policy
9090 RBAC policies: ns\[.*\]-policy\[integ-test\]-rule\[0\]
9091 DestinationRule: a\..* for "a"
   Matching subsets: v1
   No Traffic Policy
81 DestinationRule: a\..* for "a"
   Matching subsets: v1
   No Traffic Policy
7071 DestinationRule: a\..* for "a"
   Matching subsets: v1
   No Traffic Policy
`)

	describePodAOutput = regexp.MustCompile(`Pod: .*
   Pod Ports: 80 \(app\), 7070 \(app\), .* 15090 \(istio-proxy\)
--------------------
Service: a\..*
   Port: http 80/HTTP targets pod port 80
   Port: grpc 7070/GRPC targets pod port 7070
   Port: tcp 9090/TCP targets pod port 9090
   Port: auto-tcp 9091/UnsupportedProtocol targets pod port 9091
   Port: auto-http 81/UnsupportedProtocol targets pod port 81
   Port: auto-grpc 7071/UnsupportedProtocol targets pod port 7071
80 DestinationRule: a\..* for "a"
   Matching subsets: v1
   No Traffic Policy
80 VirtualService: a\..*
   when headers are end-user=jason
80 RBAC policies: ns\[.*\]-policy\[integ-test\]-rule\[0\]
7070 DestinationRule: a\..* for "a"
   Matching subsets: v1
   No Traffic Policy
7070 VirtualService: a\..*
   when headers are end-user=jason
7070 RBAC policies: ns\[.*\]-policy\[integ-test\]-rule\[0\]
9090 DestinationRule: a\..* for "a"
   Matching subsets: v1
   No Traffic Policy
9090 RBAC policies: ns\[.*\]-policy\[integ-test\]-rule\[0\]
9091 DestinationRule: a\..* for "a"
   Matching subsets: v1
   No Traffic Policy
81 DestinationRule: a\..* for "a"
   Matching subsets: v1
   No Traffic Policy
7071 DestinationRule: a\..* for "a"
   Matching subsets: v1
   No Traffic Policy
`)

	addToMeshPodAOutput = `deployment .* updated successfully with Istio sidecar injected.
Next Step: Add related labels to the deployment to align with Istio's requirement: https://istio.io/docs/setup/kubernetes/additional-setup/requirements/
`
	removeFromMeshPodAOutput = `deployment .* updated successfully with Istio sidecar un-injected.`
)

func TestWait(t *testing.T) {
	framework.NewTest(t).Features("usability.observability.wait").
		RequiresSingleCluster().
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "default",
				Inject: true,
			})
			ctx.Config().ApplyYAMLOrFail(t, ns.Name(), `
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
			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{Cluster: ctx.Environment().Clusters()[0]})
			istioCtl.InvokeOrFail(t, []string{"x", "wait", "VirtualService", "reviews." + ns.Name()})
		})
}

// This test requires `--istio.test.env=kube` because it tests istioctl doing PodExec
// TestVersion does "istioctl version --remote=true" to verify the CLI understands the data plane version data
func TestVersion(t *testing.T) {
	framework.
		NewTest(t).Features("usability.observability.version").
		RequiresSingleCluster().
		Run(func(ctx framework.TestContext) {
			cfg := i.Settings()

			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{Cluster: ctx.Environment().Clusters()[0]})
			args := []string{"version", "--remote=true", fmt.Sprintf("--istioNamespace=%s", cfg.SystemNamespace)}

			output, _ := istioCtl.InvokeOrFail(t, args)

			// istioctl will return a single "control plane version" if all control plane versions match
			controlPlaneRegex := regexp.MustCompile(`control plane version: [a-z0-9\-]*`)
			if controlPlaneRegex.MatchString(output) {
				return
			}

			ctx.Fatalf("Did not find control plane version: %v", output)

		})
}

func TestDescribe(t *testing.T) {
	framework.NewTest(t).Features("usability.observability.describe").
		RequiresSingleCluster().
		Run(func(ctx framework.TestContext) {
			deployment := file.AsStringOrFail(t, "../istioctl/testdata/a.yaml")
			ctx.Config().ApplyYAMLOrFail(t, echoNamespace.Name(), deployment)
			defer ctx.Config().DeleteYAMLOrFail(t, echoNamespace.Name(), deployment)

			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})

			podID, err := getPodID(a)
			if err != nil {
				ctx.Fatalf("Could not get Pod ID: %v", err)
			}

			// When this test passed the namespace through --namespace it was flakey
			// because istioctl uses a global variable for namespace, and this test may
			// run in parallel.
			retry.UntilSuccessOrFail(ctx, func() error {
				args := []string{"--namespace=dummy",
					"x", "describe", "svc", fmt.Sprintf("a.%s", echoNamespace.Name())}
				output, _, err := istioCtl.Invoke(args)
				if err != nil {
					return err
				}
				if !describeSvcAOutput.MatchString(output) {
					return fmt.Errorf("output:\n%v\n does not match regex:\n%v", output, describeSvcAOutput)
				}
				return nil
			}, retry.Timeout(time.Second*5))

			retry.UntilSuccessOrFail(ctx, func() error {
				args := []string{"--namespace=dummy",
					"x", "describe", "pod", fmt.Sprintf("%s.%s", podID, echoNamespace.Name())}
				output, _, err := istioCtl.Invoke(args)
				if err != nil {
					return err
				}
				if !describePodAOutput.MatchString(output) {
					return fmt.Errorf("output:\n%v\n does not match regex:\n%v", output, describeSvcAOutput)
				}
				return nil
			}, retry.Timeout(time.Second*5))
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
	framework.NewTest(t).Features("usability.helpers.add-to-mesh", "usability.helpers.remove-from-mesh").
		RequiresSingleCluster().
		RunParallel(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "istioctl-add-to-mesh",
				Inject: true,
			})

			var a echo.Instance
			echoboot.NewBuilderOrFail(ctx, ctx).
				With(&a, echoConfig(ns, "a")).
				BuildOrFail(ctx)

			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{Cluster: ctx.Environment().Clusters()[0]})

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
	framework.NewTest(t).Features("usability.observability.proxy-config").
		RequiresSingleCluster().
		Run(func(ctx framework.TestContext) {
			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})

			podID, err := getPodID(a)
			if err != nil {
				ctx.Fatalf("Could not get Pod ID: %v", err)
			}

			var output string
			var args []string
			g := gomega.NewGomegaWithT(t)

			args = []string{"--namespace=dummy",
				"pc", "bootstrap", fmt.Sprintf("%s.%s", podID, echoNamespace.Name())}
			output, _ = istioCtl.InvokeOrFail(t, args)
			jsonOutput := jsonUnmarshallOrFail(t, strings.Join(args, " "), output)
			g.Expect(jsonOutput).To(gomega.HaveKey("bootstrap"))

			args = []string{"--namespace=dummy",
				"pc", "cluster", fmt.Sprintf("%s.%s", podID, echoNamespace.Name()), "-o", "json"}
			output, _ = istioCtl.InvokeOrFail(t, args)
			jsonOutput = jsonUnmarshallOrFail(t, strings.Join(args, " "), output)
			g.Expect(jsonOutput).To(gomega.Not(gomega.BeEmpty()))

			args = []string{"--namespace=dummy",
				"pc", "endpoint", fmt.Sprintf("%s.%s", podID, echoNamespace.Name()), "-o", "json"}
			output, _ = istioCtl.InvokeOrFail(t, args)
			jsonOutput = jsonUnmarshallOrFail(t, strings.Join(args, " "), output)
			g.Expect(jsonOutput).To(gomega.Not(gomega.BeEmpty()))

			args = []string{"--namespace=dummy",
				"pc", "listener", fmt.Sprintf("%s.%s", podID, echoNamespace.Name()), "-o", "json"}
			output, _ = istioCtl.InvokeOrFail(t, args)
			jsonOutput = jsonUnmarshallOrFail(t, strings.Join(args, " "), output)
			g.Expect(jsonOutput).To(gomega.Not(gomega.BeEmpty()))

			args = []string{"--namespace=dummy",
				"pc", "route", fmt.Sprintf("%s.%s", podID, echoNamespace.Name()), "-o", "json"}
			output, _ = istioCtl.InvokeOrFail(t, args)
			jsonOutput = jsonUnmarshallOrFail(t, strings.Join(args, " "), output)
			g.Expect(jsonOutput).To(gomega.Not(gomega.BeEmpty()))

			args = []string{"--namespace=dummy",
				"pc", "secret", fmt.Sprintf("%s.%s", podID, echoNamespace.Name()), "-o", "json"}
			output, _ = istioCtl.InvokeOrFail(t, args)
			jsonOutput = jsonUnmarshallOrFail(t, strings.Join(args, " "), output)
			g.Expect(jsonOutput).To(gomega.HaveKey("dynamicActiveSecrets"))
			dump := &admin.SecretsConfigDump{}
			if err := jsonpb.UnmarshalString(output, dump); err != nil {
				t.Fatal(err)
			}
			if len(dump.DynamicWarmingSecrets) > 0 {
				t.Fatalf("found warming secrets: %v", output)
			}
			if len(dump.DynamicActiveSecrets) != 2 {
				// If the config for the SDS does not align in all locations, we may get duplicates.
				// This check ensures we do not. If this is failing, check to ensure the bootstrap config matches
				// the XDS response.
				t.Fatalf("found unexpected secrets, should have only default and ROOTCA: %v", output)
			}
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
	framework.NewTest(t).Features("usability.observability.proxy-status").
		RequiresSingleCluster().
		Run(func(ctx framework.TestContext) {
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
			g.Expect(output).To(gomega.ContainSubstring(fmt.Sprintf("%s.%s", podID, echoNamespace.Name())))

			args = []string{
				"proxy-status", fmt.Sprintf("%s.%s", podID, echoNamespace.Name())}
			output, _ = istioCtl.InvokeOrFail(t, args)
			g.Expect(output).To(gomega.ContainSubstring("Clusters Match"))
			g.Expect(output).To(gomega.ContainSubstring("Listeners Match"))
			g.Expect(output).To(gomega.ContainSubstring("Routes Match"))

			// test the --file param
			filename := "ps-configdump.json"
			cs := ctx.Environment().(*kube.Environment).KubeClusters[0]
			dump, err := cs.EnvoyDo(context.TODO(), podID, echoNamespace.Name(), "GET", "config_dump", nil)
			g.Expect(err).ShouldNot(gomega.HaveOccurred())
			err = ioutil.WriteFile(filename, dump, os.ModePerm)
			g.Expect(err).ShouldNot(gomega.HaveOccurred())
			args = []string{
				"proxy-status", fmt.Sprintf("%s.%s", podID, echoNamespace.Name()), "--file", filename}
			output, _ = istioCtl.InvokeOrFail(t, args)
			g.Expect(output).To(gomega.ContainSubstring("Clusters Match"))
			g.Expect(output).To(gomega.ContainSubstring("Listeners Match"))
			g.Expect(output).To(gomega.ContainSubstring("Routes Match"))
		})
}

func TestAuthZCheck(t *testing.T) {
	framework.NewTest(t).Features("usability.observability.authz-check").
		RequiresSingleCluster().
		Run(func(ctx framework.TestContext) {
			appPolicy := file.AsStringOrFail(t, "../istioctl/testdata/authz-a.yaml")
			gwPolicy := file.AsStringOrFail(t, "../istioctl/testdata/authz-b.yaml")
			ctx.Config().ApplyYAMLOrFail(t, echoNamespace.Name(), appPolicy)
			ctx.Config().ApplyYAMLOrFail(t, i.Settings().IngressNamespace, gwPolicy)
			defer func() {
				ctx.Config().DeleteYAMLOrFail(t, echoNamespace.Name(), appPolicy)
				ctx.Config().DeleteYAMLOrFail(t, i.Settings().IngressNamespace, gwPolicy)
			}()

			gwPods, err := ctx.Clusters().Default().GetIstioPods(
				context.TODO(), i.Settings().IngressNamespace, map[string]string{
					"labelSelector": "istio=ingressgateway",
					"fieldSelector": "status.phase=Running",
				})
			if err != nil {
				ctx.Fatalf("Could not get Pod ID: %v", err)
			}
			if len(gwPods) < 1 {
				ctx.Fatal("Could not get ingress gateway pods")
			}
			appPod, err := getPodID(a)
			if err != nil {
				ctx.Fatalf("Could not get Pod ID: %v", err)
			}

			cases := []struct {
				name  string
				pod   string
				wants []*regexp.Regexp
			}{
				{
					name: "ingressgateway",
					pod:  fmt.Sprintf("%s.%s", gwPods[0].Name, i.Settings().IngressNamespace),
					wants: []*regexp.Regexp{
						regexp.MustCompile(fmt.Sprintf(`DENY\s+deny-policy\.%s\s+2`, i.Settings().SystemNamespace)),
						regexp.MustCompile(fmt.Sprintf(`ALLOW\s+allow-policy\.%s\s+1`, i.Settings().SystemNamespace)),
					},
				},
				{
					name: "workload",
					pod:  fmt.Sprintf("%s.%s", appPod, echoNamespace.Name()),
					wants: []*regexp.Regexp{
						regexp.MustCompile(fmt.Sprintf(`DENY\s+deny-policy\.%s\s+2`, echoNamespace.Name())),
						regexp.MustCompile(`ALLOW\s+_anonymous_match_nothing_\s+1`),
						regexp.MustCompile(fmt.Sprintf(`ALLOW\s+allow-policy\.%s\s+1`, echoNamespace.Name())),
					},
				},
			}

			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{Cluster: ctx.Environment().Clusters()[0]})
			for _, c := range cases {
				args := []string{"experimental", "authz", "check", c.pod}
				ctx.NewSubTest(c.name).Run(func(ctx framework.TestContext) {
					// Verify the output matches the expected text, which is the policies loaded above.
					retry.UntilSuccessOrFail(ctx, func() error {
						output, errMsg := istioCtl.InvokeOrFail(t, args)
						for _, want := range c.wants {
							if !want.MatchString(output) {
								return fmt.Errorf("%v did not match %v: %s", output, want, errMsg)
							}
						}
						return nil
					}, retry.Timeout(time.Second*5))
				})
			}
		})
}
