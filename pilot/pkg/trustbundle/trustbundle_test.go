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
	"crypto/x509"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"sort"
	"testing"
	"time"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/util/retry"
)

func readCertFromFile(filename string) string {
	csrBytes, err := os.ReadFile(filename)
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

	// borrowed from the spiffe package, spiffe_test.go
	validSpiffeX509Bundle = `
{
	"spiffe_sequence": 1,
	"spiffe_refresh_hint": 450000,
	"keys": [
		{
		"kty": "RSA",
		"use": "x509-svid",
		"n": "r10W2IcjT-vvSTpaFsS4OAcPOX87kw-zKZuJgXhxDhkOQyBdPZpUfK4H8yZ2q14Laym4bmiMLocIeGP70k` +
		`UXcp9T4SP-P0DmBTPx3hVgP3YteHzaKsja056VtDs9kAufmFGemTSCenMt7aSlryUbLRO0H-__fTeNkCXR7uIoq` +
		`RfU6jL0nN4EBh02q724iGuX6dpJcQam5bEJjq6Kn4Ry4qn1xHXqQXM4o2f6xDT13sp4U32stpmKh0HOd1WWKr0W` +
		`RYnAh4GnToKr21QySZi9QWTea3zqeFmti-Isji1dKZkgZA2S89BdTWSLe6S_9lV0mtdXvDaT8RmaIX72jE_Abhn` +
		`bUYV84pNYv-T2LtIKoi5PjWk0raaYoexAjtCWiu3PnizxjYOnNwpzgQN9Qh_rY2jv74cgzG50_Ft1B7XUiakNFx` +
		`AiD1k6pNuiu4toY0Es7qt1yeqaC2zcIuuV7HUv1AbFBkIdF5quJHVtZ5AE1MCh1ipLPq-lIjmFdQKSRdbssVw8y` +
		`q9FtFVyVqTz9GnQtoctCIPGQqmJDWmt8E7gjFhweUQo-fGgGuTlZRl9fiPQ6luPyGQ1WL6wH79G9eu4UtmgUDNw` +
		`q7kpYq0_NQ5vw_1WQSY3LsPclfKzkZ-Lw2RVef-SFVVvUFMcd_3ALeeEnnSe4GSY-7vduPUAE5qMH7M",
		"e": "AQAB",
		"x5c": ["MIIGlDCCBHygAwIBAgIQEW25APa7S9Sj/Nj6V6GxQTANBgkqhkiG9w0BAQsFADCBwTELMAkGA1UEBhM` +
		`CVVMxEzARBgNVBAgTCkNhbGlmb3JuaWExFjAUBgNVBAcTDU1vdW50YWluIFZpZXcxEzARBgNVBAoTCkdvb2dsZS` +
		`BMTEMxDjAMBgNVBAsTBUNsb3VkMWAwXgYDVQQDDFdpc3Rpb192MV9jbG91ZF93b3JrbG9hZF9yb290LXNpZ25lc` +
		`i0wLTIwMTgtMDQtMjVUMTQ6MTE6MzMtMDc6MDAgSzoxLCAxOkg1MnZnd0VtM3RjOjA6MTgwIBcNMTgwNDI1MjEx` +
		`MTMzWhgPMjExODA0MjUyMjExMzNaMIHBMQswCQYDVQQGEwJVUzETMBEGA1UECBMKQ2FsaWZvcm5pYTEWMBQGA1U` +
		`EBxMNTW91bnRhaW4gVmlldzETMBEGA1UEChMKR29vZ2xlIExMQzEOMAwGA1UECxMFQ2xvdWQxYDBeBgNVBAMMV2` +
		`lzdGlvX3YxX2Nsb3VkX3dvcmtsb2FkX3Jvb3Qtc2lnbmVyLTAtMjAxOC0wNC0yNVQxNDoxMTozMy0wNzowMCBLO` +
		`jEsIDE6SDUydmd3RW0zdGM6MDoxODCCAiIwDQYJKoZIhvcNAQEBBQADggIPADCCAgoCggIBAK9dFtiHI0/r70k6` +
		`WhbEuDgHDzl/O5MPsymbiYF4cQ4ZDkMgXT2aVHyuB/MmdqteC2spuG5ojC6HCHhj+9JFF3KfU+Ej/j9A5gUz8d4` +
		`VYD92LXh82irI2tOelbQ7PZALn5hRnpk0gnpzLe2kpa8lGy0TtB/v/303jZAl0e7iKKkX1Ooy9JzeBAYdNqu9uI` +
		`hrl+naSXEGpuWxCY6uip+EcuKp9cR16kFzOKNn+sQ09d7KeFN9rLaZiodBzndVliq9FkWJwIeBp06Cq9tUMkmYv` +
		`UFk3mt86nhZrYviLI4tXSmZIGQNkvPQXU1ki3ukv/ZVdJrXV7w2k/EZmiF+9oxPwG4Z21GFfOKTWL/k9i7SCqIu` +
		`T41pNK2mmKHsQI7Qlortz54s8Y2DpzcKc4EDfUIf62No7++HIMxudPxbdQe11ImpDRcQIg9ZOqTboruLaGNBLO6` +
		`rdcnqmgts3CLrlex1L9QGxQZCHReariR1bWeQBNTAodYqSz6vpSI5hXUCkkXW7LFcPMqvRbRVclak8/Rp0LaHLQ` +
		`iDxkKpiQ1prfBO4IxYcHlEKPnxoBrk5WUZfX4j0Opbj8hkNVi+sB+/RvXruFLZoFAzcKu5KWKtPzUOb8P9VkEmN` +
		`y7D3JXys5Gfi8NkVXn/khVVb1BTHHf9wC3nhJ50nuBkmPu73bj1ABOajB+zAgMBAAGjgYMwgYAwDgYDVR0PAQH/` +
		`BAQDAgEGMB0GA1UdJQQWMBQGCCsGAQUFBwMBBggrBgEFBQcDAjAPBgNVHRMBAf8EBTADAQH/MB0GA1UdDgQWBBQ` +
		`/VsuyjgRDAEmcZjyJ77619Js9ijAfBgNVHSMEGDAWgBQ/VsuyjgRDAEmcZjyJ77619Js9ijANBgkqhkiG9w0BAQ` +
		`sFAAOCAgEAUc5QJOqxmMJY0E2rcHEWQYRah1vat3wuIHtEZ3SkSumyj+y9eyIHb9XTTyc4SyGyX1n8Rary8oSgQ` +
		`V4cbyJTFXEEQOGLHB9/98EKThgJtfPsos2WKe/59S8yN05onpxcaL9y4S295Kv9kcSQxLm5UfjlqsKeHJZymvxi` +
		`YzmBox7LA1zqcLYZvslJNkJxKAk5JA66iyDSQqOK7jIixn8pi305dFGCZglUFStwWqY6Rc9rR8EycVhSx2AhrvT` +
		`7OQTVdKLfoKA84D8JZJPB7hrxqKf7JJFs87Kjt7c/5bXPFJ2osmjoNYnbHjiq64bh20sSCd630qvhhePLwjjOlB` +
		`PiFyK36o/hQN871AEm1SCHy+aQcfJqF5KTgPnZQy5D+D/CGau+BfkO+WCGDVxRleYBJ4g2NbATolygB2KWXrj07` +
		`U/WaWqV2hERbkmxXFh6cUdlkX2MeoG4v6ZD2OKAPx5DpJCfp0TEq6PznP+Z1mLd/ZjGsOF8R2WGQJEuU8HRzvsr` +
		`0wsX9UyLMqf5XViDK11V/W+dcIvjHCayBpX2se3dfex5jFht+JcQc+iwB8caSXkR6tGSiargEtSJODORacO9IB8` +
		`b6W8Sm//JWf/8zyiCcMm1i2yVVphwE1kczFwunAh0JB896VaXGVxXeKEAMQoXHjgDdCYp8/Etxjb8UkCmyjU="]
		}
	]
}`
)

