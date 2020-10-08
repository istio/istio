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

package custom

import (
	"context"
	"strings"
	"testing"
	"time"

	gomega "github.com/onsi/gomega"

	mesh "istio.io/api/mesh/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	"istio.io/api/security/v1alpha1"
	"istio.io/istio/pkg/spiffe"
	mock "istio.io/istio/security/pkg/pki/signingapi/mock"
	"istio.io/istio/security/pkg/pki/util"
)

const (
	jwtPath            = "../signingapi/testdata/jwt-token.txt"
	customServerCert   = "../signingapi/testdata/custom-certs/server-cert.pem"
	customServerKey    = "../signingapi/testdata/custom-certs/server-key.pem"
	customClientCert   = "../signingapi/testdata/custom-certs/client-cert.pem"
	customClientKey    = "../signingapi/testdata/custom-certs/client-key.pem"
	customRootCert     = "../signingapi/testdata/custom-certs/root-cert.pem"
	customWorkloadCert = "../signingapi/testdata/custom-certs/workload-cert-chain.pem"
	mixingRootCerts    = "../signingapi/testdata/mixing-custom-istio-root.pem"
)

func TestCreateCertificate(t *testing.T) {
	g := gomega.NewWithT(t)
	fakeServer, err := mock.NewFakeExternalCA(true, customServerCert, customServerKey, customRootCert, customWorkloadCert)
	g.Expect(err).To(gomega.BeNil())
	addr, err := fakeServer.Serve()
	defer fakeServer.Stop()
	g.Expect(err).To(gomega.BeNil())
	keyCertBundle, err := util.NewKeyCertBundleWithRootCertFromFile(mixingRootCerts)
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	caOpts := &mesh.MeshConfig_CA{
		Address: addr.String(),
		TlsSettings: &v1alpha3.ClientTLSSettings{
			Mode:              v1alpha3.ClientTLSSettings_MUTUAL,
			CaCertificates:    customRootCert,
			ClientCertificate: customClientCert,
			PrivateKey:        customClientKey,
		},
	}

	c, err := NewCAClient(caOpts, keyCertBundle)
	g.Expect(err).ShouldNot(gomega.HaveOccurred())
	spiffeURI, err := spiffe.GenSpiffeURI("test-namespace", "test-service-account")
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	fakeCSR, _, err := util.GenCSR(util.CertOptions{
		Host:       spiffeURI,
		NotBefore:  time.Now(),
		IsCA:       false,
		RSAKeySize: 2048,
	})
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	r, err := c.CreateCertificate(context.TODO(), &v1alpha1.IstioCertificateRequest{
		Csr: string(fakeCSR),
	}, []string{spiffeURI})

	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	g.Expect(r.GetCertChain()).To(gomega.HaveLen(2))
	g.Expect(r.GetCertChain()).To(gomega.ContainElements(string(keyCertBundle.GetRootCertPem())))
}

func TestVerifyCSR(t *testing.T) {
	g := gomega.NewWithT(t)
	spiffeURI, err := spiffe.GenSpiffeURI("test-namespace", "test-service-account")
	g.Expect(err).ShouldNot(gomega.HaveOccurred())
	validCSR, _, err := util.GenCSR(util.CertOptions{
		Host:       spiffeURI,
		NotBefore:  time.Now(),
		IsCA:       false,
		RSAKeySize: 2048,
	})
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	unmatchCSR, _, err := util.GenCSR(util.CertOptions{
		Host:       "spiffe://unmatch",
		NotBefore:  time.Now(),
		IsCA:       false,
		RSAKeySize: 2048,
	})
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	emptyHostCSR, _, err := util.GenCSR(util.CertOptions{
		NotBefore:  time.Now(),
		IsCA:       false,
		RSAKeySize: 2048,
	})
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	containDNS, _, err := util.GenCSR(util.CertOptions{
		NotBefore:  time.Now(),
		Host:       "fake.dns.com",
		IsCA:       false,
		RSAKeySize: 2048,
	})
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	containIPs, _, err := util.GenCSR(util.CertOptions{
		NotBefore:  time.Now(),
		Host:       "192.168.2.1,127.0.0.1,123.123.32.32",
		IsCA:       false,
		RSAKeySize: 2048,
	})
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	containCommonName, _, err := util.GenCSR(util.CertOptions{
		NotBefore:  time.Now(),
		Host:       "FakeCommonName",
		IsCA:       false,
		RSAKeySize: 2048,
		IsDualUse:  true,
	})
	g.Expect(err).ShouldNot(gomega.HaveOccurred())

	multiIdentitiesCSR, _, err := util.GenCSR(util.CertOptions{
		Host:       strings.Join([]string{spiffeURI, "spiffe://admin/ns/admin/sa/admin"}, ","),
		NotBefore:  time.Now(),
		IsCA:       false,
		RSAKeySize: 2048,
	})
	g.Expect(err).ShouldNot(gomega.HaveOccurred())
	testCase := map[string]struct {
		csr       []byte
		expectErr string
	}{
		"valid CSR: should success": {
			csr:       validCSR,
			expectErr: "",
		},
		"unmatched CSR Host: should failed with error": {
			csr: unmatchCSR,
			expectErr: "csr's host ([spiffe://unmatch]) doesn't match with request identities " +
				"([spiffe://cluster.local/ns/test-namespace/sa/test-service-account])",
		},
		"empty CSR Host: should failed with error": {
			csr:       emptyHostCSR,
			expectErr: "cannot extract identities from CSR: the SAN extension does not exist",
		},
		"received unsupported CSR, contains DNSNames: should failed with error": {
			csr:       containDNS,
			expectErr: "received unsupported CSR, contains DNSNames: [fake.dns.com]",
		},
		"received unsupported CSR, contains IPs: should failed with error": {
			csr:       containIPs,
			expectErr: "received unsupported CSR, contains IPAddresses: [192.168.2.1 127.0.0.1 123.123.32.32]",
		},
		"received unsupported CSR, contains CommonName: should failed with error": {
			csr:       containCommonName,
			expectErr: "received unsupported CSR, contains Subject.CommonName: \"FakeCommonName\"",
		},
		"received unsupported CSR, contains : should failed with error": {
			csr: multiIdentitiesCSR,
			expectErr: "received unsupported CSR, contains more than 1 identity: " +
				"[spiffe://cluster.local/ns/test-namespace/sa/test-service-account spiffe://admin/ns/admin/sa/admin]",
		},
	}

	for id, tc := range testCase {
		t.Run(id, func(tsub *testing.T) {
			gsub := gomega.NewGomegaWithT(tsub)
			err := verifyCSRIdentities([]string{spiffeURI}, string(tc.csr))
			if tc.expectErr != "" {
				gsub.Expect(err).To(gomega.MatchError(tc.expectErr))
				return
			}
			gsub.Expect(err).ShouldNot(gomega.HaveOccurred())
		})
	}

}
