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
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/api/components"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/components/environment/kube"
)

// TestMtlsHealthCheck verifies Kubernetes HTTP health check can work when mTLS
// is enabled.

func TestMtlsHealthCheck(t *testing.T) {
	ctx := framework.GetContext(t)
	// Test requires this Helm flag to be enabled.
	// TODO: instead trying to tear down the entire system for this test...
	// might not make much sense to use helmValues by scope...
	kube.RegisterHelmOverrides(lifecycle.System, map[string]string{
		"sidecarInjectorWebhook.rewriteAppHTTPProbe": "true",
		// TODO(incfly): without this mixer livenes probe failure, investigate...
		"global.mtls.enabled": "false",
	})
	ctx.RequireOrSkip(t, lifecycle.Test, &descriptors.KubernetesEnvironment)
	path := filepath.Join(ctx.WorkDir(), "mtls-strict-healthcheck.yaml")
	err := ioutil.WriteFile(path, []byte(`apiVersion: "authentication.istio.io/v1alpha1"
kind: "Policy"
metadata:
  name: "mtls-strict-for-healthcheck"
spec:
  targets:
  - name: "healthcheck"
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
	apps.GetAppOrFail("healthcheck", t)
	time.Sleep(1000 * time.Second)
}
