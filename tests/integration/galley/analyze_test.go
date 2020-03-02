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

package galley

import (
	"fmt"
	"strings"
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/istioctl/cmd"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

const (
	serviceRoleBindingFile = "testdata/servicerolebinding.yaml"
	serviceRoleFile        = "testdata/servicerole.yaml"
	invalidFile            = "testdata/invalid.yaml"
	dirWithConfig          = "testdata/some-dir/"

	suppressDefaultMeshConfigDeprecated = "IST0121=MeshPolicy default"
)

var (
	analyzerFoundIssuesError = cmd.AnalyzerFoundIssuesError{}

	suppressor = []string{}
)

func TestEmptyCluster(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			g := NewGomegaWithT(t)

			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			istioCtl := istioctl.NewOrFail(t, ctx, istioctl.Config{})

			// For a clean istio install with injection enabled, expect no validation errors
			output, err := istioctlSafe(t, istioCtl, ns.Name(), true, suppressor...)
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

			istioCtl := istioctl.NewOrFail(t, ctx, istioctl.Config{})

			// Validation error if we have a service role binding without a service role
			output, err := istioctlSafe(t, istioCtl, ns.Name(), false, serviceRoleBindingFile)
			expectMessages(t, g, output, msg.ReferencedResourceNotFound)
			g.Expect(err).To(BeIdenticalTo(analyzerFoundIssuesError))

			// Error goes away if we include both the binding and its role
			output, err = istioctlSafe(t, istioCtl, ns.Name(), false, serviceRoleBindingFile, serviceRoleFile)
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

			istioCtl := istioctl.NewOrFail(t, ctx, istioctl.Config{})

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

			istioCtl := istioctl.NewOrFail(t, ctx, istioctl.Config{})

			// Recursive is true, so we should see two errors (SchemaValidationError and UnknownAnnotation).
			output, err := istioctlSafe(t, istioCtl, ns.Name(), false, "--recursive=true", dirWithConfig)
			expectMessages(t, g, output, msg.SchemaValidationError, msg.UnknownAnnotation)
			g.Expect(err).To(BeIdenticalTo(analyzerFoundIssuesError))
		})
}

func TestFileParseError(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			g := NewGomegaWithT(t)

			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			istioCtl := istioctl.NewOrFail(t, ctx, istioctl.Config{})

			// Parse error as the yaml file itself is not valid yaml.
			output, err := istioctlSafe(t, istioCtl, ns.Name(), false, invalidFile)
			g.Expect(output[0]).To(ContainSubstring("Error(s) adding files"))
			g.Expect(output[1]).To(ContainSubstring(fmt.Sprintf("errors parsing content \"%s\"", invalidFile)))

			g.Expect(err).To(MatchError(cmd.FileParseError{}))
		})
}

func TestKubeOnly(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			g := NewGomegaWithT(t)

			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			applyFileOrFail(t, ns.Name(), serviceRoleBindingFile)

			istioCtl := istioctl.NewOrFail(t, ctx, istioctl.Config{})

			// Validation error if we have a service role binding without a service role
			output, err := istioctlSafe(t, istioCtl, ns.Name(), true, suppressor...)
			expectMessages(t, g, output, msg.ReferencedResourceNotFound)
			g.Expect(err).To(BeIdenticalTo(analyzerFoundIssuesError))

			// Error goes away if we include both the binding and its role
			applyFileOrFail(t, ns.Name(), serviceRoleFile)
			output, err = istioctlSafe(t, istioCtl, ns.Name(), true, suppressor...)
			expectNoMessages(t, g, output)
			g.Expect(err).To(BeNil())
		})
}

func TestFileAndKubeCombined(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			g := NewGomegaWithT(t)

			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			applyFileOrFail(t, ns.Name(), serviceRoleBindingFile)

			istioCtl := istioctl.NewOrFail(t, ctx, istioctl.Config{})

			// Simulating applying the service role to a cluster that already has the binding, we should
			// fix the error and thus see no message
			suppressors := append([]string{serviceRoleFile}, suppressor...)
			output, err := istioctlSafe(t, istioCtl, ns.Name(), true, suppressors...)
			expectNoMessages(t, g, output)
			g.Expect(err).To(BeNil())
		})
}

func TestAllNamespaces(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
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

			applyFileOrFail(t, ns1.Name(), serviceRoleBindingFile)
			applyFileOrFail(t, ns2.Name(), serviceRoleBindingFile)

			istioCtl := istioctl.NewOrFail(t, ctx, istioctl.Config{})

			// If we look at one namespace, we should successfully run and see one message (and not anything from any other namespace)
			output, _ := istioctlSafe(t, istioCtl, ns1.Name(), true, suppressor...)
			expectMessages(t, g, output, msg.ReferencedResourceNotFound)

			// If we use --all-namespaces, we should successfully run and see a message from each namespace
			suppressors := append([]string{"--all-namespaces"}, suppressor...)
			output, _ = istioctlSafe(t, istioCtl, "", true, suppressors...)
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

			istioCtl := istioctl.NewOrFail(t, ctx, istioctl.Config{})

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

func istioctlSafe(t *testing.T, i istioctl.Instance, ns string, useKube bool, extraArgs ...string) ([]string, error) {
	t.Helper()

	args := []string{"analyze"}
	if ns != "" {
		args = append(args, "--namespace", ns)
	}
	args = append(args, fmt.Sprintf("--use-kube=%t", useKube))
	args = append(args, extraArgs...)

	output, err := i.Invoke(args)
	if output == "" {
		return []string{}, err
	}
	return strings.Split(strings.TrimSpace(output), "\n"), err
}

func applyFileOrFail(t *testing.T, ns, filename string) {
	t.Helper()
	if err := env.Apply(ns, filename); err != nil {
		t.Fatal(err)
	}
}
