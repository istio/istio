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

package util

import (
	"fmt"
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
	crlRootCertFile  = "../testdata/crl/root-cert.pem"
	crlCertFile      = "../testdata/crl/ca-cert.pem"
	crlKeyFile       = "../testdata/crl/ca-key.pem"
	crlCertChainFile = "../testdata/crl/cert-chain.pem"
	crlFile          = "../testdata/crl/ca-crl.pem"
	badCrlFile       = "../testdata/crl/bad-ca-crl.pem"
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
		certChainFile []string
		rootCertFile  string
		certOptions   *CertOptions
		expectedErr   string
	}{
		"No SAN RSA": {
			caCertFile:    rootCertFile,
			caKeyFile:     rootKeyFile,
			certChainFile: nil,
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
			certChainFile: nil,
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
			certChainFile: nil,
			rootCertFile:  ecRootCertFile,
			certOptions: &CertOptions{
				Host:     "watt",
				TTL:      100 * 365 * 24 * time.Hour,
				Org:      "Juju org",
				IsCA:     true,
				ECSigAlg: EcdsaSigAlg,
			},
			expectedErr: "failed to extract id the SAN extension does not exist",
		},
		"EC Success": {
			caCertFile:    ecClientCertFile,
			caKeyFile:     ecClientKeyFile,
			certChainFile: nil,
			rootCertFile:  ecRootCertFile,
			certOptions: &CertOptions{
				Host:     "watt",
				TTL:      10 * 365 * 24 * time.Hour,
				Org:      "Juju org",
				IsCA:     false,
				ECSigAlg: EcdsaSigAlg,
			},
			expectedErr: "",
		},
	}
	for id, tc := range testCases {
		k, err := NewVerifiedKeyCertBundleFromFile(tc.caCertFile, tc.caKeyFile, tc.certChainFile, tc.rootCertFile, "")
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
		certChainFile []string
		rootCertFile  string
		expectedErr   string
	}{
		"Success - 1 level CA": {
			caCertFile:    rootCertFile,
			caKeyFile:     rootKeyFile,
			certChainFile: nil,
			rootCertFile:  rootCertFile,
			expectedErr:   "",
		},
		"Success - 2 level CA": {
			caCertFile:    intCertFile,
			caKeyFile:     intKeyFile,
			certChainFile: []string{intCertChainFile},
			rootCertFile:  rootCertFile,
			expectedErr:   "",
		},
		"Success - 3 level CA": {
			caCertFile:    int2CertFile,
			caKeyFile:     int2KeyFile,
			certChainFile: []string{int2CertChainFile},
			rootCertFile:  rootCertFile,
			expectedErr:   "",
		},
		"Success - 2 level CA without cert chain file": {
			caCertFile:    intCertFile,
			caKeyFile:     intKeyFile,
			certChainFile: nil,
			rootCertFile:  rootCertFile,
			expectedErr:   "",
		},
		"Failure - invalid cert chain file": {
			caCertFile:    intCertFile,
			caKeyFile:     intKeyFile,
			certChainFile: []string{"bad.pem"},
			rootCertFile:  rootCertFile,
			expectedErr:   "open bad.pem: no such file or directory",
		},
		"Failure - no root cert file": {
			caCertFile:    intCertFile,
			caKeyFile:     intKeyFile,
			certChainFile: nil,
			rootCertFile:  "bad.pem",
			expectedErr:   "open bad.pem: no such file or directory",
		},
		"Failure - cert and key do not match": {
			caCertFile:    int2CertFile,
			caKeyFile:     anotherKeyFile,
			certChainFile: []string{int2CertChainFile},
			rootCertFile:  rootCertFile,
			expectedErr:   "the cert does not match the key",
		},
		"Failure - 3 level CA without cert chain file": {
			caCertFile:    int2CertFile,
			caKeyFile:     int2KeyFile,
			certChainFile: nil,
			rootCertFile:  rootCertFile,
			expectedErr: "cannot verify the cert with the provided root chain and " +
				"cert pool with error: x509: certificate signed by unknown authority",
		},
		"Failure - cert not verifiable from root cert": {
			caCertFile:    intCertFile,
			caKeyFile:     intKeyFile,
			certChainFile: []string{intCertChainFile},
			rootCertFile:  anotherRootCertFile,
			expectedErr: "cannot verify the cert with the provided root chain and " +
				"cert pool with error: x509: certificate is not authorized to sign " +
				"other certificates",
		},
		"Failure - invalid cert": {
			caCertFile:    badCertFile,
			caKeyFile:     intKeyFile,
			certChainFile: nil,
			rootCertFile:  rootCertFile,
			expectedErr:   "failed to parse cert PEM: invalid PEM encoded certificate",
		},
		"Failure - not existing private key": {
			caCertFile:    intCertFile,
			caKeyFile:     "bad.pem",
			certChainFile: nil,
			rootCertFile:  rootCertFile,
			expectedErr:   "open bad.pem: no such file or directory",
		},
		"Failure - invalid private key": {
			caCertFile:    intCertFile,
			caKeyFile:     badKeyFile,
			certChainFile: nil,
			rootCertFile:  rootCertFile,
			expectedErr:   "failed to parse private key PEM: invalid PEM-encoded key",
		},
		"Failure - file does not exist": {
			caCertFile:    "random/path/does/not/exist",
			caKeyFile:     intKeyFile,
			certChainFile: nil,
			rootCertFile:  rootCertFile,
			expectedErr:   "open random/path/does/not/exist: no such file or directory",
		},
	}
	for id, tc := range testCases {
		_, err := NewVerifiedKeyCertBundleFromFile(
			tc.caCertFile, tc.caKeyFile, tc.certChainFile, tc.rootCertFile, "")
		if err != nil {
			if tc.expectedErr == "" {
				t.Errorf("%s: Unexpected error: %v", id, err)
			} else if !strings.HasPrefix(err.Error(), tc.expectedErr) {
				t.Errorf("%s: Unexpected error: %v VS (expected) %s", id, err, tc.expectedErr)
			}
		} else if tc.expectedErr != "" {
			t.Errorf("%s: Expected error %s but succeeded", id, tc.expectedErr)
		}
	}
}

