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
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/security/pkg/k8s/controller"

	v1 "k8s.io/api/core/v1"
	mv1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	testNsPrefix       = "targeting-test"
	serviceAccountName = "test-service-account"
	// timeout before we give up looking for secret creation
	secretTimeout = time.Second * 15
)

// TestNamespaceTargeting verifies that Citadel respects the designated namespace targeting labels.
func TestNamespaceTargeting(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).Run(func(ctx framework.TestContext) {

		env := ctx.Environment().(*kube.Environment)

		citadelNamespace := ist.Settings().SystemNamespace
		testCases := map[string]struct {
			citadelLabels map[string]string
			targeted      bool
		}{
			"no env label, false override label": {
				citadelLabels: map[string]string{
					controller.NamespaceOverrideLabel: "false",
				},
				targeted: false,
			},
			"matching env label, no override label": {
				citadelLabels: map[string]string{
					controller.NamespaceManagedLabel: citadelNamespace,
				},
				targeted: true,
			},
			"no relevant namespace labels": {
				citadelLabels: map[string]string{},
				targeted:      true,
			},
			"matching env label, false override label": {
				citadelLabels: map[string]string{
					controller.NamespaceManagedLabel:  citadelNamespace,
					controller.NamespaceOverrideLabel: "false",
				},
				targeted: false,
			},
		}

		for name, tc := range testCases {
			ctx.NewSubTest(name).
				Run(func(ctx framework.TestContext) {
					runNamespaceTargetingTest(env, ctx, tc.citadelLabels, tc.targeted)
				})
		}
	})
}

// creates namespace with the supplied labels, checks if ServiceAccount secrets are generated,
// and compares to whether secrets should have been generated
func runNamespaceTargetingTest(env *kube.Environment, ctx framework.TestContext, citadelLabels map[string]string, targeted bool) {
	ctx.Helper()

	nsConfig := namespace.Config{
		Prefix: testNsPrefix,
		Labels: citadelLabels,
	}

	ns := namespace.NewOrFail(ctx, ctx, nsConfig)

	// create service account in newly generated namespace
	serviceAccount := &v1.ServiceAccount{
		ObjectMeta: mv1.ObjectMeta{
			Name: serviceAccountName,
		},
	}
	env.GetServiceAccount(ns.Name()).Create(serviceAccount)

	// generated service account secret will take the form istio.{service_account_name}
	secretName := fmt.Sprintf("istio.%s", serviceAccountName)
	_, err := waitForSecret(env, secretName, ns.Name(), secretTimeout)

	secretGenerated := err == nil
	if targeted != secretGenerated {
		ctx.Fatalf("expected secret generation for secret %s: %t, but got: %t",
			secretName, targeted, secretGenerated)
	}
}

// waitForSecret waits up to 30 seconds for secret generation, and returns on timeout
func waitForSecret(
	env *kube.Environment, name, ns string, timeout time.Duration) (interface{}, error) {
	sa, err := retry.Do(func() (interface{}, bool, error) {
		sa, err := env.GetSecret(ns).Get(name, mv1.GetOptions{})
		if err != nil {
			return nil, false, err
		}
		return sa, true, nil
	}, retry.Timeout(timeout))
	return sa, err
}
