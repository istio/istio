// Copyright 2020 Istio Authors
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
	"fmt"
	"os"
	"testing"
	"time"

	assert "github.com/stretchr/testify/assert"
	"golang.org/x/net/context"

	mockKeyfactor "istio.io/istio/security/pkg/nodeagent/caclient/providers/keyfactor/mock"
	nodeagentutil "istio.io/istio/security/pkg/nodeagent/util"
	pkiutil "istio.io/istio/security/pkg/pki/util"
)

var (
	// cd samples/certs
	// cat root-cert.pem
	rootCertPEM = []byte(`-----BEGIN CERTIFICATE-----
MIIFFDCCAvygAwIBAgIUNvN+0FmOtgNTyjKK730i4JgPDeMwDQYJKoZIhvcNAQEL
BQAwIjEOMAwGA1UECgwFSXN0aW8xEDAOBgNVBAMMB1Jvb3QgQ0EwHhcNMTkxMDI4
MTkzMjQxWhcNMjkxMDI1MTkzMjQxWjAiMQ4wDAYDVQQKDAVJc3RpbzEQMA4GA1UE
AwwHUm9vdCBDQTCCAiIwDQYJKoZIhvcNAQEBBQADggIPADCCAgoCggIBAMdaPD8I
ft/fFkqdLiBBQPTLhozaeGkEkGhBsXkoHw38CCdeaRekGOoZI58Ce/iOjCIACEZo
n1Y6SKYnl4FqPFjOy3uF0ZFMeCt8GA+QrlPdEfkDAnj3FFc+C1THov0R+FCv2Qrs
FR0I6OZen+CVWS53xQNagxfBi9XeFI833gKr8Qiv0WOJKuoTY3abw8FJKyPPHX4O
RnqLDwEr8BRQyqgWCQPGGL+quGV22dfI8tVxmj0lXR3fJs3kuNagCoCSSnpJSjGi
eYy80i+esUO0RCNLoA78ia9bua5juyU6sUZca7Yk28cbz29niaT79iB02vQI+U8x
DL11eq7Wg6zsUhTrzIwJKCMyhsQCEYmrYIfv3STqkxzdiePnyjGXorX3mVZsNAxT
fwerb5rGdNm8QVa+LgRMPZmLlRiMjGkut3O0S76bPthbp7dgAiYXfmHlucmrCs80
E8qpPpceZUqEHbK9IUmHeecvl3oSx2H7ym4id1dq55eyQXk7DiZbo0yciGsIhytF
PLXDpeop4r67vZfsn9EqWed8XH7PBGdZMkDH5gMp1OaO/gHmIf/qnCYB6wJOKk7z
+Dol7sggwy+KwU+gjINbJwvFX/3pwY5cIza4Ds2B7oUe4hohZo2HzCd+RaTPNM03
DDyQmKcLhwt3K5oZFVceK6rCOpWzMq7upXPVAgMBAAGjQjBAMB0GA1UdDgQWBBQ3
3/NM2B73DIxoRC4zlw9YP0kP0DAPBgNVHRMBAf8EBTADAQH/MA4GA1UdDwEB/wQE
AwIC5DANBgkqhkiG9w0BAQsFAAOCAgEAxbDDFmN84kPtl0dmAPbHqZRNkZkcZQ2n
M0i7ABVpaj9FH1zG4HeLxKyihfdHpF4N6aR95tmyzJd2OKSC1CiPDF85Lgc/+OdO
U2NRijl3wzcl2yqza1ChQK/clKkKFn4+WQgzBJbtiOmqD8NojJlw3juKclK25SAH
94bCksJg2Z834lsQY9cDIzqEackt/1NAa1IboZTQsJXzLZ9jAxv3TJWGapG7qHc5
5ojcm4h2WbDXoKWCBSU8Z2rFjT48x3YONjwWB7BPUdEOTwdbtpLoTDRFhUZttM9U
ovpsLTMumzUqmaI+2Q0gPVjQo4wvBPeouhEc2KYlvD6U7BrVz2JqEgSmsdJR4wNJ
sBf2kBuqdGiWCbDuGeJBGDc48jAmqvKdaljkt4IFigYuRUx8NFgvichbkpU/ZfuQ
CVpValVTXe7GMJadMnXLsoXMU1z57dbEdarej6TiCymOeIJ9oJF0g9ppNqq6NRnL
Y7pH4lN1U8lHxa52uPZ5HNsld3+fFKNq1tgbNhQ1Q9gn7nLalTsAr4RZJN9QMnse
k4OycvyY2i1iKYl5kcI2g38FzlIlALOrxd8nhQDBF5rRktfqp7t3HtKZubjkBwMQ
tP+N2C0otdj8D6IDHlT8OFr69n+PD4qR6P4bKxnjiYtEAqRvPlR96yrtjbdg/QgJ
0+aVGEMeDqg=
-----END CERTIFICATE-----`)

	// cd samples/certs
	// cat root-cert.pem
	// remove headers
	validRootCert1 = `
	MIIFFDCCAvygAwIBAgIUNvN+0FmOtgNTyjKK730i4JgPDeMwDQYJKoZIhvcNAQEL
	BQAwIjEOMAwGA1UECgwFSXN0aW8xEDAOBgNVBAMMB1Jvb3QgQ0EwHhcNMTkxMDI4
	MTkzMjQxWhcNMjkxMDI1MTkzMjQxWjAiMQ4wDAYDVQQKDAVJc3RpbzEQMA4GA1UE
	AwwHUm9vdCBDQTCCAiIwDQYJKoZIhvcNAQEBBQADggIPADCCAgoCggIBAMdaPD8I
	ft/fFkqdLiBBQPTLhozaeGkEkGhBsXkoHw38CCdeaRekGOoZI58Ce/iOjCIACEZo
	n1Y6SKYnl4FqPFjOy3uF0ZFMeCt8GA+QrlPdEfkDAnj3FFc+C1THov0R+FCv2Qrs
	FR0I6OZen+CVWS53xQNagxfBi9XeFI833gKr8Qiv0WOJKuoTY3abw8FJKyPPHX4O
	RnqLDwEr8BRQyqgWCQPGGL+quGV22dfI8tVxmj0lXR3fJs3kuNagCoCSSnpJSjGi
	eYy80i+esUO0RCNLoA78ia9bua5juyU6sUZca7Yk28cbz29niaT79iB02vQI+U8x
	DL11eq7Wg6zsUhTrzIwJKCMyhsQCEYmrYIfv3STqkxzdiePnyjGXorX3mVZsNAxT
	fwerb5rGdNm8QVa+LgRMPZmLlRiMjGkut3O0S76bPthbp7dgAiYXfmHlucmrCs80
	E8qpPpceZUqEHbK9IUmHeecvl3oSx2H7ym4id1dq55eyQXk7DiZbo0yciGsIhytF
	PLXDpeop4r67vZfsn9EqWed8XH7PBGdZMkDH5gMp1OaO/gHmIf/qnCYB6wJOKk7z
	+Dol7sggwy+KwU+gjINbJwvFX/3pwY5cIza4Ds2B7oUe4hohZo2HzCd+RaTPNM03
	DDyQmKcLhwt3K5oZFVceK6rCOpWzMq7upXPVAgMBAAGjQjBAMB0GA1UdDgQWBBQ3
	3/NM2B73DIxoRC4zlw9YP0kP0DAPBgNVHRMBAf8EBTADAQH/MA4GA1UdDwEB/wQE
	AwIC5DANBgkqhkiG9w0BAQsFAAOCAgEAxbDDFmN84kPtl0dmAPbHqZRNkZkcZQ2n
	M0i7ABVpaj9FH1zG4HeLxKyihfdHpF4N6aR95tmyzJd2OKSC1CiPDF85Lgc/+OdO
	U2NRijl3wzcl2yqza1ChQK/clKkKFn4+WQgzBJbtiOmqD8NojJlw3juKclK25SAH
	94bCksJg2Z834lsQY9cDIzqEackt/1NAa1IboZTQsJXzLZ9jAxv3TJWGapG7qHc5
	5ojcm4h2WbDXoKWCBSU8Z2rFjT48x3YONjwWB7BPUdEOTwdbtpLoTDRFhUZttM9U
	ovpsLTMumzUqmaI+2Q0gPVjQo4wvBPeouhEc2KYlvD6U7BrVz2JqEgSmsdJR4wNJ
	sBf2kBuqdGiWCbDuGeJBGDc48jAmqvKdaljkt4IFigYuRUx8NFgvichbkpU/ZfuQ
	CVpValVTXe7GMJadMnXLsoXMU1z57dbEdarej6TiCymOeIJ9oJF0g9ppNqq6NRnL
	Y7pH4lN1U8lHxa52uPZ5HNsld3+fFKNq1tgbNhQ1Q9gn7nLalTsAr4RZJN9QMnse
	k4OycvyY2i1iKYl5kcI2g38FzlIlALOrxd8nhQDBF5rRktfqp7t3HtKZubjkBwMQ
	tP+N2C0otdj8D6IDHlT8OFr69n+PD4qR6P4bKxnjiYtEAqRvPlR96yrtjbdg/QgJ
	0+aVGEMeDqg=
	`
	// cd samples/certs
	// cat ca-cert.pem
	// remove headers
	validCert1 = `
	MIIDnzCCAoegAwIBAgIJAON1ifrBZ2/BMA0GCSqGSIb3DQEBCwUAMIGLMQswCQYD
	VQQGEwJVUzETMBEGA1UECAwKQ2FsaWZvcm5pYTESMBAGA1UEBwwJU3Vubnl2YWxl
	MQ4wDAYDVQQKDAVJc3RpbzENMAsGA1UECwwEVGVzdDEQMA4GA1UEAwwHUm9vdCBD
	QTEiMCAGCSqGSIb3DQEJARYTdGVzdHJvb3RjYUBpc3Rpby5pbzAgFw0xODAxMjQx
	OTE1NTFaGA8yMTE3MTIzMTE5MTU1MVowWTELMAkGA1UEBhMCVVMxEzARBgNVBAgT
	CkNhbGlmb3JuaWExEjAQBgNVBAcTCVN1bm55dmFsZTEOMAwGA1UEChMFSXN0aW8x
	ETAPBgNVBAMTCElzdGlvIENBMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKC
	AQEAyzCxr/xu0zy5rVBiso9ffgl00bRKvB/HF4AX9/ytmZ6Hqsy13XIQk8/u/By9
	iCvVwXIMvyT0CbiJq/aPEj5mJUy0lzbrUs13oneXqrPXf7ir3HzdRw+SBhXlsh9z
	APZJXcF93DJU3GabPKwBvGJ0IVMJPIFCuDIPwW4kFAI7R/8A5LSdPrFx6EyMXl7K
	M8jekC0y9DnTj83/fY72WcWX7YTpgZeBHAeeQOPTZ2KYbFal2gLsar69PgFS0Tom
	ESO9M14Yit7mzB1WDK2z9g3r+zLxENdJ5JG/ZskKe+TO4Diqi5OJt/h8yspS1ck8
	LJtCole9919umByg5oruflqIlQIDAQABozUwMzALBgNVHQ8EBAMCAgQwDAYDVR0T
	BAUwAwEB/zAWBgNVHREEDzANggtjYS5pc3Rpby5pbzANBgkqhkiG9w0BAQsFAAOC
	AQEAltHEhhyAsve4K4bLgBXtHwWzo6SpFzdAfXpLShpOJNtQNERb3qg6iUGQdY+w
	A2BpmSkKr3Rw/6ClP5+cCG7fGocPaZh+c+4Nxm9suMuZBZCtNOeYOMIfvCPcCS+8
	PQ/0hC4/0J3WJKzGBssaaMufJxzgFPPtDJ998kY8rlROghdSaVt423/jXIAYnP3Y
	05n8TGERBj7TLdtIVbtUIx3JHAo3PWJywA6mEDovFMJhJERp9sDHIr1BbhXK1TFN
	Z6HNH6gInkSSMtvC4Ptejb749PTaePRPF7ID//eq/3AH8UK50F3TQcLjEqWUsJUn
	aFKltOc+RAjzDklcUPeG4Y6eMA==
	`
)

