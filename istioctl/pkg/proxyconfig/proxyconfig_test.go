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

package proxyconfig

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/rest/fake"
	cmdtesting "k8s.io/kubectl/pkg/cmd/testing"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/util/assert"
)

type execTestCase struct {
	execClientConfig map[string][]byte
	args             []string

	// Typically use one of the three
	expectedOutput string // Expected constant output
	expectedString string // String output is expected to contain

	wantException bool
}

func TestProxyConfig(t *testing.T) {
	loggingConfig := map[string][]byte{
		"details-v1-5b7f94f9bc-wp5tb": util.ReadFile(t, "../writer/envoy/logging/testdata/logging.txt"),
		"httpbin-794b576b6c-qx6pf":    []byte("{}"),
	}
	cases := []execTestCase{
		{
			args:           []string{},
			expectedString: "A group of commands used to retrieve information about",
		},
		{ // clusters invalid
			args:           strings.Split("clusters invalid", " "),
			expectedString: "unable to retrieve Pod: pods \"invalid\" not found",
			wantException:  true, // "istioctl proxy-config clusters invalid" should fail
		},
		{ // listeners invalid
			args:           strings.Split("listeners invalid", " "),
			expectedString: "unable to retrieve Pod: pods \"invalid\" not found",
			wantException:  true, // "istioctl proxy-config listeners invalid" should fail
		},
		{ // logging empty
			args:           strings.Split("log", " "),
			expectedString: "Error: log requires pod name or --selector",
			wantException:  true, // "istioctl proxy-config logging empty" should fail
		},
		{ // logging invalid
			args:           strings.Split("log invalid", " "),
			expectedString: "unable to retrieve Pod: pods \"invalid\" not found",
			wantException:  true, // "istioctl proxy-config logging invalid" should fail
		},
		{ // logging level invalid
			execClientConfig: loggingConfig,
			args:             strings.Split("log details-v1-5b7f94f9bc-wp5tb --level xxx", " "),
			expectedString:   "unrecognized logging level: xxx",
			wantException:    true,
		},
		{ // logger name invalid
			execClientConfig: loggingConfig,
			args:             strings.Split("log details-v1-5b7f94f9bc-wp5tb --level xxx:debug", " "),
			expectedString:   "unrecognized logger name: xxx",
			wantException:    true,
		},
		{ // logger name valid, but logging level invalid
			execClientConfig: loggingConfig,
			args:             strings.Split("log details-v1-5b7f94f9bc-wp5tb --level http:yyy", " "),
			expectedString:   "unrecognized logging level: yyy",
			wantException:    true,
		},
		{ // both logger name and logging level invalid
			execClientConfig: loggingConfig,
			args:             strings.Split("log details-v1-5b7f94f9bc-wp5tb --level xxx:yyy", " "),
			expectedString:   "unrecognized logger name: xxx",
			wantException:    true,
		},
		{ // routes invalid
			args:           strings.Split("routes invalid", " "),
			expectedString: "unable to retrieve Pod: pods \"invalid\" not found",
			wantException:  true, // "istioctl proxy-config routes invalid" should fail
		},
		{ // bootstrap invalid
			args:           strings.Split("bootstrap invalid", " "),
			expectedString: "unable to retrieve Pod: pods \"invalid\" not found",
			wantException:  true, // "istioctl proxy-config bootstrap invalid" should fail
		},
		{ // secret invalid
			args:           strings.Split("secret invalid", " "),
			expectedString: "unable to retrieve Pod: pods \"invalid\" not found",
			wantException:  true, // "istioctl proxy-config secret invalid" should fail
		},
		{ // endpoint invalid
			args:           strings.Split("endpoint invalid", " "),
			expectedString: "unable to retrieve Pod: pods \"invalid\" not found",
			wantException:  true, // "istioctl proxy-config endpoint invalid" should fail
		},
		{ // supplying nonexistent deployment name should result in error
			args:           strings.Split("clusters deployment/random-gibberish", " "),
			expectedString: `"deployment/random-gibberish" does not refer to a pod`,
			wantException:  true,
		},
		{ // supplying nonexistent deployment name in nonexistent namespace
			args:           strings.Split("endpoint deployment/random-gibberish.bogus", " "),
			expectedString: `"deployment/random-gibberish" does not refer to a pod`,
			wantException:  true,
		},
		{ // supplying type that doesn't select pods should fail
			args:           strings.Split("listeners serviceaccount/sleep", " "),
			expectedString: `"serviceaccount/sleep" does not refer to a pod`,
			wantException:  true,
		},
		{ // supplying valid pod name retrieves Envoy config (fails because we don't check in Envoy config unit tests)
			execClientConfig: loggingConfig,
			args:             strings.Split("clusters httpbin-794b576b6c-qx6pf", " "),
			expectedString:   `config dump has no configuration type`,
			wantException:    true,
		},
		{ // supplying valid pod name retrieves Envoy config (fails because we don't check in Envoy config unit tests)
			execClientConfig: loggingConfig,
			args:             strings.Split("bootstrap httpbin-794b576b6c-qx6pf", " "),
			expectedString:   `config dump has no configuration type`,
			wantException:    true,
		},
		{ // supplying valid pod name retrieves Envoy config (fails because we don't check in Envoy config unit tests)
			execClientConfig: loggingConfig,
			args:             strings.Split("endpoint httpbin-794b576b6c-qx6pf", " "),
			expectedString:   `ENDPOINT     STATUS     OUTLIER CHECK     CLUSTER`,
			wantException:    false,
		},
		{ // supplying valid pod name retrieves Envoy config (fails because we don't check in Envoy config unit tests)
			execClientConfig: loggingConfig,
			args:             strings.Split("listener httpbin-794b576b6c-qx6pf", " "),
			expectedString:   `config dump has no configuration type`,
			wantException:    true,
		},
		{ // supplying valid pod name retrieves Envoy config (fails because we don't check in Envoy config unit tests)
			execClientConfig: loggingConfig,
			args:             strings.Split("route httpbin-794b576b6c-qx6pf", " "),
			expectedString:   `config dump has no configuration type`,
			wantException:    true,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyExecTestOutput(t, ProxyConfig(cli.NewFakeContext(&cli.NewFakeContextOption{
				Results:   c.execClientConfig,
				Namespace: "default",
			})), c)
		})
	}
}

