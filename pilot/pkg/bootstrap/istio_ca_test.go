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

package bootstrap

import (
	"context"
	"os"
	"path"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/keycertbundle"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/security/pkg/pki/ca"
	pkiutil "istio.io/istio/security/pkg/pki/util"
)

const testNamespace = "istio-system"

func TestCheckCABundleCompleteness(t *testing.T) {
	g := NewWithT(t)

	dir := t.TempDir()

	// Create partial certificate files (missing signing key)
	rootCertFile := path.Join(dir, "root-cert.pem")
	certChainFile := path.Join(dir, "cert-chain.pem")
	caCertFile := path.Join(dir, "ca-cert.pem")

	// Create some files but not all
	rootCert, err := readSampleCertFromFile("root-cert.pem")
	g.Expect(err).Should(BeNil())
	err = os.WriteFile(rootCertFile, rootCert, 0o600)
	g.Expect(err).Should(BeNil())

	certChain, err := readSampleCertFromFile("cert-chain.pem")
	g.Expect(err).Should(BeNil())
	err = os.WriteFile(certChainFile, certChain, 0o600)
	g.Expect(err).Should(BeNil())

	caCert, err := readSampleCertFromFile("ca-cert.pem")
	g.Expect(err).Should(BeNil())
	err = os.WriteFile(caCertFile, caCert, 0o600)
	g.Expect(err).Should(BeNil())

	// Test with incomplete bundle
	signingCABundleComplete, bundleExists, err := checkCABundleCompleteness(
		path.Join(dir, "ca-key.pem"),
		path.Join(dir, "ca-cert.pem"),
		path.Join(dir, "root-cert.pem"),
		[]string{path.Join(dir, "cert-chain.pem")},
	)
	g.Expect(err).Should(BeNil())
	g.Expect(signingCABundleComplete).Should(Equal(false))
	g.Expect(bundleExists).Should(Equal(true))

	// Add missing key file to complete the bundle
	caKey, err := readSampleCertFromFile("ca-key.pem")
	g.Expect(err).Should(BeNil())
	err = os.WriteFile(path.Join(dir, "ca-key.pem"), caKey, 0o600)
	g.Expect(err).Should(BeNil())

	// Test with complete bundle
	signingCABundleComplete, bundleExists, err = checkCABundleCompleteness(
		path.Join(dir, "ca-key.pem"),
		path.Join(dir, "ca-cert.pem"),
		path.Join(dir, "root-cert.pem"),
		[]string{path.Join(dir, "cert-chain.pem")},
	)
	g.Expect(err).Should(BeNil())
	g.Expect(signingCABundleComplete).Should(Equal(true))
	g.Expect(bundleExists).Should(Equal(true))
}

