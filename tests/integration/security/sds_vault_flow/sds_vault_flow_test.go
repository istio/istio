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

package sds_vault_test

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

const (
	tlsRootCert = "-----BEGIN CERTIFICATE-----\\\\nMIIC3jCCAcagAwIBAgIRAO1S7vuRQmo2He+RtBq3" +
		"fv8wDQYJKoZIhvcNAQELBQAw\\\\nEDEOMAwGA1UEChMFVmF1bHQwIBcNMTkwNDI3MTY1ODE1WhgPMjExO" +
		"TA0MDMxNjU4\\\\nMTVaMBAxDjAMBgNVBAoTBVZhdWx0MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIB\\\\nCg" +
		"KCAQEA7/CTbnENEIvFZg9hmVtYnOx3OfMy/GNCuP7sqtAeVVTopAKKkcAAWQck\\\\nrhpBooEGpCugNxXGNCuJ" +
		"h/2nu0AfGFRfdafwSJRoI6yHwQouDm0o4r3h9uL3tu5N\\\\nD+x9j+eejbFsoZVn84CxGkEB6oyeXYHjc6eWh3" +
		"PFGMtKuOQD4pezvDH0yNCx5waK\\\\nhtPuYtl0ebfdbyh+WQuptO+Q9VSaQNqE3ipZ461y8PduwRRll241W0gQ" +
		"B2iasX03\\\\nD36F2ZrMz3KEVRVKM1yCUDCy2RPJqkXPdnVMWmDGbe8Uw69zr25JltzuRZFT9HL3\\\\nY1RnM" +
		"TecmSc4ikTUHcMhFX3PYbfR5wIDAQABozEwLzAOBgNVHQ8BAf8EBAMCBaAw\\\\nDAYDVR0TAQH/BAIwADAPBgN" +
		"VHREECDAGhwQiU4HTMA0GCSqGSIb3DQEBCwUAA4IB\\\\nAQCdLh6olDVQB71LD6srbfAE4EsxLEBbIRnv7Nf1S" +
		"0KQwgW/QxK8DHBwJBxJkr1N\\\\nzgEPx86f2Fo2UsY9m6rvgP3+iquyMsKi0ooUah3y3LSnONuZcdfSTl/HYd3" +
		"8S6Dp\\\\nVkVOZ7781xxpFVUqQ5voQX1Y1Ipn5qw0FyIcNYWLkNX+iMf1b9kpEIWQNhRC/Yiv\\\\nTS0VA/Bz" +
		"QemGyf2UB6QsuZLH+JFEZnzU859qURnNIITa1Wf4YUtka5Sp1kDnEll3\\\\nwj4IlXKU+Wl1CzxJyn4SSQAXy/" +
		"Lb08ZKrF/YSzcIISnRX5j+wa8ApOSwwA/B7iaT\\\\nTWz1g+RlV9qHap70eIjPsQvb\\\\n-----END CERTIFICATE-----"

	vaultAddr      = "https://34.83.129.211:8200"
	excludeIPRange = "34.83.129.211/32"
)

func TestSdsVaultCaFlow(t *testing.T) {
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
	cfg.Values["global.sds.customTokenDirectory"] = "/etc/sdstoken"
	cfg.Values["nodeagent.enabled"] = "true"
	cfg.Values["nodeagent.image"] = "node-agent-k8s"
	cfg.Values["nodeagent.env.CA_ADDR"] = vaultAddr
	cfg.Values["nodeagent.env.CA_PROVIDER"] = "VaultCA"
	cfg.Values["nodeagent.env.VALID_TOKEN"] = "true"
	cfg.Values["nodeagent.env.VAULT_ADDR"] = vaultAddr
	cfg.Values["nodeagent.env.VAULT_AUTH_PATH"] = "auth/kubernetes/login"
	cfg.Values["nodeagent.env.VAULT_ROLE"] = "istio-cert"
	cfg.Values["nodeagent.env.VAULT_SIGN_CSR_PATH"] = "istio_ca/sign/istio-pki-role"
	cfg.Values["nodeagent.env.VAULT_TLS_ROOT_CERT"] = tlsRootCert
	cfg.Values["global.proxy.excludeIPRanges"] = excludeIPRange
}

func TestMain(m *testing.M) {
	// Integration test for the SDS Vault CA flow, as well as mutual TLS
	// with the certificates issued by the SDS Vault CA flow.
	framework.NewSuite("sds_vault_flow_test", m).
		Label(label.CustomSetup).
		SetupOnEnv(environment.Kube, istio.Setup(&inst, setupConfig)).
		Run()
}
