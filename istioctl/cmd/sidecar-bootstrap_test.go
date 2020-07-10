// Copyright Istio Authors
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

package cmd

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"path"
	"strings"
	"testing"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	clientnetworking "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istioclient "istio.io/client-go/pkg/clientset/versioned"

	networking "istio.io/api/networking/v1alpha3"
)

type vmBootstrapTestcase struct {
	address           string
	args              []string
	cannedIstioConfig []clientnetworking.WorkloadEntry
	cannedK8sConfig   []runtime.Object
	expectedString    string
	shouldFail        bool
	certOrg           string
	tempDir           string
}

var (
	emptyIstioConfig = make([]clientnetworking.WorkloadEntry, 0)
	emptyK8sConfig   = make([]runtime.Object, 0)

	istioStaticWorkspace = []clientnetworking.WorkloadEntry{
		{
			ObjectMeta: metaV1.ObjectMeta{
				Name:      "workload",
				Namespace: "NS",
			},
			Spec: networking.WorkloadEntry{
				Address:        "127.0.0.1",
				ServiceAccount: "test",
			},
		},
	}

	// see `samples/certs` in the root of the repo
	caCert = []byte(`-----BEGIN CERTIFICATE-----
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
-----END CERTIFICATE-----`)

	caKey = []byte(`-----BEGIN RSA PRIVATE KEY-----
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
-----END RSA PRIVATE KEY-----`)

	baseTempdir, _ = ioutil.TempDir("", "vm_bootstrap_test_dir")

	k8sCertStatic = []runtime.Object{
		&coreV1.Secret{
			ObjectMeta: metaV1.ObjectMeta{
				Name:      "istio-ca-secret",
				Namespace: "istio-system",
			},
			Data: map[string][]byte{
				"ca-key.pem":  caKey,
				"ca-cert.pem": caCert,
			},
		},
	}
)

func TestVmBootstrap(t *testing.T) {
	cases := []vmBootstrapTestcase{
		// No all flag, or no workload entry.
		{
			address:           "127.0.0.1",
			args:              strings.Split("x sidecar-bootstrap", " "),
			cannedIstioConfig: emptyIstioConfig,
			cannedK8sConfig:   emptyK8sConfig,
			expectedString:    "istioctl experimental sidecar-bootstrap <workloadEntry>.<namespace> [flags]",
			shouldFail:        true,
			certOrg:           "",
			tempDir:           "",
		},
		// Workload Entry + all flag
		{
			address:           "127.0.0.1",
			args:              strings.Split("x sidecar-bootstrap --all workload.NS", " "),
			cannedIstioConfig: emptyIstioConfig,
			cannedK8sConfig:   emptyK8sConfig,
			expectedString:    "sidecar-bootstrap requires a workload entry, or the --all flag",
			shouldFail:        true,
			certOrg:           "",
			tempDir:           "",
		},
		// all flag + no namespace
		{
			address:           "127.0.0.1",
			args:              strings.Split("x sidecar-bootstrap --all", " "),
			cannedIstioConfig: emptyIstioConfig,
			cannedK8sConfig:   emptyK8sConfig,
			expectedString:    "sidecar-bootstrap needs a namespace if fetching all workspaces",
			shouldFail:        true,
			certOrg:           "",
			tempDir:           "",
		},
		// unknown workload entry, okay to have fake dumpDir here.
		{
			address:           "127.0.0.1",
			args:              strings.Split("x sidecar-bootstrap workload.fakeNS --local-dir /tmp/", " "),
			cannedIstioConfig: istioStaticWorkspace,
			cannedK8sConfig:   emptyK8sConfig,
			expectedString:    "workload entry: workload in namespace: fakeNS was not found",
			shouldFail:        true,
			certOrg:           "",
			tempDir:           "",
		},
		// known workload entry, no secret
		{
			address:           "127.0.0.1",
			args:              strings.Split("x sidecar-bootstrap workload.NS --local-dir /tmp/", " "),
			cannedIstioConfig: istioStaticWorkspace,
			cannedK8sConfig:   emptyK8sConfig,
			expectedString:    "secrets \"istio-ca-secret\" not found",
			shouldFail:        true,
			certOrg:           "",
			tempDir:           "",
		},
		// known workload entry, known secret, derived organization
		{
			address:           "127.0.0.1",
			args:              strings.Split("x sidecar-bootstrap workload.NS --local-dir "+path.Join(baseTempdir, "derived_output"), " "),
			cannedIstioConfig: istioStaticWorkspace,
			cannedK8sConfig:   k8sCertStatic,
			expectedString:    "",
			shouldFail:        false,
			certOrg:           "Istio",
			tempDir:           path.Join(baseTempdir, "derived_output"),
		},
		// known workload entry, known secret, non derive organization
		{
			address:           "127.0.0.1",
			args:              strings.Split("x sidecar-bootstrap workload.NS -o Juju --local-dir "+path.Join(baseTempdir, "derived_output"), " "),
			cannedIstioConfig: istioStaticWorkspace,
			cannedK8sConfig:   k8sCertStatic,
			expectedString:    "",
			shouldFail:        false,
			certOrg:           "Juju",
			tempDir:           path.Join(baseTempdir, "derived_output"),
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyVMCommandCaseOutput(t, c)
		})
	}
}

func verifyVMCommandCaseOutput(t *testing.T, c vmBootstrapTestcase) {
	t.Helper()

	configStoreFactory = mockClientFactoryGenerator(func(client istioclient.Interface) {
		for _, cfg := range c.cannedIstioConfig {
			_, err := client.NetworkingV1alpha3().WorkloadEntries(cfg.Namespace).Create(context.TODO(), &cfg, metaV1.CreateOptions{})
			if err != nil {
				t.Fatal(err)
			}
		}
	})
	interfaceFactory = mockInterfaceFactoryGenerator(c.cannedK8sConfig)

	var out bytes.Buffer
	rootCmd := GetRootCmd(c.args)
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	fErr := rootCmd.Execute()
	output := out.String()

	if c.expectedString != "" && !strings.Contains(output, c.expectedString) {
		t.Fatalf("Output didn't match for 'istioctl %s'\n got %v\nwant: %v", strings.Join(c.args, " "), output, c.expectedString)
	}

	if c.shouldFail {
		if fErr == nil {
			t.Fatalf("Command should have failed for 'istioctl %s', didn't get one, output was %q",
				strings.Join(c.args, " "), output)
		}
	} else {
		if fErr != nil {
			t.Fatalf("Command should not have failed for 'istioctl %s': %v", strings.Join(c.args, " "), fErr)
		}
	}

	if c.certOrg != "" {
		certFile, rErr := ioutil.ReadFile(path.Join(c.tempDir, "cert-"+c.address+".pem"))
		if rErr != nil {
			t.Fatalf("Failed to read certificate file: %v", rErr)
		}
		block, _ := pem.Decode(certFile)
		cert, cErr := x509.ParseCertificate(block.Bytes)
		if cErr != nil {
			t.Fatalf("Failed to parse certificate data:\n%s\nerror: %v", certFile, cErr)
		}
		if cert.Subject.Organization[0] != c.certOrg {
			t.Fatalf("Certificate does not have matching organization: %s is not expected: %s", cert.Subject.Organization[0], c.certOrg)
		}
	}
}
