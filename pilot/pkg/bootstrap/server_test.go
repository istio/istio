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
package bootstrap

import (
	"bytes"
	"crypto/tls"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"istio.io/istio/pkg/testcerts"
	"istio.io/pkg/filewatcher"

	"github.com/onsi/gomega"
)

func TestReloadIstiodCert(t *testing.T) {
	dir, err := ioutil.TempDir("", "istiod_certs")
	stop := make(chan struct{})
	s := &Server{
		fileWatcher: filewatcher.NewWatcher(),
	}

	defer func() {
		close(stop)
		s.fileWatcher.Close()
		_ = os.RemoveAll(dir) // nolint: errcheck
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

	// Set increased value in tests to allow copy of both files to be finished.
	watchDebounceDelay = 200 * time.Millisecond

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

	g := gomega.NewGomegaWithT(t)

	// Validate that istiod cert is updated.
	g.Eventually(func() bool {
		return checkCert(t, s, testcerts.RotatedCert, testcerts.RotatedKey)
	}, "10s", "100ms").Should(gomega.BeTrue())
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
