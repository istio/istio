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

package test

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"istio.io/istio/security/pkg/stsservice/tokenmanager/google"

	istioEnv "istio.io/istio/pkg/test/env"
	proxyEnv "istio.io/istio/mixer/test/client/env"
	xdsService "istio.io/istio/security/pkg/stsservice/mock"
	stsServer "istio.io/istio/security/pkg/stsservice/server"
	"istio.io/istio/security/pkg/stsservice/tokenmanager"
	tokenBackend "istio.io/istio/security/pkg/stsservice/tokenmanager/google/mock"
)

const (
	// Paths to credentials which will be loaded by proxy. These paths should
	// match bootstrap config in testdata/bootstrap.yaml
	certPath = "/tmp/sts-ca-certificates.crt"
	proxyTokenPath = "/tmp/sts-envoy-token"
)

type Env struct{
	proxySetUp *proxyEnv.TestSetup
	authServer *tokenBackend.AuthorizationServer
	stsServer *stsServer.Server
	xDSServer *grpc.Server
	xDSCb  *xdsService.XDSCallbacks
	ProxyListenerPort int
}

func (e *Env) TearDown() {
	e.authServer.Stop()
	e.xDSServer.GracefulStop()
	e.stsServer.Stop()
	e.proxySetUp.TearDown()
	os.Remove(proxyTokenPath)
}

func getDataFromFile(filePath string, t *testing.T) string {
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read %q", filePath)
	}
	return string(data)
}

