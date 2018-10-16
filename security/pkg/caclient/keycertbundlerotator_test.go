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

package caclient

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	pkiutil "istio.io/istio/security/pkg/pki/util"
	pkimock "istio.io/istio/security/pkg/pki/util/mock"
	"istio.io/istio/security/pkg/util"
	utilmock "istio.io/istio/security/pkg/util/mock"
)

type fakeKeyCertRetriever struct {
	NewCert    []byte
	CertChain  []byte
	PrivateKey []byte
	Err        error
}

func (r *fakeKeyCertRetriever) Retrieve(_ *pkiutil.CertOptions) (newCert, certChain, privateKey []byte, err error) {
	if r.Err != nil {
		return nil, nil, nil, r.Err
	}
	return r.NewCert, r.CertChain, r.PrivateKey, nil
}

func TestNewKeyCertBundleRotator(t *testing.T) {
	testCases := map[string]struct {
		config      *Config
		expectedErr string
	}{
		"null config": {
			config:      nil,
			expectedErr: "nil configuration passed",
		},
		"No CA address": {
			config: &Config{
				RootCertFile:  "../platform/testdata/cert-root-good.pem",
				KeyFile:       "../platform/testdata/key-from-root-good.pem",
				CertChainFile: "../platform/testdata/cert-from-root-good.pem",
				Env:           "onprem",
				CAAddress:     "",
			},
			expectedErr: "istio CA address is empty",
		},
		"Successful init with on prem env": {
			config: &Config{
				RootCertFile:             "../platform/testdata/cert-root-good.pem",
				KeyFile:                  "../platform/testdata/key-from-root-good.pem",
				CertChainFile:            "../platform/testdata/cert-from-root-good.pem",
				Env:                      "onprem",
				CAAddress:                "0.0.0.0:8060",
				CSRGracePeriodPercentage: 50,
			},
			expectedErr: "",
		},
		"Successful init with unspecified env": {
			config: &Config{
				RootCertFile:             "../platform/testdata/cert-root-good.pem",
				KeyFile:                  "../platform/testdata/key-from-root-good.pem",
				CertChainFile:            "../platform/testdata/cert-from-root-good.pem",
				Env:                      "unspecified",
				CAAddress:                "0.0.0.0:8060",
				CSRGracePeriodPercentage: 50,
			},
			expectedErr: "",
		},
	}

	for id, c := range testCases {
		_, err := NewKeyCertBundleRotator(c.config, &pkimock.FakeKeyCertBundle{})

		if len(c.expectedErr) > 0 {
			if err == nil {
				t.Errorf("%s: succeeded, error expected: %v", id, err)
			} else if err.Error() != c.expectedErr {
				t.Errorf("%s: incorrect error message: %s VS %s",
					id, err.Error(), c.expectedErr)
			}
		} else if err != nil {
			t.Errorf("%s: unexpected error: %v", id, err)
		}
	}
}

