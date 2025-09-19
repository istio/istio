// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package bootstrap

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"golang.org/x/net/http2"
	cert "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/keycertbundle"
	"istio.io/istio/pilot/pkg/server"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/filewatcher"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/testcerts"
	"istio.io/istio/security/pkg/pki/util"
)

func loadCertFilesAtPaths(t TLSFSLoadPaths) error {
	// create cert directories if not existing
	if err := os.MkdirAll(filepath.Dir(t.testTLSCertFilePath), os.ModePerm); err != nil {
		return fmt.Errorf("Mkdirall(%v) failed: %v", t.testTLSCertFilePath, err)
	}

	if err := os.MkdirAll(filepath.Dir(t.testTLSKeyFilePath), os.ModePerm); err != nil {
		return fmt.Errorf("Mkdirall(%v) failed: %v", t.testTLSKeyFilePath, err)
	}

	if err := os.MkdirAll(filepath.Dir(t.testCaCertFilePath), os.ModePerm); err != nil {
		return fmt.Errorf("Mkdirall(%v) failed: %v", t.testCaCertFilePath, err)
	}

	// load key and cert files.
	if err := os.WriteFile(t.testTLSCertFilePath, testcerts.ServerCert, 0o644); err != nil { // nolint: vetshadow
		return fmt.Errorf("WriteFile(%v) failed: %v", t.testTLSCertFilePath, err)
	}
	if err := os.WriteFile(t.testTLSKeyFilePath, testcerts.ServerKey, 0o644); err != nil { // nolint: vetshadow
		return fmt.Errorf("WriteFile(%v) failed: %v", t.testTLSKeyFilePath, err)
	}
	if err := os.WriteFile(t.testCaCertFilePath, testcerts.CACert, 0o644); err != nil { // nolint: vetshadow
		return fmt.Errorf("WriteFile(%v) failed: %v", t.testCaCertFilePath, err)
	}

	return nil
}

func cleanupCertFileSystemFiles(t TLSFSLoadPaths) error {
	if err := os.Remove(t.testTLSCertFilePath); err != nil {
		return fmt.Errorf("Test cleanup failed, could not delete %s", t.testTLSCertFilePath)
	}

	if err := os.Remove(t.testTLSKeyFilePath); err != nil {
		return fmt.Errorf("Test cleanup failed, could not delete %s", t.testTLSKeyFilePath)
	}

	if err := os.Remove(t.testCaCertFilePath); err != nil {
		return fmt.Errorf("Test cleanup failed, could not delete %s", t.testCaCertFilePath)
	}
	return nil
}

// This struct will indicate for each test case
// where tls assets will be loaded on disk
type TLSFSLoadPaths struct {
	testTLSCertFilePath string
	testTLSKeyFilePath  string
	testCaCertFilePath  string
}

