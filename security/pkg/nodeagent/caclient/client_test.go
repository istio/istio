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
	"encoding/base64"
	"fmt"
	"testing"
	"time"
)

type mockConfigMap struct {
	Cert string
	Err  error
}

func (cm *mockConfigMap) GetCATLSRootCert() (string, error) {
	if cm.Err != nil {
		return "", cm.Err
	}
	return cm.Cert, nil
}

func TestNewCAClient(t *testing.T) {
	testCases := map[string]struct {
		provider    string
		expectedErr string
	}{
		"Not supported": {
			provider:    "random",
			expectedErr: "CA provider \"random\" isn't supported. Currently Istio supports \"GoogleCA,Citadel,VaultCA\"",
		},
	}

	for id, tc := range testCases {
		_, err := NewCAClient("abc:0", tc.provider, false, nil, "", "", "", "")
		if err.Error() != tc.expectedErr {
			t.Errorf("Test case [%s]: Get error (%s) different from expected error (%s).",
				id, err.Error(), tc.expectedErr)
		}
	}
}

func TestGetCATLSRootCertFromConfigMap(t *testing.T) {
	certPem := []byte("ABCDEFG")
	encoded := base64.StdEncoding.EncodeToString(certPem)
	testCases := map[string]struct {
		cm           configMap
		expectedCert []byte
		expectedErr  string
	}{
		"Valid cert": {
			cm:           &mockConfigMap{Cert: encoded, Err: nil},
			expectedCert: certPem,
			expectedErr:  "",
		},
		"Controller error": {
			cm:           &mockConfigMap{Cert: encoded, Err: fmt.Errorf("test_error")},
			expectedCert: nil,
			expectedErr:  "exhausted all the retries (0) to fetch the CA TLS root cert",
		},
		"Decode error": {
			cm:           &mockConfigMap{Cert: "random", Err: nil},
			expectedCert: nil,
			expectedErr:  "cannot decode the CA TLS root cert: illegal base64 data at input byte 4",
		},
	}

	for id, tc := range testCases {
		cert, err := getCATLSRootCertFromConfigMap(tc.cm, time.Second, 0)
		if err != nil {
			if err.Error() != tc.expectedErr {
				t.Errorf("Test case [%s]: Get error (%s) different from expected error (%s).",
					id, err.Error(), tc.expectedErr)
			}
		} else {
			if tc.expectedErr != "" {
				t.Errorf("Test case [%s]: expect error: %s", id, tc.expectedErr)
			} else if !bytes.Equal(cert, tc.expectedCert) {
				t.Errorf("Test case [%s]: cert from ConfigMap %v does not match expected value %v", id, cert, tc.expectedCert)
			}
		}
	}
}
