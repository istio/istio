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

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
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

func TestEmptyClusterNoErrors(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			g := NewGomegaWithT(t)

			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "istioctl-analyze",
				Inject: true,
			})

			istioCtl := istioctl.NewOrFail(t, ctx, istioctl.Config{})
			output := runIstioctl(t, istioCtl, []string{"experimental", "analyze", "--use-kube", "--namespace", ns.Name()})

			g.Expect(output).To(BeEmpty())
		})
}

// TODO: Test files only case
// TODO: Test kube only case
// TODO: Test combined case
// TODO: Test service discovery overrides

func runIstioctl(t *testing.T, i istioctl.Instance, args []string) []string {
	output, err := i.Invoke(args)
	if err != nil {
		t.Fatalf("Unwanted exception for 'istioctl %s': %v", strings.Join(args, " "), err)
	}
	return strings.Split(output, "\n")
}
