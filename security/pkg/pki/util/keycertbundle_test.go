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

package util

import (
	"strings"
	"testing"
	"time"
)

const (
	rootCertFile        = "../testdata/multilevelpki/root-cert.pem"
	rootKeyFile         = "../testdata/multilevelpki/root-key.pem"
	intCertFile         = "../testdata/multilevelpki/int-cert.pem"
	intKeyFile          = "../testdata/multilevelpki/int-key.pem"
	intCertChainFile    = "../testdata/multilevelpki/int-cert-chain.pem"
	int2CertFile        = "../testdata/multilevelpki/int2-cert.pem"
	int2KeyFile         = "../testdata/multilevelpki/int2-key.pem"
	int2CertChainFile   = "../testdata/multilevelpki/int2-cert-chain.pem"
	badCertFile         = "../testdata/cert-parse-fail.pem"
	badKeyFile          = "../testdata/key-parse-fail.pem"
	anotherKeyFile      = "../testdata/key.pem"
	anotherRootCertFile = "../testdata/cert.pem"
	// These key/cert contain workload key/cert, and a self-signed root cert,
	// all with TTL 100 years.
	rootCertFile1    = "../testdata/self-signed-root-cert.pem"
	certChainFile1   = "../testdata/workload-cert.pem"
	keyFile1         = "../testdata/workload-key.pem"
	ecRootCertFile   = "../testdata/ec-root-cert.pem"
	ecRootKeyFile    = "../testdata/ec-root-key.pem"
	ecClientCertFile = "../testdata/ec-workload-cert.pem"
	ecClientKeyFile  = "../testdata/ec-workload-key.pem"
)

func TestKeyCertBundleWithRootCertFromFile(t *testing.T) {
	testCases := map[string]struct {
		rootCertFile string
		expectedErr  string
	}{
		"File not found": {
			rootCertFile: "bad.pem",
			expectedErr:  "open bad.pem: no such file or directory",
		},
		"With RSA root cert": {
			rootCertFile: rootCertFile,
			expectedErr:  "",
		},
		"With EC root cert": {
			rootCertFile: rootCertFile1,
			expectedErr:  "",
		},
	}
	for id, tc := range testCases {
		bundle, err := NewKeyCertBundleWithRootCertFromFile(tc.rootCertFile)
		if err != nil {
			if tc.expectedErr == "" {
				t.Errorf("%s: Unexpected error: %v", id, err)
			} else if strings.Compare(err.Error(), tc.expectedErr) != 0 {
				t.Errorf("%s: Unexpected error: %v VS (expected) %s", id, err, tc.expectedErr)
			}
		} else if tc.expectedErr != "" {
			t.Errorf("%s: Expected error %s but succeeded", id, tc.expectedErr)
		} else if bundle == nil {
			t.Errorf("%s: the bundle should not be empty", id)
		} else {
			cert, key, chain, root := bundle.GetAllPem()
			if len(cert) != 0 {
				t.Errorf("%s: certBytes should be empty", id)
			}
			if len(key) != 0 {
				t.Errorf("%s: privateKeyBytes should be empty", id)
			}
			if len(chain) != 0 {
				t.Errorf("%s: certChainBytes should be empty", id)
			}
			if len(root) == 0 {
				t.Errorf("%s: rootCertBytes should not be empty", id)
			}

			chain = bundle.GetCertChainPem()
			if len(chain) != 0 {
				t.Errorf("%s: certChainBytes should be empty", id)
			}

			root = bundle.GetRootCertPem()
			if len(root) == 0 {
				t.Errorf("%s: rootCertBytes should not be empty", id)
			}

			x509Cert, privKey, chain, root := bundle.GetAll()
			if x509Cert != nil {
				t.Errorf("%s: cert should be nil", id)
			}
			if privKey != nil {
				t.Errorf("%s: private key should be nil", id)
			}
			if len(chain) != 0 {
				t.Errorf("%s: certChainBytes should be empty", id)
			}
			if len(root) == 0 {
				t.Errorf("%s: rootCertBytes should not be empty", id)
			}
		}
	}
}

