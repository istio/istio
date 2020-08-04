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

package caclient

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"testing"
)

// vaultAuthHeaderName is the name of the header containing the token.
const vaultAuthHeaderName = "X-Vault-Token"

var (
	vaultLoginResp = `
	{
	  "auth": {
		  "client_token": "fake-vault-token" 
	  }
	}
  `
	vaultSignResp = `
	{
	  "data": {
		  "certificate": "fake-certificate", 
		  "ca_chain": ["fake-ca1", "fake-ca2"] 
	  }
	}
  `
	fakeCert = []string{"fake-certificate\n", "fake-ca1\n", "fake-ca2\n"}
)

type mockVaultServer struct {
	httpServer     *httptest.Server
	loginRole      string
	token          string
	vaultLoginResp string
	vaultSignResp  string
}

type clientConfig struct {
	tls              bool
	tlsCert          []byte
	vaultAddr        string
	vaultLoginRole   string
	vaultLoginPath   string
	vaultSignCsrPath string
	clientToken      string
	csr              []byte
}

type loginRequest struct {
	Jwt  string `json:"jwt"`
	Role string `json:"role"`
}

type signRequest struct {
	Format string `json:"format"`
	Csr    string `json:"csr"`
}

func TestClientOnMockVaultCA(t *testing.T) {
	testCases := map[string]struct {
		cliConfig    clientConfig
		expectedCert []string
		expectedErr  string
	}{
		"Valid certs 1": {
			cliConfig: clientConfig{tls: false, tlsCert: []byte{}, vaultLoginPath: "login",
				vaultSignCsrPath: "sign", clientToken: "fake-client-token", csr: []byte{01}},
			expectedCert: fakeCert,
			expectedErr:  "",
		},
		"Valid certs 1 (TLS)": {
			cliConfig: clientConfig{tls: true, vaultLoginPath: "login", vaultSignCsrPath: "sign",
				clientToken: "fake-client-token", csr: []byte{01}},
			expectedCert: fakeCert,
			expectedErr:  "",
		},
		"Wrong Vault addr": {
			cliConfig: clientConfig{tls: false, tlsCert: []byte{}, vaultAddr: "wrong-vault-addr",
				vaultLoginPath: "login", vaultSignCsrPath: "wrong-sign-path",
				clientToken: "fake-client-token", csr: []byte{01}},
			expectedCert: nil,
			expectedErr:  "failed to login Vault",
		},
		"Wrong login path": {
			cliConfig: clientConfig{tls: false, tlsCert: []byte{}, vaultLoginPath: "wrong-login-path",
				vaultSignCsrPath: "sign", clientToken: "fake-client-token", csr: []byte{01}},
			expectedCert: nil,
			expectedErr:  "failed to login Vault",
		},
		"Wrong client token": {
			cliConfig: clientConfig{tls: false, tlsCert: []byte{}, vaultLoginPath: "login",
				vaultSignCsrPath: "sign", clientToken: "wrong-client-token", csr: []byte{01}},
			expectedCert: nil,
			expectedErr:  "failed to login Vault",
		},
		"Wrong sign path": {
			cliConfig: clientConfig{tls: false, tlsCert: []byte{}, vaultLoginPath: "login",
				vaultSignCsrPath: "wrong-sign-path", clientToken: "fake-client-token", csr: []byte{01}},
			expectedCert: nil,
			expectedErr:  "failed to sign CSR",
		},
	}

	ch := make(chan *mockVaultServer)
	go func() {
		// create a test TLS Vault server
		server := newMockVaultServer(t, true, "", "fake-client-token", vaultLoginResp, vaultSignResp)
		ch <- server
	}()
	s1 := <-ch
	defer s1.httpServer.Close()

	go func() {
		// create a test non-TLS Vault server
		server := newMockVaultServer(t, false, "", "fake-client-token", vaultLoginResp, vaultSignResp)
		ch <- server
	}()
	s2 := <-ch
	defer s2.httpServer.Close()

	for id, tc := range testCases {
		if len(tc.cliConfig.vaultAddr) == 0 {
			// If the address of Vault is not set by the test case, use that of the test server.
			if tc.cliConfig.tls {
				tc.cliConfig.vaultAddr = s1.httpServer.URL
			} else {
				tc.cliConfig.vaultAddr = s2.httpServer.URL
			}
		}
		if tc.cliConfig.tls {
			tc.cliConfig.tlsCert = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s1.httpServer.Certificate().Raw})
			if tc.cliConfig.tlsCert == nil {
				t.Errorf("invalid TLS certificate")
			}
		}
		cli, err := NewVaultClient(tc.cliConfig.tls, tc.cliConfig.tlsCert, tc.cliConfig.vaultAddr, tc.cliConfig.vaultLoginRole,
			tc.cliConfig.vaultLoginPath, tc.cliConfig.vaultSignCsrPath)
		if err != nil {
			t.Errorf("Test case [%s]: failed to create ca client: %v", id, err)
		}

		resp, err := cli.CSRSign(context.Background(), "12345678-1234-1234-1234-123456789012",
			tc.cliConfig.csr, tc.cliConfig.clientToken, 1)
		if err != nil {
			match, _ := regexp.MatchString(tc.expectedErr+".+", err.Error())
			if !match {
				t.Errorf("Test case [%s]: error (%s) does not match expected error (%s)", id, err.Error(), tc.expectedErr)
			}
		} else {
			if tc.expectedErr != "" {
				t.Errorf("Test case [%s]: expect error: %s but got no error", id, tc.expectedErr)
			} else if !reflect.DeepEqual(resp, tc.expectedCert) {
				t.Errorf("Test case [%s]: resp: got %+v, expected %v", id, resp, tc.expectedCert)
			}
		}
	}
}