func TestNewServerCertInit(t *testing.T) {
	configDir := t.TempDir()

	tlsArgCertsDir := t.TempDir()

	tlsArgcertFile := filepath.Join(tlsArgCertsDir, "cert-file.pem")
	tlsArgkeyFile := filepath.Join(tlsArgCertsDir, "key-file.pem")
	tlsArgcaCertFile := filepath.Join(tlsArgCertsDir, "ca-cert.pem")

	cases := []struct {
		name                      string
		FSCertsPaths              TLSFSLoadPaths
		tlsOptions                *TLSOptions
		enableCA                  bool
		certProvider              string
		expNewCert                bool
		expCert                   []byte
		expKey                    []byte
		expSecureDiscoveryService bool
	}{
		{
			name:         "Load from existing DNS cert",
			FSCertsPaths: TLSFSLoadPaths{tlsArgcertFile, tlsArgkeyFile, tlsArgcaCertFile},
			tlsOptions: &TLSOptions{
				CertFile:   tlsArgcertFile,
				KeyFile:    tlsArgkeyFile,
				CaCertFile: tlsArgcaCertFile,
			},
			enableCA:                  false,
			certProvider:              constants.CertProviderIstiod,
			expNewCert:                false,
			expCert:                   testcerts.ServerCert,
			expKey:                    testcerts.ServerKey,
			expSecureDiscoveryService: true,
		},
		{
			name:         "Create new DNS cert using Istiod",
			FSCertsPaths: TLSFSLoadPaths{},
			tlsOptions: &TLSOptions{
				CertFile:   "",
				KeyFile:    "",
				CaCertFile: "",
			},
			enableCA:                  true,
			certProvider:              constants.CertProviderIstiod,
			expNewCert:                true,
			expCert:                   []byte{},
			expKey:                    []byte{},
			expSecureDiscoveryService: true,
		},
		{
			name:         "No DNS cert created because CA is disabled",
			FSCertsPaths: TLSFSLoadPaths{},
			tlsOptions:   &TLSOptions{},
			enableCA:     false,
			certProvider: constants.CertProviderIstiod,
			expNewCert:   false,
			expCert:      []byte{},
			expKey:       []byte{},
		},
		{
			name: "DNS cert loaded because it is in known even if CA is Disabled",
			FSCertsPaths: TLSFSLoadPaths{
				constants.DefaultPilotTLSCert,
				constants.DefaultPilotTLSKey,
				constants.DefaultPilotTLSCaCert,
			},
			tlsOptions:                &TLSOptions{},
			enableCA:                  false,
			certProvider:              constants.CertProviderNone,
			expNewCert:                false,
			expCert:                   testcerts.ServerCert,
			expKey:                    testcerts.ServerKey,
			expSecureDiscoveryService: true,
		},
		{
			name: "DNS cert loaded from known location, even if CA is Disabled, with a fallback CA path",
			FSCertsPaths: TLSFSLoadPaths{
				constants.DefaultPilotTLSCert,
				constants.DefaultPilotTLSKey,
				constants.DefaultPilotTLSCaCertAlternatePath,
			},
			tlsOptions:                &TLSOptions{},
			enableCA:                  false,
			certProvider:              constants.CertProviderNone,
			expNewCert:                false,
			expCert:                   testcerts.ServerCert,
			expKey:                    testcerts.ServerKey,
			expSecureDiscoveryService: true,
		},
		{
			name:         "No cert provider",
			FSCertsPaths: TLSFSLoadPaths{},
			tlsOptions:   &TLSOptions{},
			enableCA:     true,
			certProvider: constants.CertProviderNone,
			expNewCert:   false,
			expCert:      []byte{},
			expKey:       []byte{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			test.SetForTest(t, &features.PilotCertProvider, c.certProvider)
			test.SetForTest(t, &features.EnableCAServer, c.enableCA)

			// Run test in an isolated directory so we don't read test files written by other cases
			assert.NoError(t, os.Chdir(t.TempDir()))
			// check if we have some tls assets to write for test
			if c.FSCertsPaths != (TLSFSLoadPaths{}) {
				err := loadCertFilesAtPaths(c.FSCertsPaths)
				if err != nil {
					t.Fatal(err.Error())
				}

				defer cleanupCertFileSystemFiles(c.FSCertsPaths)
			}

			args := NewPilotArgs(func(p *PilotArgs) {
				p.Namespace = "istio-system"
				p.ServerOptions = DiscoveryServerOptions{
					// Dynamically assign all ports.
					HTTPAddr:       ":0",
					MonitoringAddr: ":0",
					GRPCAddr:       ":0",
					SecureGRPCAddr: ":0",
					TLSOptions:     *c.tlsOptions,
				}
				p.RegistryOptions = RegistryOptions{
					FileDir: configDir,
				}

				p.ShutdownDuration = 1 * time.Millisecond
			})
			g := NewWithT(t)
			s, err := NewServer(args, func(s *Server) {
				s.kubeClient = kube.NewFakeClient()
			})
			g.Expect(err).To(Succeed())
			stop := make(chan struct{})
			g.Expect(s.Start(stop)).To(Succeed())
			defer func() {
				close(stop)
				s.WaitUntilCompletion()
			}()

			if c.expNewCert {
				if istiodCert, err := s.getIstiodCertificate(nil); istiodCert == nil || err != nil {
					t.Errorf("Istiod failed to generate new DNS cert")
				}
			} else {
				if len(c.expCert) != 0 {
					if !checkCert(t, s, c.expCert, c.expKey) {
						t.Errorf("Istiod certificate does not match the expectation")
					}
				} else {
					if _, err := s.getIstiodCertificate(nil); err == nil {
						t.Errorf("Istiod should not generate new DNS cert")
					}
				}
			}

			if c.expSecureDiscoveryService {
				if s.secureGrpcServer == nil {
					t.Errorf("Istiod secure grpc server was not started.")
				}
			}
		})
	}
}

