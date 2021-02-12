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

var testCerts map[string]string = map[string]string{
	"MalformedCert":      "Malformed",
	"RootCACert":         readCertFromFile(path.Join(env.IstioSrc, "samples/certs", "root-cert.pem")),
	"NonCaCert":          readCertFromFile(path.Join(env.IstioSrc, "samples/certs", "workload-bar-cert.pem")),
	"IntermediateCACert": readCertFromFile(path.Join(env.IstioSrc, "samples/certs", "ca-cert.pem")),
}

func TestCheckSameCerts(t *testing.T) {
	if checkSameCerts([]string{"a", "b"}, []string{}) {
		t.Errorf("cert check test case 1 failed")
	}
	if checkSameCerts([]string{"a", "b"}, []string{"b"}) {
		t.Errorf("cert check test case 2 failed")
	}
	if !checkSameCerts([]string{"a", "b", "c"}, []string{"a", "b", "c"}) {
		t.Errorf("cert check test case 3 failed")
	}
}

func TestVerifyTrustAnchor(t *testing.T) {
	testCases := []struct {
		errExp bool
		cert   string
	}{
		{
			cert:   testCerts["RootCACert"],
			errExp: false,
		},
		{
			cert:   testCerts["MalformedCert"],
			errExp: true,
		},
		{
			cert:   testCerts["NonCaCert"],
			errExp: true,
		},
		{
			cert:   testCerts["IntermediateCACert"],
			errExp: false,
		},
	}
	for i, tc := range testCases {
		err := NewTrustBundle(func() {}).verifyTrustAnchor(tc.cert)
		if tc.errExp && err == nil {
			t.Errorf("test case %v failed. Expected Error but got none", i)
		} else if !tc.errExp && err != nil {
			t.Errorf("test case %v failed. Expected no error but got %v", i, err)
		}
	}
}

func TestUpdateTrustAnchor(t *testing.T) {
	var cbCounter int = 0
	tb := NewTrustBundle(func() { cbCounter++ })
	var trustedCerts []string

	// Add First Cert update
	tb.UpdateTrustAnchor(&TrustAnchorUpdate{TrustAnchorConfig{Source: SourceMeshConfig, Certs: []string{testCerts["RootCACert"]}}})
	trustedCerts = tb.GetTrustBundle()
	if !checkSameCerts(trustedCerts, []string{testCerts["RootCACert"]}) || cbCounter != 1 {
		t.Errorf("Basic trustbundle update test failed. Callback value is %v", cbCounter)
	}

	// Add Second Cert update
	// ensure intermediate CA certs accepted, it replaces the first completely, and lib dedupes duplicate cert
	tb.UpdateTrustAnchor(&TrustAnchorUpdate{TrustAnchorConfig{Source: SourceMeshConfig,
		Certs: []string{testCerts["IntermediateCACert"], testCerts["IntermediateCACert"]}}})
	trustedCerts = tb.GetTrustBundle()
	if !checkSameCerts(trustedCerts, []string{testCerts["IntermediateCACert"]}) || cbCounter != 2 {
		t.Errorf("trustbundle intermediate cert update test failed. Callback value is %v", cbCounter)
	}

	// Try adding one more cert to a different source
	// Ensure both certs are not present
	tb.UpdateTrustAnchor(&TrustAnchorUpdate{TrustAnchorConfig{Source: SourceIstioCA, Certs: []string{testCerts["RootCACert"]}}})
	trustedCerts = tb.GetTrustBundle()
	sort.Strings(trustedCerts)
	result := []string{testCerts["IntermediateCACert"], testCerts["RootCACert"]}
	sort.Strings(result)
	if !checkSameCerts(trustedCerts, result) || cbCounter != 3 {
		t.Errorf("multicert update failed. Callback value is %v", cbCounter)
	}

	// Try added same cert again. Ensure cb doesn't increment
	tb.UpdateTrustAnchor(&TrustAnchorUpdate{TrustAnchorConfig{Source: SourceIstioCA, Certs: []string{testCerts["RootCACert"]}}})
	trustedCerts = tb.GetTrustBundle()
	sort.Strings(trustedCerts)
	if !checkSameCerts(trustedCerts, result) || cbCounter != 3 {
		t.Errorf("duplicate multicert update failed. Callback value is %v", cbCounter)
	}

	// Try added one good cert, one bogus Cert
	// Verify Update should not go through and no change to cb
	tb.UpdateTrustAnchor(&TrustAnchorUpdate{TrustAnchorConfig{Source: SourceIstioCA, Certs: []string{testCerts["Malformed"]}}})
	trustedCerts = tb.GetTrustBundle()
	sort.Strings(trustedCerts)
	if !checkSameCerts(trustedCerts, result) || cbCounter != 3 {
		t.Errorf("bad cert update failed. Callback value is %v", cbCounter)
	}
}
