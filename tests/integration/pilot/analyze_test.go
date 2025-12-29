//go:build integ

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
	"os"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"

	"istio.io/istio/istioctl/pkg/analyze"
	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/tests/integration/helm"
)

const (
	gatewayFile          = "testdata/gateway.yaml"
	jsonGatewayFile      = "testdata/gateway.json"
	destinationRuleFile  = "testdata/destinationrule.yaml"
	virtualServiceFile   = "testdata/virtualservice.yaml"
	invalidFile          = "testdata/invalid.yaml"
	invalidExtensionFile = "testdata/invalid.md"
	dirWithConfig        = "testdata/some-dir/"
	jsonOutput           = "-ojson"
)

var analyzerFoundIssuesError = analyze.AnalyzerFoundIssuesError{}

func TestEmptyCluster(t *testing.T) {
	// nolint: staticcheck
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Run(func(t framework.TestContext) {
			g := NewWithT(t)

			ns := namespace.NewOrFail(t, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			istioCtl := istioctl.NewOrFail(t, istioctl.Config{})

			// For a clean istio install with injection enabled, expect no validation errors
			output, err := istioctlSafe(t, istioCtl, ns.Name(), true)
			expectNoMessages(t, g, output)
			g.Expect(err).To(BeNil())
		})
}

func TestFileOnly(t *testing.T) {
	// nolint: staticcheck
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Run(func(t framework.TestContext) {
			g := NewWithT(t)

			ns := namespace.NewOrFail(t, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			istioCtl := istioctl.NewOrFail(t, istioctl.Config{})

			// Validation error if we have a virtual service with subset not defined.
			output, err := istioctlSafe(t, istioCtl, ns.Name(), false, virtualServiceFile)
			expectMessages(t, g, output, msg.ReferencedResourceNotFound)
			g.Expect(err).To(BeIdenticalTo(analyzerFoundIssuesError))

			// Error goes away if we define the subset in the destination rule.
			output, err = istioctlSafe(t, istioCtl, ns.Name(), false, destinationRuleFile)
			expectNoMessages(t, g, output)
			g.Expect(err).To(BeNil())
		})
}

func TestDirectory(t *testing.T) {
	// nolint: staticcheck
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Run(func(t framework.TestContext) {
			g := NewWithT(t)

			ns := namespace.NewOrFail(t, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			istioCtl := istioctl.NewOrFail(t, istioctl.Config{})

			// Hardcore recursive to true, so we should see one error (SchemaValidationError).
			output, err := istioctlSafe(t, istioCtl, ns.Name(), false, dirWithConfig)
			expectMessages(t, g, output, msg.SchemaValidationError)
			g.Expect(err).To(BeIdenticalTo(analyzerFoundIssuesError))
		})
}

func TestInvalidFileError(t *testing.T) {
	// nolint: staticcheck
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Run(func(t framework.TestContext) {
			g := NewWithT(t)

			ns := namespace.NewOrFail(t, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			istioCtl := istioctl.NewOrFail(t, istioctl.Config{})

			// Skip the file with invalid extension and produce no errors.
			output, err := istioctlSafe(t, istioCtl, ns.Name(), false, invalidExtensionFile)
			g.Expect(output[0]).To(ContainSubstring(fmt.Sprintf("Skipping file %v, recognized file extensions are: [.json .yaml .yml]", invalidExtensionFile)))
			g.Expect(err).To(BeNil())

			// Parse error as the yaml file itself is not valid yaml.
			output, err = istioctlSafe(t, istioCtl, ns.Name(), false, invalidFile)
			g.Expect(strings.Join(output, "\n")).To(ContainSubstring("Error(s) adding files"))
			g.Expect(strings.Join(output, "\n")).To(ContainSubstring(fmt.Sprintf("errors parsing content \"%s\"", invalidFile)))

			g.Expect(err).To(MatchError(analyze.FileParseError{}))

			// Parse error as the yaml file itself is not valid yaml, but ignore.
			output, err = istioctlSafe(t, istioCtl, ns.Name(), false, invalidFile, "--ignore-unknown=true")
			g.Expect(strings.Join(output, "\n")).To(ContainSubstring("Error(s) adding files"))
			g.Expect(strings.Join(output, "\n")).To(ContainSubstring(fmt.Sprintf("errors parsing content \"%s\"", invalidFile)))

			g.Expect(err).To(BeNil())
		})
}

func TestJsonInputFile(t *testing.T) {
	// nolint: staticcheck
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Run(func(t framework.TestContext) {
			g := NewWithT(t)

			ns := namespace.NewOrFail(t, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			istioCtl := istioctl.NewOrFail(t, istioctl.Config{})

			// Validation error if we have a gateway with invalid selector.
			applyFileOrFail(t, ns.Name(), jsonGatewayFile)
			output, err := istioctlSafe(t, istioCtl, ns.Name(), true)
			expectMessages(t, g, output, msg.ReferencedResourceNotFound)
			g.Expect(err).To(BeIdenticalTo(analyzerFoundIssuesError))
		})
}

func TestJsonOutput(t *testing.T) {
	// nolint: staticcheck
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Run(func(t framework.TestContext) {
			g := NewWithT(t)

			ns := namespace.NewOrFail(t, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			istioCtl := istioctl.NewOrFail(t, istioctl.Config{})

			t.NewSubTest("no other output except analysis json output").Run(func(t framework.TestContext) {
				applyFileOrFail(t, ns.Name(), jsonGatewayFile)
				stdout, _, err := istioctlWithStderr(t, istioCtl, ns.Name(), true, jsonOutput)
				expectJSONMessages(t, g, stdout, msg.ReferencedResourceNotFound)
				g.Expect(err).To(BeNil())
			})

			t.NewSubTest("invalid file does not output error in stdout").Run(func(t framework.TestContext) {
				stdout, _, err := istioctlWithStderr(t, istioCtl, ns.Name(), false, invalidExtensionFile, jsonOutput)
				expectJSONMessages(t, g, stdout)
				g.Expect(err).To(BeNil())
			})
		})
}

func TestKubeOnly(t *testing.T) {
	// nolint: staticcheck
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Run(func(t framework.TestContext) {
			g := NewWithT(t)

			ns := namespace.NewOrFail(t, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			applyFileOrFail(t, ns.Name(), gatewayFile)

			istioCtl := istioctl.NewOrFail(t, istioctl.Config{})

			// Validation error if we have a gateway with invalid selector.
			output, err := istioctlSafe(t, istioCtl, ns.Name(), true)
			expectMessages(t, g, output, msg.ReferencedResourceNotFound)
			g.Expect(err).To(BeIdenticalTo(analyzerFoundIssuesError))
		})
}

func TestFileAndKubeCombined(t *testing.T) {
	// nolint: staticcheck
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Run(func(t framework.TestContext) {
			g := NewWithT(t)

			ns := namespace.NewOrFail(t, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			applyFileOrFail(t, ns.Name(), virtualServiceFile)

			istioCtl := istioctl.NewOrFail(t, istioctl.Config{})

			// Simulating applying the destination rule that defines the subset, we should
			// fix the error and thus see no message
			output, err := istioctlSafe(t, istioCtl, ns.Name(), true, destinationRuleFile)
			expectNoMessages(t, g, output)
			g.Expect(err).To(BeNil())
		})
}

func TestAllNamespaces(t *testing.T) {
	// nolint: staticcheck
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Run(func(t framework.TestContext) {
			g := NewWithT(t)

			ns1 := namespace.NewOrFail(t, namespace.Config{
				Prefix: "istioctl-analyze-1",
				Inject: true,
			})
			ns2 := namespace.NewOrFail(t, namespace.Config{
				Prefix: "istioctl-analyze-2",
				Inject: true,
			})

			applyFileOrFail(t, ns1.Name(), gatewayFile)
			applyFileOrFail(t, ns2.Name(), gatewayFile)

			istioCtl := istioctl.NewOrFail(t, istioctl.Config{})

			// If we look at one namespace, we should successfully run and see one message (and not anything from any other namespace)
			output, _ := istioctlSafe(t, istioCtl, ns1.Name(), true)
			expectMessages(t, g, output, msg.ReferencedResourceNotFound, msg.ConflictingGateways)

			// If we use --all-namespaces, we should successfully run and see a message from each namespace
			output, _ = istioctlSafe(t, istioCtl, "", true, "--all-namespaces")
			// Since this test runs in a cluster with lots of other namespaces we don't actually care about, only look for ns1 and ns2
			foundCount := 0
			for _, line := range output {
				if strings.Contains(line, ns1.Name()) {
					if strings.Contains(line, msg.ReferencedResourceNotFound.Code()) {
						g.Expect(line).To(ContainSubstring(msg.ReferencedResourceNotFound.Code()))
						foundCount++
					}
					// There are 2 conflictings can be detected, A to B and B to A
					if strings.Contains(line, msg.ConflictingGateways.Code()) {
						g.Expect(line).To(ContainSubstring(msg.ConflictingGateways.Code()))
						foundCount++
					}
				}
				if strings.Contains(line, ns2.Name()) {
					if strings.Contains(line, msg.ReferencedResourceNotFound.Code()) {
						g.Expect(line).To(ContainSubstring(msg.ReferencedResourceNotFound.Code()))
						foundCount++
					}
					// There are 2 conflictings can be detected, B to A and A to B
					if strings.Contains(line, msg.ConflictingGateways.Code()) {
						g.Expect(line).To(ContainSubstring(msg.ConflictingGateways.Code()))
						foundCount++
					}
				}
			}
			g.Expect(foundCount).To(Equal(6))
		})
}

func TestTimeout(t *testing.T) {
	t.Skip("https://github.com/istio/istio/issues/25893")
	// nolint: staticcheck
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Run(func(t framework.TestContext) {
			g := NewWithT(t)

			ns := namespace.NewOrFail(t, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			istioCtl := istioctl.NewOrFail(t, istioctl.Config{})

			// We should time out immediately.
			_, err := istioctlSafe(t, istioCtl, ns.Name(), true, "--timeout=0s")
			g.Expect(err.Error()).To(ContainSubstring("timed out"))
		})
}

// Verify the error line number in the message is correct
func TestErrorLine(t *testing.T) {
	// nolint: staticcheck
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Run(func(t framework.TestContext) {
			g := NewWithT(t)

			ns := namespace.NewOrFail(t, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			istioCtl := istioctl.NewOrFail(t, istioctl.Config{})

			// Validation error if we have a gateway with invalid selector.
			output, err := istioctlSafe(t, istioCtl, ns.Name(), true, gatewayFile, virtualServiceFile)

			g.Expect(strings.Join(output, "\n")).To(ContainSubstring("testdata/gateway.yaml:9"))
			g.Expect(strings.Join(output, "\n")).To(ContainSubstring("testdata/virtualservice.yaml:11"))
			g.Expect(err).To(BeIdenticalTo(analyzerFoundIssuesError))
		})
}

// Verify the output contains messages of the expected type, in order, followed by boilerplate lines
func expectMessages(t test.Failer, g *GomegaWithT, outputLines []string, expected ...*diag.MessageType) {
	t.Helper()

	// The boilerplate lines that appear if any issues are found
	boilerplateLines := strings.Split(analyzerFoundIssuesError.Error(), "\n")

	g.Expect(outputLines).To(HaveLen(len(expected) + len(boilerplateLines)))

	for i, line := range outputLines {
		if i < len(expected) {
			g.Expect(line).To(ContainSubstring(expected[i].Code()))
		} else {
			g.Expect(line).To(ContainSubstring(boilerplateLines[i-len(expected)]))
		}
	}
}

func expectNoMessages(t test.Failer, g *GomegaWithT, output []string) {
	t.Helper()
	g.Expect(output).To(HaveLen(1))
	g.Expect(output[0]).To(ContainSubstring("No validation issues found when analyzing"))
}

func expectJSONMessages(t test.Failer, g *GomegaWithT, output string, expected ...*diag.MessageType) {
	t.Helper()

	var j []map[string]any
	if err := json.Unmarshal([]byte(output), &j); err != nil {
		t.Fatal(err, output)
	}

	g.Expect(j).To(HaveLen(len(expected)))

	for i, m := range j {
		g.Expect(m["level"]).To(Equal(expected[i].Level().String()))
		g.Expect(m["code"]).To(Equal(expected[i].Code()))
	}
}

// istioctlSafe calls istioctl analyze with certain flags set. Stdout and Stderr are merged
func istioctlSafe(t test.Failer, i istioctl.Instance, ns string, useKube bool, extraArgs ...string) ([]string, error) {
	output, stderr, err := istioctlWithStderr(t, i, ns, useKube, extraArgs...)
	return strings.Split(strings.TrimSpace(output+stderr), "\n"), err
}

func istioctlWithStderr(t test.Failer, i istioctl.Instance, ns string, useKube bool, extraArgs ...string) (string, string, error) {
	t.Helper()

	args := []string{"analyze"}
	if ns != "" {
		args = append(args, "--namespace", ns)
	}
	// Suppress some cluster-wide checks. This ensures we do not fail tests when running on clusters that trigger
	// analyzers we didn't intended to test.
	args = append(args, fmt.Sprintf("--use-kube=%t", useKube), "--suppress=IST0139=*", "--suppress=IST0002=CustomResourceDefinition *")
	args = append(args, extraArgs...)

	return i.Invoke(args)
}

// applyFileOrFail applies the given yaml file and deletes it during context cleanup
func applyFileOrFail(t framework.TestContext, ns, filename string) {
	t.Helper()
	if err := t.Clusters().Default().ApplyYAMLFiles(ns, filename); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = t.Clusters().Default().DeleteYAMLFiles(ns, filename)
	})
}

func TestMultiClusterWithSecrets(t *testing.T) {
	// nolint: staticcheck
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			if len(t.Environment().Clusters()) < 2 {
				t.Skip("skipping test, need at least 2 clusters")
			}

			g := NewWithT(t)

			ns := namespace.NewOrFail(t, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			// create remote secrets for analysis
			secrets := map[string]string{}
			for _, c := range t.Environment().Clusters() {
				istioCtl := istioctl.NewOrFail(t, istioctl.Config{
					Cluster: c,
				})
				secret, _, err := createRemoteSecret(t, istioCtl, c.Name())
				g.Expect(err).To(BeNil())
				secrets[c.Name()] = secret
			}
			for ind, c := range t.Environment().Clusters() {
				// apply remote secret to be used for analysis
				for sc, secret := range secrets {
					if c.Name() == sc {
						continue
					}
					err := c.ApplyYAMLFiles(helm.IstioNamespace, secret)
					g.Expect(err).To(BeNil())
				}

				svc := fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata:
  name: reviews
spec:
  selector:
    app: reviews
  type: ClusterIP
  ports:
  - name: http-%d
    port: 8080
    protocol: TCP
    targetPort: 8080
`, ind)
				// apply inconsistent services
				err := c.ApplyYAMLContents(ns.Name(), svc)
				g.Expect(err).To(BeNil())
			}

			istioCtl := istioctl.NewOrFail(t, istioctl.Config{Cluster: t.Clusters().Configs().Default()})
			output, _ := istioctlSafe(t, istioCtl, ns.Name(), true)
			g.Expect(strings.Join(output, "\n")).To(ContainSubstring("is inconsistent across clusters"))
		})
}

func createRemoteSecret(t test.Failer, i istioctl.Instance, cluster string) (string, string, error) {
	t.Helper()

	args := []string{"create-remote-secret"}
	args = append(args, "--name", cluster)

	return i.Invoke(args)
}

func TestMultiClusterWithContexts(t *testing.T) {
	// nolint: staticcheck
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			if len(t.Environment().Clusters()) < 2 {
				t.Skip("skipping test, need at least 2 clusters")
			}

			g := NewWithT(t)

			ns := namespace.NewOrFail(t, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			for ind, c := range t.Environment().Clusters() {
				svc := fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata:
  name: reviews
spec:
  selector:
    app: reviews
  type: ClusterIP
  ports:
  - name: http-%d
    port: 8080
    protocol: TCP
    targetPort: 8080
`, ind)
				// apply inconsistent services
				err := c.ApplyYAMLContents(ns.Name(), svc)
				g.Expect(err).To(BeNil())

			}

			mergedConfig := api.NewConfig()
			mergedKubeconfig, err := os.CreateTemp("", "merged_kubeconfig.yaml")
			g.Expect(err).To(BeNil(), fmt.Sprintf("Create file for storing merged kubeconfig: %v", err))
			defer os.Remove(mergedKubeconfig.Name())
			for _, c := range t.Environment().Clusters() {
				filenamer, ok := c.(istioctl.Filenamer)
				g.Expect(ok).To(BeTrue(), fmt.Sprintf("Cluster %v does not support fetching kubeconfig", c))
				config, err := clientcmd.LoadFromFile(filenamer.Filename())
				g.Expect(err).To(BeNil(), fmt.Sprintf("Load config from file %q: %v", filenamer.Filename(), err))
				if mergedConfig.CurrentContext == "" {
					mergedConfig.CurrentContext = config.CurrentContext
				}
				mergedConfig.Clusters = maps.MergeCopy(mergedConfig.Clusters, config.Clusters)
				mergedConfig.AuthInfos = maps.MergeCopy(mergedConfig.AuthInfos, config.AuthInfos)
				mergedConfig.Contexts = maps.MergeCopy(mergedConfig.Contexts, config.Contexts)
			}
			err = clientcmd.WriteToFile(*mergedConfig, mergedKubeconfig.Name())
			g.Expect(err).To(BeNil(), fmt.Sprintf("Write merged kubeconfig to file %q: %v", mergedKubeconfig.Name(), err))
			ctxArgs := []string{"--kubeconfig", mergedKubeconfig.Name()}
			for _, ctx := range maps.Keys(mergedConfig.Contexts) {
				if ctx == mergedConfig.CurrentContext {
					continue
				}
				ctxArgs = append(ctxArgs, "--remote-contexts", ctx)
			}

			istioCtl := istioctl.NewOrFail(t, istioctl.Config{Cluster: t.Clusters().Configs().Default()})
			output, _ := istioctlSafe(t, istioCtl, ns.Name(), true, ctxArgs...)
			g.Expect(strings.Join(output, "\n")).To(ContainSubstring("is inconsistent across clusters"))
		})
}
