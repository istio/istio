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

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/framework/components/apps"
	"istio.io/istio/pkg/test/framework/components/deployment"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/scopes"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
)

var (
	ist istio.Instance
)

// go test -v ./tests/integration/security/healthcheck   -istio.test.env  \
// kube -istio.test.hub "gcr.io/istio-release" -istio.test.tag "master-latest-daily"
func TestMain(m *testing.M) {
	// TODO: remove before merge.
	scopes.Framework.SetOutputLevel(log.DebugLevel)
	scopes.CI.SetOutputLevel(log.DebugLevel)
	cfg, err := istio.DefaultConfig2()
	if err != nil {
		log.Errorf("failed with error %v", err)
		os.Exit(-1)
	}
	cfg.Values["sidecarInjectorWebhook.rewriteAppHTTPProbe"] = "true"
	framework.Main("mtls_healthcheck", m, istio.SetupOnKube(&ist, &cfg))
}

func setup(ctx framework.SuiteContext) error {
	return nil
}

// TestMtlsHealthCheck verifies Kubernetes HTTP health check can work when mTLS
// is enabled.
func TestMtlsHealthCheck(t *testing.T) {
	ctx := framework.NewContext(t)
	ctx.RequireOrSkip(t, environment.Kube)

	ns := namespace.ClaimOrFail(t, ctx, "default")
	_, err := deployment.New(ctx, deployment.Config{
		Yaml: `apiVersion: "authentication.istio.io/v1alpha1"
kind: "Policy"
metadata:
  name: "mtls-strict-for-healthcheck"
spec:
  targets:
  - name: "healthcheck"
  peers:
    - mtls:
        mode: STRICT
`,
		Namespace: ns,
	})
	if err != nil {
		t.Error(err)
	}
	pilot := pilot.NewOrFail(t, ctx, pilot.Config{})
	aps := apps.NewOrFail(ctx, t, apps.Config{Pilot: pilot, AppParams: []apps.AppParam{
		apps.AppParam{Name: "healthcheck"},
	}})
	aps.GetAppOrFail("healthcheck", t)
	// TODO(incfly): add a negative test once we have a per deployment annotation support for this feature.
}