// The test of NewVerifiedKeyCertBundleFromPem, VerifyAndSetAll can be covered by this test.
func TestNewVerifiedKeyCertBundleFromFileWithCrl(t *testing.T) {
	testCases := map[string]struct {
		caCertFile    string
		caKeyFile     string
		certChainFile []string
		rootCertFile  string
		crlFile       string
		expectedErr   string
	}{
		"valid crl": {
			caCertFile:    crlCertFile,
			caKeyFile:     crlKeyFile,
			certChainFile: []string{crlCertChainFile},
			rootCertFile:  crlRootCertFile,
			crlFile:       crlFile,
			expectedErr:   "",
		},
		"invalid crl": {
			caCertFile:    crlCertFile,
			caKeyFile:     crlKeyFile,
			certChainFile: []string{crlCertChainFile},
			rootCertFile:  crlRootCertFile,
			crlFile:       badCrlFile,
			expectedErr:   "missing CRL signed by certificates: [CN=Intermediate CA CN=Root CA]",
		},
	}
	for id, tc := range testCases {
		t.Run(id, func(t *testing.T) {
			_, err := NewVerifiedKeyCertBundleFromFile(
				tc.caCertFile, tc.caKeyFile, tc.certChainFile, tc.rootCertFile, tc.crlFile)
			if err != nil {
				if tc.expectedErr == "" {
					t.Errorf("%s: Unexpected error: %v", id, err)
				} else if !strings.HasPrefix(err.Error(), tc.expectedErr) {
					t.Errorf("%s: Unexpected error: %v VS (expected) %s", id, err, tc.expectedErr)
				}
			} else if tc.expectedErr != "" {
				t.Errorf("%s: Expected error %s but succeeded", id, tc.expectedErr)
			}
		})
	}
}