// newMockVaultServer creates a mock Vault server for testing purpose.
// token: required access token
func newMockVaultServer(t *testing.T, tls bool, loginRole, token, loginResp, signResp string) *mockVaultServer {
	vaultServer := &mockVaultServer{
		loginRole:      loginRole,
		token:          token,
		vaultLoginResp: loginResp,
		vaultSignResp:  signResp,
	}

	handler := http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		t.Logf("request: %+v", *req)
		switch req.URL.Path {
		case "/v1/login":
			t.Logf("%v", req.URL)
			body, err := ioutil.ReadAll(req.Body)
			if err != nil {
				t.Logf("failed to read the request body: %v", err)
				resp.WriteHeader(http.StatusBadRequest)
				return
			}
			loginReq := loginRequest{}
			err = json.Unmarshal(body, &loginReq)
			if err != nil {
				t.Logf("failed to parse the request body: %v", err)
				resp.WriteHeader(http.StatusBadRequest)
				return
			}
			if vaultServer.loginRole != loginReq.Role {
				t.Logf("invalid login role: %v", loginReq.Role)
				resp.WriteHeader(http.StatusBadRequest)
				return
			}
			if vaultServer.token != loginReq.Jwt {
				t.Logf("invalid login token: %v", loginReq.Jwt)
				resp.WriteHeader(http.StatusBadRequest)
				return
			}
			resp.Header().Set("Content-Type", "application/json")
			resp.Write([]byte(vaultServer.vaultLoginResp))

		case "/v1/sign":
			t.Logf("%v", req.URL)
			if req.Header.Get(vaultAuthHeaderName) != "fake-vault-token" {
				t.Logf("the vault token is invalid: %v", req.Header.Get(vaultAuthHeaderName))
				resp.WriteHeader(http.StatusBadRequest)
				return
			}
			body, err := ioutil.ReadAll(req.Body)
			if err != nil {
				t.Logf("failed to read the request body: %v", err)
				resp.WriteHeader(http.StatusBadRequest)
				return
			}
			signReq := signRequest{}
			err = json.Unmarshal(body, &signReq)
			if err != nil {
				t.Logf("failed to parse the request body: %v", err)
				resp.WriteHeader(http.StatusBadRequest)
				return
			}
			if signReq.Format != "pem" {
				t.Logf("invalid sign format: %v", signReq.Format)
				resp.WriteHeader(http.StatusBadRequest)
				return
			}
			if len(signReq.Csr) == 0 {
				t.Logf("empty CSR")
				resp.WriteHeader(http.StatusBadRequest)
				return
			}
			resp.Header().Set("Content-Type", "application/json")
			resp.Write([]byte(vaultServer.vaultSignResp))

		default:
			t.Logf("The request contains invalid path: %v", req.URL)
			resp.WriteHeader(http.StatusNotFound)
		}
	})

	if tls {
		vaultServer.httpServer = httptest.NewTLSServer(handler)
	} else {
		vaultServer.httpServer = httptest.NewServer(handler)
	}

	t.Logf("Serving Vault at: %v", vaultServer.httpServer.URL)

	return vaultServer
}
