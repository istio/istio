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
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
)

func TestMain(m *testing.M) {
	framework.
		NewSuite("istioctl_analyze_test", m).
		Label(label.CustomSetup).
		SetupOnEnv(environment.Kube, istio.Setup(nil, nil)).
		Run()
}

func TestEmptyCluster(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			g := NewGomegaWithT(t)

			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			istioCtl := istioctl.NewOrFail(t, ctx, istioctl.Config{})

			// For a clean istio install, expect no validation errors
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

			output := runIstioctl(t, istioCtl,
				[]string{"experimental", "analyze", "--namespace", ns.Name(), "testdata/servicerolebinding.yaml"})
			g.Expect(output).To(HaveLen(1))
			g.Expect(output[0]).To(ContainSubstring(msg.ReferencedResourceNotFound.Code()))

			//TODO: Also test fixed with both files
		})
}

func TestKubeOnly(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			g := NewGomegaWithT(t)

			env := ctx.Environment().(*kube.Environment)

			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			serviceRoleBindingYaml := readFileOrFail(t, "testdata/servicerolebinding.yaml")
			err := env.ApplyContents(ns.Name(), serviceRoleBindingYaml)
			if err != nil {
				t.Fatalf("Error applying serviceRoleBindingYaml for test scenario: %v", err)
			}

			istioCtl := istioctl.NewOrFail(t, ctx, istioctl.Config{})

			output := runIstioctl(t, istioCtl,
				[]string{"experimental", "analyze", "--namespace", ns.Name(), "--use-kube"})
			g.Expect(output).To(HaveLen(1))
			g.Expect(output[0]).To(ContainSubstring(msg.ReferencedResourceNotFound.Code()))

			// TODO: Also test fixed with both files
		})
}

func TestFileAndKubeCombined(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			g := NewGomegaWithT(t)

			env := ctx.Environment().(*kube.Environment)

			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			serviceRoleBindingYaml := readFileOrFail(t, "testdata/servicerolebinding.yaml")
			err := env.ApplyContents(ns.Name(), serviceRoleBindingYaml)
			if err != nil {
				t.Fatalf("Error applying serviceRoleBindingYaml for test scenario: %v", err)
			}

			istioCtl := istioctl.NewOrFail(t, ctx, istioctl.Config{})

			output := runIstioctl(t, istioCtl,
				[]string{"experimental", "analyze", "--namespace", ns.Name(), "--use-kube", "testdata/servicerole.yaml"})
			g.Expect(output).To(BeEmpty())
		})
}

// TODO: Test service discovery aspects

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