func TestIsEqSpliceStr(t *testing.T) {
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
		certSame := isEqSliceStr(tc.certs1, tc.certs2)
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
	cbCounter := 0
	tb := NewTrustBundle(nil)
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
	if !isEqSliceStr(trustedCerts, []string{rootCACert}) || cbCounter != 1 {
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
	if !isEqSliceStr(trustedCerts, []string{intermediateCACert}) || cbCounter != 2 {
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
	if !isEqSliceStr(trustedCerts, result) || cbCounter != 3 {
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
	if !isEqSliceStr(trustedCerts, result) || cbCounter != 3 {
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
	if !isEqSliceStr(trustedCerts, result) || cbCounter != 3 {
		t.Errorf("bad cert update failed. Callback value is %v", cbCounter)
	}

	// Finally, remove all certs and ensure trustBundle is clean
	err = tb.UpdateTrustAnchor(&TrustAnchorUpdate{
		TrustAnchorConfig: TrustAnchorConfig{Certs: []string{}},
		Source:            SourceIstioCA,
	})
	if err != nil {
		t.Errorf("clear cert update failed. Error: %v", err)
	}
	err = tb.UpdateTrustAnchor(&TrustAnchorUpdate{
		TrustAnchorConfig: TrustAnchorConfig{Certs: []string{}},
		Source:            SourceMeshConfig,
	})
	if err != nil {
		t.Errorf("clear cert update failed. Error: %v", err)
	}
	trustedCerts = tb.GetTrustBundle()
	if !isEqSliceStr(trustedCerts, []string{}) || cbCounter != 5 {
		t.Errorf("cert removal update failed. Callback value is %v", cbCounter)
	}
}

func expectTbCount(t *testing.T, tb *TrustBundle, expAnchorCount int, ti time.Duration, strPrefix string) {
	t.Helper()
	retry.UntilSuccessOrFail(t, func() error {
		certs := tb.GetTrustBundle()
		if len(certs) != expAnchorCount {
			return fmt.Errorf("%s. Got %v, expected %v", strPrefix, len(certs), expAnchorCount)
		}
		return nil
	}, retry.Timeout(ti))
}

func TestAddMeshConfigUpdate(t *testing.T) {
	caCertPool, err := x509.SystemCertPool()
	if err != nil {
		t.Fatalf("failed to get SystemCertPool: %v", err)
	}
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })

	// Mock response from TLS Spiffe Server
	validHandler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(validSpiffeX509Bundle))
	})

	server1 := httptest.NewTLSServer(validHandler)
	caCertPool.AddCert(server1.Certificate())
	defer server1.Close()

	server2 := httptest.NewTLSServer(validHandler)
	caCertPool.AddCert(server2.Certificate())
	defer server2.Close()

	tb := NewTrustBundle(caCertPool)

	// Change global remote timeout interval for the duration of the unit test
	remoteTimeout = 30 * time.Millisecond

	// Test1: Ensure that MeshConfig PEM certs are updated correctly
	tb.AddMeshConfigUpdate(&meshconfig.MeshConfig{CaCertificates: []*meshconfig.MeshConfig_CertificateData{
		{CertificateData: &meshconfig.MeshConfig_CertificateData_Pem{Pem: rootCACert}},
	}})
	expectTbCount(t, tb, 1, 1*time.Second, "meshConfig pem trustAnchor not updated in bundle")

	// Test2: Append server1 as spiffe endpoint to existing MeshConfig

	// Start processing remote anchor update with poll frequency.
	go tb.ProcessRemoteTrustAnchors(stop, 200*time.Millisecond)
	tb.AddMeshConfigUpdate(&meshconfig.MeshConfig{CaCertificates: []*meshconfig.MeshConfig_CertificateData{
		{CertificateData: &meshconfig.MeshConfig_CertificateData_SpiffeBundleUrl{SpiffeBundleUrl: server1.Listener.Addr().String()}},
		{CertificateData: &meshconfig.MeshConfig_CertificateData_Pem{Pem: rootCACert}},
	}})
	if !isEqSliceStr(tb.endpoints, []string{server1.Listener.Addr().String()}) {
		t.Errorf("server1 endpoint not correctly updated in trustbundle. Trustbundle endpoints: %v", tb.endpoints)
	}
	// Check server1's anchor has been added along with meshConfig pem cert
	expectTbCount(t, tb, 2, 3*time.Second, "server1(running) trustAnchor not updated in bundle")

	// Test3: Stop server1
	server1.Close()
	// Check server1's valid trustAnchor is no longer in the trustbundle within poll frequency window
	expectTbCount(t, tb, 1, 6*time.Second, "server1(stopped) trustAnchor not removed from bundle")

	// Test4: Update with server1, server2 and mesh pem ca
	tb.AddMeshConfigUpdate(&meshconfig.MeshConfig{CaCertificates: []*meshconfig.MeshConfig_CertificateData{
		{CertificateData: &meshconfig.MeshConfig_CertificateData_SpiffeBundleUrl{SpiffeBundleUrl: server2.Listener.Addr().String()}},
		{CertificateData: &meshconfig.MeshConfig_CertificateData_SpiffeBundleUrl{SpiffeBundleUrl: server1.Listener.Addr().String()}},
		{CertificateData: &meshconfig.MeshConfig_CertificateData_Pem{Pem: rootCACert}},
	}})
	if !isEqSliceStr(tb.endpoints, []string{server2.Listener.Addr().String(), server1.Listener.Addr().String()}) {
		t.Errorf("server2 endpoint not correctly updated in trustbundle. Trustbundle endpoints: %v", tb.endpoints)
	}
	// Check only server 2's trustanchor is present along with meshConfig pem and not server 1 (since it is down)
	expectTbCount(t, tb, 2, 3*time.Second, "server2(running) trustAnchor not updated in bundle")

	// Test5. remove everything
	tb.AddMeshConfigUpdate(&meshconfig.MeshConfig{CaCertificates: []*meshconfig.MeshConfig_CertificateData{}})
	expectTbCount(t, tb, 0, 3*time.Second, "trustAnchor not updated in bundle after meshConfig cleared")
}
