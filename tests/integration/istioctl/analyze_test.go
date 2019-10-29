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
	"strings"
	"testing"

	. "github.com/onsi/gomega"

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
			g.Expect(output).To(HaveLen(2))
			g.Expect(output[0]).To(ContainSubstring(msg.ReferencedResourceNotFound.Code()))
			g.Expect(output[1]).To(ContainSubstring(analyzerFoundIssuesError.Error()))
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
			g.Expect(output).To(HaveLen(2))
			g.Expect(output[0]).To(ContainSubstring(msg.ReferencedResourceNotFound.Code()))
			g.Expect(output[1]).To(ContainSubstring(analyzerFoundIssuesError.Error()))
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
			g.Expect(output[0]).To(ContainSubstring(msg.ReferencedResourceNotFound.Code()))
			g.Expect(output[1]).To(ContainSubstring(analyzerFoundIssuesError.Error()))
			g.Expect(err).To(BeIdenticalTo(analyzerFoundIssuesError))

			// Error goes away if we include the service definition in the resources being analyzed
			output, err = istioctlSafe(t, istioCtl, ns.Name(), "--discovery=true", reviewsVsAndDrFile, reviewsSvcFile)
			expectNoMessages(t, g, output)
			g.Expect(err).To(BeNil())
		})
}

func expectNoMessages(t *testing.T, g *GomegaWithT, output []string) {
	t.Helper()
	g.Expect(output).To(HaveLen(1))
	g.Expect(output[0]).To(ContainSubstring("No validation issues found"))
}

func istioctlSafe(t *testing.T, i istioctl.Instance, ns string, extraArgs ...string) ([]string, error) {
	t.Helper()
	args := []string{"experimental", "analyze", "--namespace", ns}
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
