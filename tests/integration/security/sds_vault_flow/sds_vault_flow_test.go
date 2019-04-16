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
	"istio.io/istio/pkg/test/util/connection"
	"istio.io/istio/pkg/test/util/policy"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	inst istio.Instance
)

const (
	tlsRootCert = "-----BEGIN CERTIFICATE-----\\\\nMIIC3jCCAcagAwIBAgIRAIcSFH1jneS0XPz5r2Q" +
		"DbigwDQYJKoZIhvcNAQELBQAw\\\\nEDEOMAwGA1UEChMFVmF1bHQwIBcNMTgxMjI2MDkwM" +
		"DU3WhgPMjExODEyMDIwOTAw\\\\nNTdaMBAxDjAMBgNVBAoTBVZhdWx0MIIBIjANBgkqhki" +
		"G9w0BAQEFAAOCAQ8AMIIB\\\\nCgKCAQEA2q5lfJCLAOTEjX3xV8qMLEX8zUQpd0AjD6zzO" +
		"Mzx51GVM7Plf7CJmaDq\\\\nyloRz3zcrTEltHUrln5fvouvp4TetOlqEU979vvccnFLgXr" +
		"Spn+Zt/EyjE0rUYY3\\\\n5e2qxy9bP2E7zJSKONIT6zRDd2zUQGH3zUem1ZG0GFY1ZL5qF" +
		"SOIy+PvuQ4u8HCa\\\\n1CcnHmI613fVDbFbaxuF2G2MIwCZ/Fg6KBd9kgU7uCOvkbR4AtR" +
		"e0ntwweIjOIas\\\\nFiohPQzVY4obrYZiTV43HT4lGti7ySn2c96UnRSnmHLWyBb7cafd4" +
		"WZN/t+OmYSd\\\\nooxCVQ2Zqub6NlZ5OySYOz/0BJq6DQIDAQABozEwLzAOBgNVHQ8BAf8" +
		"EBAMCBaAw\\\\nDAYDVR0TAQH/BAIwADAPBgNVHREECDAGhwQj6fn5MA0GCSqGSIb3DQEBC" +
		"wUAA4IB\\\\nAQBORvUcW0wgg/Wo1aKFaZQuPPFVLjOZat0QpCJYNDhsSIO4Y0JS+Y1cEIk" +
		"vXB3S\\\\nQ3D7IfNP0gh1fhtP/d45LQSPqpyJF5vKWAvwa/LSPKpw2+Zys4oDahcH+SEKi" +
		"Qco\\\\nIhkkHNEgC4LEKEaGvY4A8Cw7uWWquUJB16AapSSnkeD2vTcxErfCO59yR7yEWDa" +
		"6\\\\n8j6QNzmGNj2YXtT86+Mmedhfh65Rrh94mhAPQHBAdCNGCUwZ6zHPQ6Z1rj+x3" +
		"Wm9\\\\ngqpveVq2olloNbnLNmM3V6F9mqSZACgADmRqf42bixeHczkTfRDKThJcpY5" +
		"U44vy\\\\nw4Nm32yDWhD6AC68rDkXX68m\\\\n-----END CERTIFICATE-----"
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
	cfg.Values["nodeagent.env.CA_ADDR"] = "https://35.233.249.249:8200"
	cfg.Values["nodeagent.env.CA_PROVIDER"] = "VaultCA"
	cfg.Values["nodeagent.env.VALID_TOKEN"] = "true"
	cfg.Values["nodeagent.env.VAULT_ADDR"] = "https://35.233.249.249:8200"
	cfg.Values["nodeagent.env.VAULT_AUTH_PATH"] = "auth/kubernetes/login"
	cfg.Values["nodeagent.env.VAULT_ROLE"] = "istio-cert"
	cfg.Values["nodeagent.env.VAULT_SIGN_CSR_PATH"] = "istio_ca/sign/istio-pki-role"
	cfg.Values["nodeagent.env.VAULT_TLS_ROOT_CERT"] = tlsRootCert
	cfg.Values["global.proxy.excludeIPRanges"] = "35.233.249.249/32"
}

func TestMain(m *testing.M) {
	// Integration test for the SDS Vault CA flow, as well as mutual TLS
	// with the certificates issued by the SDS Vault CA flow.
	framework.Main("sds_vault_flow_test", m, istio.SetupOnKube(&inst, setupConfig))
}