func TestReloadIstiodCert(t *testing.T) {
	dir := t.TempDir()
	stop := make(chan struct{})
	s := &Server{
		fileWatcher:             filewatcher.NewWatcher(),
		server:                  server.New(),
		istiodCertBundleWatcher: keycertbundle.NewWatcher(),
	}

	defer func() {
		close(stop)
		_ = s.fileWatcher.Close()
	}()

	certFile := filepath.Join(dir, "cert-file.yaml")
	keyFile := filepath.Join(dir, "key-file.yaml")
	caFile := filepath.Join(dir, "ca-file.yaml")

	// load key and cert files.
	if err := os.WriteFile(certFile, testcerts.ServerCert, 0o644); err != nil { // nolint: vetshadow
		t.Fatalf("WriteFile(%v) failed: %v", certFile, err)
	}
	if err := os.WriteFile(keyFile, testcerts.ServerKey, 0o644); err != nil { // nolint: vetshadow
		t.Fatalf("WriteFile(%v) failed: %v", keyFile, err)
	}

	if err := os.WriteFile(caFile, testcerts.CACert, 0o644); err != nil { // nolint: vetshadow
		t.Fatalf("WriteFile(%v) failed: %v", caFile, err)
	}

	tlsOptions := TLSOptions{
		CertFile:   certFile,
		KeyFile:    keyFile,
		CaCertFile: caFile,
	}

	// setup cert watches.
	if err := s.initFileCertificateWatches(tlsOptions); err != nil {
		t.Fatalf("initCertificateWatches failed: %v", err)
	}

	if err := s.initIstiodCertLoader(); err != nil {
		t.Fatal("istiod unable to load its cert")
	}

	if err := s.server.Start(stop); err != nil {
		t.Fatalf("Could not invoke startFuncs: %v", err)
	}

	// Validate that the certs are loaded.
	if !checkCert(t, s, testcerts.ServerCert, testcerts.ServerKey) {
		t.Errorf("Istiod certificate does not match the expectation")
	}

	// Update cert/key files.
	if err := os.WriteFile(tlsOptions.CertFile, testcerts.RotatedCert, 0o644); err != nil { // nolint: vetshadow
		t.Fatalf("WriteFile(%v) failed: %v", tlsOptions.CertFile, err)
	}
	if err := os.WriteFile(tlsOptions.KeyFile, testcerts.RotatedKey, 0o644); err != nil { // nolint: vetshadow
		t.Fatalf("WriteFile(%v) failed: %v", tlsOptions.KeyFile, err)
	}

	g := NewWithT(t)

	// Validate that istiod cert is updated.
	g.Eventually(func() bool {
		return checkCert(t, s, testcerts.RotatedCert, testcerts.RotatedKey)
	}, "10s", "100ms").Should(BeTrue())
}