// setUpCredentialsInFile prepares initial credential for proxy to load
func setUpCredentialsInFile(path string, cred string) error {
	if path == "" {
		return errors.New("empty file path")
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err = f.WriteString(cred); err != nil {
		return err
	}
	f.Sync()
	return nil
}

// SetUpTest starts Envoy, XDS server, STS server, token manager, and a token service backend.
// Envoy loads a test config that requires token credential to access XDS server.
// That token credential is provisioned by STS server.
// Here is a map between ports and servers
// auth server    : MixerPort
// STS server     : ServerProxyPort
// proxy listener : ClientProxyPort
// XDS server     : DiscoveryPort
// test backend   : BackendPort
// proxy admin    : AdminPort
func SetUpTest(t *testing.T, cb *xdsService.XDSCallbacks) *Env {
	env := &Env{}
	// Set up test environment for Proxy
	proxySetUp := proxyEnv.NewTestSetup(proxyEnv.STSTest, t)
	proxySetUp.SetNoMixer(true)
	proxySetUp.EnvoyTemplate = getDataFromFile("testdata/bootstrap.yaml", t)
	env.proxySetUp = proxySetUp
	// Set up auth server that provides token service
	backend, err := tokenBackend.StartNewServer(t, int(proxySetUp.Ports().MixerPort))
	if err != nil {
		t.Fatalf("failed to start a auth backend: %v", err)
	}
	env.authServer = backend
	// Set up STS server
	stsServer, err := setUpSTS(int(proxySetUp.Ports().ServerProxyPort), backend.URL)
	if err != nil {
		t.Fatalf("failed to start a STS server: %v", err)
	}
	env.stsServer = stsServer

	env.WaitForStsFlowReady(t)

	// Set up XDS server
	env.ProxyListenerPort = int(proxySetUp.Ports().ClientProxyPort)
	ls := &xdsService.DynamicListener{Port: env.ProxyListenerPort}
	env.xDSServer = xdsService.StartXDSServer(t,
		xdsService.XDSConf{Port: int(proxySetUp.Ports().DiscoveryPort),
			CertFile: istioEnv.IstioSrc + "/security/pkg/stsservice/test/testdata/server-certificate.crt",
			KeyFile: istioEnv.IstioSrc + "/security/pkg/stsservice/test/testdata/server-key.key"}, cb, ls)

	// Set up credential files for bootstrap config
	jwtToken := getDataFromFile("testdata/trustworthy-jwt.jwt", t)
	if err := setUpCredentialsInFile(proxyTokenPath, jwtToken); err != nil {
		t.Fatalf("failed to set up token file %s: %v", proxyTokenPath, err)
	}
	caCert := getDataFromFile("testdata/ca-certificate.crt", t)
	if err := setUpCredentialsInFile(certPath, caCert); err != nil {
		t.Fatalf("failed to set up ca certificate file %s: %v", certPath, err)
	}
	return env
}

func (e *Env) StartProxy(t *testing.T) {
	//e.proxySetUp.SetUp()
	if err := e.proxySetUp.SetUp(); err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
}

// WaitForStsFlowReady sends STS requests to STS server using HTTP client, and
// verifies that the STS flow is ready.
func (e *Env) WaitForStsFlowReady(t *testing.T) {
	t.Logf("Check if all servers in the STS flow are up and ready")
	addr, _ := net.ResolveTCPAddr("tcp", fmt.Sprintf("127.0.0.1:%d", e.proxySetUp.Ports().ServerProxyPort))
	stsServerAddress := addr.String()
	hTTPClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				t.Logf("set up server address to dial %s", addr)
				addr = stsServerAddress
				return net.Dial(network, addr)
			},
		},
	}
	// keep sending requests periodically until a success STS response is received
	req := e.genStsReq(t, stsServerAddress)
	for i := 0; i < 20; i++ {
		resp, err := hTTPClient.Do(req)
		if err == nil {
			if resp.StatusCode == http.StatusOK && resp.Header.Get("Content-Type") == "application/json" {
				t.Logf("All servers in the STS flow are up and ready")
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("STS flow is not ready")
}

func (e *Env) genStsReq(t *testing.T, stsAddr string) (req *http.Request) {
	stsQuery := url.Values{}
	stsQuery.Set("grant_type", stsServer.TokenExchangeGrantType)
	stsQuery.Set("resource", "https//:backend.example.com")
	stsQuery.Set("audience", "audience")
	stsQuery.Set("scope", "https://www.googleapis.com/auth/cloud-platform")
	stsQuery.Set("requested_token_type", "urn:ietf:params:oauth:token-type:access_token")
	stsQuery.Set("subject_token", tokenBackend.FakeSubjectToken)
	stsQuery.Set("subject_token_type", stsServer.SubjectTokenType)
	stsQuery.Set("actor_token", "")
	stsQuery.Set("actor_token_type", "")
	stsURL := "http://" + stsAddr + stsServer.TokenPath
	req, _ = http.NewRequest("POST", stsURL, strings.NewReader(stsQuery.Encode()))
	req.Header.Set("Content-Type", stsServer.URLEncodedForm)
	return req
}

func setUpSTS(stsPort int, backendUrl string) (*stsServer.Server, error) {
	// Create token exchange Google plugin
	tokenExchangePlugin, _ := google.CreateTokenManagerPlugin(tokenBackend.FakeTrustDomain, tokenBackend.FakeProjectNum)
	federatedTokenTestingEndpoint := backendUrl + "/v1/identitybindingtoken"
	accessTokenTestingEndpoint := backendUrl + "/v1/projects/-/serviceAccounts/service-%s@gcp-sa-meshdataplane.iam.gserviceaccount.com:generateAccessToken"
	tokenExchangePlugin.SetEndpoints(federatedTokenTestingEndpoint, accessTokenTestingEndpoint)
	// Create token manager
	tm := tokenmanager.CreateTokenManager(tokenmanager.GoogleTokenExchange,
		tokenmanager.Config{TrustDomain: tokenBackend.FakeTrustDomain})
	tm.(*tokenmanager.TokenManager).SetPlugin(tokenExchangePlugin)
	// Create STS server
	addr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("127.0.0.1:%d", stsPort))
	if err != nil {
		return nil, fmt.Errorf("failed to create address %v", err)
	}
	return stsServer.NewServer(stsServer.Config{LocalHostAddr: addr.IP.String(), LocalPort: addr.Port}, tm)
}