func TestKeyfactorWithTLSEnabled(t *testing.T) {

	testCases := map[string]struct {
		rootCert    []byte
		expectedErr string
		enabledTLS  bool
	}{
		"Empty root-cert, using systems cert pool instead": {
			rootCert:    nil,
			expectedErr: "",
			enabledTLS:  true,
		},

		"With valid root certification, create client success": {
			rootCert:    rootCertPEM,
			expectedErr: "",
			enabledTLS:  true,
		},

		"Invalid certificate": {
			rootCert:    []byte("Invaliddddddddd certificate"),
			expectedErr: fmt.Sprintf("invalid root-cert.pem: %v", "Invaliddddddddd certificate"),
			enabledTLS:  true,
		},
		"Disabled TLS": {
			rootCert:    nil,
			expectedErr: "",
			enabledTLS:  true,
		},
	}

	for testID, tc := range testCases {
		t.Run(testID, func(tsub *testing.T) {

			os.Setenv("KEYFACTOR_CONFIG_PATH", "./testdata/valid.json")
			defer func() {
				os.Unsetenv("KEYFACTOR_CONFIG_PATH")
			}()

			_, err := NewKeyFactorCAClient("", tc.enabledTLS, tc.rootCert, KeyfactorCAClientMetadata{})

			if tc.expectedErr != "" && err == nil {
				tsub.Errorf("Test case [%s]: error (nil) does not match expected error (%s)", testID, tc.expectedErr)
			}

			if err != nil {
				if err.Error() != tc.expectedErr {
					tsub.Errorf("Test case [%s]: error (%s) does not match expected error (%s)", testID, err.Error(), tc.expectedErr)
				}
			}
		})
	}

}

