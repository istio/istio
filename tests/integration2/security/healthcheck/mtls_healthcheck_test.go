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
	"io/ioutil"
	"os"
	"testing"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/deployment"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/components/environment/kube"
	"istio.io/istio/pkg/test/scopes"
)

func TestMain(m *testing.M) {
	rt, _ := framework.Run("mtls_healthcheck", m)
	os.Exit(rt)
}

// TestMtlsHealthCheck verifies Kubernetes HTTP health check can work when mTLS
// is enabled.

func TestMtlsHealthCheck(t *testing.T) {
	ctx := framework.GetContext(t)
	scopes.Framework.SetOutputLevel(log.DebugLevel)
	scopes.CI.SetOutputLevel(log.DebugLevel)

	// Test requires this Helm flag to be enabled.
	kube.RegisterHelmOverrides(map[string]string{
		"sidecarInjectorWebhook.rewriteAppHTTPProbe": "true",
	})

	// Kube environment only used for this test since it requires istio installed with specific helm
	// values.
	ctx.RequireOrSkip(t, lifecycle.Test, &descriptors.KubernetesEnvironment)
	env := kube.GetEnvironmentOrFail(ctx, t)
	_, err := env.DeployYaml("healthcheck_mtls.yaml", lifecycle.Test)
	if err != nil {
		t.Error(err)
	}

	// Deploy app now.
	_, err = deployTestApp(env, lifecycle.Test)
	if err != nil {
		t.Errorf("failed to deploy %v", err)
	}
}

func deployTestApp(e *kube.Environment, scope lifecycle.Scope) (*deployment.Instance, error) {
	helmValues := e.HelmValueMap()
	b, err := ioutil.ReadFile("healthcheck.yaml")
	if err != nil {
		return nil, err
	}
	templateContent := string(b)
	result, err := e.EvaluateWithParams(templateContent, map[string]string{
		"Hub":             helmValues[kube.HubValuesKey],
		"Tag":             helmValues[kube.TagValuesKey],
		"ImagePullPolicy": helmValues[kube.ImagePullPolicyValuesKey],
	})
	if err != nil {
		return nil, err
	}

	out := deployment.NewYamlContentDeployment(e.NamespaceForScope(scope), result)
	if err = out.Deploy(e.Accessor, true); err != nil {
		return nil, err
	}
	return out, nil
}
