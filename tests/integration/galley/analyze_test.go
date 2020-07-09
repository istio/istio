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

package galley

import (
	"fmt"
	"strings"
	"testing"

	"encoding/json"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/istioctl/cmd"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
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

var analyzerFoundIssuesError = cmd.AnalyzerFoundIssuesError{}

func TestEmptyCluster(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			g := NewGomegaWithT(t)

			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})

			// For a clean istio install with injection enabled, expect no validation errors
			output, err := istioctlSafe(t, istioCtl, ns.Name(), true)
			expectNoMessages(t, g, output)
			g.Expect(err).To(BeNil())

		})
}

func TestFileOnly(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			g := NewGomegaWithT(t)

			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})

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

func TestDirectoryWithoutRecursion(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			g := NewGomegaWithT(t)

			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})

			// Recursive is false, so we should only analyze
			// testdata/some-dir/missing-gateway.yaml and get a
			// SchemaValidationError (if we did recurse, we'd get a
			// UnknownAnnotation as well).
			output, err := istioctlSafe(t, istioCtl, ns.Name(), false, "--recursive=false", dirWithConfig)
			expectMessages(t, g, output, msg.SchemaValidationError)
			g.Expect(err).To(BeIdenticalTo(analyzerFoundIssuesError))
		})
}

func TestDirectoryWithRecursion(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			g := NewGomegaWithT(t)

			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})

			// Recursive is true, so we should see two errors (SchemaValidationError and UnknownAnnotation).
			output, err := istioctlSafe(t, istioCtl, ns.Name(), false, "--recursive=true", dirWithConfig)
			expectMessages(t, g, output, msg.SchemaValidationError, msg.UnknownAnnotation)
			g.Expect(err).To(BeIdenticalTo(analyzerFoundIssuesError))
		})
}

func TestInvalidFileError(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			g := NewGomegaWithT(t)

			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})

			// Skip the file with invalid extension and produce no errors.
			output, err := istioctlSafe(t, istioCtl, ns.Name(), false, invalidExtensionFile)
			g.Expect(output[0]).To(ContainSubstring(fmt.Sprintf("Skipping file %v, recognized file extensions are: [.json .yaml .yml]", invalidExtensionFile)))
			g.Expect(err).To(BeNil())

			// Parse error as the yaml file itself is not valid yaml.
			output, err = istioctlSafe(t, istioCtl, ns.Name(), false, invalidFile)
			g.Expect(strings.Join(output, "\n")).To(ContainSubstring("Error(s) adding files"))
			g.Expect(strings.Join(output, "\n")).To(ContainSubstring(fmt.Sprintf("errors parsing content \"%s\"", invalidFile)))

			g.Expect(err).To(MatchError(cmd.FileParseError{}))
		})
}

func TestJsonInputFile(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			g := NewGomegaWithT(t)

			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})

			// Validation error if we have a gateway with invalid selector.
			output, err := istioctlSafe(t, istioCtl, ns.Name(), false, jsonGatewayFile)
			expectMessages(t, g, output, msg.ReferencedResourceNotFound)
			g.Expect(err).To(BeIdenticalTo(analyzerFoundIssuesError))

		})
}

func TestJsonOutput(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			g := NewGomegaWithT(t)

			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})

			testcases := []struct {
				name     string
				args     []string
				messages []*diag.MessageType
			}{
				{
					name:     "no other output except analysis json output",
					args:     []string{jsonGatewayFile, jsonOutput},
					messages: []*diag.MessageType{msg.ReferencedResourceNotFound},
				},
				{
					name:     "invalid file does not output error in stdout",
					args:     []string{invalidExtensionFile, jsonOutput},
					messages: []*diag.MessageType{},
				},
			}

			for _, tc := range testcases {
				ctx.NewSubTest(tc.name).Run(func(ctx framework.TestContext) {
					stdout, _, err := istioctlWithStderr(t, istioCtl, ns.Name(), false, tc.args...)
					expectJSONMessages(t, g, stdout, tc.messages...)
					g.Expect(err).To(BeNil())
				})
			}

		})
}

func TestKubeOnly(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			g := NewGomegaWithT(t)

			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			applyFileOrFail(t, ns.Name(), gatewayFile)

			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})

			// Validation error if we have a gateway with invalid selector.
			output, err := istioctlSafe(t, istioCtl, ns.Name(), true)
			expectMessages(t, g, output, msg.ReferencedResourceNotFound)
			g.Expect(err).To(BeIdenticalTo(analyzerFoundIssuesError))
		})
}

