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

package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

var (
	// A sample JWT without expiration.
	validJWT = "eyJhbGciOiJSUzI1NiIsImtpZCI6IlNsQ282WGxzUmtUZFFlTlFjb3ZCaTI3RmpPTFRuS1NsMEdwS0luLVFMdlEifQ.eyJpc3MiO" +
		"iJrdWJlcm5ldGVzL3NlcnZpY2VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9uYW1lc3BhY2UiOiJpc3Rpby1zeXN0" +
		"ZW0iLCJrdWJlcm5ldGVzLmlvL3NlcnZpY2VhY2NvdW50L3NlY3JldC5uYW1lIjoiaXN0aW8tcGlsb3Qtc2VydmljZS1hY2NvdW50LXRva2V" +
		"uLXQ3NnJnIiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9zZXJ2aWNlLWFjY291bnQubmFtZSI6ImlzdGlvLXBpbG90LXNlcnZpY2" +
		"UtYWNjb3VudCIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2VydmljZS1hY2NvdW50LnVpZCI6IjBjNDYyNDZmLWRkZGItNDk2M" +
		"S05YjUxLTAyMzg3MGMxNjkzZSIsInN1YiI6InN5c3RlbTpzZXJ2aWNlYWNjb3VudDppc3Rpby1zeXN0ZW06aXN0aW8tcGlsb3Qtc2Vydmlj" +
		"ZS1hY2NvdW50In0.xXm2vr1dR2qyrAdBqHJqa_VDuBV8mOe7jiRwG1rZl0aMRtYF--DOMd4hGfaK-I-GieYP6TNzFrkqrT0DLGUOMiBnzb6" +
		"0PM103xEfuuFFlkxA4IlJgN2_e-vB5QO2QhLghxSI7up4xFBxvfTN5TXGmTdnnlFw4XVKRrM1Vgl8YlVyL6xc9CC4ckfF0SMNNyMOQk3v5g" +
		"BwAzImdFw198YLDxrNzXMfAPQ_PnJYFBXnin-BrGVEd2HknamUuG9XNvBhIpI-o-oXgUEEEm_zTQd_qbpOCZd0utvKhknTzjb92kUVRjxG1" +
		"3vscVKNHqBVOYu1M69uHlx53Bt3o903oAB6rA"

	// A sample JWT without expiration.
	invalidJWT = "eyJhbGciOiJSUzI1NiIsImtpZCI6IlNsQ282WGxzUmtUZFFlTlFjb3ZCaTI3RmpPTFRuS1NsMEdwS0luLVFMdlEifQ.eyJpc3M" +
		"iOiJrdWJlcm5ldGVzL3NlcnZpY2VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9uYW1lc3BhY2UiOiJpc3Rpby1zeX" +
		"N0ZW0iLCJrdWJlcm5ldGVzLmlvL3NlcnZpY2VhY2NvdW50L3NlY3JldC5uYW1lIjoiaXN0aW8taW5ncmVzc2dhdGV3YXktc2VydmljZS1hY" +
		"2NvdW50LXRva2VuLXp2cmhmIiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9zZXJ2aWNlLWFjY291bnQubmFtZSI6ImlzdGlvLWlu" +
		"Z3Jlc3NnYXRld2F5LXNlcnZpY2UtYWNjb3VudCIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2VydmljZS1hY2NvdW50LnVpZCI" +
		"6IjkwNjVkZjVlLTcxNDQtNDg0YS05ZTdjLTQxNTFlMTM3MGVlNSIsInN1YiI6InN5c3RlbTpzZXJ2aWNlYWNjb3VudDppc3Rpby1zeXN0ZW" +
		"06aXN0aW8taW5ncmVzc2dhdGV3YXktc2VydmljZS1hY2NvdW50In0.YK_FVAfeW6x1khFt40_m_kXrdnp3VTLjLr8CckRBZMz7R12jHL7FU" +
		"E7l7oxM1YJGPs-jusM_1DnVEuJaA360EPhzAsEw2i_AEhnS5m1sZEsTFmPf3_86wH53p_F9ioAZESHyOwPiGSY9Y4N8ccMEO7lDZIWsps1-" +
		"3sMxhqwdU1iMf6zja5TilC_87aB99pcTtty6NMtcIqmgy3T9wj1HDEHZMyg-6rOKkMz-_8KSME-5a8WyfjqQY_3_uhXqvJvIenQdIG9p29k" +
		"aKq4UKYzHmhn9B6U5ZMeGXYQI2CsGxsXEJLrwRFlArIYggQhcJHlWyYgTLXmNuAWrpymXuWSsZQ"

	validRole = "testrole"

	loginResp = `{
	  "auth": {
		  "client_token": "fake-vault-token"
	  }
	}`

	loginResp2 = `{
	  "auth": {
		  "client_token": "fake-vault-token2"
	  }
	}`

	kvResp = `{
		"data": {
			"clientcert": "TEST_CLIENT_CERT",
			"clientkey": "TEST_CLIENT_KEY",
			"servercert": "TEST_SERVER_CERT",
			"PIN": "PIN"
		}
	}`

	kvResp2 = `{
		"data": {
			"clientcert": "TEST_CLIENT_CERT",
			"clientkey": "TEST_CLIENT_KEY",
			"servercert": "TEST_SERVER_CERT"
		}
	}`

	validLoginPath           = "auth/kubernetes/login"
	validLoginPath2          = "auth/kubernetes2/login"
	tlsRootCertCmName        = "vaulttlscert"
	tlsRootCertCmNs          = "istio-system"
	tlsRootCertCmName2       = "vaulttlscert2"
	tlsRootCertCmNs2         = "vault-test"
	invalidTLSRootCertCmName = "invalidvaulttlscert"
	validKVPath              = "asm/kvpath"
	invalidKVPath            = "asm/kvpath2"
)