func TestKeyCertBundleRotator(t *testing.T) {
	oldCert, oldKey, oldCertChain, oldRootCert :=
		[]byte("old_cert"), []byte("old_key"), []byte("old_certchain"), []byte("root")
	newCert, newCertChain, newKey := []byte("new_cert"), []byte("new_certchain"), []byte("new_key")

	testCases := map[string]struct {
		retriever   KeyCertRetriever
		certutil    util.CertUtil
		keycert     pkiutil.KeyCertBundle
		updated     bool
		expectedErr string
	}{
		"Successful update after wait": {
			retriever: &fakeKeyCertRetriever{
				NewCert:    newCert,
				CertChain:  newCertChain,
				PrivateKey: newKey,
			},
			certutil: &utilmock.FakeCertUtil{Duration: time.Duration(time.Millisecond * 300)},
			keycert: &pkimock.FakeKeyCertBundle{
				CertBytes:      oldCert,
				PrivKeyBytes:   oldKey,
				CertChainBytes: oldCertChain,
				RootCertBytes:  oldRootCert,
			},
			updated:     true,
			expectedErr: "",
		},
		"Successful update when cert empty": {
			retriever: &fakeKeyCertRetriever{
				NewCert:    newCert,
				CertChain:  newCertChain,
				PrivateKey: newKey,
			},
			certutil:    &utilmock.FakeCertUtil{Duration: time.Duration(time.Millisecond * 300)},
			keycert:     &pkimock.FakeKeyCertBundle{RootCertBytes: oldRootCert},
			updated:     true,
			expectedErr: "",
		},
		"Wait update": {
			retriever: &fakeKeyCertRetriever{
				NewCert:    newCert,
				CertChain:  newCertChain,
				PrivateKey: newKey,
			},
			certutil: &utilmock.FakeCertUtil{Duration: time.Duration(time.Hour)},
			keycert: &pkimock.FakeKeyCertBundle{
				CertBytes:      oldCert,
				PrivKeyBytes:   oldKey,
				CertChainBytes: oldCertChain,
				RootCertBytes:  oldRootCert,
			},
			updated:     false,
			expectedErr: "",
		},
		"CA Client error": {
			retriever: &fakeKeyCertRetriever{Err: fmt.Errorf("CA client error")},
			certutil:  &utilmock.FakeCertUtil{Duration: time.Duration(0)},
			keycert: &pkimock.FakeKeyCertBundle{
				CertBytes:      oldCert,
				PrivKeyBytes:   oldKey,
				CertChainBytes: oldCertChain,
				RootCertBytes:  oldRootCert,
			},
			updated:     false,
			expectedErr: "error retrieving the key and cert: CA client error, abort auto rotation",
		},
		"Cert options error": {
			retriever: &fakeKeyCertRetriever{
				NewCert:    newCert,
				CertChain:  newCertChain,
				PrivateKey: newKey,
			},
			certutil: &utilmock.FakeCertUtil{Duration: time.Duration(0)},
			keycert: &pkimock.FakeKeyCertBundle{
				CertBytes:      oldCert,
				PrivKeyBytes:   oldKey,
				CertChainBytes: oldCertChain,
				RootCertBytes:  oldRootCert,
				CertOptionsErr: fmt.Errorf("cert options error"),
			},
			updated:     false,
			expectedErr: "failed to extact CertOptions from bundle: cert options error, abort auto rotation",
		},
		"Key/cert verification error": {
			retriever: &fakeKeyCertRetriever{
				NewCert:    newCert,
				CertChain:  newCertChain,
				PrivateKey: newKey,
			},
			certutil: &utilmock.FakeCertUtil{Duration: time.Duration(0)},
			keycert: &pkimock.FakeKeyCertBundle{
				CertBytes:       oldCert,
				PrivKeyBytes:    oldKey,
				CertChainBytes:  oldCertChain,
				RootCertBytes:   oldRootCert,
				VerificationErr: fmt.Errorf("verification error"),
			},
			updated:     false,
			expectedErr: "cannot verify the retrieved key and cert: verification error, abort auto rotation",
		},
	}

	for id, tc := range testCases {
		rotator := KeyCertBundleRotator{
			certUtil:  tc.certutil,
			retriever: tc.retriever,
			keycert:   tc.keycert,
			stopCh:    make(chan bool, 1),
			stopped:   true,
		}
		errCh := make(chan error)
		go rotator.Start(errCh)

		select {
		case err := <-errCh:
			if err.Error() != tc.expectedErr {
				t.Errorf("Test case [%s]: Get error (%s) different from expected error (%s).",
					id, err.Error(), tc.expectedErr)
			}
		case <-time.After(time.Millisecond * 500):
			if len(tc.expectedErr) != 0 {
				t.Errorf("Test case [%s]: Expected error (%s) but got no error.", id, tc.expectedErr)
			}
		}
		rotator.Stop() // Stop the KeyCertBundleRotator anyway.

		certBytes, keyBytes, certchainBytes, rootcertBytes := tc.keycert.GetAllPem()
		if tc.updated {
			if !bytes.Equal(certBytes, newCert) {
				t.Errorf("Test case [%s]: Cert bytes are different from expected value: %v VS %v.",
					id, certBytes, newCert)
			}
			if !bytes.Equal(keyBytes, newKey) {
				t.Errorf("Test case [%s]: Key bytes are different from expected value: %v VS %v.",
					id, keyBytes, newKey)
			}
			if !bytes.Equal(certchainBytes, newCertChain) {
				t.Errorf("Test case [%s]: Cert chain bytes are different from expected value: %v VS %v.",
					id, certchainBytes, newCertChain)
			}
		} else {
			if !bytes.Equal(certBytes, oldCert) {
				t.Errorf("Test case [%s]: Cert bytes are different from expected value: %v VS %v.",
					id, certBytes, oldCert)
			}
			if !bytes.Equal(keyBytes, oldKey) {
				t.Errorf("Test case [%s]: Key bytes are different from expected value: %v VS %v.",
					id, keyBytes, oldKey)
			}
			if !bytes.Equal(certchainBytes, oldCertChain) {
				t.Errorf("Test case [%s]: Cert chain bytes are different from expected value: %v VS %v.",
					id, certchainBytes, oldCertChain)
			}
		}
		if !bytes.Equal(rootcertBytes, oldRootCert) {
			t.Errorf("Test case [%s]: Root cert bytes are different from expected value: %v VS %v.",
				id, rootcertBytes, oldRootCert)
		}
	}
}