func TestFileAndKubeCombined(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			g := NewGomegaWithT(t)

			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			applyFileOrFail(t, ns.Name(), virtualServiceFile)

			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})

			// Simulating applying the destination rule that defines the subset, we should
			// fix the error and thus see no message
			output, err := istioctlSafe(t, istioCtl, ns.Name(), true, destinationRuleFile)
			expectNoMessages(t, g, output)
			g.Expect(err).To(BeNil())
		})
}

func TestAllNamespaces(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			g := NewGomegaWithT(t)

			ns1 := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "istioctl-analyze-1",
				Inject: true,
			})
			ns2 := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "istioctl-analyze-2",
				Inject: true,
			})

			applyFileOrFail(t, ns1.Name(), gatewayFile)
			applyFileOrFail(t, ns2.Name(), gatewayFile)

			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})

			// If we look at one namespace, we should successfully run and see one message (and not anything from any other namespace)
			output, _ := istioctlSafe(t, istioCtl, ns1.Name(), true)
			expectMessages(t, g, output, msg.ReferencedResourceNotFound)

			// If we use --all-namespaces, we should successfully run and see a message from each namespace
			output, _ = istioctlSafe(t, istioCtl, "", true, "--all-namespaces")
			// Since this test runs in a cluster with lots of other namespaces we don't actually care about, only look for ns1 and ns2
			foundCount := 0
			for _, line := range output {
				if strings.Contains(line, ns1.Name()) {
					g.Expect(line).To(ContainSubstring(msg.ReferencedResourceNotFound.Code()))
					foundCount++
				}
				if strings.Contains(line, ns2.Name()) {
					g.Expect(line).To(ContainSubstring(msg.ReferencedResourceNotFound.Code()))
					foundCount++
				}
			}
			g.Expect(foundCount).To(Equal(2))
		})
}

func TestTimeout(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			g := NewGomegaWithT(t)

			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})

			// We should time out immediately.
			_, err := istioctlSafe(t, istioCtl, ns.Name(), true, "--timeout=0s")
			g.Expect(err.Error()).To(ContainSubstring("timed out"))
		})
}

// Verify the output contains messages of the expected type, in order, followed by boilerplate lines
func expectMessages(t *testing.T, g *GomegaWithT, outputLines []string, expected ...*diag.MessageType) {
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

func expectNoMessages(t *testing.T, g *GomegaWithT, output []string) {
	t.Helper()
	g.Expect(output).To(HaveLen(1))
	g.Expect(output[0]).To(ContainSubstring("No validation issues found when analyzing"))
}

func expectJSONMessages(t *testing.T, g *GomegaWithT, output string, expected ...*diag.MessageType) {
	t.Helper()

	var j []map[string]interface{}
	if err := json.Unmarshal([]byte(output), &j); err != nil {
		t.Fatal(err)
	}

	g.Expect(j).To(HaveLen(len(expected)))

	for i, m := range j {
		g.Expect(m["level"]).To(Equal(expected[i].Level().String()))
		g.Expect(m["code"]).To(Equal(expected[i].Code()))
	}
}

// istioctlSafe calls istioctl analyze with certain flags set. Stdout and Stderr are merged
func istioctlSafe(t *testing.T, i istioctl.Instance, ns string, useKube bool, extraArgs ...string) ([]string, error) {
	output, stderr, err := istioctlWithStderr(t, i, ns, useKube, extraArgs...)
	return strings.Split(strings.TrimSpace(output+"\n"+stderr), "\n"), err
}

func istioctlWithStderr(t *testing.T, i istioctl.Instance, ns string, useKube bool, extraArgs ...string) (string, string, error) {
	t.Helper()

	args := []string{"analyze"}
	if ns != "" {
		args = append(args, "--namespace", ns)
	}
	args = append(args, fmt.Sprintf("--use-kube=%t", useKube))
	args = append(args, extraArgs...)

	return i.Invoke(args)
}

func applyFileOrFail(t *testing.T, ns, filename string) {
	t.Helper()
	if err := cluster.ApplyYAMLFiles(ns, filename); err != nil {
		t.Fatal(err)
	}
}
