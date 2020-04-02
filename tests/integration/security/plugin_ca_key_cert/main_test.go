// Copyright 2020 Istio Authors
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

package plugincakeycert

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/environment"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	inst istio.Instance
	g    galley.Instance
	p    pilot.Instance
)

const (
	// From samples/certs/ca-cert.pem
	CACert = `-----BEGIN CERTIFICATE-----
MIIDnzCCAoegAwIBAgIJAON1ifrBZ2/BMA0GCSqGSIb3DQEBCwUAMIGLMQswCQYD
VQQGEwJVUzETMBEGA1UECAwKQ2FsaWZvcm5pYTESMBAGA1UEBwwJU3Vubnl2YWxl
MQ4wDAYDVQQKDAVJc3RpbzENMAsGA1UECwwEVGVzdDEQMA4GA1UEAwwHUm9vdCBD
QTEiMCAGCSqGSIb3DQEJARYTdGVzdHJvb3RjYUBpc3Rpby5pbzAgFw0xODAxMjQx
OTE1NTFaGA8yMTE3MTIzMTE5MTU1MVowWTELMAkGA1UEBhMCVVMxEzARBgNVBAgT
CkNhbGlmb3JuaWExEjAQBgNVBAcTCVN1bm55dmFsZTEOMAwGA1UEChMFSXN0aW8x
ETAPBgNVBAMTCElzdGlvIENBMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKC
AQEAyzCxr/xu0zy5rVBiso9ffgl00bRKvB/HF4AX9/ytmZ6Hqsy13XIQk8/u/By9
iCvVwXIMvyT0CbiJq/aPEj5mJUy0lzbrUs13oneXqrPXf7ir3HzdRw+SBhXlsh9z
APZJXcF93DJU3GabPKwBvGJ0IVMJPIFCuDIPwW4kFAI7R/8A5LSdPrFx6EyMXl7K
M8jekC0y9DnTj83/fY72WcWX7YTpgZeBHAeeQOPTZ2KYbFal2gLsar69PgFS0Tom
ESO9M14Yit7mzB1WDK2z9g3r+zLxENdJ5JG/ZskKe+TO4Diqi5OJt/h8yspS1ck8
LJtCole9919umByg5oruflqIlQIDAQABozUwMzALBgNVHQ8EBAMCAgQwDAYDVR0T
BAUwAwEB/zAWBgNVHREEDzANggtjYS5pc3Rpby5pbzANBgkqhkiG9w0BAQsFAAOC
AQEAltHEhhyAsve4K4bLgBXtHwWzo6SpFzdAfXpLShpOJNtQNERb3qg6iUGQdY+w
A2BpmSkKr3Rw/6ClP5+cCG7fGocPaZh+c+4Nxm9suMuZBZCtNOeYOMIfvCPcCS+8
PQ/0hC4/0J3WJKzGBssaaMufJxzgFPPtDJ998kY8rlROghdSaVt423/jXIAYnP3Y
05n8TGERBj7TLdtIVbtUIx3JHAo3PWJywA6mEDovFMJhJERp9sDHIr1BbhXK1TFN
Z6HNH6gInkSSMtvC4Ptejb749PTaePRPF7ID//eq/3AH8UK50F3TQcLjEqWUsJUn
aFKltOc+RAjzDklcUPeG4Y6eMA==
-----END CERTIFICATE-----`
	// From samples/certs/ca-key.pem
	CAKey = `-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEAyzCxr/xu0zy5rVBiso9ffgl00bRKvB/HF4AX9/ytmZ6Hqsy1
3XIQk8/u/By9iCvVwXIMvyT0CbiJq/aPEj5mJUy0lzbrUs13oneXqrPXf7ir3Hzd
Rw+SBhXlsh9zAPZJXcF93DJU3GabPKwBvGJ0IVMJPIFCuDIPwW4kFAI7R/8A5LSd
PrFx6EyMXl7KM8jekC0y9DnTj83/fY72WcWX7YTpgZeBHAeeQOPTZ2KYbFal2gLs
ar69PgFS0TomESO9M14Yit7mzB1WDK2z9g3r+zLxENdJ5JG/ZskKe+TO4Diqi5OJ
t/h8yspS1ck8LJtCole9919umByg5oruflqIlQIDAQABAoIBAGZI8fnUinmd5R6B
C941XG3XFs6GAuUm3hNPcUFuGnntmv/5I0gBpqSyFO0nDqYg4u8Jma8TTCIkmnFN
ogIeFU+LiJFinR3GvwWzTE8rTz1FWoaY+M9P4ENd/I4pVLxUPuSKhfA2ChAVOupU
8F7D9Q/dfBXQQCT3VoUaC+FiqjL4HvIhji1zIqaqpK7fChGPraC/4WHwLMNzI0Zg
oDdAanwVygettvm6KD7AeKzhK94gX1PcnsOi3KuzQYvkenQE1M6/K7YtEc5qXCYf
QETj0UCzB55btgdF36BGoZXf0LwHqxys9ubfHuhwKBpY0xg2z4/4RXZNhfIDih3w
J3mihcECgYEA6FtQ0cfh0Zm03OPDpBGc6sdKxTw6aBDtE3KztfI2hl26xHQoeFqp
FmV/TbnExnppw+gWJtwx7IfvowUD8uRR2P0M2wGctWrMpnaEYTiLAPhXsj69HSM/
CYrh54KM0YWyjwNhtUzwbOTrh1jWtT9HV5e7ay9Atk3UWljuR74CFMUCgYEA392e
DVoDLE0XtbysmdlfSffhiQLP9sT8+bf/zYnr8Eq/4LWQoOtjEARbuCj3Oq7bP8IE
Vz45gT1mEE3IacC9neGwuEa6icBiuQi86NW8ilY/ZbOWrRPLOhk3zLiZ+yqkt+sN
cqWx0JkIh7IMKWI4dVQgk4I0jcFP7vNG/So4AZECgYEA426eSPgxHQwqcBuwn6Nt
yJCRq0UsljgbFfIr3Wfb3uFXsntQMZ3r67QlS1sONIgVhmBhbmARrcfQ0+xQ1SqO
wqnOL4AAd8K11iojoVXLGYP7ssieKysYxKpgPE8Yru0CveE9fkx0+OGJeM2IO5hY
qHAoTt3NpaPAuz5Y3XgqaVECgYA0TONS/TeGjxA9/jFY1Cbl8gp35vdNEKKFeM5D
Z7h+cAg56FE8tyFyqYIAGVoBFL7WO26mLzxiDEUfA/0Rb90c2JBfzO5hpleqIPd5
cg3VR+cRzI4kK16sWR3nLy2SN1k6OqjuovVS5Z3PjfI3bOIBz0C5FY9Pmt0g1yc7
mDRzcQKBgQCXWCZStbdjewaLd5u5Hhbw8tIWImMVfcfs3H1FN669LLpbARM8RtAa
8dYwDVHmWmevb/WX03LiSE+GCjCBO79fa1qc5RKAalqH/1OYxTuvYOeTUebSrg8+
lQFlP2OC4GGolKrN6HVWdxtf+F+SdjwX6qGCfYkXJRLYXIFSFjFeuw==
-----END RSA PRIVATE KEY-----`
	// From samples/certs/cert-chain.pem
	CACertChain = `-----BEGIN CERTIFICATE-----
MIIDnzCCAoegAwIBAgIJAON1ifrBZ2/BMA0GCSqGSIb3DQEBCwUAMIGLMQswCQYD
VQQGEwJVUzETMBEGA1UECAwKQ2FsaWZvcm5pYTESMBAGA1UEBwwJU3Vubnl2YWxl
MQ4wDAYDVQQKDAVJc3RpbzENMAsGA1UECwwEVGVzdDEQMA4GA1UEAwwHUm9vdCBD
QTEiMCAGCSqGSIb3DQEJARYTdGVzdHJvb3RjYUBpc3Rpby5pbzAgFw0xODAxMjQx
OTE1NTFaGA8yMTE3MTIzMTE5MTU1MVowWTELMAkGA1UEBhMCVVMxEzARBgNVBAgT
CkNhbGlmb3JuaWExEjAQBgNVBAcTCVN1bm55dmFsZTEOMAwGA1UEChMFSXN0aW8x
ETAPBgNVBAMTCElzdGlvIENBMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKC
AQEAyzCxr/xu0zy5rVBiso9ffgl00bRKvB/HF4AX9/ytmZ6Hqsy13XIQk8/u/By9
iCvVwXIMvyT0CbiJq/aPEj5mJUy0lzbrUs13oneXqrPXf7ir3HzdRw+SBhXlsh9z
APZJXcF93DJU3GabPKwBvGJ0IVMJPIFCuDIPwW4kFAI7R/8A5LSdPrFx6EyMXl7K
M8jekC0y9DnTj83/fY72WcWX7YTpgZeBHAeeQOPTZ2KYbFal2gLsar69PgFS0Tom
ESO9M14Yit7mzB1WDK2z9g3r+zLxENdJ5JG/ZskKe+TO4Diqi5OJt/h8yspS1ck8
LJtCole9919umByg5oruflqIlQIDAQABozUwMzALBgNVHQ8EBAMCAgQwDAYDVR0T
BAUwAwEB/zAWBgNVHREEDzANggtjYS5pc3Rpby5pbzANBgkqhkiG9w0BAQsFAAOC
AQEAltHEhhyAsve4K4bLgBXtHwWzo6SpFzdAfXpLShpOJNtQNERb3qg6iUGQdY+w
A2BpmSkKr3Rw/6ClP5+cCG7fGocPaZh+c+4Nxm9suMuZBZCtNOeYOMIfvCPcCS+8
PQ/0hC4/0J3WJKzGBssaaMufJxzgFPPtDJ998kY8rlROghdSaVt423/jXIAYnP3Y
05n8TGERBj7TLdtIVbtUIx3JHAo3PWJywA6mEDovFMJhJERp9sDHIr1BbhXK1TFN
Z6HNH6gInkSSMtvC4Ptejb749PTaePRPF7ID//eq/3AH8UK50F3TQcLjEqWUsJUn
aFKltOc+RAjzDklcUPeG4Y6eMA==
-----END CERTIFICATE-----`
	// From samples/certs/root-cert.pem
	CARootCert = `-----BEGIN CERTIFICATE-----
MIID7TCCAtWgAwIBAgIJAOIRDhOcxsx6MA0GCSqGSIb3DQEBCwUAMIGLMQswCQYD
VQQGEwJVUzETMBEGA1UECAwKQ2FsaWZvcm5pYTESMBAGA1UEBwwJU3Vubnl2YWxl
MQ4wDAYDVQQKDAVJc3RpbzENMAsGA1UECwwEVGVzdDEQMA4GA1UEAwwHUm9vdCBD
QTEiMCAGCSqGSIb3DQEJARYTdGVzdHJvb3RjYUBpc3Rpby5pbzAgFw0xODAxMjQx
OTE1NTFaGA8yMTE3MTIzMTE5MTU1MVowgYsxCzAJBgNVBAYTAlVTMRMwEQYDVQQI
DApDYWxpZm9ybmlhMRIwEAYDVQQHDAlTdW5ueXZhbGUxDjAMBgNVBAoMBUlzdGlv
MQ0wCwYDVQQLDARUZXN0MRAwDgYDVQQDDAdSb290IENBMSIwIAYJKoZIhvcNAQkB
FhN0ZXN0cm9vdGNhQGlzdGlvLmlvMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIB
CgKCAQEA38uEfAatzQYqbaLou1nxJ348VyNzumYMmDDt5pbLYRrCo2pS3ki1ZVDN
8yxIENJFkpKw9UctTGdbNGuGCiSDP7uqF6BiVn+XKAU/3pnPFBbTd0S33NqbDEQu
IYraHSl/tSk5rARbC1DrQRdZ6nYD2KrapC4g0XbjY6Pu5l4y7KnFwSunnp9uqpZw
uERv/BgumJ5QlSeSeCmhnDhLxooG8w5tC2yVr1yDpsOHGimP/mc8Cds4V0zfIhQv
YzfIHphhE9DKjmnjBYLOdj4aycv44jHnOGc+wvA1Jqsl60t3wgms+zJTiWwABLdw
zgMAa7yxLyoV0+PiVQud6k+8ZoIFcwIDAQABo1AwTjAdBgNVHQ4EFgQUOUYGtUyh
euxO4lGe4Op1y8NVoagwHwYDVR0jBBgwFoAUOUYGtUyheuxO4lGe4Op1y8NVoagw
DAYDVR0TBAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEANXLyfAs7J9rmBamGJvPZ
ltx390WxzzLFQsBRAaH6rgeipBq3dR9qEjAwb6BTF+ROmtQzX+fjstCRrJxCto9W
tC8KvXTdRfIjfCCZjhtIOBKqRxE4KJV/RBfv9xD5lyjtCPCQl3Ia6MSf42N+abAK
WCdU6KCojA8WB9YhSCzza3aQbPTzd26OC/JblJpVgtus5f8ILzCsz+pbMimgTkhy
AuhYRppJaQ24APijsEC9+GIaVKPg5IwWroiPoj+QXNpshuvqVQQXvGaRiq4zoSnx
xAJz+w8tjrDWcf826VN14IL+/Cmqlg/rIfB5CHdwVIfWwpuGB66q/UiPegZMNs8a
3g==
-----END CERTIFICATE-----`
)