func TestReloadcacerts(t *testing.T) {
	var err error

	// Run test in an isolated directory so we don't read test files written by other cases
	// set ROOT_CA_DIR to temp cacertsDir and create the directory
	cacertsDir := filepath.Join(t.TempDir(), "/etc/cacerts")
	if err := os.MkdirAll(cacertsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%v) failed: %v", cacertsDir, err)
	}
	test.SetEnvForTest(t, "ROOT_CA_DIR", cacertsDir)
	test.SetForTest(t, &features.EnableCAServer, true)
	assert.NoError(t, os.Chdir(t.TempDir()))

	// load cacerts files into memory
	var caCert, caKey, certChain, rootCert []byte
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
	// write cacerts files to temp dir
	if err := os.WriteFile(filepath.Join(cacertsDir, "ca-cert.pem"), caCert, 0o644); err != nil { // nolint: vetshadow
		t.Fatalf("WriteFile(%v) failed: %v", filepath.Join(cacertsDir, "ca-cert.pem"), err)
	}
	if err := os.WriteFile(filepath.Join(cacertsDir, "ca-key.pem"), caKey, 0o644); err != nil { // nolint: vetshadow
		t.Fatalf("WriteFile(%v) failed: %v", filepath.Join(cacertsDir, "ca-key.pem"), err)
	}
	if err := os.WriteFile(filepath.Join(cacertsDir, "cert-chain.pem"), certChain, 0o644); err != nil { // nolint: vetshadow
		t.Fatalf("WriteFile(%v) failed: %v", filepath.Join(cacertsDir, "cert-chain.pem"), err)
	}
	if err := os.WriteFile(filepath.Join(cacertsDir, "root-cert.pem"), rootCert, 0o644); err != nil { // nolint: vetshadow
		t.Fatalf("WriteFile(%v) failed: %v", filepath.Join(cacertsDir, "root-cert.pem"), err)
	}

	stop := make(chan struct{})
	s := &Server{
		istiodCertBundleWatcher: keycertbundle.NewWatcher(),
		server:                  server.New(),
	}

	defer func() {
		close(stop)
		_ = s.cacertsWatcher.Close()
	}()

	// start server
	if err := s.server.Start(stop); err != nil {
		t.Fatalf("Could not invoke startFuncs: %v", err)
	}

	// create server CA for load cacerts files
	if err = s.maybeCreateCA(&caOptions{}); err != nil {
		t.Fatalf("Could not create CA: %v", err)
	}

	// validate that the cacerts files are loaded
	g := NewWithT(t)
	g.Eventually(func() bool {
		return len(s.CA.GetCAKeyCertBundle().GetCertChainPem()) > 0
	}, "10s", "100ms").Should(BeTrue())

	// update cert chain of cert bundle to alt version
	certChainAlt, err := readSampleCertFromFile("cert-chain-alt.pem")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacertsDir, "cert-chain.pem"), certChainAlt, 0o644); err != nil { // nolint: vetshadow
		t.Fatalf("WriteFile(%v) failed: %v", filepath.Join(cacertsDir, "cert-chain.pem"), err)
	}

	// validate that the cert chain is updated
	g.Eventually(func() bool {
		currentCertChain := s.CA.GetCAKeyCertBundle().GetCertChainPem()
		return bytes.Equal(currentCertChain, certChainAlt)
	}, "10s", "100ms").Should(BeTrue())
}

func TestNewServer(t *testing.T) {
	// All of the settings to apply and verify. Currently just testing domain suffix,
	// but we should expand this list.
	cases := []struct {
		name             string
		domain           string
		expectedDomain   string
		enableSecureGRPC bool
		jwtRule          string
	}{
		{
			name:           "default domain",
			domain:         "",
			expectedDomain: constants.DefaultClusterLocalDomain,
		},
		{
			name:           "default domain with JwtRule",
			domain:         "",
			expectedDomain: constants.DefaultClusterLocalDomain,
			jwtRule:        `{"issuer": "foo", "jwks_uri": "baz", "audiences": ["aud1", "aud2"]}`,
		},
		{
			name:           "override domain",
			domain:         "mydomain.com",
			expectedDomain: "mydomain.com",
		},
		{
			name:             "override default secured grpc port",
			domain:           "",
			expectedDomain:   constants.DefaultClusterLocalDomain,
			enableSecureGRPC: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			configDir := t.TempDir()

			secureGRPCPort := ""
			if c.enableSecureGRPC {
				secureGRPCPort = ":0"
			}

			args := NewPilotArgs(func(p *PilotArgs) {
				p.Namespace = "istio-system"
				p.ServerOptions = DiscoveryServerOptions{
					// Dynamically assign all ports.
					HTTPAddr:       ":0",
					MonitoringAddr: ":0",
					GRPCAddr:       ":0",
					SecureGRPCAddr: secureGRPCPort,
				}
				p.RegistryOptions = RegistryOptions{
					KubeOptions: kubecontroller.Options{
						DomainSuffix: c.domain,
					},
					FileDir: configDir,
				}

				p.ShutdownDuration = 1 * time.Millisecond

				p.JwtRule = c.jwtRule
			})

			g := NewWithT(t)
			s, err := NewServer(args, func(s *Server) {
				s.kubeClient = kube.NewFakeClient()
			})
			g.Expect(err).To(Succeed())
			stop := make(chan struct{})
			g.Expect(s.Start(stop)).To(Succeed())
			defer func() {
				close(stop)
				s.WaitUntilCompletion()
			}()

			g.Expect(s.environment.DomainSuffix).To(Equal(c.expectedDomain))

			assert.Equal(t, s.secureGrpcServer != nil, c.enableSecureGRPC)
		})
	}
}