// The test of CertOptions
func TestCertOptionsAndRetrieveID(t *testing.T) {
	testCases := map[string]struct {
		caCertFile    string
		caKeyFile     string
		certChainFile string
		rootCertFile  string
		certOptions   *CertOptions
		expectedErr   string
	}{
		"No SAN RSA": {
			caCertFile:    rootCertFile,
			caKeyFile:     rootKeyFile,
			certChainFile: "",
			rootCertFile:  rootCertFile,
			certOptions: &CertOptions{
				Host:       "test_ca.com",
				TTL:        time.Hour,
				Org:        "MyOrg",
				IsCA:       true,
				RSAKeySize: 2048,
			},
			expectedErr: "failed to extract id the SAN extension does not exist",
		},
		"RSA Success": {
			caCertFile:    certChainFile1,
			caKeyFile:     keyFile1,
			certChainFile: "",
			rootCertFile:  rootCertFile1,
			certOptions: &CertOptions{
				Host:       "watt",
				TTL:        100 * 365 * 24 * time.Hour,
				Org:        "Juju org",
				IsCA:       false,
				RSAKeySize: 2048,
			},
			expectedErr: "",
		},
		"No SAN EC": {
			caCertFile:    ecRootCertFile,
			caKeyFile:     ecRootKeyFile,
			certChainFile: "",
			rootCertFile:  ecRootCertFile,
			certOptions: &CertOptions{
				Host: "watt",
				TTL:  100 * 365 * 24 * time.Hour,
				Org:  "Juju org",
				IsCA: true,
				IsEC: true,
			},
			expectedErr: "failed to extract id the SAN extension does not exist",
		},
		"EC Success": {
			caCertFile:    ecClientCertFile,
			caKeyFile:     ecClientKeyFile,
			certChainFile: "",
			rootCertFile:  ecRootCertFile,
			certOptions: &CertOptions{
				Host: "watt",
				TTL:  365 * 24 * time.Hour,
				Org:  "Juju org",
				IsCA: false,
				IsEC: true,
			},
			expectedErr: "",
		}}
	for id, tc := range testCases {
		k, err := NewVerifiedKeyCertBundleFromFile(tc.caCertFile, tc.caKeyFile, tc.certChainFile, tc.rootCertFile)
		if err != nil {
			t.Fatalf("%s: Unexpected error: %v", id, err)
		}
		opts, err := k.CertOptions()
		if err != nil {
			if tc.expectedErr == "" {
				t.Errorf("%s: Unexpected error: %v", id, err)
			} else if strings.Compare(err.Error(), tc.expectedErr) != 0 {
				t.Errorf("%s: Unexpected error: %v VS (expected) %s", id, err, tc.expectedErr)
			}
		} else if tc.expectedErr != "" {
			t.Errorf("%s: expected error %s but have error %v", id, tc.expectedErr, err)
		} else {
			compareCertOptions(opts, tc.certOptions, t)
		}
	}
}

func compareCertOptions(actual, expected *CertOptions, t *testing.T) {
	if actual.Host != expected.Host {
		t.Errorf("host does not match, %s vs %s", actual.Host, expected.Host)
	}
	if actual.TTL != expected.TTL {
		t.Errorf("TTL does not match")
	}
	if actual.Org != expected.Org {
		t.Errorf("Org does not match")
	}
	if actual.IsCA != expected.IsCA {
		t.Errorf("IsCA does not match")
	}
	if actual.RSAKeySize != expected.RSAKeySize {
		t.Errorf("RSAKeySize does not match")
	}
}

