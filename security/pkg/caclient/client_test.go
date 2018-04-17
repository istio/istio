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

	mockclient "istio.io/istio/security/pkg/caclient/grpc/mock"
	"istio.io/istio/security/pkg/platform"
	mockpc "istio.io/istio/security/pkg/platform/mock"
	pb "istio.io/istio/security/proto"
)

func TestRetrieveNewKeyCert(t *testing.T) {
	signedCert := []byte(`TESTCERT`)
	certChain := []byte(`CERTCHAIN`)
	testCases := map[string]struct {
		pltfmc            platform.Client
		ptclc             *mockclient.FakeCAClient
		keySize           int
		ttl               time.Duration
		maxRetries        int
		interval          time.Duration
		expectedErr       string
		expectedCert      []byte
		expectedCertChain []byte
		sendTimes         int
	}{
		"Success": {
			pltfmc: mockpc.FakeClient{nil, "", "service1", "", []byte{}, "", true},
			ptclc: &mockclient.FakeCAClient{
				0, &pb.CsrResponse{IsApproved: true, SignedCert: signedCert, CertChain: certChain}, nil},
			keySize:           512,
			ttl:               time.Hour,
			maxRetries:        0,
			interval:          time.Second,
			expectedErr:       "",
			expectedCert:      signedCert,
			expectedCertChain: certChain,
			sendTimes:         1,
		},
		"Create CSR error": {
			pltfmc: mockpc.FakeClient{nil, "", "service1", "", []byte{}, "", true},
			ptclc: &mockclient.FakeCAClient{
				0, &pb.CsrResponse{IsApproved: true, SignedCert: signedCert, CertChain: certChain}, nil},
			// 128 is too small for a RSA private key. GenCSR will return error.
			keySize:     128,
			ttl:         time.Hour,
			maxRetries:  0,
			interval:    time.Second,
			expectedErr: "CSR creation failed (crypto/rsa: message too long for RSA public key size)",
			sendTimes:   0,
		},
		"Getting platform credential error": {
			pltfmc:      mockpc.FakeClient{nil, "", "service1", "", nil, "Err1", true},
			ptclc:       &mockclient.FakeCAClient{0, nil, nil},
			keySize:     512,
			ttl:         time.Hour,
			maxRetries:  0,
			interval:    time.Second,
			expectedErr: "request creation fails on getting platform credential (Err1)",
			sendTimes:   0,
		},
		"SendCSR empty response error": {
			pltfmc:      mockpc.FakeClient{nil, "", "service1", "", []byte{}, "", true},
			ptclc:       &mockclient.FakeCAClient{0, nil, nil},
			keySize:     512,
			ttl:         time.Hour,
			maxRetries:  2,
			interval:    time.Millisecond,
			expectedErr: "CA client cannot get the CSR approved from Istio CA after max number of retries (2)",
			sendTimes:   3,
		},
		"SendCSR returns error": {
			pltfmc:      mockpc.FakeClient{nil, "", "service1", "", []byte{}, "", true},
			ptclc:       &mockclient.FakeCAClient{0, nil, fmt.Errorf("error returned from CA")},
			keySize:     512,
			ttl:         time.Hour,
			maxRetries:  1,
			interval:    time.Millisecond,
			expectedErr: "CA client cannot get the CSR approved from Istio CA after max number of retries (1)",
			sendTimes:   2,
		},
		"SendCSR not approved": {
			pltfmc:      mockpc.FakeClient{nil, "", "service1", "", []byte{}, "", true},
			ptclc:       &mockclient.FakeCAClient{0, &pb.CsrResponse{IsApproved: false}, nil},
			keySize:     512,
			ttl:         time.Hour,
			maxRetries:  1,
			interval:    time.Millisecond,
			expectedErr: "CA client cannot get the CSR approved from Istio CA after max number of retries (1)",
			sendTimes:   2,
		},
	}

	for id, c := range testCases {
		caAddr := "CA address"
		org := "Org"
		forCA := true
		client, err := NewCAClient(c.pltfmc, c.ptclc, caAddr, org, c.keySize, c.ttl, forCA, c.maxRetries, c.interval)
		if err != nil {
			t.Errorf("Test case [%s]: CA creation error: %v", id, err)
		}
		cert, certChain, _, err := client.Retrieve()
		if err == nil {
			if len(c.expectedErr) != 0 {
				t.Errorf("Test case [%s]: succeeded but expected error: %v", id, c.expectedErr)
			}
		} else {
			if err.Error() != c.expectedErr {
				t.Errorf("Test case [%s]: incorrect error message: %v VS (expected) %s", id, err, c.expectedErr)
			}
			continue
		}
		if !bytes.Equal(c.expectedCert, cert) {
			t.Errorf("Test case [%s]: cert content incorrect: %s VS (expected) %s",
				id, cert, c.expectedCert)
		}
		if !bytes.Equal(c.expectedCertChain, certChain) {
			t.Errorf("Test case [%s]: cert chain content incorrect: %s VS (expected) %s",
				id, certChain, c.expectedCertChain)
		}
		if c.ptclc.Counter != c.sendTimes {
			t.Errorf("Test case [%s]: sendCSR is called incorrect times: %d VS (expected) %d",
				id, c.ptclc.Counter, c.sendTimes)
		}
	}
}
