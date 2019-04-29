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

package vault

import (
	"context"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/label"

	vault "istio.io/istio/security/pkg/nodeagent/caclient/providers/vault"
)

var (
	vaultServerTLSCert = `
-----BEGIN CERTIFICATE-----
MIIC3jCCAcagAwIBAgIRAO1S7vuRQmo2He+RtBq3fv8wDQYJKoZIhvcNAQELBQAw
EDEOMAwGA1UEChMFVmF1bHQwIBcNMTkwNDI3MTY1ODE1WhgPMjExOTA0MDMxNjU4
MTVaMBAxDjAMBgNVBAoTBVZhdWx0MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIB
CgKCAQEA7/CTbnENEIvFZg9hmVtYnOx3OfMy/GNCuP7sqtAeVVTopAKKkcAAWQck
rhpBooEGpCugNxXGNCuJh/2nu0AfGFRfdafwSJRoI6yHwQouDm0o4r3h9uL3tu5N
D+x9j+eejbFsoZVn84CxGkEB6oyeXYHjc6eWh3PFGMtKuOQD4pezvDH0yNCx5waK
htPuYtl0ebfdbyh+WQuptO+Q9VSaQNqE3ipZ461y8PduwRRll241W0gQB2iasX03
D36F2ZrMz3KEVRVKM1yCUDCy2RPJqkXPdnVMWmDGbe8Uw69zr25JltzuRZFT9HL3
Y1RnMTecmSc4ikTUHcMhFX3PYbfR5wIDAQABozEwLzAOBgNVHQ8BAf8EBAMCBaAw
DAYDVR0TAQH/BAIwADAPBgNVHREECDAGhwQiU4HTMA0GCSqGSIb3DQEBCwUAA4IB
AQCdLh6olDVQB71LD6srbfAE4EsxLEBbIRnv7Nf1S0KQwgW/QxK8DHBwJBxJkr1N
zgEPx86f2Fo2UsY9m6rvgP3+iquyMsKi0ooUah3y3LSnONuZcdfSTl/HYd38S6Dp
VkVOZ7781xxpFVUqQ5voQX1Y1Ipn5qw0FyIcNYWLkNX+iMf1b9kpEIWQNhRC/Yiv
TS0VA/BzQemGyf2UB6QsuZLH+JFEZnzU859qURnNIITa1Wf4YUtka5Sp1kDnEll3
wj4IlXKU+Wl1CzxJyn4SSQAXy/Lb08ZKrF/YSzcIISnRX5j+wa8ApOSwwA/B7iaT
TWz1g+RlV9qHap70eIjPsQvb
-----END CERTIFICATE-----
  `
	testCsr1 = `
-----BEGIN CERTIFICATE REQUEST-----
MIICojCCAYoCAQAwEzERMA8GA1UEAxMId29ya2xvYWQwggEiMA0GCSqGSIb3DQEB
AQUAA4IBDwAwggEKAoIBAQCtUKHNG598mQ0wo5+AfZhn2yA8HhL1QV0XERJgBU2p
PbH/4yIHq++kugWWbj4REE7OPvKjJRdo8yJ9OpjDXA8s5t7fchdr6BePLF6+GfkQ
ACmnKAziRHMg22Zy+crdVEiyrMAzwujbiBxiI5hcHHB15TX+6lAxaLZJ3BLC4NBd
YHUeEwvuBV4zLLvKSVE6jFQIvxHKk/Nh/sJvvvSIOWmXPgS6raFPKPTDJ3MjFyCU
VEz8/HWyaEptX4C91NQxa7/CIJ/DYXtKVbP+jXGaLrLQUX+2r95H2cU604OfMz2Z
PmYgYUovtb93llwgLKoJk3MjIGEvy4AluGqegrDe5ghfAgMBAAGgSjBIBgkqhkiG
9w0BCQ4xOzA5MDcGA1UdEQQwMC6GLHNwaWZmZTovL2NsdXN0ZXIubG9jYWwvbnMv
ZGVmYXVsdC9zYS9kZWZhdWx0MA0GCSqGSIb3DQEBCwUAA4IBAQCRnzNqI46M1FJL
IWaQsZj7QeJPrPmuwcGzQ5qRlXBmxAe95N+9DKpmiTwU0tOz375EEjXwVYvs1cZT
d75Br1kaAMT70LnPUxvSjlcTNItLwlu6LoH/BuaFa5VL1dKFvjRQC3aKFKD634pX
U82yKWa7kAVPWJAizoz+wf0RIF2KEp0wpd/FPQJaFkAiTrC8rwEhPIfKTLads4HL
5pWcfODn5eMC7+htiteWsfdhK8Bxjz0VyzSs3BbgAHs+LFkIBGkKe0sl/ii96Bik
SQYzPWVk89gu6nKV+fS2pA9C8dAnYOzVu9XXc+PGlcIhjnuS+/P74hN5D3aIGljW
7WsYeEkp
-----END CERTIFICATE REQUEST-----
  `

	citadelSANonTLS = "eyJhbGciOiJSUzI1NiIsImtpZCI6IiJ9.eyJpc3MiOiJrdWJlcm5ldGVzL3Nlcn" +
		"ZpY2VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9uYW1lc3BhY2" +
		"UiOiJkZWZhdWx0Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9zZWNyZXQubm" +
		"FtZSI6InZhdWx0LWNpdGFkZWwtc2EtdG9rZW4tNjhsNzgiLCJrdWJlcm5ldGVzLmlvL3" +
		"NlcnZpY2VhY2NvdW50L3NlcnZpY2UtYWNjb3VudC5uYW1lIjoidmF1bHQtY2l0YWRlbC" +
		"1zYSIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2VydmljZS1hY2NvdW50Ln" +
		"VpZCI6IjcxNzllZGEzLTY4YWYtMTFlOS04MTgyLTQyMDEwYThhMDE1YiIsInN1YiI6In" +
		"N5c3RlbTpzZXJ2aWNlYWNjb3VudDpkZWZhdWx0OnZhdWx0LWNpdGFkZWwtc2EifQ.P5i" +
		"HNjeIfdvp376YBpM3rmNc7cDzlEWTdDxH6HIkeQtqlDmk_Xf6j4orb5zLYqJspW0DSe0" +
		"cfdbpoUraFwayaf95N5OrbkHCtPAe5ySOLmKBKO83YRnNGPrpFSQTqb8rvlU291JYEHC" +
		"LgsuHEeqjNWcGMgwXaHZzInwg6EOlqNvfss0ZxqGTSUIspxQDLrfGD5laO2PoXo29Xk_" +
		"-xULq8uSaPXb1PATI8d48f7pcWQrxnbhpvTqD2GCqa9kdr_1DiKxkS385K_8HkqIkSvR" +
		"4iVCISymvFxXv5v4ZSL3bo0cdOySIttdv7kUOWu4_9lo2-k3Hfz8LcQJcGl8paYsIUQ"

	citadelSATLS = "eyJhbGciOiJSUzI1NiIsImtpZCI6IiJ9.eyJpc3MiOiJrdWJlcm5ldGVzL3Nlcn" +
		"ZpY2VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9uYW1lc3BhY2" +
		"UiOiJkZWZhdWx0Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9zZWNyZXQubm" +
		"FtZSI6InZhdWx0LWNpdGFkZWwtc2EtdG9rZW4tNzR0d3MiLCJrdWJlcm5ldGVzLmlvL3" +
		"NlcnZpY2VhY2NvdW50L3NlcnZpY2UtYWNjb3VudC5uYW1lIjoidmF1bHQtY2l0YWRlbC" +
		"1zYSIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2VydmljZS1hY2NvdW50Ln" +
		"VpZCI6IjJhYzAzYmEyLTY5MTUtMTFlOS05NjkwLTQyMDEwYThhMDExNCIsInN1YiI6In" +
		"N5c3RlbTpzZXJ2aWNlYWNjb3VudDpkZWZhdWx0OnZhdWx0LWNpdGFkZWwtc2EifQ.pZ8" +
		"SiyNeO0p1p8HB9oXvXOAI1XCJZKk2wVHXBsTSzKWxlVD9HrHbAcSbO2dlhFpeCgknt6e" +
		"ZywvhShZJh2F6-iHP_YoUVoCqQmzjPoB3c3JoYFpJo-9jTN1_mNRtZUcNvYl-tDlTmBl" +
		"aKEvoC5P2WGVUF3AoLsES66u4FG9WllmLV92LG1WNqx_ltkT1tahSy9WiHQgyzPqwtwE" +
		"72T1jAGdgVIoJy1lfSaLam_bo9rqkRlgSg-au9BAjZiDGtm9tf3lwrcgfbxccdlG4jAs" +
		"TFa2aNs3dW4NLk7mFnWCJa-iWj-TgFxf9TW-9XPK0g3oYIQ0Id0CIW2SiFxKGPAjB-g"

	vaultNonTLSAddr = "http://35.233.183.49:8200"
	vaultTLSAddr    = "https://34.83.129.211:8200"
)