// The test of NewVerifiedKeyCertBundleFromPem, VerifyAndSetAll can be covered by this test.
func TestNewVerifiedKeyCertBundleFromFile(t *testing.T) {
	testCases := map[string]struct {
		caCertFile    string
		caKeyFile     string
		certChainFile string
		rootCertFile  string
		expectedErr   string
	}{
		"Success - 1 level CA": {
			caCertFile:    rootCertFile,
			caKeyFile:     rootKeyFile,
			certChainFile: "",
			rootCertFile:  rootCertFile,
			expectedErr:   "",
		},
		"Success - 2 level CA": {
			caCertFile:    intCertFile,
			caKeyFile:     intKeyFile,
			certChainFile: intCertChainFile,
			rootCertFile:  rootCertFile,
			expectedErr:   "",
		},
		"Success - 3 level CA": {
			caCertFile:    int2CertFile,
			caKeyFile:     int2KeyFile,
			certChainFile: int2CertChainFile,
			rootCertFile:  rootCertFile,
			expectedErr:   "",
		},
		"Success - 2 level CA without cert chain file": {
			caCertFile:    intCertFile,
			caKeyFile:     intKeyFile,
			certChainFile: "",
			rootCertFile:  rootCertFile,
			expectedErr:   "",
		},
		"Failure - invalid cert chain file": {
			caCertFile:    intCertFile,
			caKeyFile:     intKeyFile,
			certChainFile: "bad.pem",
			rootCertFile:  rootCertFile,
			expectedErr:   "open bad.pem: no such file or directory",
		},
		"Failure - no root cert file": {
			caCertFile:    intCertFile,
			caKeyFile:     intKeyFile,
			certChainFile: "",
			rootCertFile:  "bad.pem",
			expectedErr:   "open bad.pem: no such file or directory",
		},
		"Failure - cert and key do not match": {
			caCertFile:    int2CertFile,
			caKeyFile:     anotherKeyFile,
			certChainFile: int2CertChainFile,
			rootCertFile:  rootCertFile,
			expectedErr:   "the cert does not match the key",
		},
		"Failure - 3 level CA without cert chain file": {
			caCertFile:    int2CertFile,
			caKeyFile:     int2KeyFile,
			certChainFile: "",
			rootCertFile:  rootCertFile,
			expectedErr: "cannot verify the cert with the provided root chain and " +
				"cert pool with error: x509: certificate signed by unknown authority",
		},
		"Failure - cert not verifiable from root cert": {
			caCertFile:    intCertFile,
			caKeyFile:     intKeyFile,
			certChainFile: intCertChainFile,
			rootCertFile:  anotherRootCertFile,
			expectedErr: "cannot verify the cert with the provided root chain and " +
				"cert pool with error: x509: certificate is not authorized to sign " +
				"other certificates",
		},
		"Failure - invalid cert": {
			caCertFile:    badCertFile,
			caKeyFile:     intKeyFile,
			certChainFile: "",
			rootCertFile:  rootCertFile,
			expectedErr:   "failed to parse cert PEM: invalid PEM encoded certificate",
		},
		"Failure - not existing private key": {
			caCertFile:    intCertFile,
			caKeyFile:     "bad.pem",
			certChainFile: "",
			rootCertFile:  rootCertFile,
			expectedErr:   "open bad.pem: no such file or directory",
		},
		"Failure - invalid private key": {
			caCertFile:    intCertFile,
			caKeyFile:     badKeyFile,
			certChainFile: "",
			rootCertFile:  rootCertFile,
			expectedErr:   "failed to parse private key PEM: invalid PEM-encoded key",
		},
		"Failure - file does not exist": {
			caCertFile:    "random/path/does/not/exist",
			caKeyFile:     intKeyFile,
			certChainFile: "",
			rootCertFile:  rootCertFile,
			expectedErr:   "open random/path/does/not/exist: no such file or directory",
		},
	}
	for id, tc := range testCases {
		_, err := NewVerifiedKeyCertBundleFromFile(
			tc.caCertFile, tc.caKeyFile, tc.certChainFile, tc.rootCertFile)
		if err != nil {
			if tc.expectedErr == "" {
				t.Errorf("%s: Unexpected error: %v", id, err)
			} else if strings.Compare(err.Error(), tc.expectedErr) != 0 {
				t.Errorf("%s: Unexpected error: %v VS (expected) %s", id, err, tc.expectedErr)
			}
		} else if tc.expectedErr != "" {
			t.Errorf("%s: Expected error %s but succeeded", id, tc.expectedErr)
		}
	}
}