func TestKeyfactorSignCSR(t *testing.T) {

	fakeURL := "https://fake091234keyfactor.com/bad/url"

	fakeResponse := KeyfactorResponse{
		CertificateInformation: CertificateInformation{
			Certificates: []string{validCert1, validRootCert1},
		},
	}

	options := pkiutil.CertOptions{
		Host:       "spiffe://cluster.local/ns/default/sa/default",
		RSAKeySize: 2048,
		Org:        "Istio Test",
		IsCA:       false,
		IsDualUse:  false,
		PKCS8Key:   false,
		TTL:        24 * time.Hour,
	}

	// Generate the cert/key, send CSR to CA.
	csrPEM, _, err := pkiutil.GenCSR(options)

	if err != nil {
		t.Errorf("Test case: failed to create CSRPem : %v", err)
	}

	testCases := map[string]struct {
		turnOnServer bool
		serverError  bool
		expectedErr  string
		responseData KeyfactorResponse
	}{
		"Signed CSR successful": {
			turnOnServer: true,
			expectedErr:  "",
			responseData: fakeResponse,
		},
		"On Server Error": {
			turnOnServer: true,
			serverError:  true,
			responseData: KeyfactorResponse{},
			expectedErr:  "request failed with status: 500, message: 500",
		},
	}

	for testID, tc := range testCases {
		t.Run(testID, func(tsub *testing.T) {

			os.Setenv("KEYFACTOR_CONFIG_PATH", "./testdata/valid.json")

			defer func() {
				os.Unsetenv("KEYFACTOR_CONFIG_PATH")
			}()

			mockServer := mockKeyfactor.CreateServer(tc.serverError, tc.responseData, nil)

			defer mockServer.Server.Close()

			caEndpoint := mockServer.Address

			if !tc.turnOnServer {
				caEndpoint = fakeURL
			}

			cl, _ := NewKeyFactorCAClient(caEndpoint, false, nil, KeyfactorCAClientMetadata{})
			certchain, err := cl.CSRSign(context.TODO(), "", csrPEM, "Istio", 6400)

			if err != nil {
				if err.Error() != tc.expectedErr {
					tsub.Errorf("Test case [%s]: error (%s) does not match expected error (%s)", testID, err.Error(), tc.expectedErr)
				}
			}

			for _, cert := range certchain {
				if _, err := nodeagentutil.ParseCertAndGetExpiryTimestamp([]byte(cert)); err != nil {
					t.Errorf("Expect not error, but got: %v", err)
				}
			}
		})
	}

}

