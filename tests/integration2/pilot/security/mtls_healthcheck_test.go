//  Copyright 2019 Istio Authors
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

// Package basic contains an example test suite for showcase purposes.
package security

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/api/components"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/components/environment/kube"
)

// TestMtlsHealthCheck verifies Kubernetes HTTP health check can work when mTLS
// is enabled.
// TODO(incfly): next step for integration test istio.io/istio/pkg/test/framework/runtime/components/apps/kube.go
// Allow ourselves to on demand deploy apps, modifying on existing app yaml does not work.
// Changes compared to existing:
// - Declare readiness probe http.
// - Declare service port for 3333 or other service.

func TestMtlsHealthCheck(t *testing.T) {
	ctx := framework.GetContext(t)
	ctx.RequireOrSkip(t, lifecycle.Test, &descriptors.KubernetesEnvironment)
	path := filepath.Join(ctx.WorkDir(), "mtls-strict-healthcheck.yaml")
	err := ioutil.WriteFile(path, []byte(`apiVersion: "authentication.istio.io/v1alpha1"
kind: "Policy"
metadata:
  name: "mtls-strict-for-a"
spec:
  targets:
  - name: "a"
  peers:
    - mtls:
        mode: STRICT
`), 0666)
	if err != nil {
		t.Errorf("failed to create authn policy in file %v", err)
	}
	env := kube.GetEnvironmentOrFail(ctx, t)
	_, err = env.DeployYaml(path, lifecycle.Test)
	if err != nil {
		t.Error(err)
	}

	// Deploy app now.
	ctx.RequireOrFail(t, lifecycle.Test, &descriptors.Apps)
	apps := components.GetApps(ctx, t)
	apps.GetAppOrFail("a", t)
}
