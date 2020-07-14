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
	"bytes"
	"crypto/tls"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"istio.io/pkg/filewatcher"

	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/testcerts"
)

func TestReloadIstiodCert(t *testing.T) {
	dir, err := ioutil.TempDir("", "istiod_certs")
	stop := make(chan struct{})
	s := &Server{
		fileWatcher: filewatcher.NewWatcher(),
	}

	defer func() {
		close(stop)
		_ = s.fileWatcher.Close()
		_ = os.RemoveAll(dir)
	}()
	if err != nil {
		t.Fatalf("TempDir() failed: %v", err)
	}

	certFile := filepath.Join(dir, "cert-file.yaml")
	keyFile := filepath.Join(dir, "key-file.yaml")

	// load key and cert files.
	if err := ioutil.WriteFile(certFile, testcerts.ServerCert, 0644); err != nil { // nolint: vetshadow
		t.Fatalf("WriteFile(%v) failed: %v", certFile, err)
	}
	if err := ioutil.WriteFile(keyFile, testcerts.ServerKey, 0644); err != nil { // nolint: vetshadow
		t.Fatalf("WriteFile(%v) failed: %v", keyFile, err)
	}

	tlsOptions := TLSOptions{
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	// setup cert watches.
	err = s.initCertificateWatches(tlsOptions)
	for _, fn := range s.startFuncs {
		if err := fn(stop); err != nil {
			t.Fatalf("Could not invoke startFuncs: %v", err)
		}
	}

	if err != nil {
		t.Fatalf("initCertificateWatches failed: %v", err)
	}

	// Validate that the certs are loaded.
	checkCert(t, s, testcerts.ServerCert, testcerts.ServerKey)

	// Update cert/key files.
	if err := ioutil.WriteFile(tlsOptions.CertFile, testcerts.RotatedCert, 0644); err != nil { // nolint: vetshadow
		t.Fatalf("WriteFile(%v) failed: %v", tlsOptions.CertFile, err)
	}
	if err := ioutil.WriteFile(tlsOptions.KeyFile, testcerts.RotatedKey, 0644); err != nil { // nolint: vetshadow
		t.Fatalf("WriteFile(%v) failed: %v", tlsOptions.KeyFile, err)
	}

	g := NewGomegaWithT(t)

	// Validate that istiod cert is updated.
	g.Eventually(func() bool {
		return checkCert(t, s, testcerts.RotatedCert, testcerts.RotatedKey)
	}, "10s", "100ms").Should(BeTrue())
}

func TestNewServer(t *testing.T) {
	// All of the settings to apply and verify. Currently just testing domain suffix,
	// but we should expand this list.
	cases := []struct {
		name           string
		domain         string
		expectedDomain string
		secureGRPCport string
	}{
		{
			name:           "default domain",
			domain:         "",
			expectedDomain: constants.DefaultKubernetesDomain,
		},
		{
			name:           "override domain",
			domain:         "mydomain.com",
			expectedDomain: "mydomain.com",
		},
		{
			name:           "override default secured grpc port",
			domain:         "",
			expectedDomain: constants.DefaultKubernetesDomain,
			secureGRPCport: ":31128",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			configDir, err := ioutil.TempDir("", "TestNewServer")
			if err != nil {
				t.Fatal(err)
			}

			defer func() {
				_ = os.RemoveAll(configDir)
			}()

			args := NewPilotArgs(func(p *PilotArgs) {
				p.Namespace = "istio-system"
				p.ServerOptions = DiscoveryServerOptions{
					// Dynamically assign all ports.
					HTTPAddr:       ":0",
					MonitoringAddr: ":0",
					GRPCAddr:       ":0",
					SecureGRPCAddr: c.secureGRPCport,
				}
				p.RegistryOptions = RegistryOptions{
					KubeOptions: kubecontroller.Options{
						DomainSuffix: c.domain,
					},
					FileDir: configDir,
				}

				// Include all of the default plugins for integration with Mixer, etc.
				p.Plugins = DefaultPlugins
				p.ShutdownDuration = 1 * time.Millisecond
			})

			g := NewGomegaWithT(t)
			s, err := NewServer(args)
			g.Expect(err).To(Succeed())

			stop := make(chan struct{})
			g.Expect(s.Start(stop)).To(Succeed())
			defer func() {
				close(stop)
				s.WaitUntilCompletion()
			}()

			g.Expect(s.environment.GetDomainSuffix()).To(Equal(c.expectedDomain))
		})
	}
}

func checkCert(t *testing.T, s *Server, cert, key []byte) bool {
	t.Helper()
	actual, _ := s.getIstiodCertificate(nil)
	expected, err := tls.X509KeyPair(cert, key)
	if err != nil {
		t.Fatalf("fail to load test certs.")
	}
	return bytes.Equal(actual.Certificate[0], expected.Certificate[0])
}
