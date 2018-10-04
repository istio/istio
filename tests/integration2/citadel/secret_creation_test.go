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

package citadel

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/dependency"
)

// TestSecretCreationKubernetes verifies that Citadel creates secret and stores as Kubernetes secrets,
// and that when secrets are deleted, new secrets will be created.
func TestSecretCreationKubernetes(t *testing.T) {
	framework.Requires(t, dependency.Kubernetes, dependency.Citadel)
	env := framework.AcquireEnvironment(t)
	c := env.GetCitadelOrFail(t)

	// Test the existence of istio.default secret.
	s, err := c.WaitForSecretToExist()
	if err != nil {
		t.Error(err)
	}

	t.Log(`checking secret "istio.default" is correctly created`)
	if err := ExamineSecret(s); err != nil {
		t.Error(err)
	}

	// Delete the istio.default secret immediately
	if err := c.DeleteSecret(); err != nil {
		t.Error(err)
	}

	t.Log(`secret "istio.default" has been deleted`)

	// Test that the deleted secret is re-created properly.
	if _, err := c.WaitForSecretToExist(); err != nil {
		t.Error(err)
	}
	t.Log(`checking secret "istio.default" is correctly re-created`)
	if err := ExamineSecret(s); err != nil {
		t.Error(err)
	}
}

func TestMain(m *testing.M) {
	framework.Run("citadel_test", m)
}