func TestMain(m *testing.M) {
	// Integration test for plugging in the CA key and certificate and mutual TLS
	// with the CA issued certificates.
	framework.
		NewSuite("plugin_ca_key_cert_test", m).
		// k8s is required because the plugin CA key and certificate are stored in a k8s secret.
		RequireEnvironment(environment.Kube).
		RequireSingleCluster().
		Label(label.CustomSetup).
		SetupOnEnv(environment.Kube, istio.Setup(&inst, setupConfig)).
		Setup(func(ctx resource.Context) (err error) {
			if g, err = galley.New(ctx, galley.Config{}); err != nil {
				return err
			}
			if p, err = pilot.New(ctx, pilot.Config{
				Galley: g,
			}); err != nil {
				return err
			}
			// Write CA key and certificate to the k8s secret "cacerts"
			err = createCASecret(ctx)
			if err != nil {
				return err
			}
			return nil
		}).
		Run()
}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.Values["global.mtls.enabled"] = "true"
}

// createCASecret creates a k8s secret "cacerts" to store the CA key and cert.
func createCASecret(ctx resource.Context) error {
	name := "cacerts"
	systemNs, err := namespace.ClaimSystemNamespace(ctx)
	if err != nil {
		return err
	}

	kubeAccessor := ctx.Environment().(*kube.Environment).KubeClusters[0]
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: systemNs.Name(),
		},
		Data: map[string][]byte{
			"ca-cert.pem":    []byte(CACert),
			"ca-key.pem":     []byte(CAKey),
			"cert-chain.pem": []byte(CACertChain),
			"root-cert.pem":  []byte(CARootCert),
		},
	}

	err = kubeAccessor.CreateSecret(systemNs.Name(), secret)
	if err != nil {
		return err
	}
	return nil
}