// Test the root cert expiry timestamp can be extracted correctly.
func TestExtractRootCertExpiryTimestamp(t *testing.T) {
	testCases := map[string]struct {
		notBefore time.Time
		ttl       time.Duration
	}{
		"Success - Unix 0": {
			notBefore: time.Unix(0, 0),
			ttl:       time.Minute,
		},
		"Success - Arbitrary Date 1": {
			notBefore: time.Unix(1721769413, 1000),
			ttl:       time.Minute * 70,
		},
		"Success - Arbitrary Date 2": {
			notBefore: time.Unix(2721769413, 1000),
			ttl:       time.Minute * 10,
		},
		"Success - 32-bit max": {
			notBefore: time.Unix((1<<31)-2, 1000),
			ttl:       time.Minute * 10,
		},
		"Success - Generalized time max": {
			// 253402300799 corresponds to December 31, 9999 23:58:59, the latest date that can be represented in Generalized Time.
			notBefore: time.Unix(253402300799, 0).Add(-time.Minute),
			ttl:       time.Minute,
		},
	}

	for id, tc := range testCases {
		t.Run(fmt.Sprintf("Test case %s", id), func(t *testing.T) {
			cert, key, err := GenCertKeyFromOptions(CertOptions{
				Host:         "citadel.testing.istio.io",
				NotBefore:    tc.notBefore,
				TTL:          tc.ttl,
				Org:          "MyOrg",
				IsCA:         true,
				IsSelfSigned: true,
				IsServer:     true,
				RSAKeySize:   2048,
			})
			if err != nil {
				t.Errorf("failed to gen cert for Citadel self signed cert %v", err)
			}
			// Don't use NewVerifiedKeyCertBundleFromPem because we want to test expired key bundles too
			kb := NewKeyCertBundleFromPem(cert, key, nil, cert, nil)
			// will return error if expired
			expiryTimestamp, err := kb.ExtractRootCertExpiryTimestamp()
			expectedExpiryTimestamp := tc.notBefore.Add(tc.ttl)
			// One second toleration because x509 cert times have one second of precision
			tol := time.Second
			if err != nil {
				t.Errorf("Did not expect error, got %v", err)
				t.FailNow()
			}
			if expiryTimestamp.Sub(expectedExpiryTimestamp).Abs() > tol {
				t.Errorf("Expected %d and %d to be almost equal", expiryTimestamp.Unix(), expectedExpiryTimestamp.Unix())
			}
		})
	}

	t.Run("Failure - Malformed Cert", func(t *testing.T) {
		errorMessage := "failed to parse the root cert: invalid PEM encoded certificate"
		garbage := []byte{0, 0, 0, 0}
		kb := NewKeyCertBundleFromPem(garbage, garbage, nil, garbage, nil)
		// will return error if expired
		_, err := kb.ExtractRootCertExpiryTimestamp()
		if err == nil {
			t.Errorf("Expected error parsing malformed cert")
		} else if err.Error() != errorMessage {
			t.Errorf("Expected error %s, but got %s", errorMessage, err.Error())
		}
	})
}

