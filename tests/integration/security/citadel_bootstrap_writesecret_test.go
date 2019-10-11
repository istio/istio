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

package security

import (
	"reflect"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework/label"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/security/pkg/pki/ca"
)

const (
	noReadRBACConfigTmpl = "testdata/citadel-noread-rbac.yaml.tmpl"
	normalRBACConfigTmpl = "testdata/citadel-rbac.yaml.tmpl"
)

// TestCitadelBootstrapKubernetes verifies that Citadel exits if:
// It fails to read the CA-root secret due to network issue, etc. (this is simulated via RBAC policy),
// and then it tries to create a new CA-root secret and fails because the secret already exists.
// This test verifies that when Citadel can't read the existing CA-root secret due to network issue,
// it should not generate a new cert and overwrite the existing secret. Instead, the Citadel pod should
// immediately exit with error.
func TestCitadelBootstrapKubernetes(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Label(label.CustomSetup). // test depends on the clusterrole name being "istio-citadel-istio-system"
		Run(func(ctx framework.TestContext) {
			ns := namespace.ClaimOrFail(t, ctx, ist.Settings().IstioNamespace)
			namespaceTmpl := map[string]string{
				"Namespace": ns.Name(),
			}

			// Apply the RBAC policy to prevent Citadel from reading the CA-root secret.
			policies := tmpl.EvaluateAllOrFail(t, namespaceTmpl,
				file.AsStringOrFail(t, noReadRBACConfigTmpl))

			g.ApplyConfigOrFail(t, ns, policies...)

			// Sleep 10 seconds for the policy to take effect.
			time.Sleep(10 * time.Second)

			// Retrieve Citadel CA-root secret. Keep it for later comparison.
			env := ctx.Environment().(*kube.Environment)
			secrets := env.GetSecret(ns.Name())
			citadelSecret, err := secrets.Get(ca.CASecret, metav1.GetOptions{})
			if err != nil {
				t.Fatal("Failed to retrieve Citadel CA-root secret.")
			}

			// Delete Citadel pod, a new pod will be started.
			pods, err := env.GetPods(ns.Name(), "istio=citadel")
			if err != nil {
				t.Fatal("Failed to get Citadel pods.")
			}
			env.DeletePod(ns.Name(), pods[0].GetName())

			// Sleep 60 seconds for the recreated Citadel to detect the write error and exit.
			time.Sleep(60 * time.Second)

			pods, err = env.GetPods(ns.Name(), "istio=citadel")
			if err != nil {
				t.Fatalf("failed to get Citadel pod")
			}
			cl, cErr := env.Logs(ns.Name(), pods[0].Name, "citadel", false /* previousLog */)
			pl, pErr := env.Logs(ns.Name(), pods[0].Name, "citadel", true /* previousLog */)
			expectedErr := []string{"Failed to write secret to CA", "Failed to create a self-signed Citadel"}
			if cl != "" && cErr == nil && strings.Contains(cl, expectedErr[0]) && strings.Contains(cl, expectedErr[1]) {
				// Found the strings in the current log.
			} else if pl != "" && pErr == nil && strings.Contains(pl, expectedErr[0]) && strings.Contains(pl, expectedErr[1]) {
				// Found the strings in the previous log.
			} else {
				t.Errorf("log does not match, expected strings %s and %s not found in logs. Current log: %s, previous log: %s",
					expectedErr[0], expectedErr[1], cl, pl)
			}

			// Verify that the CA-root secret remains intact.
			currentSecret, err := secrets.Get(ca.CASecret, metav1.GetOptions{})
			if err != nil {
				t.Fatal("Failed to retrieve Citadel CA-root secret.")
			}
			if !reflect.DeepEqual(citadelSecret, currentSecret) {
				t.Fatal("Citadel CA-root secret is changed!")
			}

			// Recover the normal setup for other tests.
			// Recover the normal RBAC policy.
			policies = tmpl.EvaluateAllOrFail(t, namespaceTmpl,
				file.AsStringOrFail(t, normalRBACConfigTmpl))

			g.ApplyConfigOrFail(t, ns, policies...)

			// Sleep 10 seconds for the policy to take effect.
			time.Sleep(10 * time.Second)

			// Delete Citadel pod, a new pod will be started.
			pods, err = env.GetPods(ns.Name(), "istio=citadel")
			if err != nil {
				t.Fatal("Failed to get Citadel pods.")
			}
			env.DeletePod(ns.Name(), pods[0].GetName())
			// Sleep 10 seconds for the Citadel pod to recreate.
			time.Sleep(10 * time.Second)
		})
}
