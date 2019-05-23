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

package framework

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

func TestNamespace(t *testing.T) {
	var namespaceName string
	var noCleanup bool
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			noCleanup = ctx.Settings().NoCleanup
			ns := namespace.NewOrFail(t, ctx, "testns", true)
			namespaceName = ns.Name()

			env := ctx.Environment().(*kube.Environment)
			if !env.Accessor.NamespaceExists(ns.Name()) {
				t.Fatalf("The namespace %q should have existed.", ns.Name())
			}

			n, err := env.Accessor.GetNamespace(ns.Name())
			if err != nil {
				t.Fatalf("Error getting the namespace(%q): %v", ns.Name(), err)
			}

			_, found := n.Labels["istio-injection"]
			if !found {
				t.Fatalf("injection label not found: ns: %s", ns.Name())
			}
		})

	if !noCleanup {
		// Check after run to see that the namespace is gone.
		if err := env.WaitForNamespaceDeletion(namespaceName); err != nil {
			t.Fatalf("WaitiForNamespaceDeletion failed: %v", err)
		}
	}
}
