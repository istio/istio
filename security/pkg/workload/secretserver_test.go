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

package workload

import (
	"fmt"
	"os"
	"testing"
	"time"

	"io/ioutil"

	"istio.io/istio/security/pkg/pki/util"
)

// TODO(incfly): merge this with createCA() in ca_test.go in a testing library later.
func createBundle() (util.KeyCertBundle, error) {
	rootCAOpts := util.CertOptions{
		IsCA:         true,
		IsSelfSigned: true,
		TTL:          time.Hour,
		Org:          "Root CA",
		RSAKeySize:   2048,
	}
	rootCertBytes, rootKeyBytes, err := util.GenCertKeyFromOptions(rootCAOpts)
	if err != nil {
		return nil, err
	}

	rootCert, err := util.ParsePemEncodedCertificate(rootCertBytes)
	if err != nil {
		return nil, err
	}

	rootKey, err := util.ParsePemEncodedKey(rootKeyBytes)
	if err != nil {
		return nil, err
	}

	intermediateCAOpts := util.CertOptions{
		IsCA:         true,
		IsSelfSigned: false,
		TTL:          time.Hour,
		Org:          "Intermediate CA",
		RSAKeySize:   2048,
		SignerCert:   rootCert,
		SignerPriv:   rootKey,
	}
	intermediateCert, intermediateKey, err := util.GenCertKeyFromOptions(intermediateCAOpts)
	if err != nil {
		return nil, err
	}

	bundle, err := util.NewVerifiedKeyCertBundleFromPem(
		intermediateCert, intermediateKey, intermediateCert, rootCertBytes)
	if err != nil {
		return nil, err
	}
	return bundle, nil
}

func setupTempDir(t *testing.T) (string, func()) {
	t.Helper()
	path, err := ioutil.TempDir("./", t.Name())
	if err != nil {
		t.Errorf("failed to create temp dir for test %v err %v", t.Name(), err)
	}
	return path, func() {
		if err := os.RemoveAll(path); err != nil {
			t.Errorf("failed to clean up temp dir %v", err)
		}
	}
}

func TestNewSecretServer_SecretFile(t *testing.T) {
	ss, err := NewSecretServer(&Config{Mode: SecretDiscoveryServiceAPI})
	if err != nil {
		t.Errorf("server error")
	}

	if ss == nil {
		t.Errorf("secretServer should not be nil")
	}
	kb, err := createBundle()
	if err != nil {
		t.Errorf("failed to create KeyCertBundle %v", kb)
	}
	if err := ss.Save(kb); err != nil {
		t.Errorf("failed to write key cert %v", err)
	}
}

func TestNewSecretServer_WorkloadAPI(t *testing.T) {
	path, cleanup := setupTempDir(t)
	defer cleanup()
	actual, err := NewSecretServer(&Config{Mode: SecretFile, SecretDirectory: path})
	if err != nil {
		t.Errorf("server error")
	}

	if actual == nil {
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
