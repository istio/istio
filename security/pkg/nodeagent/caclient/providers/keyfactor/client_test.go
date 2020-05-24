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
	"encoding/json"
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

	validRootCert1 = "MIIFFDCCAvygAwIBAgIUNvN+0FmOtgNTyjKK730i4JgPDeMwDQYJKoZIhvcNAQELBQAwIjEOMAwGA1UECgwFSXN0aW8xEDAOBgNVBAMMB1Jvb3QgQ0EwHhcNMTkxMDI4MTkzMjQxWhcNMjkxMDI1MTkzMjQxWjAiMQ4wDAYDVQQKDAVJc3RpbzEQMA4GA1UEAwwHUm9vdCBDQTCCAiIwDQYJKoZIhvcNAQEBBQADggIPADCCAgoCggIBAMdaPD8Ift/fFkqdLiBBQPTLhozaeGkEkGhBsXkoHw38CCdeaRekGOoZI58Ce/iOjCIACEZon1Y6SKYnl4FqPFjOy3uF0ZFMeCt8GA+QrlPdEfkDAnj3FFc+C1THov0R+FCv2QrsFR0I6OZen+CVWS53xQNagxfBi9XeFI833gKr8Qiv0WOJKuoTY3abw8FJKyPPHX4ORnqLDwEr8BRQyqgWCQPGGL+quGV22dfI8tVxmj0lXR3fJs3kuNagCoCSSnpJSjGieYy80i+esUO0RCNLoA78ia9bua5juyU6sUZca7Yk28cbz29niaT79iB02vQI+U8xDL11eq7Wg6zsUhTrzIwJKCMyhsQCEYmrYIfv3STqkxzdiePnyjGXorX3mVZsNAxTfwerb5rGdNm8QVa+LgRMPZmLlRiMjGkut3O0S76bPthbp7dgAiYXfmHlucmrCs80E8qpPpceZUqEHbK9IUmHeecvl3oSx2H7ym4id1dq55eyQXk7DiZbo0yciGsIhytFPLXDpeop4r67vZfsn9EqWed8XH7PBGdZMkDH5gMp1OaO/gHmIf/qnCYB6wJOKk7z+Dol7sggwy+KwU+gjINbJwvFX/3pwY5cIza4Ds2B7oUe4hohZo2HzCd+RaTPNM03DDyQmKcLhwt3K5oZFVceK6rCOpWzMq7upXPVAgMBAAGjQjBAMB0GA1UdDgQWBBQ33/NM2B73DIxoRC4zlw9YP0kP0DAPBgNVHRMBAf8EBTADAQH/MA4GA1UdDwEB/wQEAwIC5DANBgkqhkiG9w0BAQsFAAOCAgEAxbDDFmN84kPtl0dmAPbHqZRNkZkcZQ2nM0i7ABVpaj9FH1zG4HeLxKyihfdHpF4N6aR95tmyzJd2OKSC1CiPDF85Lgc/+OdOU2NRijl3wzcl2yqza1ChQK/clKkKFn4+WQgzBJbtiOmqD8NojJlw3juKclK25SAH94bCksJg2Z834lsQY9cDIzqEackt/1NAa1IboZTQsJXzLZ9jAxv3TJWGapG7qHc55ojcm4h2WbDXoKWCBSU8Z2rFjT48x3YONjwWB7BPUdEOTwdbtpLoTDRFhUZttM9UovpsLTMumzUqmaI+2Q0gPVjQo4wvBPeouhEc2KYlvD6U7BrVz2JqEgSmsdJR4wNJsBf2kBuqdGiWCbDuGeJBGDc48jAmqvKdaljkt4IFigYuRUx8NFgvichbkpU/ZfuQCVpValVTXe7GMJadMnXLsoXMU1z57dbEdarej6TiCymOeIJ9oJF0g9ppNqq6NRnLY7pH4lN1U8lHxa52uPZ5HNsld3+fFKNq1tgbNhQ1Q9gn7nLalTsAr4RZJN9QMnsek4OycvyY2i1iKYl5kcI2g38FzlIlALOrxd8nhQDBF5rRktfqp7t3HtKZubjkBwMQtP+N2C0otdj8D6IDHlT8OFr69n+PD4qR6P4bKxnjiYtEAqRvPlR96yrtjbdg/QgJ0+aVGEMeDqg="
	validCert1     = "MIIFiDCCA3ACCQDriJFARkUboTANBgkqhkiG9w0BAQsFADCBhTELMAkGA1UEBhMCVVMxEzARBgNVBAgTCkNhbGlmb3JuaWExEjAQBgNVBAcTCVN1bm55dmFsZTEOMAwGA1UEChMFSXN0aW8xDTALBgNVBAsTBFRlc3QxEDAOBgNVBAMTB1Jvb3QgQ0ExHDAaBgkqhkiG9w0BCQEWDXRlc3RAaXN0aW8uaW8wHhcNMTgwMjI1MDg0NjA4WhcNMjgwMjIzMDg0NjA4WjCBhTELMAkGA1UEBhMCVVMxEzARBgNVBAgTCkNhbGlmb3JuaWExEjAQBgNVBAcTCVN1bm55dmFsZTEOMAwGA1UEChMFSXN0aW8xDTALBgNVBAsTBFRlc3QxEDAOBgNVBAMTB1Jvb3QgQ0ExHDAaBgkqhkiG9w0BCQEWDXRlc3RAaXN0aW8uaW8wggIiMA0GCSqGSIb3DQEBAQUAA4ICDwAwggIKAoICAQCbA6YHmfD5VhvfvqASVm7nY/Wssna19SSJ5ToInAwIFOoF2MtloZvnbs0t2K75/Vlxgfcpb8HvFFInP6MwqJo0T9l6T14ddZFPFJL6zSlBpPzXdkTFElCA5Tzd/2W6csZ/99N58ovmTmvlsStpomWNtyQXmTLFt0h9WRHkTmiZq/e4aOqsUyLcMXC1+prw+YJi84H9tUdpF67kR3LiBMzAy3SI6MvpT5MEA99P0BHRnBH8f6ITkvs6A738OAJrIB2RHljmr07SrUSZcz4BJgUOJtYhi0qutjB+rQbCiEReykLj6+TscGp7apT1+OH5RrpaYNr6WIlOaPg6girYa96+0YBOryoI8rczvZN+VoiXs4vlC2gkTIeRPBUx0tOJb7yZjudAfu3ivRyEYJnhzbc9u8JDwfbvi2BtgswNlF/1hZ4ApTMVQJuln9cmQTe7f5O9pID1VcBJyVj2JJw071qjGOfV6AI3hhDJPtFFZInrsmD+Y8p3hYhQ0MdEuO1BVfVXWq8C/GPU6liUzQwURM63liLqlq4D/4jvLR3PhWby+LgrrY1qXScXfNwA+cc5pvn6gzHoo0PZJiDFJD+wTMPqL84f+/yWN1StvzvECj9VSpFusu4BgLBIQZEcdGhv2or/Cp889qZK06yBgcNs4bF4qYN1nd0iXGpaFQVAdRvXiQIDAQABMA0GCSqGSIb3DQEBCwUAA4ICAQA8W6ATS6zn6WEXRvXAD389QKPtEHQdU8cuiAfxwmAhilJisJk+snTLspB9BtXBH0ZyyNRd4Jg3aVGu9ylF7Bir0/i3Za+SB7jKjs4XnUnItzJHQXeo1cKY145ubI6bmgaFSNIqA+pyXDfAQ7PHFPVVRLdXtKgQaJ5qvs++IoVzQTWKUq6XK2WHVlhPtsmovexsI1o0RUxR/cAdF4qkNamQm00jemjJTaC3VKPREILNGUudiQ1uX5n8yWvtnakGbkNKciO2jCW6Y0xYinrGM/HmRzEfCloFO7Gq7Q+JZXQsVQbCesw52MgtgXm7H36dMSAkP3YVaFbIfGN0mpv93kXy0YjzExBh5xYDfFnSDB/1tz7gCb4IEqhCq/iHz4qTBQ4NeUNQv/m6dabQ0UYmRupB2ALj9+4cXiCQbn4hxiv7OKeGcR/Z6VI11/sPaxe/uZUdqQheqnKUZhHhyibzviYLU5KasT4IsQe4eOu5V/qzSYpB5p2Cw8foDclW7Ekxa/E0CACzCEn9xDtFZjjYJDw1ujo23Kc6k/FBKF5QQhzDA5nWVubT3hC/RK6FR/4GtzwR2QUei5RRLNCjX3KV3JXoAiksmn7sE8Bi6fJjYOStK0EQSPTqUWOe+32iSFDC/FX6PU5syPI4TT95yXzBqNaTU1+Igrk2nl7aDTcOKrpwxg=="
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
			expectedErr: fmt.Sprintf("Invalid root-cert.pem: %v", "Invaliddddddddd certificate"),
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

			os.Setenv("KEYFACTOR_CA", "FakeCA")
			os.Setenv("KEYFACTOR_AUTH_TOKEN", "FakeAuthToken")
			os.Setenv("KEYFACTOR_APPKEY", "FakeAppKey")
			os.Setenv("KEYFACTOR_CA_TEMPLATE", "Istio")

			metadataJSON, _ := json.Marshal(&[]FieldAlias{{Name: "Cluster", Alias: "Cluster_Alias"}})
			os.Setenv("KEYFACTOR_METADATA_JSON", string(metadataJSON))

			defer func() {
				os.Unsetenv("KEYFACTOR_CA")
				os.Unsetenv("KEYFACTOR_AUTH_TOKEN")
				os.Unsetenv("KEYFACTOR_APPKEY")
				os.Unsetenv("KEYFACTOR_CA_TEMPLATE")
				os.Unsetenv("KEYFACTOR_METADATA_JSON")
			}()

			_, err := NewKeyFactorCAClient("", tc.enabledTLS, tc.rootCert, &KeyfactorCAClientMetadata{})

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

	fakeURL := "fake091234keyfactor.com/bad/url"

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
			expectedErr:  fmt.Sprintf("Request failed with status: 500, message: 500"),
		},
		"On server not available": {
			turnOnServer: false,
			expectedErr:  fmt.Sprintf("Could not request to KeyfactorCA server: %v", fakeURL),
		},
	}

	for testID, tc := range testCases {
		t.Run(testID, func(tsub *testing.T) {

			os.Setenv("KEYFACTOR_CONFIG_NAME", "config_for_client.yaml")
			defer os.Unsetenv("KEYFACTOR_CONFIG_NAME")

			os.Setenv("KEYFACTOR_CONFIG_PATH", "./testdata")
			defer os.Unsetenv("KEYFACTOR_CONFIG_PATH")

			requestBodyChan := make(chan map[string]interface{})
			mockServer := mockKeyfactor.CreateServer(tc.serverError, tc.responseData, requestBodyChan)

			defer mockServer.Server.Close()

			caEndpoint := mockServer.Address

			if !tc.turnOnServer {
				caEndpoint = fakeURL
			}

			cl, err := NewKeyFactorCAClient(caEndpoint, false, nil, &KeyfactorCAClientMetadata{})
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
		customMetadatas   []FieldAlias
		expectMetadataMap map[string]string
		expectedErr       string
	}{
		"Valid metadata configuration": {
			customMetadatas:   []FieldAlias{{Name: "Cluster", Alias: "Fake_Alias_Cluster"}},
			expectMetadataMap: map[string]string{"Fake_Alias_Cluster": "FakeClusterName"},
			expectedErr:       "",
		},
		"Empty metadata configuration": {
			customMetadatas:   []FieldAlias{},
			expectMetadataMap: make(map[string]string),
			expectedErr:       "",
		},
		"With full metadata configuration": {
			customMetadatas: []FieldAlias{
				{Name: "Cluster", Alias: "Fake_Alias_Cluster"},
				{Name: "Service", Alias: "Fake_Alias_Service"},
				{Name: "PodName", Alias: "Fake_Alias_PodName"},
				{Name: "PodNamespace", Alias: "Fake_Alias_PodNamespace"},
				{Name: "PodIP", Alias: "Fake_Alias_PodIP"},
			},
			expectMetadataMap: map[string]string{
				"Fake_Alias_Cluster":      "FakeClusterName",
				"Fake_Alias_Service":      "FakeService",
				"Fake_Alias_PodName":      "FakeService-v1-PodID",
				"Fake_Alias_PodNamespace": "FakePodNamespace",
				"Fake_Alias_PodIP":        "FakePodIP",
			},
			expectedErr: "",
		},
		"Error with further redundant metadata configurations": {
			customMetadatas: []FieldAlias{
				{Name: "Cluster", Alias: "Fake_Alias_Cluster"},
				{Name: "Service", Alias: "Fake_Alias_Service"},
				{Name: "PodName", Alias: "Fake_Alias_PodName"},
				{Name: "PodNamespace", Alias: "Fake_Alias_PodNamespace"},
				{Name: "PodIP", Alias: "Fake_Alias_PodIP"},
				{Name: "MoreMoreMore", Alias: "MoreMoreMore"},
			},
			expectMetadataMap: map[string]string{
				"Fake_Alias_Cluster":      "FakeClusterName",
				"Fake_Alias_Service":      "FakeService",
				"Fake_Alias_PodName":      "FakeService-v1-PodID",
				"Fake_Alias_PodNamespace": "FakePodNamespace",
				"Fake_Alias_PodIP":        "FakePodIP",
			},
			expectedErr: "Cannot load keyfactor config: Do not support Metadata field name: MoreMoreMore",
		},
	}

	for testID, tc := range testCases {
		t.Run(testID, func(tsub *testing.T) {

			os.Setenv("KEYFACTOR_CA", "FakeCA")
			os.Setenv("KEYFACTOR_AUTH_TOKEN", "FakeAuthToken")
			os.Setenv("KEYFACTOR_APPKEY", "FakeAppKey")
			os.Setenv("KEYFACTOR_CA_TEMPLATE", "Istio")

			metadataJSON, _ := json.Marshal(tc.customMetadatas)
			os.Setenv("KEYFACTOR_METADATA_JSON", string(metadataJSON))

			defer func() {
				os.Unsetenv("KEYFACTOR_CA")
				os.Unsetenv("KEYFACTOR_AUTH_TOKEN")
				os.Unsetenv("KEYFACTOR_APPKEY")
				os.Unsetenv("KEYFACTOR_CA_TEMPLATE")
				os.Unsetenv("KEYFACTOR_METADATA_JSON")
			}()
			requestBodyChan := make(chan map[string]interface{})
			mockServer := mockKeyfactor.CreateServer(false, fakeResponse, requestBodyChan)

			defer mockServer.Server.Close()

			caEndpoint := mockServer.Address

			cl, err := NewKeyFactorCAClient(caEndpoint, false, nil, &KeyfactorCAClientMetadata{
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
				time.Sleep(200 * time.Millisecond)
				_, err = cl.CSRSign(context.TODO(), "", csrPEM, "Istio", 6400)
				if err != nil {
					tsub.Errorf("Test case [%s]: got error on call CSRSign: %v", testID, err.Error())
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
					assert.Equalf(tsub, respMetadata[k].(string), tc.expectMetadataMap[k], "Custom meta should have value: %v - but got (%v)", tc.expectMetadataMap[k], respMetadata[k].(string))
				}
				return
			}
			assert.Lenf(tsub, respMetadata, 0, "Metadata should be empty but got %v", respMetadata)
		})
	}

}
