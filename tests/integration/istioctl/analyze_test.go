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
	"io/ioutil"
	"strings"
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

const (
	serviceRoleBindingFile = "testdata/servicerolebinding.yaml"
	serviceRoleFile        = "testdata/servicerole.yaml"
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
			output := runIstioctl(t, istioCtl,
				[]string{"experimental", "analyze", "--namespace", ns.Name(), "--use-kube"})
			g.Expect(output).To(BeEmpty())
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
			output := runIstioctl(t, istioCtl,
				[]string{"experimental", "analyze", "--namespace", ns.Name(), serviceRoleBindingFile})
			g.Expect(output).To(HaveLen(1))
			g.Expect(output[0]).To(ContainSubstring(msg.ReferencedResourceNotFound.Code()))

			// Error goes away if we include both the binding and its role
			output = runIstioctl(t, istioCtl,
				[]string{"experimental", "analyze", "--namespace", ns.Name(), serviceRoleBindingFile, serviceRoleFile})
			g.Expect(output).To(BeEmpty())
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

			applyFileToCluster(t, ns.Name(), serviceRoleBindingFile)

			istioCtl := istioctl.NewOrFail(t, ctx, istioctl.Config{})

			// Validation error if we have a service role binding without a service role
			output := runIstioctl(t, istioCtl,
				[]string{"experimental", "analyze", "--namespace", ns.Name(), "--use-kube"})
			g.Expect(output).To(HaveLen(1))
			g.Expect(output[0]).To(ContainSubstring(msg.ReferencedResourceNotFound.Code()))

			// Error goes away if we include both the binding and its role
			applyFileToCluster(t, ns.Name(), serviceRoleFile)
			output = runIstioctl(t, istioCtl,
				[]string{"experimental", "analyze", "--namespace", ns.Name(), "--use-kube"})
			g.Expect(output).To(BeEmpty())
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

			applyFileToCluster(t, ns.Name(), serviceRoleBindingFile)

			istioCtl := istioctl.NewOrFail(t, ctx, istioctl.Config{})

			// Simulating applying the service role to a cluster that already has the binding, we should
			// fix the error and thus see no message
			output := runIstioctl(t, istioCtl,
				[]string{"experimental", "analyze", "--namespace", ns.Name(), "--use-kube", serviceRoleFile})
			g.Expect(output).To(BeEmpty())
		})
}

func runIstioctl(t *testing.T, i istioctl.Instance, args []string) []string {
	output, err := i.Invoke(args)
	if err != nil {
		t.Fatalf("Unwanted exception for 'istioctl %s': %v", strings.Join(args, " "), err)
	}
	if output == "" {
		return []string{}
	}
	return strings.Split(strings.TrimSpace(output), "\n")
}

func readFileOrFail(t *testing.T, file string) string {
	b, err := ioutil.ReadFile(file)
	if err != nil {
		t.Fatalf("Error reading file %q: %v", file, err)
	}
	return string(b)
}

func applyFileToCluster(t *testing.T, ns, fileName string) {
	yaml := readFileOrFail(t, fileName)
	err := env.ApplyContents(ns, yaml)
	if err != nil {
		t.Fatalf("Error applying file %q to cluster: %v", fileName, err)
	}

}
