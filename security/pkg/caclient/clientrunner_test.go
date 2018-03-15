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

	caclientmock "istio.io/istio/security/pkg/caclient/mock"
	pkiutil "istio.io/istio/security/pkg/pki/util"
	pkimock "istio.io/istio/security/pkg/pki/util/mock"
	"istio.io/istio/security/pkg/util"
	utilmock "istio.io/istio/security/pkg/util/mock"
)

func TestClientRunner(t *testing.T) {
	oldCert, oldKey, oldCertChain, oldRootCert :=
		[]byte("old_cert"), []byte("old_key"), []byte("old_certchain"), []byte("root")
	newCert, newCertChain, newKey := []byte("new_cert"), []byte("new_certchain"), []byte("new_key")

	testCases := map[string]struct {
		client      CAClient
		certutil    util.CertUtil
		bundle      pkiutil.KeyCertBundle
		updated     bool
		expectedErr string
	}{
		"Successful update after wait": {
			client: &caclientmock.FakeClient{
				NewCert:    newCert,
				CertChain:  newCertChain,
				PrivateKey: newKey,
			},
			certutil: &utilmock.FakeCertUtil{Duration: time.Duration(time.Millisecond * 300)},
			bundle: &pkimock.FakeKeyCertBundle{
				CertBytes:      oldCert,
				PrivKeyBytes:   oldKey,
				CertChainBytes: oldCertChain,
				RootCertBytes:  oldRootCert,
			},
			updated:     true,
			expectedErr: "",
		},
		"Successful update when cert empty": {
			client: &caclientmock.FakeClient{
				NewCert:    newCert,
				CertChain:  newCertChain,
				PrivateKey: newKey,
			},
			certutil:    &utilmock.FakeCertUtil{Duration: time.Duration(time.Millisecond * 300)},
			bundle:      &pkimock.FakeKeyCertBundle{RootCertBytes: oldRootCert},
			updated:     true,
			expectedErr: "",
		},
		"Wait update": {
			client: &caclientmock.FakeClient{
				NewCert:    newCert,
				CertChain:  newCertChain,
				PrivateKey: newKey,
			},
			certutil: &utilmock.FakeCertUtil{Duration: time.Duration(time.Hour)},
			bundle: &pkimock.FakeKeyCertBundle{
				CertBytes:      oldCert,
				PrivKeyBytes:   oldKey,
				CertChainBytes: oldCertChain,
				RootCertBytes:  oldRootCert,
			},
			updated:     false,
			expectedErr: "",
		},
		"Client error": {
			client:   &caclientmock.FakeClient{Err: fmt.Errorf("Error1")},
			certutil: &utilmock.FakeCertUtil{Duration: time.Duration(0)},
			bundle: &pkimock.FakeKeyCertBundle{
				CertBytes:      oldCert,
				PrivKeyBytes:   oldKey,
				CertChainBytes: oldCertChain,
				RootCertBytes:  oldRootCert,
			},
			updated:     false,
			expectedErr: "error retrieving the key and cert: Error1, abort auto rotation",
		},
		"Key/cert verification error": {
			client: &caclientmock.FakeClient{
				NewCert:    newCert,
				CertChain:  newCertChain,
				PrivateKey: newKey,
			},
			certutil: &utilmock.FakeCertUtil{Duration: time.Duration(0)},
			bundle: &pkimock.FakeKeyCertBundle{
				CertBytes:      oldCert,
				PrivKeyBytes:   oldKey,
				CertChainBytes: oldCertChain,
				RootCertBytes:  oldRootCert,
				ReturnErr:      fmt.Errorf("Error2"),
			},
			updated:     false,
			expectedErr: "cannot verify the retrieved key and cert: Error2, abort auto rotation",
		},
	}

	for id, tc := range testCases {
		clientRunner := NewClientRunner(tc.bundle, tc.certutil, tc.client)
		stopCh := make(chan struct{})
		errCh := make(chan error)
		go clientRunner.Run(stopCh, errCh)

		select {
		case err := <-errCh:
			if err.Error() != tc.expectedErr {
				t.Errorf("Test case [%s]: Get error (%s) different from expected error (%s).",
					id, err.Error(), tc.expectedErr)
			}
			// The ClientRunner exists when an error happens.
		case <-time.After(time.Millisecond * 500):
			if len(tc.expectedErr) != 0 {
				t.Errorf("Test case [%s]: Expected error (%s) but got no error.", id, tc.expectedErr)
			}
			stopCh <- struct{}{} // Terminate the ClientRunner.
		}

		certBytes, keyBytes, certchainBytes, rootcertBytes := tc.bundle.GetAllPem()
		if tc.updated {
			if bytes.Compare(certBytes, newCert) != 0 {
				t.Errorf("Test case [%s]: Cert bytes are different from expected value: %v VS %v.",
					id, certBytes, newCert)
			}
			if bytes.Compare(keyBytes, newKey) != 0 {
				t.Errorf("Test case [%s]: Key bytes are different from expected value: %v VS %v.",
					id, keyBytes, newKey)
			}
			if bytes.Compare(certchainBytes, newCertChain) != 0 {
				t.Errorf("Test case [%s]: Cert chain bytes are different from expected value: %v VS %v.",
					id, certchainBytes, newCertChain)
			}
		} else {
			if bytes.Compare(certBytes, oldCert) != 0 {
				t.Errorf("Test case [%s]: Cert bytes are different from expected value: %v VS %v.",
					id, certBytes, oldCert)
			}
			if bytes.Compare(keyBytes, oldKey) != 0 {
				t.Errorf("Test case [%s]: Key bytes are different from expected value: %v VS %v.",
					id, keyBytes, oldKey)
			}
			if bytes.Compare(certchainBytes, oldCertChain) != 0 {
				t.Errorf("Test case [%s]: Cert chain bytes are different from expected value: %v VS %v.",
					id, certchainBytes, oldCertChain)
			}
		}
		if bytes.Compare(rootcertBytes, oldRootCert) != 0 {
			t.Errorf("Test case [%s]: Root cert bytes are different from expected value: %v VS %v.",
				id, rootcertBytes, oldRootCert)
		}
	}
}