func TestCreateIstioCADisableSelfSignedCA(t *testing.T) {
	pluggedCerts := []string{"ca-cert.pem", "ca-key.pem", "cert-chain.pem", "root-cert.pem"}
	cases := []struct {
		name                string
		disableSelfSigned   bool
		useCacertsForSelfCA bool
		cacertsFiles        []string
		istioGenerated      bool
		expectErr           string
	}{
		{
			name:              "self-signed allowed, no cacerts",
			disableSelfSigned: false,
		},
		{
			name:              "self-signed disabled, no cacerts",
			disableSelfSigned: true,
			expectErr:         "self-signed Istio CA is disabled",
		},
		{
			name:              "self-signed disabled, plugged cacerts",
			disableSelfSigned: true,
			cacertsFiles:      pluggedCerts,
		},
		{
			name:                "self-signed disabled, istio-generated cacerts with USE_CACERTS_FOR_SELF_SIGNED_CA",
			disableSelfSigned:   true,
			useCacertsForSelfCA: true,
			cacertsFiles:        pluggedCerts,
			istioGenerated:      true,
			expectErr:           "self-signed Istio CA is disabled",
		},
		{
			name:              "self-signed disabled, istio-generated cacerts without USE_CACERTS_FOR_SELF_SIGNED_CA",
			disableSelfSigned: true,
			cacertsFiles:      pluggedCerts,
			istioGenerated:    true,
		},
		{
			name:              "self-signed disabled, incomplete cacerts",
			disableSelfSigned: true,
			cacertsFiles:      []string{"ca-cert.pem", "cert-chain.pem", "root-cert.pem"},
			expectErr:         "incomplete signing CA bundle",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			dir := t.TempDir()
			test.SetEnvForTest(t, "ROOT_CA_DIR", dir)
			test.SetForTest(t, &features.DisableSelfSignedCA, tt.disableSelfSigned)
			test.SetForTest(t, &features.UseCacertsForSelfSignedCA, tt.useCacertsForSelfCA)

			for _, f := range tt.cacertsFiles {
				b, err := readSampleCertFromFile(f)
				g.Expect(err).Should(BeNil())
				g.Expect(os.WriteFile(path.Join(dir, f), b, 0o600)).Should(Succeed())
			}
			if tt.istioGenerated {
				g.Expect(os.WriteFile(path.Join(dir, ca.IstioGenerated), []byte{}, 0o600)).Should(Succeed())
			}

			s := &Server{
				internalStop:            make(chan struct{}),
				istiodCertBundleWatcher: keycertbundle.NewWatcher(),
			}
			t.Cleanup(func() {
				close(s.internalStop)
				if s.cacertsWatcher != nil {
					_ = s.cacertsWatcher.Close()
				}
			})

			istioCA, err := s.createIstioCA(&caOptions{Namespace: testNamespace, TrustDomain: "cluster.local"})
			if tt.expectErr != "" {
				g.Expect(err).Should(MatchError(ContainSubstring(tt.expectErr)))
				g.Expect(istioCA).Should(BeNil())
				return
			}
			g.Expect(err).Should(BeNil())
			g.Expect(istioCA).ShouldNot(BeNil())
			g.Expect(istioCA.GetCAKeyCertBundle().GetRootCertPem()).ShouldNot(BeEmpty())
		})
	}
}

func TestCreateIstioCADisableSelfSignedCAWithKubeSecret(t *testing.T) {
	cases := []struct {
		name              string
		disableSelfSigned bool
		existingSecret    bool
		expectErr         bool
	}{
		{
			name:              "self-signed allowed, generates istio-ca-secret",
			disableSelfSigned: false,
		},
		{
			name:              "self-signed allowed, reuses istio-ca-secret",
			disableSelfSigned: false,
			existingSecret:    true,
		},
		{
			name:              "self-signed disabled, does not generate istio-ca-secret",
			disableSelfSigned: true,
			expectErr:         true,
		},
		{
			name:              "self-signed disabled, does not reuse istio-ca-secret",
			disableSelfSigned: true,
			existingSecret:    true,
			expectErr:         true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			test.SetEnvForTest(t, "ROOT_CA_DIR", t.TempDir())
			test.SetForTest(t, &features.DisableSelfSignedCA, tt.disableSelfSigned)

			client := kube.NewFakeClient()
			client.RunAndWait(test.NewStop(t))
			secrets := client.Kube().CoreV1().Secrets(testNamespace)
			var existingCert []byte
			if tt.existingSecret {
				cert, key, err := pkiutil.GenCertKeyFromOptions(pkiutil.CertOptions{
					TTL:          time.Hour,
					Org:          "cluster.local",
					IsCA:         true,
					IsSelfSigned: true,
					RSAKeySize:   2048,
				})
				g.Expect(err).Should(BeNil())
				existingCert = cert
				_, err = secrets.Create(context.Background(), &v1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: ca.CASecret, Namespace: testNamespace},
					Data: map[string][]byte{
						ca.CACertFile:       cert,
						ca.CAPrivateKeyFile: key,
					},
				}, metav1.CreateOptions{})
				g.Expect(err).Should(BeNil())
			}

			s := &Server{
				kubeClient:              client,
				internalStop:            make(chan struct{}),
				istiodCertBundleWatcher: keycertbundle.NewWatcher(),
			}
			t.Cleanup(func() { close(s.internalStop) })

			istioCA, err := s.createIstioCA(&caOptions{Namespace: testNamespace, TrustDomain: "cluster.local"})
			secret, _ := secrets.Get(context.Background(), ca.CASecret, metav1.GetOptions{})
			if tt.expectErr {
				g.Expect(err).Should(MatchError(ContainSubstring("self-signed Istio CA is disabled")))
				g.Expect(istioCA).Should(BeNil())
				if !tt.existingSecret {
					g.Expect(secret.GetName()).Should(BeEmpty(), "istio-ca-secret should not be created")
				}
				return
			}
			g.Expect(err).Should(BeNil())
			g.Expect(secret).ShouldNot(BeNil())
			root := istioCA.GetCAKeyCertBundle().GetRootCertPem()
			if tt.existingSecret {
				g.Expect(root).Should(Equal(existingCert))
			} else {
				g.Expect(root).Should(Equal(secret.Data[ca.CACertFile]))
			}
		})
	}
}

