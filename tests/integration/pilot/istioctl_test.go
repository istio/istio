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

package pilot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	admin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	commonDeployment "istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/protomarshal"
)

var (
	// The full describe output is much larger, but testing for it requires a change anytime the test
	// app changes which is tedious. Instead, just check a minimum subset; unit test cover the
	// details.
	describeSvcAOutput = regexp.MustCompile(`(?s)Service: a\..*
   Port: http 80/HTTP targets pod port 18080
.*
80:
   DestinationRule: a\..* for "a"
      Matching subsets: v1
      No Traffic Policy
`)

	describePodAOutput = describeSvcAOutput
)

// This test requires `--istio.test.env=kube` because it tests istioctl doing PodExec
// TestVersion does "istioctl version --remote=true" to verify the CLI understands the data plane version data
func TestVersion(t *testing.T) {
	// nolint: staticcheck
	framework.
		NewTest(t).RequiresSingleCluster().
		Run(func(t framework.TestContext) {
			cfg := i.Settings()

			istioCtl := istioctl.NewOrFail(t, istioctl.Config{Cluster: t.Environment().Clusters()[0]})
			args := []string{"version", "--remote=true", fmt.Sprintf("--istioNamespace=%s", cfg.SystemNamespace)}

			output, _ := istioCtl.InvokeOrFail(t, args)

			// istioctl will return a single "control plane version" if all control plane versions match
			controlPlaneRegex := regexp.MustCompile(`control plane version: [a-z0-9\-]*`)
			if controlPlaneRegex.MatchString(output) {
				return
			}

			t.Fatalf("Did not find control plane version: %v", output)
		})
}

// This test requires `--istio.test.env=kube` because it tests istioctl doing PodExec
// TestVersion does "istioctl version --remote=true" to verify the CLI understands the data plane version data
func TestXdsVersion(t *testing.T) {
	// nolint: staticcheck
	framework.
		NewTest(t).RequiresSingleCluster().
		RequireIstioVersion("1.10.0").
		Run(func(t framework.TestContext) {
			cfg := i.Settings()

			istioCtl := istioctl.NewOrFail(t, istioctl.Config{Cluster: t.Clusters().Default()})
			args := []string{"x", "version", "--remote=true", fmt.Sprintf("--istioNamespace=%s", cfg.SystemNamespace)}

			output, _ := istioCtl.InvokeOrFail(t, args)

			// istioctl will return a single "control plane version" if all control plane versions match.
			// This test accepts any version with a "." (period) in it -- we mostly want to fail on "MISSING CP VERSION"
			controlPlaneRegex := regexp.MustCompile(`control plane version: [a-z0-9\-]+\.[a-z0-9\-]+`)
			if controlPlaneRegex.MatchString(output) {
				return
			}

			t.Fatalf("Did not find valid control plane version: %v", output)
		})
}

func TestDescribe(t *testing.T) {
	// nolint: staticcheck
	framework.NewTest(t).RequiresSingleCluster().
		Run(func(t framework.TestContext) {
			t.ConfigIstio().File(apps.Namespace.Name(), "testdata/a.yaml").ApplyOrFail(t)

			istioCtl := istioctl.NewOrFail(t, istioctl.Config{})

			// When this test passed the namespace through --namespace it was flakey
			// because istioctl uses a global variable for namespace, and this test may
			// run in parallel.
			retry.UntilSuccessOrFail(t, func() error {
				args := []string{
					"--namespace=dummy",
					"x", "describe", "svc", fmt.Sprintf("%s.%s", commonDeployment.ASvc, apps.Namespace.Name()),
				}
				output, _, err := istioCtl.Invoke(args)
				if err != nil {
					return err
				}
				if !describeSvcAOutput.MatchString(output) {
					return fmt.Errorf("output:\n%v\n does not match regex:\n%v", output, describeSvcAOutput)
				}
				return nil
			}, retry.Timeout(time.Second*20))

			retry.UntilSuccessOrFail(t, func() error {
				podID, err := getPodID(apps.A[0])
				if err != nil {
					return fmt.Errorf("could not get Pod ID: %v", err)
				}
				args := []string{
					"--namespace=dummy",
					"x", "describe", "pod", fmt.Sprintf("%s.%s", podID, apps.Namespace.Name()),
				}
				output, _, err := istioCtl.Invoke(args)
				if err != nil {
					return err
				}
				if !describePodAOutput.MatchString(output) {
					return fmt.Errorf("output:\n%v\n does not match regex:\n%v", output, describePodAOutput)
				}
				return nil
			}, retry.Timeout(time.Second*20))
		})
}

