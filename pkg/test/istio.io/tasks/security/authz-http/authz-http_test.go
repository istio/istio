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
package tests

import (
	"os/exec"
	"testing"

	"istio.io/istio/pkg/test/istio.io/examples"
	"istio.io/istio/pkg/test/scopes"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"

	"istio.io/istio/pkg/test/framework/components/istio"
)

var (
	ist istio.Instance
)

const (
	ns = "default"
)

func TestMain(m *testing.M) {
	framework.NewSuite("authz-http", m).
		SetupOnEnv(environment.Kube, istio.Setup(&ist, nil)).
		RequireEnvironment(environment.Kube).
		Run()
}

// TestAuthzHTTP simulates the task in https://www.istio.io/docs/tasks/security/authz-http/
func TestAuthzHTTP(t *testing.T) {
	defer func() {
		cmd := exec.Command("./clean.sh")
		output, err := cmd.CombinedOutput()
		if err != nil {
			scopes.CI.Errorf("cleanup failed: %s", string(output))
		}
	}()

	setup(t)

	enablingIstioAuthorization(t)

	enforcingNamespaceLevelAccessControl(t)
	enforcingServiceLevelAccessControlStep1(t)
	enforcingServiceLevelAccessControlStep2(t)
	enforcingServiceLevelAccessControlStep3(t)
}

func setup(t *testing.T) {
	ex := examples.New(t, "Setup")
	defer ex.Run()

	ex.RunScript("setup.sh", examples.TextOutput)
	ex.Apply(ns, "samples/bookinfo/platform/kube/bookinfo.yaml")
	ex.Apply(ns, "samples/bookinfo/networking/bookinfo-gateway.yaml")
	ex.Apply(ns, "samples/sleep/sleep.yaml")
	ex.RunScript("wait.sh", examples.TextOutput)
}

func enablingIstioAuthorization(t *testing.T) {
	ex := examples.New(t, "Enabling Istio authorization")
	defer ex.Run()

	ex.Apply("", "samples/bookinfo/platform/kube/rbac/rbac-config-ON.yaml")
	ex.RunScript("verify-enablingIstioAuthorization.sh", examples.TextOutput)
}

func enforcingNamespaceLevelAccessControl(t *testing.T) {
	ex := examples.New(t, "Enforcing Namespace-level access control")
	defer ex.Run()

	ex.Apply(ns, "samples/bookinfo/platform/kube/rbac/namespace-policy.yaml")
	ex.RunScript("verify-enforcingNamespaceLevelAccessControl.sh", examples.TextOutput)
	ex.Delete(ns, "samples/bookinfo/platform/kube/rbac/namespace-policy.yaml")
	ex.RunScript("verify-enablingIstioAuthorization.sh", examples.TextOutput)
}

func enforcingServiceLevelAccessControlStep1(t *testing.T) {
	ex := examples.New(t, "Enforcing Service-level access control Step 1")
	defer ex.Run()

	ex.Apply(ns, "samples/bookinfo/platform/kube/rbac/productpage-policy.yaml")
	ex.RunScript("verify-enforcingServiceLevelAccessControlStep1.sh", examples.TextOutput)
}

func enforcingServiceLevelAccessControlStep2(t *testing.T) {
	ex := examples.New(t, "Enforcing Service-level access control Step 2")
	defer ex.Run()

	ex.Apply(ns, "samples/bookinfo/platform/kube/rbac/details-reviews-policy.yaml")
	ex.RunScript("verify-enforcingServiceLevelAccessControlStep2.sh", examples.TextOutput)
}

func enforcingServiceLevelAccessControlStep3(t *testing.T) {
	ex := examples.New(t, "Enforcing Service-level access control Step 3")
	defer ex.Run()

	ex.Apply(ns, "samples/bookinfo/platform/kube/rbac/ratings-policy.yaml")
	ex.RunScript("verify-enforcingServiceLevelAccessControlStep3.sh", examples.TextOutput)
}