// Test the root cert expiry timestamp can be extracted correctly.
func TestExtractRootCertExpiryTimestamp(t *testing.T) {
	cert, key, err := GenCertKeyFromOptions(CertOptions{
		Host:         "citadel.testing.istio.io",
		NotBefore:    time.Now(),
		TTL:          time.Second * 5,
		Org:          "MyOrg",
		IsCA:         true,
		IsSelfSigned: true,
		IsServer:     true,
		RSAKeySize:   2048,
	})
	if err != nil {
		t.Errorf("failed to gen cert for Citadel self signed cert %v", err)
	}
	kb, err := NewVerifiedKeyCertBundleFromPem(cert, key, nil, cert)
	if err != nil {
		t.Errorf("failed to create key cert bundle %v", err)
	}
	testCases := []struct {
		name     string
		ttlRange []float64
		sleep    int
	}{
		{
			name:     "ttl-valid",
			ttlRange: []float64{3, 5},
			sleep:    3,
		},
		{
			name:     "ttl-valid-3s-less",
			ttlRange: []float64{0, 2},
			sleep:    3,
		},
	}
	for _, tc := range testCases {
		expiryTimestamp, err := kb.ExtractRootCertExpiryTimestamp()
		if err != nil {
			t.Errorf("failed to extract root cert expiry timestamp %v", err)
		}

		sec := expiryTimestamp - float64(time.Now().Unix())
		if sec < tc.ttlRange[0] || sec > tc.ttlRange[1] {
			t.Errorf("[%v] failed, expect within range [%v, %v], got %v", tc.name, tc.ttlRange[0], tc.ttlRange[1], sec)
		}
		if tc.sleep != 0 {
			time.Sleep(time.Duration(tc.sleep) * time.Second)
		}
	}
}

// Test the CA cert expiry timestamp can be extracted correctly.
func TestExtractCACertExpiryTimestamp(t *testing.T) {
	rootCertBytes, rootKeyBytes, err := GenCertKeyFromOptions(CertOptions{
		Host:         "citadel.testing.istio.io",
		Org:          "MyOrg",
		NotBefore:    time.Now(),
		IsCA:         true,
		IsSelfSigned: true,
		TTL:          time.Hour,
		RSAKeySize:   2048,
	})
	if err != nil {
		t.Errorf("failed to gen root cert for Citadel self signed cert %v", err)
	}

	rootCert, err := ParsePemEncodedCertificate(rootCertBytes)
	if err != nil {
		t.Errorf("failed to parsing pem for root cert %v", err)
	}

	rootKey, err := ParsePemEncodedKey(rootKeyBytes)
	if err != nil {
		t.Errorf("failed to parsing pem for root key cert %v", err)
	}

	caCertBytes, caCertKeyBytes, err := GenCertKeyFromOptions(CertOptions{
		Host:         "citadel.testing.istio.io",
		Org:          "MyOrg",
		NotBefore:    time.Now(),
		TTL:          time.Second * 5,
		IsServer:     true,
		IsCA:         true,
		IsSelfSigned: false,
		RSAKeySize:   2048,
		SignerCert:   rootCert,
		SignerPriv:   rootKey,
	})
	if err != nil {
		t.Errorf("failed to gen CA cert for Citadel self signed cert %v", err)
	}

	kb, err := NewVerifiedKeyCertBundleFromPem(
		caCertBytes, caCertKeyBytes, caCertBytes, rootCertBytes)
	if err != nil {
		t.Errorf("failed to create key cert bundle %v", err)
	}

	testCases := []struct {
		name     string
		ttlRange []float64
		sleep    int
	}{
		{
			name:     "ttl-valid",
			ttlRange: []float64{3, 5},
			sleep:    3,
		},
		{
			name:     "ttl-valid-3s-less",
			ttlRange: []float64{0, 2},
			sleep:    3,
		},
	}
	for _, tc := range testCases {
		expiryTimestamp, err := kb.ExtractCACertExpiryTimestamp()
		if err != nil {
			t.Errorf("failed to extract CA cert expiry timestamp %v", err)
		}

		sec := expiryTimestamp - float64(time.Now().Unix())
		if sec < tc.ttlRange[0] || sec > tc.ttlRange[1] {
			t.Errorf("[%v] failed, expect within range [%v, %v], got %v", tc.name, tc.ttlRange[0], tc.ttlRange[1], sec)
		}

		if tc.sleep != 0 {
			time.Sleep(time.Duration(tc.sleep) * time.Second)
		}
	}
}