type mockVaultServer struct {
	httpServer *httptest.Server
	loginRole  string
	token      string
	loginResp  string
	loginResp2 string
	kvResp     string
	kvResp2    string
}

type TestSetup struct {
	Server         *mockVaultServer
	jwtFile        *os.File
	invalidJwtFile *os.File
	k8sClient      *fake.Clientset
}

func TestNewVaultClient(t *testing.T) {
	setup := PrepareTest(t)
	defer setup.CleanUp()

	testCases := map[string]struct {
		vaultAddr     string
		tlsRootCertCM string
		jwtPath       string
		loginRole     string
		loginPath     string
		expErrRegEx   string
	}{
		"Missing ENV variable": {
			vaultAddr:     setup.Server.httpServer.URL,
			tlsRootCertCM: "",
			jwtPath:       setup.jwtFile.Name(),
			loginRole:     validRole,
			loginPath:     validLoginPath,
			expErrRegEx:   envVaultTLSCertCM + " is not configured",
		},
		"Valid login": {
			vaultAddr:     setup.Server.httpServer.URL,
			tlsRootCertCM: tlsRootCertCmName + "." + tlsRootCertCmNs,
			jwtPath:       setup.jwtFile.Name(),
			loginRole:     validRole,
			loginPath:     validLoginPath,
			expErrRegEx:   "",
		},
		"Load TLS root cert from ConfigMap in customized namespace": {
			vaultAddr:     setup.Server.httpServer.URL,
			tlsRootCertCM: tlsRootCertCmName2 + "." + tlsRootCertCmNs2,
			jwtPath:       setup.jwtFile.Name(),
			loginRole:     validRole,
			loginPath:     validLoginPath,
			expErrRegEx:   "",
		},
		"Invalid TLS root cert ConfigMap name": {
			vaultAddr:     setup.Server.httpServer.URL,
			tlsRootCertCM: "invalidname",
			jwtPath:       setup.jwtFile.Name(),
			loginRole:     validRole,
			loginPath:     validLoginPath,
			expErrRegEx:   "failed to load TLS root cert ConfigMap invalidname in istio-system+",
		},
		"Invalid TLS root cert in ConfigMap": {
			vaultAddr:     setup.Server.httpServer.URL,
			tlsRootCertCM: invalidTLSRootCertCmName + "." + tlsRootCertCmNs,
			jwtPath:       setup.jwtFile.Name(),
			loginRole:     validRole,
			loginPath:     validLoginPath,
			expErrRegEx:   "failed to append certificate .+ to the certificate pool",
		},
		"Wrong login path": {
			vaultAddr:     setup.Server.httpServer.URL,
			tlsRootCertCM: tlsRootCertCmName + "." + tlsRootCertCmNs,
			jwtPath:       setup.jwtFile.Name(),
			loginRole:     validRole,
			loginPath:     "auth/wrongpath/login",
			expErrRegEx:   "failed to login Vault at .+",
		},
		"Non-exist JWT file": {
			vaultAddr:     setup.Server.httpServer.URL,
			tlsRootCertCM: tlsRootCertCmName + "." + tlsRootCertCmNs,
			jwtPath:       "/non/exist/jwt/path",
			loginRole:     validRole,
			loginPath:     validLoginPath,
			expErrRegEx:   "failed to read JWT.+",
		},
		"Invalid JWT": {
			vaultAddr:     setup.Server.httpServer.URL,
			tlsRootCertCM: tlsRootCertCmName + "." + tlsRootCertCmNs,
			jwtPath:       setup.invalidJwtFile.Name(),
			loginRole:     validRole,
			loginPath:     validLoginPath,
			expErrRegEx:   "failed to login Vault at .+",
		},
		"Invalid role": {
			vaultAddr:     setup.Server.httpServer.URL,
			tlsRootCertCM: tlsRootCertCmName + "." + tlsRootCertCmNs,
			jwtPath:       setup.jwtFile.Name(),
			loginRole:     "invalidrole",
			loginPath:     validLoginPath,
			expErrRegEx:   "failed to login Vault at .+",
		},
	}

	for id, tc := range testCases {
		os.Setenv(envVaultAddr, tc.vaultAddr)
		os.Setenv(envVaultTLSCertCM, tc.tlsRootCertCM)
		os.Setenv(envJwtPath, tc.jwtPath)
		os.Setenv(envLoginRole, tc.loginRole)
		os.Setenv(envLoginPath, tc.loginPath)
		os.Setenv(envVaultKVPath, validKVPath)
		var err error
		_, err = NewVaultClient(setup.k8sClient.CoreV1())
		if err != nil {
			if len(tc.expErrRegEx) == 0 {
				t.Errorf("Test case [%s]: received error while not expected: %v", id, err)
			}
			match, _ := regexp.MatchString(tc.expErrRegEx, err.Error())
			if !match {
				t.Errorf("Test case [%s]: error (%s) does not match expected error (%s)", id, err.Error(), tc.expErrRegEx)
			}
		} else if tc.expErrRegEx != "" {
			t.Errorf("Test case [%s]: expect error: %s but got no error", id, tc.expErrRegEx)
		}
	}
}

