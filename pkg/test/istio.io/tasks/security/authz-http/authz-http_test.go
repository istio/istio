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
	"testing"

	"istio.io/istio/pkg/test/istio.io/examples"

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
	ex := examples.New(t, "authz-http")

	setup(&ex)

	enablingIstioAuthorization(&ex)
	enforcingNamespaceLevelAccessControl(&ex)
	enforcingServiceLevelAccessControlStep1(&ex)
	enforcingServiceLevelAccessControlStep2(&ex)
	enforcingServiceLevelAccessControlStep3(&ex)

	clean(&ex)

	ex.Run()
}

func setup(ex *examples.Example) {
	ex.AddScript(ns, "setup.sh", examples.TextOutput)
	ex.AddFile(ns, "samples/bookinfo/platform/kube/bookinfo.yaml")
	ex.AddFile(ns, "samples/bookinfo/networking/bookinfo-gateway.yaml")
	ex.AddFile(ns, "samples/sleep/sleep.yaml")
	ex.AddScript(ns, "wait.sh", examples.TextOutput)
}

func enablingIstioAuthorization(ex *examples.Example) {
	ex.AddFile("", "samples/bookinfo/platform/kube/rbac/rbac-config-ON.yaml")
	ex.AddScript(ns, "verify-enablingIstioAuthorization.sh", examples.TextOutput)
}

func enforcingNamespaceLevelAccessControl(ex *examples.Example) {
	ex.AddFile(ns, "samples/bookinfo/platform/kube/rbac/namespace-policy.yaml")
	ex.AddScript(ns, "verify-enforcingNamespaceLevelAccessControl.sh", examples.TextOutput)
	ex.DeleteFile(ns, "samples/bookinfo/platform/kube/rbac/namespace-policy.yaml")
}

func enforcingServiceLevelAccessControlStep1(ex *examples.Example) {
	ex.AddFile(ns, "samples/bookinfo/platform/kube/rbac/productpage-policy.yaml")
	ex.AddScript(ns, "verify-enforcingServiceLevelAccessControlStep1.sh", examples.TextOutput)
}

func enforcingServiceLevelAccessControlStep2(ex *examples.Example) {
	ex.AddFile(ns, "samples/bookinfo/platform/kube/rbac/details-reviews-policy.yaml")
	ex.AddScript(ns, "verify-enforcingServiceLevelAccessControlStep2.sh", examples.TextOutput)
}

func enforcingServiceLevelAccessControlStep3(ex *examples.Example) {
	ex.AddFile(ns, "samples/bookinfo/platform/kube/rbac/ratings-policy.yaml")
	ex.AddScript(ns, "verify-enforcingServiceLevelAccessControlStep3.sh", examples.TextOutput)
}

func clean(ex *examples.Example) {
	ex.AddScript(ns, "clean.sh", examples.TextOutput)
	ex.DeleteFile(ns, "samples/bookinfo/platform/kube/bookinfo.yaml")
	ex.DeleteFile(ns, "samples/bookinfo/networking/bookinfo-gateway.yaml")
	ex.DeleteFile(ns, "samples/sleep/sleep.yaml")
	ex.DeleteFile("", "samples/bookinfo/platform/kube/rbac/rbac-config-ON.yaml")
	ex.DeleteFile(ns, "samples/bookinfo/platform/kube/rbac/productpage-policy.yaml")
	ex.DeleteFile(ns, "samples/bookinfo/platform/kube/rbac/details-reviews-policy.yaml")
	ex.DeleteFile(ns, "samples/bookinfo/platform/kube/rbac/ratings-policy.yaml")
}
