// Copyright 2018 Istio Authors
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

package secrets

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"testing"
	"time"

	"istio.io/istio/security/pkg/pki/util"
)

// TODO(incfly): put this in a testing library.
func createBundle(t *testing.T, host string) util.KeyCertBundle {
	rootCAOpts := util.CertOptions{
		Host:         host,
		IsCA:         true,
		IsSelfSigned: true,
		TTL:          time.Hour,
		Org:          "Root CA",
	}
	rootCertBytes, rootKeyBytes, err := util.GenCertKeyFromOptions(rootCAOpts)
	if err != nil {
		t.Errorf("failed to genecreate key for %v", host)
	}
	bundle, err := util.NewVerifiedKeyCertBundleFromPem(
		rootCertBytes, rootKeyBytes, rootCertBytes, rootCertBytes)
	if err != nil {
		t.Errorf("failed to create key cert bundle for %v", host)
	}
	return bundle
}

func setupTempDir(t *testing.T) (string, func()) {
	t.Helper()
	path, err := ioutil.TempDir("./", t.Name())
	if err != nil {
		t.Errorf("failed to create temp dir for test %v err %v", t.Name(), err)
	}
	return path, func() {
		_ = os.RemoveAll(path)
	}
}

func TestNewSecretServer_FileMode(t *testing.T) {
	dir, cleanup := setupTempDir(t)
	defer cleanup()
	ss, err := NewSecretServer(&Config{Mode: SecretFile, SecretDirectory: dir})
	if err != nil {
		t.Errorf("failed to create file mode secret server err (%v)", err)
	}
	bundleMap := map[string]util.KeyCertBundle{}
	hosts := []string{"sa1", "sa2"}
	for _, host := range hosts {
		bundleMap[host] = createBundle(t, host)
	}
	for _, host := range hosts {
		b := bundleMap[host]
		if err := ss.Put(host, b); err != nil {
			t.Errorf("failed to save secret for %v, err %v", host, err)
		}
	}

	// Verify each identity has correct key certs saved in the file.
	for _, host := range hosts {
		b := bundleMap[host]
		_, key, cert, root := b.GetAllPem()

		keyBytes, err := ioutil.ReadFile(path.Join(dir, host, "key.pem"))
		if err != nil {
			t.Errorf("failed to read key file for %v, error %v", host, err)
		}
		if !bytes.Equal(key, keyBytes) {
			t.Errorf("unexpecte key for %v, want\n%v\ngot\n%v", host, string(key), string(keyBytes))
		}

		certBytes, err := ioutil.ReadFile(path.Join(dir, host, "cert-chain.pem"))
		if err != nil {
			t.Errorf("failed to read cert file for %v, error %v", host, err)
		}
		if !bytes.Equal(cert, certBytes) {
			t.Errorf("unexpecte cert for %v, want\n%v\ngot\n%v", host, string(cert), string(certBytes))
		}

		rootBytes, err := ioutil.ReadFile(path.Join(dir, host, "root-cert.pem"))
		if err != nil {
			t.Errorf("failed to read root file for %v, error %v", host, err)
		}
		if !bytes.Equal(root, rootBytes) {
			t.Errorf("unexpecte root for %v, want\n%v\ngot\n%v", host, string(root), string(rootBytes))
		}
	}
}

func TestNewSecretServer_WorkloadAPI(t *testing.T) {
	ss, err := NewSecretServer(&Config{Mode: SecretDiscoveryServiceAPI})
	if err != nil {
		t.Errorf("failed to create SDS mode secret server err (%v)", err)
	}
	if ss == nil {
		t.Errorf("secretServer should not be nil")
	}
}

func TestNewSecretServer_Unsupported(t *testing.T) {
	actual, err := NewSecretServer(&Config{Mode: SecretDiscoveryServiceAPI + 1})
	expectedErr := fmt.Errorf("mode: 2 is not supported")
	if err == nil || err.Error() != expectedErr.Error() {
		t.Errorf("error message mismatch got %v want %v", err, expectedErr)
	}

	if actual != nil {
		t.Errorf("server should be nil")
	}
}