func TestGetHSMCredentilals(t *testing.T) {
	setup := PrepareTest(t)
	defer setup.CleanUp()

	testCases := map[string]struct {
		kvPath      string
		expV        [][]byte
		expErrRegEx string
	}{
		"Success": {
			kvPath:      validKVPath,
			expV:        [][]byte{[]byte("TEST_CLIENT_CERT"), []byte("TEST_CLIENT_KEY"), []byte("TEST_SERVER_CERT"), []byte("PIN")},
			expErrRegEx: "",
		},
		"Missing keys": {
			kvPath:      invalidKVPath,
			expV:        [][]byte{},
			expErrRegEx: "no value for PIN in Vault",
		},
	}

	for id, tc := range testCases {
		os.Setenv(envVaultAddr, setup.Server.httpServer.URL)
		os.Setenv(envVaultTLSCertCM, tlsRootCertCmName+"."+tlsRootCertCmNs)
		os.Setenv(envJwtPath, setup.jwtFile.Name())
		os.Setenv(envLoginRole, validRole)
		os.Setenv(envLoginPath, validLoginPath)
		os.Setenv(envVaultKVPath, tc.kvPath)
		var err error
		client, err := NewVaultClient(setup.k8sClient.CoreV1())
		if err != nil {
			t.Errorf("Test case [%s]: error setting up Vault client: %v", id, err)
		}
		v1, v2, v3, v4, err := client.GetHSMCredentilals()
		if err != nil {
			if len(tc.expErrRegEx) == 0 {
				t.Errorf("Test case [%s]: received error while not expected: %v", id, err)
			}
			match, _ := regexp.MatchString(tc.expErrRegEx, err.Error())
			if !match {
				t.Errorf("Test case [%s]: error (%s) does not match expected error (%s)", id, err.Error(), tc.expErrRegEx)
			}
		} else if tc.expErrRegEx != "" {
			t.Errorf("Test case [%s]: expect error: %s but got no error", id, tc.expErrRegEx)
		} else if !bytes.Equal(v1, tc.expV[0]) ||
			!bytes.Equal(v2, tc.expV[1]) ||
			!bytes.Equal(v3, tc.expV[2]) ||
			!bytes.Equal(v4, tc.expV[3]) {
			t.Errorf("Test case [%s]: returned values does not match expectation.", id)
		}
	}
}