func TestCustomMetadata(t *testing.T) {

	fakeResponse := KeyfactorResponse{
		CertificateInformation: CertificateInformation{
			Certificates: []string{validCert1, validRootCert1},
		},
	}

	options := pkiutil.CertOptions{
		Host:       "spiffe://cluster.local/ns/default/sa/default",
		RSAKeySize: 2048,
		Org:        "Istio Test",
		IsCA:       false,
		IsDualUse:  false,
		PKCS8Key:   false,
		TTL:        24 * time.Hour,
	}

	// Generate the cert/key, send CSR to CA.
	csrPEM, _, err := pkiutil.GenCSR(options)

	if err != nil {
		t.Errorf("Test case: failed to create CSRPem : %v", err)
	}

	testCases := map[string]struct {
		configPath        string
		expectMetadataMap map[string]string
		expectedErr       string
	}{
		"Valid metadata configuration": {
			configPath:        "./testdata/valid.json",
			expectMetadataMap: map[string]string{"Fake_Alias_Cluster": "FakeClusterName"},
			expectedErr:       "",
		},
		"Valid metadata configuration with empty alias": {
			configPath:        "./testdata/metadata_alias_empty.json",
			expectMetadataMap: map[string]string{"Fake_Alias_Cluster": "FakeClusterName"},
			expectedErr:       "",
		},
		"Empty metadata configuration": {
			configPath:        "./testdata/empty_metadata.json",
			expectMetadataMap: make(map[string]string),
			expectedErr:       "",
		},
		"Nil metadata configuration": {
			configPath:        "./testdata/nil_metadata.json",
			expectMetadataMap: make(map[string]string),
			expectedErr:       "",
		},
		"With full metadata configuration": {
			configPath: "./testdata/valid_metadata.json",
			expectMetadataMap: map[string]string{
				"Fake_Alias_Cluster":      "FakeClusterName",
				"Fake_Alias_Service":      "FakeService",
				"Fake_Alias_PodName":      "FakeService-v1-PodID",
				"Fake_Alias_PodNamespace": "FakePodNamespace",
				"Fake_Alias_PodIP":        "FakePodIP",
				"Fake_Trust_Domain":       "FakeTrustDomain",
			},
			expectedErr: "",
		},
		"Error with further redundant metadata configurations": {
			configPath:        "./testdata/invalid_metadata.json",
			expectMetadataMap: map[string]string{},
			expectedErr:       "cannot load keyfactor config: do not support Metadata field name: Invalid",
		},
	}

	for testID, tc := range testCases {
		t.Run(testID, func(tsub *testing.T) {

			os.Setenv("KEYFACTOR_CONFIG_PATH", tc.configPath)

			defer func() {
				os.Unsetenv("KEYFACTOR_CONFIG_PATH")
			}()

			requestBodyChan := make(chan map[string]interface{})
			mockServer := mockKeyfactor.CreateServer(false, fakeResponse, requestBodyChan)

			defer mockServer.Server.Close()

			caEndpoint := mockServer.Address

			cl, err := NewKeyFactorCAClient(caEndpoint, false, nil, KeyfactorCAClientMetadata{
				ClusterID:    "FakeClusterName",
				TrustDomain:  "FakeTrustDomain",
				PodNamespace: "FakePodNamespace",
				PodName:      "FakeService-v1-PodID",
				PodIP:        "FakePodIP",
			})

			if err != nil {
				assert.EqualError(tsub, err, tc.expectedErr, "Expect throw error on create keyfactor client")
				return
			}

			go func() {
				time.Sleep(50 * time.Millisecond)
				_, err = cl.CSRSign(context.TODO(), "", csrPEM, "Istio", 6400)
				if err != nil {
					tsub.Errorf("Test case [%s]: got error on call CSRSign: %v", testID, err)
				}
			}()

			reqBody := <-requestBodyChan

			if reqBody["Metadata"] == nil {
				if len(tc.expectMetadataMap) > 0 {
					tsub.Errorf("Expect have metadata: %v, but got empty", tc.expectMetadataMap)
				}
			}

			respMetadata := reqBody["Metadata"].(map[string]interface{})

			if len(tc.expectMetadataMap) > 0 {
				for k := range tc.expectMetadataMap {
					assert.NotNilf(tsub, respMetadata[k], "The two map Custom Metadata should be equal - but missing key (%v)", k)
					assert.Equalf(tsub, respMetadata[k].(string), tc.expectMetadataMap[k],
						"Custom meta should have value: %v - but got (%v)", tc.expectMetadataMap[k], respMetadata[k].(string))
				}
				return
			}
			assert.Lenf(tsub, respMetadata, 0, "Metadata should be empty but got %v", respMetadata)
		})
	}

}
