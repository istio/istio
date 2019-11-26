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

package versionanalyze

import (
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
	reviewsVsAndDrFile     = "testdata/reviews-vs-and-dr.yaml"
	reviewsSvcFile         = "testdata/reviews-svc.yaml"
)

var analyzerFoundIssuesError = cmd.AnalyzerFoundIssuesError{}

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
			output, err := istioctlSafe(t, istioCtl, ns.Name(), "--use-kube")
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
			output, err := istioctlSafe(t, istioCtl, ns.Name(), serviceRoleBindingFile)
			expectMessages(t, g, output, msg.ReferencedResourceNotFound)
			g.Expect(err).To(BeIdenticalTo(analyzerFoundIssuesError))

			// Error goes away if we include both the binding and its role
			output, err = istioctlSafe(t, istioCtl, ns.Name(), serviceRoleBindingFile, serviceRoleFile)
			expectNoMessages(t, g, output)
			g.Expect(err).To(BeNil())
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
			output, err := istioctlSafe(t, istioCtl, ns.Name(), "--use-kube")
			expectMessages(t, g, output, msg.ReferencedResourceNotFound)
			g.Expect(err).To(BeIdenticalTo(analyzerFoundIssuesError))

			// Error goes away if we include both the binding and its role
			applyFileOrFail(t, ns.Name(), serviceRoleFile)
			output, err = istioctlSafe(t, istioCtl, ns.Name(), "--use-kube")
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
			output, err := istioctlSafe(t, istioCtl, ns.Name(), "--use-kube", serviceRoleFile)
			expectNoMessages(t, g, output)
			g.Expect(err).To(BeNil())
		})
}

func TestServiceDiscovery(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			g := NewGomegaWithT(t)

			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			istioCtl := istioctl.NewOrFail(t, ctx, istioctl.Config{})

			// Given a valid virtualservice and destinationrule, but without the accompanying service:
			// With service discovery off, expect no errors
			output, err := istioctlSafe(t, istioCtl, ns.Name(), "--discovery=false", reviewsVsAndDrFile)
			expectNoMessages(t, g, output)
			g.Expect(err).To(BeNil())

			// With service discovery on, do expect an error
			output, err = istioctlSafe(t, istioCtl, ns.Name(), "--discovery=true", reviewsVsAndDrFile)
			expectMessages(t, g, output, msg.ReferencedResourceNotFound)
			g.Expect(err).To(BeIdenticalTo(analyzerFoundIssuesError))

			// Error goes away if we include the service definition in the resources being analyzed
			output, err = istioctlSafe(t, istioCtl, ns.Name(), "--discovery=true", reviewsVsAndDrFile, reviewsSvcFile)
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
			output, _ := istioctlSafe(t, istioCtl, ns1.Name(), "--use-kube")
			expectMessages(t, g, output, msg.ReferencedResourceNotFound)

			// If we use --all-namespaces, we should successfully run and see a message from each namespace
			output, _ = istioctlSafe(t, istioCtl, "", "--use-kube", "--all-namespaces")
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
	g.Expect(output[0]).To(ContainSubstring(cmd.NoIssuesString))
}

func istioctlSafe(t *testing.T, i istioctl.Instance, ns string, extraArgs ...string) ([]string, error) {
	t.Helper()
	args := []string{"experimental", "analyze"}
	if ns != "" {
		args = append(args, "--namespace", ns)
	}
	output, err := i.Invoke(append(args, extraArgs...))
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