type clientConfig struct {
	vaultAddr        string
	vaultLoginRole   string
	vaultLoginPath   string
	vaultSignCsrPath string
	clientToken      string
	csr              []byte
}

func TestClientOnExampleHttpVaultCA(t *testing.T) {
	testCases := map[string]struct {
		cliConfig clientConfig
	}{
		"Valid certs 1": {
			cliConfig: clientConfig{vaultAddr: vaultNonTLSAddr, vaultLoginPath: "auth/kubernetes/login",
				vaultLoginRole: "istio-cert", vaultSignCsrPath: "istio_ca/sign/istio-pki-role",
				clientToken: citadelSANonTLS, csr: []byte(testCsr1)},
		},
	}

	for id, tc := range testCases {
		vaultAddr := tc.cliConfig.vaultAddr
		cli, err := vault.NewVaultClient(false, []byte{}, vaultAddr, tc.cliConfig.vaultLoginRole,
			tc.cliConfig.vaultLoginPath, tc.cliConfig.vaultSignCsrPath)
		if err != nil {
			t.Errorf("Test case [%s]: failed to create ca client: %v", id, err)
		}

		resp, err := cli.CSRSign(context.Background(), tc.cliConfig.csr, tc.cliConfig.clientToken, 1)
		if err != nil {
			t.Errorf("Test case [%s]:  error (%v) is not expected", id, err.Error())
		} else if len(resp) != 3 {
			t.Errorf("Test case [%s]: the certificate chain length (%v) is unexpected", id, len(resp))
		}
	}
}