// Test the CA cert expiry timestamp can be extracted correctly.
func TestExtractCACertExpiryTimestamp(t *testing.T) {
	testCases := map[string]struct {
		notBefore time.Time
		ttl       time.Duration
	}{
		"Success - Unix 0": {
			notBefore: time.Unix(0, 0),
			ttl:       time.Minute,
		},
		"Success - Arbitrary Date 1": {
			notBefore: time.Unix(1721769413, 1000),
			ttl:       time.Minute * 70,
		},
		"Success - Arbitrary Date 2": {
			notBefore: time.Unix(2721769413, 1000),
			ttl:       time.Minute * 10,
		},
		"Success - 32-bit max": {
			notBefore: time.Unix((1<<31)-2, 1000),
			ttl:       time.Minute * 10,
		},
		"Success - Generalized time max": {
			// 253402300799 corresponds to December 31, 9999 23:58:59, the latest date that can be represented in Generalized Time.
			notBefore: time.Unix(253402300799, 0).Add(-time.Minute),
			ttl:       time.Minute,
		},
	}
	for id, tc := range testCases {
		t.Run(fmt.Sprintf("Test case %s", id), func(t *testing.T) {
			rootCertBytes, rootKeyBytes, err := GenCertKeyFromOptions(CertOptions{
				Host:         "citadel.testing.istio.io",
				Org:          "MyOrg",
				NotBefore:    tc.notBefore,
				IsCA:         true,
				IsSelfSigned: true,
				TTL:          tc.ttl,
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
				NotBefore:    tc.notBefore,
				TTL:          tc.ttl,
				IsServer:     true,
				IsCA:         true,
				IsSelfSigned: false,
				RSAKeySize:   2048,
				SignerCert:   rootCert,
				SignerPriv:   rootKey,
			})
			if err != nil {
				t.Fatalf("failed to gen CA cert for Citadel self signed cert %v", err)
			}

			kb := NewKeyCertBundleFromPem(
				caCertBytes, caCertKeyBytes, caCertBytes, rootCertBytes, nil,
			)

			expiryTimestamp, _ := kb.ExtractCACertExpiryTimestamp()
			// Ignore error; it just indicates cert is expired
			expectedExpiryTimestamp := tc.notBefore.Add(tc.ttl)
			// One second toleration because x509 cert times have one second of precision
			tol := time.Second
			if err != nil {
				t.Errorf("Did not expect error, got %v", err)
				t.FailNow()
			}
			if expiryTimestamp.Sub(expectedExpiryTimestamp).Abs() > tol {
				t.Errorf("Expected %d and %d to be almost equal", expiryTimestamp.Unix(), expectedExpiryTimestamp.Unix())
			}
		})
	}
	t.Run("Failure - Malformed Cert", func(t *testing.T) {
		errorMessage := "failed to parse the CA cert: invalid PEM encoded certificate"
		garbage := []byte{0, 0, 0, 0}
		kb := NewKeyCertBundleFromPem(garbage, garbage, nil, garbage, nil)
		// will return error if expired
		_, err := kb.ExtractCACertExpiryTimestamp()
		if err == nil {
			t.Errorf("Expected error parsing malformed cert")
		} else if err.Error() != errorMessage {
			t.Errorf("Expected error %s, but got %s", errorMessage, err.Error())
		}
	})
}

func TestTimeBeforeCertExpires(t *testing.T) {
	t0 := time.Now()
	certTTL := time.Second * 60
	rootCertBytes, _, err := GenCertKeyFromOptions(CertOptions{
		Host:         "citadel.testing.istio.io",
		Org:          "MyOrg",
		NotBefore:    t0,
		IsCA:         true,
		IsSelfSigned: true,
		TTL:          certTTL,
		RSAKeySize:   2048,
	})
	if err != nil {
		t.Errorf("failed to gen root cert for Citadel self signed cert %v", err)
	}

	testCases := []struct {
		name         string
		cert         []byte
		expectedTime time.Duration
		timeNow      time.Time
		expectedErr  error
	}{
		{
			name:         "TTL left should be equal to cert TTL",
			cert:         rootCertBytes,
			timeNow:      t0,
			expectedTime: certTTL,
		},
		{
			name:         "TTL left should be ca cert ttl minus 5 seconds",
			cert:         rootCertBytes,
			timeNow:      t0.Add(5 * time.Second),
			expectedTime: 55 * time.Second,
		},
		{
			name:         "TTL left should be negative because already got expired",
			cert:         rootCertBytes,
			timeNow:      t0.Add(120 * time.Second),
			expectedTime: -60 * time.Second,
		},
		{
			name:        "no cert, so it should return an error",
			cert:        nil,
			timeNow:     t0,
			expectedErr: fmt.Errorf("no certificate found"),
		},
		{
			name:        "invalid cert",
			cert:        []byte("invalid cert"),
			timeNow:     t0,
			expectedErr: fmt.Errorf("failed to extract cert expiration timestamp: failed to parse the cert: invalid PEM encoded certificate"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			expiryDuration, err := TimeBeforeCertExpires(tc.cert, tc.timeNow)
			if err != nil {
				if tc.expectedErr == nil {
					t.Fatalf("Unexpected error: %v", err)
				} else if strings.Compare(err.Error(), tc.expectedErr.Error()) != 0 {
					t.Errorf("expected error: %v got %v", err, tc.expectedErr)
				}
				return
			}

			// One second toleration because x509 cert times have one second of precision
			tol := time.Second
			if (expiryDuration - tc.expectedTime).Abs() > tol {
				t.Fatalf("expected time %v to be close to %v", tc.expectedTime, expiryDuration)
			}
		})
	}
}