func getPodID(i echo.Instance) (string, error) {
	wls, err := i.Workloads()
	if err != nil {
		return "", nil
	}

	for _, wl := range wls {
		return wl.PodName(), nil
	}

	return "", fmt.Errorf("no workloads")
}

func TestProxyConfig(t *testing.T) {
	// nolint: staticcheck
	framework.NewTest(t).RequiresSingleCluster().
		Run(func(t framework.TestContext) {
			istioCtl := istioctl.NewOrFail(t, istioctl.Config{})

			podID, err := getPodID(apps.A[0])
			if err != nil {
				t.Fatalf("Could not get Pod ID: %v", err)
			}

			var output string
			var args []string
			g := NewWithT(t)

			args = []string{
				"--namespace=dummy",
				"pc", "bootstrap", fmt.Sprintf("%s.%s", podID, apps.Namespace.Name()),
			}
			output, _ = istioCtl.InvokeOrFail(t, args)
			jsonOutput := jsonUnmarshallOrFail(t, strings.Join(args, " "), output)
			g.Expect(jsonOutput).To(HaveKey("bootstrap"))

			args = []string{
				"--namespace=dummy",
				"pc", "cluster", fmt.Sprintf("%s.%s", podID, apps.Namespace.Name()), "-o", "json",
			}
			output, _ = istioCtl.InvokeOrFail(t, args)
			jsonOutput = jsonUnmarshallOrFail(t, strings.Join(args, " "), output)
			g.Expect(jsonOutput).To(Not(BeEmpty()))

			args = []string{
				"--namespace=dummy",
				"pc", "endpoint", fmt.Sprintf("%s.%s", podID, apps.Namespace.Name()), "-o", "json",
			}
			output, _ = istioCtl.InvokeOrFail(t, args)
			jsonOutput = jsonUnmarshallOrFail(t, strings.Join(args, " "), output)
			g.Expect(jsonOutput).To(Not(BeEmpty()))

			args = []string{
				"--namespace=dummy",
				"pc", "listener", fmt.Sprintf("%s.%s", podID, apps.Namespace.Name()), "-o", "json",
			}
			output, _ = istioCtl.InvokeOrFail(t, args)
			jsonOutput = jsonUnmarshallOrFail(t, strings.Join(args, " "), output)
			g.Expect(jsonOutput).To(Not(BeEmpty()))

			args = []string{
				"--namespace=dummy",
				"pc", "route", fmt.Sprintf("%s.%s", podID, apps.Namespace.Name()), "-o", "json",
			}
			output, _ = istioCtl.InvokeOrFail(t, args)
			jsonOutput = jsonUnmarshallOrFail(t, strings.Join(args, " "), output)
			g.Expect(jsonOutput).To(Not(BeEmpty()))

			args = []string{
				"--namespace=dummy",
				"pc", "all", fmt.Sprintf("%s.%s", podID, apps.Namespace.Name()), "-o", "json",
			}
			output, _ = istioCtl.InvokeOrFail(t, args)
			jsonOutput = jsonUnmarshallOrFail(t, strings.Join(args, " "), output)
			dumpAll, ok := jsonOutput.(map[string]any)
			if !ok {
				t.Fatalf("Failed to parse istioctl %s config dump to top level map", strings.Join(args, " "))
			}
			rawConfigs, ok := dumpAll["configs"].([]any)
			if !ok {
				t.Fatalf("Failed to parse istioctl %s config dump to slice of any", strings.Join(args, " "))
			}
			hasEndpoints := false
			for _, rawConfig := range rawConfigs {
				configDump, ok := rawConfig.(map[string]any)
				if !ok {
					t.Fatalf("Failed to parse istioctl %s raw config dump element to map of any", strings.Join(args, " "))
				}
				if configDump["@type"] == "type.googleapis.com/envoy.admin.v3.EndpointsConfigDump" {
					hasEndpoints = true
					break
				}
			}

			g.Expect(hasEndpoints).To(BeTrue())

			args = []string{
				"--namespace=dummy",
				"pc", "secret", fmt.Sprintf("%s.%s", podID, apps.Namespace.Name()), "-o", "json",
			}
			output, _ = istioCtl.InvokeOrFail(t, args)
			jsonOutput = jsonUnmarshallOrFail(t, strings.Join(args, " "), output)
			g.Expect(jsonOutput).To(HaveKey("dynamicActiveSecrets"))
			dump := &admin.SecretsConfigDump{}
			if err := protomarshal.Unmarshal([]byte(output), dump); err != nil {
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

func jsonUnmarshallOrFail(t test.Failer, context, s string) any {
	t.Helper()
	var val any

	// this is guarded by prettyPrint
	if err := json.Unmarshal([]byte(s), &val); err != nil {
		t.Fatalf("Could not unmarshal %s response %s", context, s)
	}
	return val
}

func TestProxyStatus(t *testing.T) {
	// nolint: staticcheck
	framework.NewTest(t).RequiresSingleCluster().
		RequiresLocalControlPlane(). // https://github.com/istio/istio/issues/37051
		Run(func(t framework.TestContext) {
			const timeoutFlag = "--timeout=10s"
			istioCtl := istioctl.NewOrFail(t, istioctl.Config{})

			podID, err := getPodID(apps.A[0])
			if err != nil {
				t.Fatalf("Could not get Pod ID: %v", err)
			}

			var output string
			var args []string

			expectSubstrings := func(have string, wants ...string) error {
				for _, want := range wants {
					if !strings.Contains(have, want) {
						return fmt.Errorf("substring %q not found; have %q", want, have)
					}
				}
				return nil
			}
			retry.UntilSuccessOrFail(t, func() error {
				args = []string{"proxy-status", timeoutFlag}
				output, _ = istioCtl.InvokeOrFail(t, args)
				return expectSubstrings(output, fmt.Sprintf("%s.%s", podID, apps.Namespace.Name()))
			})

			retry.UntilSuccessOrFail(t, func() error {
				args = []string{
					"proxy-status", fmt.Sprintf("%s.%s", podID, apps.Namespace.Name()), timeoutFlag,
				}
				output, _, err := istioCtl.Invoke(args)
				if err != nil {
					return err
				}
				return expectSubstrings(output, "Clusters Match", "Listeners Match", "Routes Match")
			})

			// test the --file param
			retry.UntilSuccessOrFail(t, func() error {
				d := t.TempDir()
				filename := filepath.Join(d, "ps-configdump.json")
				cs := t.Clusters().Default()
				dump, err := cs.EnvoyDoWithPort(context.TODO(), podID, apps.Namespace.Name(), "GET", "config_dump", kube.DefaultProxyAdminPort)
				if err != nil {
					return err
				}
				err = os.WriteFile(filename, dump, os.ModePerm)
				if err != nil {
					return err
				}
				args = []string{
					"proxy-status", fmt.Sprintf("%s.%s", podID, apps.Namespace.Name()), "--file", filename, timeoutFlag,
				}
				output, _, err = istioCtl.Invoke(args)
				if err != nil {
					return err
				}
				return expectSubstrings(output, "Clusters Match", "Listeners Match", "Routes Match")
			})

			// test namespace filtering
			retry.UntilSuccessOrFail(t, func() error {
				args = []string{"proxy-status", "-n", apps.Namespace.Name(), timeoutFlag}
				output, _ = istioCtl.InvokeOrFail(t, args)
				return expectSubstrings(output, fmt.Sprintf("%s.%s", podID, apps.Namespace.Name()))
			})
		})
}

func TestAuthZCheck(t *testing.T) {
	// nolint: staticcheck
	framework.NewTest(t).RequiresSingleCluster().
		Run(func(t framework.TestContext) {
			istioLabel := "ingressgateway"
			if labelOverride := i.Settings().IngressGatewayIstioLabel; labelOverride != "" {
				istioLabel = labelOverride
			}
			t.ConfigIstio().File(apps.Namespace.Name(), "testdata/authz-a.yaml").ApplyOrFail(t)
			t.ConfigIstio().EvalFile(i.Settings().SystemNamespace, map[string]any{
				"GatewayIstioLabel": istioLabel,
			}, "testdata/authz-b.yaml").ApplyOrFail(t)

			ingress := i.IngressFor(t.Clusters().Default())
			gwPod, err := ingress.PodID(0)
			if err != nil {
				t.Fatalf("Could not get Pod ID: %v", err)
			}
			appPod, err := getPodID(apps.A[0])
			if err != nil {
				t.Fatalf("Could not get Pod ID: %v", err)
			}

			cases := []struct {
				name  string
				pod   string
				wants []*regexp.Regexp
			}{
				{
					name: "ingressgateway",
					pod:  fmt.Sprintf("%s.%s", gwPod, ingress.Namespace()),
					wants: []*regexp.Regexp{
						regexp.MustCompile(fmt.Sprintf(`DENY\s+deny-policy\.%s\s+2`, i.Settings().SystemNamespace)),
						regexp.MustCompile(fmt.Sprintf(`ALLOW\s+allow-policy\.%s\s+1`, i.Settings().SystemNamespace)),
					},
				},
				{
					name: "workload",
					pod:  fmt.Sprintf("%s.%s", appPod, apps.Namespace.Name()),
					wants: []*regexp.Regexp{
						regexp.MustCompile(fmt.Sprintf(`DENY\s+deny-policy\.%s\s+2`, apps.Namespace.Name())),
						regexp.MustCompile(`ALLOW\s+_anonymous_match_nothing_\s+1`),
						regexp.MustCompile(fmt.Sprintf(`ALLOW\s+allow-policy\.%s\s+1`, apps.Namespace.Name())),
					},
				},
			}

			istioCtl := istioctl.NewOrFail(t, istioctl.Config{Cluster: t.Clusters().Default()})
			for _, c := range cases {
				args := []string{"experimental", "authz", "check", c.pod}
				t.NewSubTest(c.name).Run(func(t framework.TestContext) {
					// Verify the output matches the expected text, which is the policies loaded above.
					retry.UntilSuccessOrFail(t, func() error {
						output, _, err := istioCtl.Invoke(args)
						if err != nil {
							return err
						}
						for _, want := range c.wants {
							if !want.MatchString(output) {
								return fmt.Errorf("%v did not match %v", output, want)
							}
						}
						return nil
					}, retry.Timeout(time.Second*30))
				})
			}
		})
}

func TestKubeInject(t *testing.T) {
	// nolint: staticcheck
	framework.NewTest(t).RequiresSingleCluster().
		Run(func(t framework.TestContext) {
			istioCtl := istioctl.NewOrFail(t, istioctl.Config{})
			var output string
			args := []string{"kube-inject", "-f", "testdata/hello.yaml", "--revision=" + t.Settings().Revisions.Default()}
			output, _ = istioCtl.InvokeOrFail(t, args)
			if !strings.Contains(output, "istio-proxy") {
				t.Fatal("istio-proxy has not been injected")
			}
		})
}

func TestRemoteClusters(t *testing.T) {
	// nolint: staticcheck
	framework.NewTest(t).RequiresMinClusters(2).
		Run(func(t framework.TestContext) {
			for _, cluster := range t.Clusters().Primaries() {
				t.NewSubTest(cluster.StableName()).Run(func(t framework.TestContext) {
					istioCtl := istioctl.NewOrFail(t, istioctl.Config{Cluster: cluster})
					var output string
					args := []string{"remote-clusters"}
					output, _ = istioCtl.InvokeOrFail(t, args)
					for _, otherName := range t.Clusters().Exclude(cluster).Names() {
						if !strings.Contains(output, otherName) {
							t.Fatalf("remote-clusters output did not contain %s; got:\n%s", otherName, output)
						}
					}
				})
			}
		})
}
