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

package sds_citadel_test

import (
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/apps"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/util/connection"
	"istio.io/istio/pkg/test/util/policy"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	inst istio.Instance
)

func TestSdsCitadelCaFlow(t *testing.T) {
	ctx := framework.NewContext(t)
	defer ctx.Done(t)
	ctx.RequireOrSkip(t, environment.Kube)

	istioCfg, err := istio.DefaultConfig(ctx)
	if err != nil {
		t.Fatalf("Fail to get default config: %v", err)
	}
	env := ctx.Environment().(*kube.Environment)
	g := galley.NewOrFail(t, ctx, galley.Config{})
	p := pilot.NewOrFail(t, ctx, pilot.Config{
		Galley: g,
	})
	appInst := apps.NewOrFail(t, ctx, apps.Config{Pilot: p, Galley: g})

	aApp, _ := appInst.GetAppOrFail("a", t).(apps.KubeApp)
	bApp, _ := appInst.GetAppOrFail("b", t).(apps.KubeApp)

	connections := []connection.Connection{
		{
			From:            aApp,
			To:              bApp,
			Port:            80,
			Protocol:        apps.AppProtocolHTTP,
			ExpectedSuccess: true,
		},
	}

	namespace := istioCfg.SystemNamespace
	configFile := "global-mtls.yaml"

	testPolicy := policy.ApplyPolicyFile(t, env, namespace, configFile)
	defer testPolicy.TearDown()
	// Sleep 3 seconds for the policy to take effect.
	time.Sleep(3 * time.Second)
	for _, conn := range connections {
		retry.UntilSuccessOrFail(t, func() error {
			return connection.CheckConnection(t, conn)
		}, retry.Delay(time.Second), retry.Timeout(10*time.Second))
	}
}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.Values["sidecarInjectorWebhook.rewriteAppHTTPProbe"] = "true"
	cfg.Values["global.controlPlaneSecurityEnabled"] = "false"
	cfg.Values["global.mtls.enabled"] = "true"
	cfg.Values["global.sds.enabled"] = "true"
	cfg.Values["global.sds.udsPath"] = "unix:/var/run/sds/uds_path"
	cfg.Values["global.sds.useNormalJwt"] = "true"
	cfg.Values["nodeagent.enabled"] = "true"
	cfg.Values["nodeagent.image"] = "node-agent-k8s"
	cfg.Values["nodeagent.env.CA_PROVIDER"] = "Citadel"
	cfg.Values["nodeagent.env.CA_ADDR"] = "istio-citadel:8060"
	cfg.Values["nodeagent.env.VALID_TOKEN"] = "true"
}

func TestMain(m *testing.M) {
	// Integration test for the SDS Citadel CA flow, as well as mutual TLS
	// with the certificates issued by the SDS Citadel CA flow.
	framework.
		NewSuite("sds_citadel_flow_test", m).
		Label(label.CustomSetup).
		SetupOnEnv(environment.Kube, istio.Setup(&inst, setupConfig)).
		Run()

}