func PrepareTest(t *testing.T) *TestSetup {
	ch := make(chan *mockVaultServer)
	go func() {
		// create a test TLS Vault server
		server := newMockVaultServer(t)
		ch <- server
	}()
	tlsServer := <-ch

	certMem := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tlsServer.httpServer.Certificate().Raw})
	client := fake.NewSimpleClientset()
	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tlsRootCertCmName,
			Namespace: tlsRootCertCmNs,
		},
		Data: map[string]string{tlsRootCertKeyInCm: string(certMem)},
	}
	if _, err := client.CoreV1().ConfigMaps(tlsRootCertCmNs).Create(context.TODO(), cm, metav1.CreateOptions{}); err != nil {
		t.Errorf("Failed to insert configmap %v", err)
	}
	cm2 := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tlsRootCertCmName2,
			Namespace: tlsRootCertCmNs2,
		},
		Data: map[string]string{tlsRootCertKeyInCm: string(certMem)},
	}
	if _, err := client.CoreV1().ConfigMaps(tlsRootCertCmNs2).Create(context.TODO(), cm2, metav1.CreateOptions{}); err != nil {
		t.Errorf("Failed to insert configmap %v", err)
	}
	invalidCM := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      invalidTLSRootCertCmName,
			Namespace: tlsRootCertCmNs,
		},
		Data: map[string]string{tlsRootCertKeyInCm: "invalidcertcontent"},
	}
	if _, err := client.CoreV1().ConfigMaps(tlsRootCertCmNs).Create(context.TODO(), invalidCM, metav1.CreateOptions{}); err != nil {
		t.Errorf("Failed to insert configmap %v", err)
	}

	jwtFile, err := ioutil.TempFile("", "jwt")
	if err != nil {
		t.Fatalf("Failed to create tmp jwt file: %v", err)
	}

	if _, err := jwtFile.Write([]byte(validJWT)); err != nil {
		t.Fatalf("Failed to write jwt to tmp file: %v", err)
	}

	invalidJwtFile, err := ioutil.TempFile("", "invalidjwt")
	if err != nil {
		t.Fatalf("Failed to create tmp invalid jwt file: %v", err)
	}

	if _, err := invalidJwtFile.Write([]byte(invalidJWT)); err != nil {
		t.Fatalf("Failed to write invalid jwt to tmp file: %v", err)
	}

	// Set default configuration for the tests. Each test can override the configuration.
	os.Setenv(envVaultAddr, tlsServer.httpServer.URL)
	os.Setenv(envVaultTLSCertCM, tlsRootCertCmName+"."+tlsRootCertCmNs)
	os.Setenv(envJwtPath, jwtFile.Name())
	os.Setenv(envLoginRole, validRole)
	os.Setenv(envLoginPath, validLoginPath)

	return &TestSetup{
		Server:         tlsServer,
		jwtFile:        jwtFile,
		invalidJwtFile: invalidJwtFile,
		k8sClient:      client,
	}
}