func TestRemoteCerts(t *testing.T) {
	g := NewWithT(t)

	dir := t.TempDir()

	s := Server{
		kubeClient: kube.NewFakeClient(),
	}
	s.kubeClient.RunAndWait(test.NewStop(t))
	caOpts := &caOptions{
		Namespace: testNamespace,
	}

	// Should do nothing because cacerts doesn't exist.
	err := s.loadCACerts(caOpts, dir)
	g.Expect(err).Should(BeNil())

	_, err = os.Stat(path.Join(dir, "root-cert.pem"))
	g.Expect(os.IsNotExist(err)).Should(Equal(true))

	// Should load remote cacerts successfully.
	createCASecret(t, s.kubeClient)

	err = s.loadCACerts(caOpts, dir)
	g.Expect(err).Should(BeNil())

	expectedRoot, err := readSampleCertFromFile("root-cert.pem")
	g.Expect(err).Should(BeNil())
	g.Expect(os.ReadFile(path.Join(dir, "root-cert.pem"))).Should(Equal(expectedRoot))

	// Should do nothing because certs already exist locally.
	err = s.loadCACerts(caOpts, dir)
	g.Expect(err).Should(BeNil())
}

func TestRemoteTLSCerts(t *testing.T) {
	g := NewWithT(t)

	dir := t.TempDir()

	s := Server{
		kubeClient: kube.NewFakeClient(),
	}
	s.kubeClient.RunAndWait(test.NewStop(t))
	caOpts := &caOptions{
		Namespace: testNamespace,
	}

	// Should do nothing because cacerts doesn't exist.
	err := s.loadCACerts(caOpts, dir)
	g.Expect(err).Should(BeNil())

	_, err = os.Stat(path.Join(dir, "ca.crt"))
	g.Expect(os.IsNotExist(err)).Should(Equal(true))

	// Should load remote cacerts successfully.
	createCATLSSecret(t, s.kubeClient)

	err = s.loadCACerts(caOpts, dir)
	g.Expect(err).Should(BeNil())

	expectedRoot, err := readSampleCertFromFile("root-cert.pem")
	g.Expect(err).Should(BeNil())

	g.Expect(os.ReadFile(path.Join(dir, "ca.crt"))).Should(Equal(expectedRoot))

	// Should do nothing because certs already exist locally.
	err = s.loadCACerts(caOpts, dir)
	g.Expect(err).Should(BeNil())
}

func createCATLSSecret(t test.Failer, client kube.Client) {
	var caCert, caKey, rootCert []byte
	var err error
	if caCert, err = readSampleCertFromFile("ca-cert.pem"); err != nil {
		t.Fatal(err)
	}
	if caKey, err = readSampleCertFromFile("ca-key.pem"); err != nil {
		t.Fatal(err)
	}
	if rootCert, err = readSampleCertFromFile("root-cert.pem"); err != nil {
		t.Fatal(err)
	}

	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      "cacerts",
		},
		Type: v1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": caCert,
			"tls.key": caKey,
			"ca.crt":  rootCert,
		},
	}
	clienttest.NewWriter[*v1.Secret](t, client).Create(secret)
}