func verifyExecTestOutput(t *testing.T, cmd *cobra.Command, c execTestCase) {
	t.Helper()

	var out bytes.Buffer
	cmd.SetArgs(c.args)
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	fErr := cmd.Execute()
	output := out.String()

	if c.expectedOutput != "" && c.expectedOutput != output {
		t.Fatalf("Unexpected output for 'istioctl %s'\n got: %q\nwant: %q", strings.Join(c.args, " "), output, c.expectedOutput)
	}

	if c.expectedString != "" && !strings.Contains(output, c.expectedString) {
		t.Fatalf("Output didn't match for '%s %s'\n got %v\nwant: %v", cmd.Name(), strings.Join(c.args, " "), output, c.expectedString)
	}

	if c.wantException {
		if fErr == nil {
			t.Fatalf("Wanted an exception for 'istioctl %s', didn't get one, output was %q",
				strings.Join(c.args, " "), output)
		}
	} else {
		if fErr != nil {
			t.Fatalf("Unwanted exception for 'istioctl %s': %v", strings.Join(c.args, " "), fErr)
		}
	}
}

func TestPrintProxyConfigSummary(t *testing.T) {
	cmd := ProxyConfig(cli.NewFakeContext(&cli.NewFakeContextOption{
		Namespace: "default",
	}))
	cmd.SetArgs([]string{
		"all",
		"-f", "testdata/config_dump.json",
	})
	out := bytes.Buffer{}
	cmd.SetOut(&out)
	assert.NoError(t, cmd.Execute())
	expected := util.ReadFile(t, "testdata/config_dump_summary.txt")

	if err := assert.Compare(out.String(), string(expected)); err != nil {
		t.Fatalf("Unexpected output for 'istioctl proxy-config all'\n got: %q\nwant: %q", out.String(), expected)
	}
}

func TestMatchRootCACerts(t *testing.T) {
	certA, _ := createTestCertificate("A")
	certB, _ := createTestCertificate("B")

	pemCertA := createPEMCert(certA)
	pemCertB := createPEMCert(certB)

	pemCertAPlusB := append(pemCertA, pemCertB...)

	tests := []struct {
		name           string
		rootCAPod1Data []byte
		rootCAPod2Data []byte
		expectedMatch  bool
		expectedError  string
	}{
		{
			name:           "Matching Certificates - both rootCA identical",
			rootCAPod1Data: pemCertA,
			rootCAPod2Data: pemCertA,
			expectedMatch:  true,
		},
		{
			name:           "Non-Matching Certificates",
			rootCAPod1Data: pemCertA,
			rootCAPod2Data: pemCertB,
			expectedMatch:  false,
		},
		{
			name:           "Subset of Certificates",
			rootCAPod1Data: pemCertAPlusB,
			rootCAPod2Data: pemCertB,
			expectedMatch:  true,
		},
		{
			name:           "Invalid Certificate Data",
			rootCAPod1Data: []byte("invalid data"),
			rootCAPod2Data: pemCertA,
			expectedMatch:  false,
			expectedError:  "failed to parse certificates: invalid data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match, err := checkRootCACertMatchExist(tt.rootCAPod1Data, tt.rootCAPod2Data)

			if tt.expectedError != "" {
				if err == nil || tt.expectedError != err.Error() {
					t.Errorf("expected error: %v, got: %v", tt.expectedError, err)
				}
			}

			if match != tt.expectedMatch {
				t.Errorf("expected match: %v, got: %v", tt.expectedMatch, match)
			}
		})
	}
}

// Helper functions to create test certificates
func createPEMCert(cert *x509.Certificate) []byte {
	pemBlock := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	}
	return pem.EncodeToMemory(pemBlock)
}

func createTestCertificate(commonName string) (*x509.Certificate, error) {
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
	}
	privKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	certBytes, _ := x509.CreateCertificate(rand.Reader, template, template, &privKey.PublicKey, privKey)
	return x509.ParseCertificate(certBytes)
}

func init() {
	cli.MakeKubeFactory = func(k kube.CLIClient) cmdutil.Factory {
		tf := cmdtesting.NewTestFactory()
		_, _, codec := cmdtesting.NewExternalScheme()
		tf.UnstructuredClient = &fake.RESTClient{
			NegotiatedSerializer: resource.UnstructuredPlusDefaultContentConfig().NegotiatedSerializer,
			Resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     cmdtesting.DefaultHeader(),
				Body: cmdtesting.ObjBody(codec,
					cmdtesting.NewInternalType("", "", "foo")),
			},
		}
		return tf
	}
}