func (s *TestSetup) CleanUp() {
	s.Server.httpServer.Close()
	os.Remove(s.jwtFile.Name())
	os.Remove(s.invalidJwtFile.Name())
}

// newMockVaultServer creates a mock Vault server for testing purpose.
// token: required access token
func newMockVaultServer(t *testing.T) *mockVaultServer {
	vaultServer := &mockVaultServer{
		loginRole:  validRole,
		token:      validJWT,
		loginResp:  loginResp,
		loginResp2: loginResp2,
		kvResp:     kvResp,
		kvResp2:    kvResp2,
	}

	handler := http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/v1/" + validLoginPath:
			body, err := ioutil.ReadAll(req.Body)
			if err != nil {
				resp.WriteHeader(http.StatusBadRequest)
				return
			}
			var objmap map[string]json.RawMessage
			if err := json.Unmarshal(body, &objmap); err != nil {
				resp.WriteHeader(http.StatusBadRequest)
				resp.Write([]byte("Unable to unmarchal the message body."))
				return
			}
			var role, jwt string
			if err := json.Unmarshal(objmap["role"], &role); err != nil {
				resp.WriteHeader(http.StatusBadRequest)
				resp.Write([]byte("Unable to unmarchal the role field."))
				return
			}
			if err := json.Unmarshal(objmap["jwt"], &jwt); err != nil {
				resp.WriteHeader(http.StatusBadRequest)
				resp.Write([]byte("Unable to unmarchal the jwt field."))
				return
			}

			if vaultServer.loginRole != role {
				resp.WriteHeader(http.StatusBadRequest)
				return
			}
			if vaultServer.token != jwt {
				resp.WriteHeader(http.StatusBadRequest)
				return
			}
			resp.Header().Set("Content-Type", "application/json")
			resp.Write([]byte(vaultServer.loginResp))

		case "/v1/" + validLoginPath2:
			body, err := ioutil.ReadAll(req.Body)
			if err != nil {
				resp.WriteHeader(http.StatusBadRequest)
				return
			}
			var objmap map[string]json.RawMessage
			if err := json.Unmarshal(body, &objmap); err != nil {
				resp.WriteHeader(http.StatusBadRequest)
				resp.Write([]byte("Unable to unmarchal the message body."))
				return
			}
			var role, jwt string
			if err := json.Unmarshal(objmap["role"], &role); err != nil {
				resp.WriteHeader(http.StatusBadRequest)
				resp.Write([]byte("Unable to unmarchal the role field."))
				return
			}
			if err := json.Unmarshal(objmap["jwt"], &jwt); err != nil {
				resp.WriteHeader(http.StatusBadRequest)
				resp.Write([]byte("Unable to unmarchal the jwt field."))
				return
			}

			if vaultServer.loginRole != role {
				resp.WriteHeader(http.StatusBadRequest)
				return
			}
			if vaultServer.token != jwt {
				resp.WriteHeader(http.StatusBadRequest)
				return
			}
			resp.Header().Set("Content-Type", "application/json")
			resp.Write([]byte(vaultServer.loginResp2))

		case "/v1/" + validKVPath:
			resp.Header().Set("Content-Type", "application/json")
			resp.Write([]byte(vaultServer.kvResp))

		case "/v1/" + invalidKVPath:
			resp.Header().Set("Content-Type", "application/json")
			resp.Write([]byte(vaultServer.kvResp2))

		default:
			resp.WriteHeader(http.StatusNotFound)
		}
	})

	vaultServer.httpServer = httptest.NewTLSServer(handler)

	t.Logf("Serving Vault at: %v", vaultServer.httpServer.URL)

	return vaultServer
}
