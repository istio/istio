//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
package statusupdate

import (
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
)

func TestMain(m *testing.M) {
	framework.
		NewSuite("statusupdate_test", m).
		Label(label.CustomSetup).
		SetupOnEnv(environment.Kube, istio.Setup(nil, setupConfig)).
		Run()
}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.Values["galley.enableAnalysis"] = "true"
	cfg.ControlPlaneValues = `
components:
  galley:
    enabled: true
  citadel:
    enabled: true
values:
  galley:
    enableAnalysis: true
`
}

const (
	serviceRoleBindingYaml = `
apiVersion: rbac.istio.io/v1alpha1
kind: ServiceRoleBinding
metadata:
  name: service-role-binding
spec:
  mode: PERMISSIVE
  roleRef:
    kind: ServiceRole
    name: service-role
  subjects:
  - user: '*'
`
	serviceRoleYaml = `
apiVersion: rbac.istio.io/v1alpha1
kind: ServiceRole
metadata:
  name: service-role
spec:
  rules:
  - services: ["*"]
`
	timeout         = "10s"
	pollingInterval = "1s"
)

func TestStatusUpdate(t *testing.T) {
	framework.NewTest(t).
		// The status update implementation we're testing is kube-specific
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			g := NewGomegaWithT(t)

			env := ctx.Environment().(*kube.Environment)

			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "statusupdate",
				Inject: true,
			})

			gvr := schema.GroupVersionResource{
				Group:    "rbac.istio.io",
				Version:  "v1alpha1",
				Resource: "servicerolebindings",
			}
			name := "service-role-binding"

			// We define the function here so that it can take no args (as required by Gomega) and access the needed variables via closure
			getStatusFn := func() string {
				u, err := env.GetUnstructured(gvr, ns.Name(), name)
				if err != nil {
					t.Fatalf("Couldn't get status for resource %v", name)
				}
				return fmt.Sprintf("%v", u.Object["status"])
			}

			// Apply config that should generate a validation message
			err := env.ApplyContents(ns.Name(), serviceRoleBindingYaml)
			if err != nil {
				t.Fatalf("Error applying serviceRoleBindingYaml for test scenario: %v", err)
			}

			// Verify we have the expected validation message
			g.Eventually(getStatusFn, timeout, pollingInterval).
				Should(ContainSubstring(msg.ReferencedResourceNotFound.Code()))

			// Apply config that should fix the problem and remove the validation message
			err = env.ApplyContents(ns.Name(), serviceRoleYaml)
			if err != nil {
				t.Fatalf("Error applying serviceRoleYaml for test scenario: %v", err)
			}

			// Verify the validation message disappears
			g.Eventually(getStatusFn, timeout, pollingInterval).
				Should(Not(ContainSubstring(msg.ReferencedResourceNotFound.Code())))
		})
}