func TestMultiplex(t *testing.T) {
	configDir := t.TempDir()

	var secureGRPCPort int
	var err error

	args := NewPilotArgs(func(p *PilotArgs) {
		p.Namespace = "istio-system"
		p.ServerOptions = DiscoveryServerOptions{
			// Dynamically assign all ports.
			HTTPAddr:       ":0",
			MonitoringAddr: ":0",
			GRPCAddr:       "",
			SecureGRPCAddr: fmt.Sprintf(":%d", secureGRPCPort),
		}
		p.RegistryOptions = RegistryOptions{
			FileDir: configDir,
		}

		p.ShutdownDuration = 1 * time.Millisecond
	})

	g := NewWithT(t)
	s, err := NewServer(args, func(s *Server) {
		s.kubeClient = kube.NewFakeClient()
	})
	g.Expect(err).To(Succeed())
	stop := make(chan struct{})
	g.Expect(s.Start(stop)).To(Succeed())
	defer func() {
		close(stop)
		s.WaitUntilCompletion()
	}()
	t.Run("h1", func(t *testing.T) {
		c := http.Client{}
		defer c.CloseIdleConnections()
		resp, err := c.Get("http://" + s.httpAddr + "/validate")
		assert.NoError(t, err)
		// Validate returns 400 on no body; if we got this the request works
		assert.Equal(t, resp.StatusCode, 400)
		resp.Body.Close()
	})
	t.Run("h2", func(t *testing.T) {
		c := http.Client{
			Transport: &http2.Transport{
				// Golang doesn't have first class support for h2c, so we provide some workarounds
				// See https://www.mailgun.com/blog/http-2-cleartext-h2c-client-example-go/
				// So http2.Transport doesn't complain the URL scheme isn't 'https'
				AllowHTTP: true,
				// Pretend we are dialing a TLS endpoint. (Note, we ignore the passed tls.Config)
				DialTLSContext: func(_ context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
					return net.Dial(network, addr)
				},
			},
		}
		defer c.CloseIdleConnections()

		resp, err := c.Get("http://" + s.httpAddr + "/validate")
		assert.NoError(t, err)
		// Validate returns 400 on no body; if we got this the request works
		assert.Equal(t, resp.StatusCode, 400)
		resp.Body.Close()
	})
}

func TestIstiodCipherSuites(t *testing.T) {
	cases := []struct {
		name               string
		serverCipherSuites []uint16
		clientCipherSuites []uint16
		expectSuccess      bool
	}{
		{
			name:          "default cipher suites",
			expectSuccess: true,
		},
		{
			name:               "client and istiod cipher suites match",
			serverCipherSuites: []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256, tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
			clientCipherSuites: []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256, tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
			expectSuccess:      true,
		},
		{
			name:               "client and istiod cipher suites mismatch",
			serverCipherSuites: []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256, tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
			clientCipherSuites: []uint16{tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384, tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384},
			expectSuccess:      false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			configDir := t.TempDir()
			args := NewPilotArgs(func(p *PilotArgs) {
				p.Namespace = "istio-system"
				p.ServerOptions = DiscoveryServerOptions{
					// Dynamically assign all ports.
					HTTPAddr:       ":0",
					MonitoringAddr: ":0",
					GRPCAddr:       ":0",
					HTTPSAddr:      ":0",
					TLSOptions: TLSOptions{
						CipherSuites: c.serverCipherSuites,
					},
				}
				p.RegistryOptions = RegistryOptions{
					KubeConfig: "config",
					FileDir:    configDir,
				}

				// Include all of the default plugins
				p.ShutdownDuration = 1 * time.Millisecond
			})

			g := NewWithT(t)
			s, err := NewServer(args, func(s *Server) {
				s.kubeClient = kube.NewFakeClient()
			})
			g.Expect(err).To(Succeed())

			stop := make(chan struct{})
			g.Expect(s.Start(stop)).To(Succeed())
			defer func() {
				close(stop)
				s.WaitUntilCompletion()
			}()
		})
	}
}

