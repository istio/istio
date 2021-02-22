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

package trustbundle

import (
	"io/ioutil"
	"path"
	"sort"
	"testing"

	"istio.io/istio/pkg/test/env"
)

func readCertFromFile(filename string) string {
	csrBytes, err := ioutil.ReadFile(filename)
	if err != nil {
		return ""
	}
	return string(csrBytes)
}

var (
	malformedCert      string = "Malformed"
	rootCACert         string = readCertFromFile(path.Join(env.IstioSrc, "samples/certs", "root-cert.pem"))
	nonCaCert          string = readCertFromFile(path.Join(env.IstioSrc, "samples/certs", "workload-bar-cert.pem"))
	intermediateCACert string = readCertFromFile(path.Join(env.IstioSrc, "samples/certs", "ca-cert.pem"))
)

func TestCheckSameCerts(t *testing.T) {
	testCases := []struct {
		certs1  []string
		certs2  []string
		expSame bool
	}{
		{
			certs1:  []string{"a", "b"},
			certs2:  []string{},
			expSame: false,
		},
		{
			certs1:  []string{"a", "b"},
			certs2:  []string{"b"},
			expSame: false,
		},
		{
			certs1:  []string{"a", "b"},
			certs2:  []string{"a", "b"},
			expSame: true,
		},
	}
	for _, tc := range testCases {
		certSame := checkSameCerts(tc.certs1, tc.certs2)
		if (certSame && !tc.expSame) || (!certSame && tc.expSame) {
			t.Errorf("cert compare testcase failed. tc: %v", tc)
		}
	}
}

func TestVerifyTrustAnchor(t *testing.T) {
	testCases := []struct {
		errExp bool
		cert   string
	}{
		{
			cert:   rootCACert,
			errExp: false,
		},
		{
			cert:   malformedCert,
			errExp: true,
		},
		{
			cert:   nonCaCert,
			errExp: true,
		},
		{
			cert:   intermediateCACert,
			errExp: false,
		},
	}
	for i, tc := range testCases {
		err := verifyTrustAnchor(tc.cert)
		if tc.errExp && err == nil {
			t.Errorf("test case %v failed. Expected Error but got none", i)
		} else if !tc.errExp && err != nil {
			t.Errorf("test case %v failed. Expected no error but got %v", i, err)
		}
	}
}

func TestUpdateTrustAnchor(t *testing.T) {
	var cbCounter int = 0
	tb := NewTrustBundle()
	tb.UpdateCb(func() { cbCounter++ })

	var trustedCerts []string
	var err error

	// Add First Cert update
	err = tb.UpdateTrustAnchor(&TrustAnchorUpdate{
		TrustAnchorConfig: TrustAnchorConfig{Certs: []string{rootCACert}},
		Source:            SourceMeshConfig,
	})
	if err != nil {
		t.Errorf("Basic trustbundle update test failed. Error: %v", err)
	}
	trustedCerts = tb.GetTrustBundle()
	if !checkSameCerts(trustedCerts, []string{rootCACert}) || cbCounter != 1 {
		t.Errorf("Basic trustbundle update test failed. Callback value is %v", cbCounter)
	}

	// Add Second Cert update
	// ensure intermediate CA certs accepted, it replaces the first completely, and lib dedupes duplicate cert
	err = tb.UpdateTrustAnchor(&TrustAnchorUpdate{
		TrustAnchorConfig: TrustAnchorConfig{Certs: []string{intermediateCACert, intermediateCACert}},
		Source:            SourceMeshConfig,
	})
	if err != nil {
		t.Errorf("trustbundle intermediate cert update test failed. Error: %v", err)
	}
	trustedCerts = tb.GetTrustBundle()
	if !checkSameCerts(trustedCerts, []string{intermediateCACert}) || cbCounter != 2 {
		t.Errorf("trustbundle intermediate cert update test failed. Callback value is %v", cbCounter)
	}

	// Try adding one more cert to a different source
	// Ensure both certs are not present
	err = tb.UpdateTrustAnchor(&TrustAnchorUpdate{
		TrustAnchorConfig: TrustAnchorConfig{Certs: []string{rootCACert}},
		Source:            SourceIstioCA,
	})
	if err != nil {
		t.Errorf("multicert update failed. Error: %v", err)
	}
	trustedCerts = tb.GetTrustBundle()
	result := []string{intermediateCACert, rootCACert}
	sort.Strings(result)
	if !checkSameCerts(trustedCerts, result) || cbCounter != 3 {
		t.Errorf("multicert update failed. Callback value is %v", cbCounter)
	}

	// Try added same cert again. Ensure cb doesn't increment
	err = tb.UpdateTrustAnchor(&TrustAnchorUpdate{
		TrustAnchorConfig: TrustAnchorConfig{Certs: []string{rootCACert}},
		Source:            SourceIstioCA,
	})
	if err != nil {
		t.Errorf("duplicate multicert update failed. Error: %v", err)
	}
	trustedCerts = tb.GetTrustBundle()
	if !checkSameCerts(trustedCerts, result) || cbCounter != 3 {
		t.Errorf("duplicate multicert update failed. Callback value is %v", cbCounter)
	}

	// Try added one good cert, one bogus Cert
	// Verify Update should not go through and no change to cb
	err = tb.UpdateTrustAnchor(&TrustAnchorUpdate{
		TrustAnchorConfig: TrustAnchorConfig{Certs: []string{malformedCert}},
		Source:            SourceIstioCA,
	})
	if err == nil {
		t.Errorf("bad cert update failed. Expected error")
	}
	trustedCerts = tb.GetTrustBundle()
	if !checkSameCerts(trustedCerts, result) || cbCounter != 3 {
		t.Errorf("bad cert update failed. Callback value is %v", cbCounter)
	}
}
