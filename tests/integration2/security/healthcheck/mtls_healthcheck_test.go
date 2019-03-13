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

// Package healthcheck contains a test to support kubernetes app health check when mTLS is turned on.
// https://github.com/istio/istio/issues/9150.
package healthcheck

import (
	"os"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/api/components"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/components/apps"
	"istio.io/istio/pkg/test/framework/runtime/components/environment/kube"
)

func TestMain(m *testing.M) {
	rt, _ := framework.Run("mtls_healthcheck", m)
	os.Exit(rt)
}

// TestMtlsHealthCheck verifies Kubernetes HTTP health check can work when mTLS
// is enabled.
func TestMtlsHealthCheck(t *testing.T) {
	ctx := framework.GetContext(t)

	// TODO: remove before merge.
	// scopes.Framework.SetOutputLevel(log.DebugLevel)
	// scopes.CI.SetOutputLevel(log.DebugLevel)

	// Kube environment only used for this test since it requires istio installed with specific helm
	// values.
	kubeEnvironment := descriptors.KubernetesEnvironment

	// Test requires this Helm flag to be enabled.
	// No need for special clean up since this is system level component.
	kubeEnvironment.Configuration = kube.IstioConfiguration{
		Values: map[string]string{
			"sidecarInjectorWebhook.rewriteAppHTTPProbe": "true",
		},
	}
	ctx.RequireOrSkip(t, lifecycle.Test, &kubeEnvironment)
	env := kube.GetEnvironmentOrFail(ctx, t)
	_, err := env.DeployYaml("healthcheck_mtls.yaml", lifecycle.Test)
	if err != nil {
		t.Error(err)
	}
	healthcheckApp := apps.KubeApp{
		Deployment:     "healthcheck",
		Service:        "healthcheck",
		Version:        "v1",
		Port1:          80,
		Port2:          8080,
		Port3:          90,
		Port4:          9090,
		Port5:          70,
		Port6:          7070,
		InjectProxy:    true,
		Headless:       false,
		ServiceAccount: true,
	}
	// Deploy app now.
	kubeApps := &descriptors.Apps
	kubeApps.Configuration = apps.KubeAppsConfig{healthcheckApp}
	ctx.RequireOrFail(t, lifecycle.Test, kubeApps)
	apps := ctx.GetComponentOrFail(kubeApps, t).(components.Apps)

	// Being able to resolve healthcheck apps means that the health check is done.
	apps.GetAppOrFail("healthcheck", t)
}
