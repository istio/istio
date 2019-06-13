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

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/citadel"
	"istio.io/istio/tests/integration/security/util/secret"
)

// TestSecretCreationKubernetes verifies that Citadel creates secret and stores as Kubernetes secrets,
// and that when secrets are deleted, new secrets will be created.
func TestSecretCreationKubernetes(t *testing.T) {
	framework.NewTest(t).Run(func(ctx framework.TestContext) {
		c := citadel.NewOrFail(t, ctx, citadel.Config{Istio: ist})

		// Test the existence of istio.default secret.
		s := c.WaitForSecretToExistOrFail(t)

		t.Log(`checking secret "istio.default" is correctly created`)
		secret.ExamineOrFail(t, s)

		// Delete the istio.default secret immediately
		c.DeleteSecretOrFail(t)

		t.Log(`secret "istio.default" has been deleted`)

		// Test that the deleted secret is re-created properly.
		s = c.WaitForSecretToExistOrFail(t)
		t.Log(`checking secret "istio.default" is correctly re-created`)
		secret.ExamineOrFail(t, s)
	})
}