func TestIstiodReadinessHandler(t *testing.T) {
	configDir := t.TempDir()

	args := NewPilotArgs(func(p *PilotArgs) {
		p.Namespace = "istio-system"
		p.ServerOptions = DiscoveryServerOptions{
			// Dynamically assign all ports.
			HTTPAddr:       ":0",
			HTTPSAddr:      ":0",
			MonitoringAddr: ":0",
			GRPCAddr:       ":0",
		}
		p.RegistryOptions = RegistryOptions{
			KubeConfig: "config",
			FileDir:    configDir,
		}
		p.ShutdownDuration = 1 * time.Millisecond
	})

	g := NewWithT(t)
	s, err := NewServer(args, func(s *Server) {
		s.kubeClient = kube.NewFakeClient()
	})
	g.Expect(err).To(Succeed())

	stop := make(chan struct{})
	g.Expect(s.Start(stop)).To(Succeed())
	defer func() {
		close(stop)
		s.WaitUntilCompletion()
	}()

	c := http.Client{}
	c.Transport = &http.Transport{
		// nolint: gosec
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}
	defer c.CloseIdleConnections()

	for _, url := range []string{"http://" + s.httpAddr, "https://" + s.httpsAddr} {
		resp, err := c.Get(url + "/ready")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
		g.Expect(resp.Body.Close()).To(Succeed())
	}
}

