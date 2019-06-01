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

package citadel

import (
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/citadel"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/security/pkg/pki/ca"
)

const (
	rbacConfigTmpl = "testdata/citadel-noread-rbac.yaml.tmpl"
)

// TestCitadelBootstrapKubernetes verifies that Citadel exits if:
// It fails to read the CA-root secret, and then -
// It tries to create a new CA-root secret and fails because the secret already exists.
func TestCitadelBootstrapKubernetes(t *testing.T) {
	framework.NewTest(t).Run(func(ctx framework.TestContext) {
		ns := namespace.ClaimOrFail(t, ctx, ist.Settings().IstioNamespace)

		namespaceTmpl := map[string]string{
			"Namespace": ns.Name(),
		}
		// Create the Citadel's CA-root secret for signing key and cert.
		env := ctx.Environment().(*kube.Environment)
		secrets := env.GetSecret(ns.Name())
		pemCert := []byte("ABC")
		pemKey := []byte("DEF")
		citadelSecret := ca.BuildSecret("", ca.CASecret, ns.Name(), nil, nil, nil, pemCert, pemKey, "istio.io/ca-root")
		secrets.Create(citadelSecret)

		// Apply the RBAC policy to prevent Citadel to read the CA-root secret.
		policies := tmpl.EvaluateAllOrFail(t, namespaceTmpl,
			file.AsStringOrFail(t, rbacConfigTmpl))

		g.ApplyConfigOrFail(t, ns, policies...)
		defer g.DeleteConfigOrFail(t, ns, policies...)

		// Sleep 60 seconds for the policy to take effect.
		time.Sleep(60 * time.Second)

		// Start Citadel
		_ = citadel.NewOrFail(t, ctx, citadel.Config{Istio: ist})
		t.Log(`checking citadel exits after a while`)
		time.Sleep(10 * time.Second)
		fetchFn := env.NewSinglePodFetch(ns.Name(), "istio=citadel")
		err := env.WaitUntilPodsAreDeleted(fetchFn)
		if err != nil {
			t.Fatal("Citadel pod is not deleted when writing to CA-root secret fails.")
		}

		// [TODO] Verify that the secret is untouched.
	})
}
