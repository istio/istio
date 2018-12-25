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
	testCsr1 = `
-----BEGIN CERTIFICATE REQUEST-----
MIICojCCAYoCAQAwEzERMA8GA1UEAxMId29ya2xvYWQwggEiMA0GCSqGSIb3DQEB
AQUAA4IBDwAwggEKAoIBAQCtUKHNG598mQ0wo5+AfZhn2yA8HhL1QV0XERJgBU2p
PbH/4yIHq++kugWWbj4REE7OPvKjJRdo8yJ9OpjDXA8s5t7fchdr6BePLF6+GfkQ
ACmnKAziRHMg22Zy+crdVEiyrMAzwujbiBxiI5hcHHB15TX+6lAxaLZJ3BLC4NBd
YHUeEwvuBV4zLLvKSVE6jFQIvxHKk/Nh/sJvvvSIOWmXPgS6raFPKPTDJ3MjFyCU
VEz8/HWyaEptX4C91NQxa7/CIJ/DYXtKVbP+jXGaLrLQUX+2r95H2cU604OfMz2Z
PmYgYUovtb93llwgLKoJk3MjIGEvy4AluGqegrDe5ghfAgMBAAGgSjBIBgkqhkiG
9w0BCQ4xOzA5MDcGA1UdEQQwMC6GLHNwaWZmZTovL2NsdXN0ZXIubG9jYWwvbnMv
ZGVmYXVsdC9zYS9kZWZhdWx0MA0GCSqGSIb3DQEBCwUAA4IBAQCRnzNqI46M1FJL
IWaQsZj7QeJPrPmuwcGzQ5qRlXBmxAe95N+9DKpmiTwU0tOz375EEjXwVYvs1cZT
d75Br1kaAMT70LnPUxvSjlcTNItLwlu6LoH/BuaFa5VL1dKFvjRQC3aKFKD634pX
U82yKWa7kAVPWJAizoz+wf0RIF2KEp0wpd/FPQJaFkAiTrC8rwEhPIfKTLads4HL
5pWcfODn5eMC7+htiteWsfdhK8Bxjz0VyzSs3BbgAHs+LFkIBGkKe0sl/ii96Bik
SQYzPWVk89gu6nKV+fS2pA9C8dAnYOzVu9XXc+PGlcIhjnuS+/P74hN5D3aIGljW
7WsYeEkp
-----END CERTIFICATE REQUEST-----
  `
	testSA1  = "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJrdWJlcm5ldGVzL3NlcnZpY2VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9uYW1lc3BhY2UiOiJkZWZhdWx0Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9zZWNyZXQubmFtZSI6InZhdWx0LWNpdGFkZWwtc2EtdG9rZW4tenRzN3ciLCJrdWJlcm5ldGVzLmlvL3NlcnZpY2VhY2NvdW50L3NlcnZpY2UtYWNjb3VudC5uYW1lIjoidmF1bHQtY2l0YWRlbC1zYSIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2VydmljZS1hY2NvdW50LnVpZCI6IjUyODUxNTNlLWY2ZGItMTFlOC04Y2ZhLTQyMDEwYThhMDAxNCIsInN1YiI6InN5c3RlbTpzZXJ2aWNlYWNjb3VudDpkZWZhdWx0OnZhdWx0LWNpdGFkZWwtc2EifQ.lxNRxmL2oLhh3XGLI7XPdhh2hpdQo02WPq9M8awhuguYExOMa2ToIAROe_ia0RkugLHCIX2jd-gohUcAyUxh5oBIFgeP8QVyu2hXUUVeZQgZLpjsd2nlPRq5CPw-21mXQntbWsmT4kFhQ-BF3m9H-5UDxRb4jt-t5YhQb4PHq-H-i9QN4_7seqLu3RPBjmkhzV-8tqr-baleRby2Kj5s-qWsnMtPcF8kWZ3hzoY9P2nKPSimhNFkv58K15t9gJ-EJTsQhsY-kozYGpfoFEfchw6t-qIBVjH74z3BrlP0edSnDh8UtADnwqArZrgcFzorOZ2bH3mlpiK7jZ0YDI4CqA"
	fakeCert = []string{"fake-certificate", "fake-ca1", "fake-ca2"}
)

