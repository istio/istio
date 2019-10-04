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
}

const (
	serviceRoleBindingYaml = `
apiVersion: rbac.istio.io/v1alpha1
kind: ServiceRoleBinding
metadata:
  name: service-role-binding
  namespace: default
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
  namespace: default
spec:
  rules:
  - services: ["*"]
`
	namespace       = "default"
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

			gvr := schema.GroupVersionResource{
				Group:    "rbac.istio.io",
				Version:  "v1alpha1",
				Resource: "servicerolebindings",
			}
			name := "service-role-binding"

			// Apply config that should generate a validation message
			err := env.ApplyContents(namespace, serviceRoleBindingYaml)
			defer func() { _ = env.DeleteContents(namespace, serviceRoleBindingYaml) }()

			if err != nil {
				t.Fatalf("Error applying serviceRoleBindingYaml for test scenario: %v", err)
			}

			// Verify we have the expected validation message
			g.Eventually(func() string { return getStatus(t, env, gvr, namespace, name) }, timeout, pollingInterval).
				Should(ContainSubstring(msg.ReferencedResourceNotFound.Code()))

			// Apply config that should fix the problem and remove the validation message
			err = env.ApplyContents("default", serviceRoleYaml)
			defer func() { _ = env.DeleteContents(namespace, serviceRoleYaml) }()
			if err != nil {
				t.Fatalf("Error applying serviceRoleYaml for test scenario: %v", err)
			}

			//Verify the validation message disappears. (With "eventually" / timeout)
			g.Eventually(func() string { return getStatus(t, env, gvr, namespace, name) }, timeout, pollingInterval).
				Should(Not(ContainSubstring(msg.ReferencedResourceNotFound.Code())))
		})
}

func getStatus(t *testing.T, env *kube.Environment, gvr schema.GroupVersionResource, namespace, name string) string {
	u, err := env.GetUnstructured(gvr, namespace, name)
	if err != nil {
		t.Errorf("Couldn't get status for resource %v", name)
	}
	return fmt.Sprintf("%v", u.Object["status"])
}
