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

package signingapi

import (
	"context"
	"testing"
	"time"

	gomega "github.com/onsi/gomega"

	"istio.io/api/networking/v1alpha3"
	"istio.io/api/security/v1alpha1"
	mock "istio.io/istio/security/pkg/pki/signingapi/mock"
)

const (
	customServerCert   = "testdata/custom-certs/server-cert.pem"
	customServerKey    = "testdata/custom-certs/server-key.pem"
	customClientCert   = "testdata/custom-certs/client-cert.pem"
	customClientKey    = "testdata/custom-certs/client-key.pem"
	customRootCert     = "testdata/custom-certs/root-cert.pem"
	customWorkloadCert = "testdata/custom-certs/workload-cert-chain.pem"
)

func TestCreateSigningAPICLient(t *testing.T) {

	g := gomega.NewWithT(t)
	fakeTLSServer, err := mock.NewFakeExternalCA(true, customServerCert, customServerKey, customRootCert, customWorkloadCert)
	g.Expect(err).To(gomega.BeNil())

	addr, err := fakeTLSServer.Serve()
	g.Expect(err).To(gomega.BeNil())
	defer fakeTLSServer.Stop()

	fakeInsecureServer, err := mock.NewFakeExternalCA(false, "", "", "", "")
	g.Expect(err).To(gomega.BeNil())
	insecureAddress, err := fakeInsecureServer.Serve()
	g.Expect(err).To(gomega.BeNil())
	defer fakeInsecureServer.Stop()

	g.Expect(err).ShouldNot(gomega.HaveOccurred())
	testCases := map[string]struct {
		tlsMode            v1alpha3.ClientTLSSettings_TLSmode
		clientCertPath     string
		clientKeyPath      string
		rootCertPath       string
		caAddr             string
		connectExpectError string
		createCertErr      string
	}{
		"valid client certificate: should successful": {
			tlsMode:        v1alpha3.ClientTLSSettings_MUTUAL,
			clientCertPath: customClientCert,
			clientKeyPath:  customClientKey,
			rootCertPath:   customRootCert,
			caAddr:         addr.String(),
		},
		"Missing client certificate: should failed": {
			tlsMode:        v1alpha3.ClientTLSSettings_MUTUAL,
			clientCertPath: "./missing.pem",
			clientKeyPath:  "./missing.pem",
			rootCertPath:   customRootCert,
			caAddr:         addr.String(),
			connectExpectError: "get client security option failed: create certificate " +
				"files watcher failed: open ./missing.pem: no such file or directory",
		},
		"Missing root certificate: should failed": {
			tlsMode:        v1alpha3.ClientTLSSettings_MUTUAL,
			clientCertPath: customClientCert,
			clientKeyPath:  customClientKey,
			rootCertPath:   "./missing-root.pem",
			caAddr:         addr.String(),
			connectExpectError: "get client security option failed: create certificate files " +
				"watcher failed: error loading CA certificate file (./missing-root.pem): " +
				"open ./missing-root.pem: no such file or directory",
		},
		"Invalid root certificate: should failed": {
			tlsMode:        v1alpha3.ClientTLSSettings_MUTUAL,
			clientCertPath: customClientCert,
			clientKeyPath:  customClientKey,
			rootCertPath:   "testdata/istio-certs/root-cert.pem",
			caAddr:         addr.String(),
			createCertErr:  "authentication handshake failed: x509: certificate signed by unknown authority",
		},
		"Invalid client certificate: should failed": {
			tlsMode:        v1alpha3.ClientTLSSettings_MUTUAL,
			clientCertPath: "testdata/istio-certs/workload-cert.pem",
			clientKeyPath:  "testdata/istio-certs/workload-key.pem",
			rootCertPath:   customRootCert,
			caAddr:         addr.String(),
			createCertErr:  "invoke CreateCertificate failed: rpc error: code = Unavailable desc",
		},
		"TLS DISABLED MODE: should success": {
			tlsMode: v1alpha3.ClientTLSSettings_DISABLE,
			caAddr:  insecureAddress.String(),
		},
		"SIMPLE MODE: should failed with error": {
			tlsMode: v1alpha3.ClientTLSSettings_SIMPLE,
			connectExpectError: "get client security option failed: missing config: " +
				"TLSClientSetting.CaCertificates should not empty",
			caAddr: addr.String(),
		},
		"SIMPLE MODE: invalid root cert path": {
			tlsMode: v1alpha3.ClientTLSSettings_SIMPLE,
			connectExpectError: "get client security option failed: read CA Certificate (not/found/path) failed: " +
				"open not/found/path: no such file or directory",
			caAddr:       addr.String(),
			rootCertPath: "not/found/path",
		},
		"SIMPLE MODE: should run success": {
			tlsMode:      v1alpha3.ClientTLSSettings_SIMPLE,
			rootCertPath: customRootCert,
			caAddr:       addr.String(),
		},
	}

	for id, tc := range testCases {
		t.Run(id, func(tsub *testing.T) {
			gsub := gomega.NewWithT(tsub)

			client := New(tc.caAddr, &v1alpha3.ClientTLSSettings{
				Mode:              tc.tlsMode,
				CaCertificates:    tc.rootCertPath,
				ClientCertificate: tc.clientCertPath,
				PrivateKey:        tc.clientKeyPath,
			}, 5*time.Second)

			err := client.Connect(nil)

			if tc.connectExpectError != "" {
				gsub.Expect(err).To(gomega.MatchError(tc.connectExpectError))
				return
			}

			gsub.Expect(err).ShouldNot(gomega.HaveOccurred())

			_, err = client.CreateCertificate(context.TODO(), &v1alpha1.IstioCertificateRequest{
				Csr: "FAKE_CSR",
			})

			if tc.createCertErr != "" {
				gsub.Expect(err).ToNot(gomega.BeNil())
				gsub.Expect(err.Error()).To(gomega.ContainSubstring(tc.createCertErr))
				return
			}
			gsub.Expect(err).To(gomega.BeNil())

		})
	}
}