func TestInitOIDC(t *testing.T) {
	tests := []struct {
		name      string
		expectErr bool
		jwtRule   string
	}{
		{
			name:      "valid jwt rule",
			expectErr: false,
			jwtRule:   `{"issuer": "foo", "jwks_uri": "baz", "audiences": ["aud1", "aud2"]}`,
		},
		{
			name:      "invalid jwt rule",
			expectErr: true,
			jwtRule:   "invalid",
		},
		{
			name:      "jwt rule with invalid audiences",
			expectErr: true,
			// audiences must be a string array
			jwtRule: `{"issuer": "foo", "jwks_uri": "baz", "audiences": "aud1"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := &PilotArgs{JwtRule: tt.jwtRule}

			_, err := initOIDC(args, nil)
			gotErr := err != nil
			if gotErr != tt.expectErr {
				t.Errorf("expect error is %v while actual error is %v", tt.expectErr, gotErr)
			}
		})
	}
}

func TestWatchDNSCertForK8sCA(t *testing.T) {
	tests := []struct {
		name        string
		certToWatch []byte
		certRotated bool
	}{
		{
			name:        "expired cert rotation",
			certToWatch: testcerts.ExpiredServerCert,
			certRotated: true,
		},
		{
			name:        "valid cert no rotation",
			certToWatch: testcerts.ServerCert,
			certRotated: false,
		},
	}

	csr := &cert.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: "abc.xyz",
		},
		Status: cert.CertificateSigningRequestStatus{
			Certificate: testcerts.ServerCert,
		},
	}
	s := &Server{
		server:                  server.New(),
		istiodCertBundleWatcher: keycertbundle.NewWatcher(),
		kubeClient:              kube.NewFakeClient(csr),
		dnsNames:                []string{"abc.xyz"},
	}
	s.kubeClient.RunAndWait(test.NewStop(t))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stop := make(chan struct{})

			s.istiodCertBundleWatcher.SetAndNotify(testcerts.ServerKey, tt.certToWatch, testcerts.CACert)
			go s.RotateDNSCertForK8sCA(stop, "", "test-signer", true, time.Duration(0))

			var certRotated bool
			var rotatedCertBytes []byte
			err := retry.Until(func() bool {
				rotatedCertBytes = s.istiodCertBundleWatcher.GetKeyCertBundle().CertPem
				st := string(rotatedCertBytes)
				certRotated = st != string(tt.certToWatch)
				return certRotated == tt.certRotated
			}, retry.Timeout(10*time.Second))

			close(stop)
			if err != nil {
				t.Fatalf("expect certRotated is %v while actual certRotated is %v", tt.certRotated, certRotated)
			}
			cert, certErr := util.ParsePemEncodedCertificate(rotatedCertBytes)
			if certErr != nil {
				t.Fatal("rotated cert is not valid")
			}
			currTime := time.Now()
			timeToExpire := cert.NotAfter.Sub(currTime)
			if timeToExpire < 0 {
				t.Fatal("rotated cert is already expired")
			}
		})
	}
}

func checkCert(t *testing.T, s *Server, cert, key []byte) bool {
	t.Helper()
	actual, err := s.getIstiodCertificate(nil)
	if err != nil {
		t.Fatal("fail to load fetch certs.")
	}
	expected, err := tls.X509KeyPair(cert, key)
	if err != nil {
		t.Fatal("fail to load test certs.")
	}
	return bytes.Equal(actual.Certificate[0], expected.Certificate[0])
}

func TestGetDNSNames(t *testing.T) {
	tests := []struct {
		name             string
		customHost       string
		discoveryAddress string
		revision         string
		sans             []string
	}{
		{
			name:             "no customHost",
			customHost:       "",
			discoveryAddress: "istiod.istio-system.svc.cluster.local",
			revision:         "default",
			sans: []string{
				"istio-pilot.istio-system.svc",
				"istiod-remote.istio-system.svc",
				"istiod.istio-system.svc",
				"istiod.istio-system.svc.cluster.local",
			},
		},
		{
			name:             "default revision",
			customHost:       "a.com,b.com,c.com",
			discoveryAddress: "istiod.istio-system.svc.cluster.local",
			revision:         "default",
			sans: []string{
				"a.com", "b.com", "c.com",
				"istio-pilot.istio-system.svc",
				"istiod-remote.istio-system.svc",
				"istiod.istio-system.svc",
				"istiod.istio-system.svc.cluster.local",
			},
		},
		{
			name:             "empty revision",
			customHost:       "a.com,b.com,c.com",
			discoveryAddress: "istiod.istio-system.svc.cluster.local",
			revision:         "",
			sans: []string{
				"a.com", "b.com", "c.com",
				"istio-pilot.istio-system.svc",
				"istiod-remote.istio-system.svc",
				"istiod.istio-system.svc",
				"istiod.istio-system.svc.cluster.local",
			},
		},
		{
			name:             "canary revision",
			customHost:       "a.com,b.com,c.com",
			discoveryAddress: "istiod.istio-system.svc.cluster.local",
			revision:         "canary",
			sans: []string{
				"a.com", "b.com", "c.com",
				"istio-pilot.istio-system.svc",
				"istiod-canary.istio-system.svc",
				"istiod-remote.istio-system.svc",
				"istiod.istio-system.svc",
				"istiod.istio-system.svc.cluster.local",
			},
		},
		{
			name:             "customHost has duplicate hosts with inner default",
			customHost:       "a.com,b.com,c.com,istiod",
			discoveryAddress: "istiod.istio-system.svc.cluster.local",
			revision:         "canary",
			sans: []string{
				"a.com", "b.com", "c.com",
				"istio-pilot.istio-system.svc",
				"istiod", // from the customHost
				"istiod-canary.istio-system.svc",
				"istiod-remote.istio-system.svc",
				"istiod.istio-system.svc",
				"istiod.istio-system.svc.cluster.local",
			},
		},
		{
			name:             "customHost has duplicate hosts with discovery address",
			customHost:       "a.com,b.com,c.com,test.com",
			discoveryAddress: "test.com",
			revision:         "canary",
			sans: []string{
				"a.com", "b.com", "c.com",
				"istio-pilot.istio-system.svc",
				"istiod-canary.istio-system.svc",
				"istiod-remote.istio-system.svc",
				"istiod.istio-system.svc",
				"test.com",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			features.IstiodServiceCustomHost = tc.customHost
			var args PilotArgs
			args.Revision = tc.revision
			args.Namespace = "istio-system"
			sans := getDNSNames(&args, tc.discoveryAddress)
			assert.Equal(t, sans, tc.sans)
		})
	}
}