type MockVaultServer struct {
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
			cliConfig:    clientConfig{tls: false, tlsCert: []byte{}, vaultLoginPath: "login", vaultSignCsrPath: "sign", clientToken: "fake-client-token", csr: []byte{01}},
			expectedCert: fakeCert,
			expectedErr:  "",
		},
		"Valid certs 1 (TLS)": {
			cliConfig:    clientConfig{tls: true, vaultLoginPath: "login", vaultSignCsrPath: "sign", clientToken: "fake-client-token", csr: []byte{01}},
			expectedCert: fakeCert,
			expectedErr:  "",
		},
		"Wrong Vault addr": {
			cliConfig:    clientConfig{tls: false, tlsCert: []byte{}, vaultAddr: "wrong-vault-addr", vaultLoginPath: "login", vaultSignCsrPath: "wrong-sign-path", clientToken: "fake-client-token", csr: []byte{01}},
			expectedCert: nil,
			expectedErr:  "failed to login Vault",
		},
		"Wrong login path": {
			cliConfig:    clientConfig{tls: false, tlsCert: []byte{}, vaultLoginPath: "wrong-login-path", vaultSignCsrPath: "sign", clientToken: "fake-client-token", csr: []byte{01}},
			expectedCert: nil,
			expectedErr:  "failed to login Vault",
		},
		"Wrong client token": {
			cliConfig:    clientConfig{tls: false, tlsCert: []byte{}, vaultLoginPath: "login", vaultSignCsrPath: "sign", clientToken: "wrong-client-token", csr: []byte{01}},
			expectedCert: nil,
			expectedErr:  "failed to login Vault",
		},
		"Wrong sign path": {
			cliConfig:    clientConfig{tls: false, tlsCert: []byte{}, vaultLoginPath: "login", vaultSignCsrPath: "wrong-sign-path", clientToken: "fake-client-token", csr: []byte{01}},
			expectedCert: nil,
			expectedErr:  "failed to sign CSR",
		},
	}

	for id, tc := range testCases {
		ch := make(chan *MockVaultServer)
		go func() {
			// create a test Vault server
			server := NewMockVaultServer(t, tc.cliConfig.tls, "", "fake-client-token", vaultLoginResp, vaultSignResp)
			ch <- server
		}()
		s := <-ch
		defer s.httpServer.Close()

		if len(tc.cliConfig.vaultAddr) == 0 {
			// If the address of Vault is not set by the test case, use that of the test server.
			tc.cliConfig.vaultAddr = s.httpServer.URL
		}
		if tc.cliConfig.tls {
			tc.cliConfig.tlsCert = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.httpServer.Certificate().Raw})
			if tc.cliConfig.tlsCert == nil {
				t.Errorf("invalid TLS certificate")
			}
		}
		cli, err := NewVaultClient1(tc.cliConfig.tls, tc.cliConfig.tlsCert, tc.cliConfig.vaultAddr, tc.cliConfig.vaultLoginRole,
			tc.cliConfig.vaultLoginPath, tc.cliConfig.vaultSignCsrPath)
		if err != nil {
			t.Errorf("Test case [%s]: failed to create ca client: %v", id, err)
		}

		resp, err := cli.CSRSign(context.Background(), tc.cliConfig.csr, tc.cliConfig.clientToken, 1)
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

func TestClientOnExampleHttpVaultCA(t *testing.T) {
	testCases := map[string]struct {
		cliConfig clientConfig
	}{
		"Valid certs 1": {
			cliConfig: clientConfig{vaultAddr: "http://35.247.45.173:8200", vaultLoginPath: "auth/kubernetes/login", vaultLoginRole: "istio-cert", vaultSignCsrPath: "istio_ca/sign/istio-pki-role", clientToken: testSA1, csr: []byte(testCsr1)},
		},
	}

	for id, tc := range testCases {
		var vaultAddr string
		vaultAddr = tc.cliConfig.vaultAddr
		cli, err := NewVaultClient1(false, []byte{}, vaultAddr, tc.cliConfig.vaultLoginRole,
			tc.cliConfig.vaultLoginPath, tc.cliConfig.vaultSignCsrPath)
		if err != nil {
			t.Errorf("Test case [%s]: failed to create ca client: %v", id, err)
		}

		resp, err := cli.CSRSign(context.Background(), tc.cliConfig.csr, tc.cliConfig.clientToken, 1)
		if err != nil {
			t.Errorf("Test case [%s]:  error (%v) is not expected", id, err.Error())
		} else {
			if len(resp) != 3 {
				t.Errorf("Test case [%s]: the certificate chain length (%v) is unexpected", id, len(resp))
			}
		}
	}
}

// NewMockVaultServer creates a mock Vault server for testing purpose.
// token: required access token
func NewMockVaultServer(t *testing.T, tls bool, loginRole, token, loginResp, signResp string) *MockVaultServer {
	vaultServer := &MockVaultServer{
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
			break
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
			if "pem" != signReq.Format {
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
			break
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