func TestClientOnExampleHttpsVaultCA(t *testing.T) {
	testCases := map[string]struct {
		cliConfig clientConfig
	}{
		"Valid certs 1": {
			cliConfig: clientConfig{vaultAddr: vaultTLSAddr, vaultLoginPath: "auth/kubernetes/login",
				vaultLoginRole: "istio-cert", vaultSignCsrPath: "istio_ca/sign/istio-pki-role",
				clientToken: citadelSATLS, csr: []byte(testCsr1)},
		},
	}

	for id, tc := range testCases {
		vaultAddr := tc.cliConfig.vaultAddr
		cli, err := vault.NewVaultClient(true, []byte(vaultServerTLSCert), vaultAddr, tc.cliConfig.vaultLoginRole,
			tc.cliConfig.vaultLoginPath, tc.cliConfig.vaultSignCsrPath)
		if err != nil {
			t.Errorf("Test case [%s]: failed to create ca client: %v", id, err)
		}

		resp, err := cli.CSRSign(context.Background(), tc.cliConfig.csr, tc.cliConfig.clientToken, 1)
		if err != nil {
			t.Errorf("Test case [%s]:  error (%v) is not expected", id, err.Error())
		} else if len(resp) != 3 {
			t.Errorf("Test case [%s]: the certificate chain length (%v) is unexpected", id, len(resp))
		}
	}
}

// Opt-in to the test framework, implementing a TestMain and calling Run()
func TestMain(m *testing.M) {
	// Labels that the whole suite will be ran in post-submit.
	framework.NewSuite("citadel_agent_vault_client_test", m).Label(label.Postsubmit).Run()
}