func createCASecret(t test.Failer, client kube.Client) {
	var caCert, caKey, certChain, rootCert []byte
	var err error
	if caCert, err = readSampleCertFromFile("ca-cert.pem"); err != nil {
		t.Fatal(err)
	}
	if caKey, err = readSampleCertFromFile("ca-key.pem"); err != nil {
		t.Fatal(err)
	}
	if certChain, err = readSampleCertFromFile("cert-chain.pem"); err != nil {
		t.Fatal(err)
	}
	if rootCert, err = readSampleCertFromFile("root-cert.pem"); err != nil {
		t.Fatal(err)
	}

	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      "cacerts",
		},
		Data: map[string][]byte{
			ca.CACertFile:       caCert,
			ca.CAPrivateKeyFile: caKey,
			ca.CertChainFile:    certChain,
			ca.RootCertFile:     rootCert,
		},
	}

	clienttest.NewWriter[*v1.Secret](t, client).Create(secret)
}

func readSampleCertFromFile(f string) ([]byte, error) {
	return os.ReadFile(path.Join(env.IstioSrc, "samples/certs", f))
}

func TestPemBundleHasSubsetRelation(t *testing.T) {
	certA, err := readSampleCertFromFile("root-cert.pem")
	if err != nil {
		t.Fatal(err)
	}
	certB, err := readSampleCertFromFile("ca-cert.pem")
	if err != nil {
		t.Fatal(err)
	}
	certC, err := readSampleCertFromFile("ca-cert-alt.pem")
	if err != nil {
		t.Fatal(err)
	}

	bundleAB := append(append([]byte{}, certA...), certB...)
	bundleBA := append(append([]byte{}, certB...), certA...)
	bundleABC := append(append(append([]byte{}, certA...), certB...), certC...)
	bundleCBA := append(append(append([]byte{}, certC...), certB...), certA...)
	bundleAC := append(append([]byte{}, certA...), certC...)

	cases := []struct {
		name     string
		a        []byte
		b        []byte
		expected bool
	}{
		{
			name:     "identical single cert",
			a:        certA,
			b:        certA,
			expected: true,
		},
		{
			name:     "identical bundle",
			a:        bundleAB,
			b:        bundleAB,
			expected: true,
		},
		{
			name:     "superset and subset",
			a:        bundleAB,
			b:        certA,
			expected: true,
		},
		{
			name:     "subset and superset",
			a:        certA,
			b:        bundleAB,
			expected: true,
		},
		{
			name:     "reordered bundle",
			a:        bundleBA,
			b:        bundleAB,
			expected: true,
		},
		{
			name:     "disjoint certs",
			a:        certA,
			b:        certB,
			expected: false,
		},
		{
			name:     "empty a",
			a:        []byte{},
			b:        certA,
			expected: false,
		},
		{
			name:     "empty b",
			a:        certA,
			b:        []byte{},
			expected: false,
		},
		{
			name:     "invalid pem",
			a:        []byte("not a pem"),
			b:        certA,
			expected: false,
		},
		{
			name:     "three certs rotation adds one",
			a:        bundleABC,
			b:        bundleAB,
			expected: true,
		},
		{
			name:     "three certs rotation removes one",
			a:        bundleAB,
			b:        bundleABC,
			expected: true,
		},
		{
			name:     "three certs reordered",
			a:        bundleCBA,
			b:        bundleABC,
			expected: true,
		},
		{
			name:     "three certs partial overlap not subset",
			a:        bundleAC,
			b:        bundleAB,
			expected: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := pemBundleHasSubsetRelation(tc.a, tc.b)
			if result != tc.expected {
				t.Errorf("pemBundleHasSubsetRelation() = %v, want %v", result, tc.expected)
			}
		})
	}
}
