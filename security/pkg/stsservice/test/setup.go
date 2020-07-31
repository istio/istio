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

package test

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
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
	xdsService "istio.io/istio/security/pkg/stsservice/mock"
	stsServer "istio.io/istio/security/pkg/stsservice/server"
	"istio.io/istio/security/pkg/stsservice/tokenmanager"
	tokenBackend "istio.io/istio/security/pkg/stsservice/tokenmanager/google/mock"
)

const (
	jwtToken = "thisisafakejwt"
)

// Env manages test setup and teardown.
type Env struct {
	ProxySetup *istioEnv.TestSetup
	AuthServer *tokenBackend.AuthorizationServer

	stsServer           *stsServer.Server
	xdsServer           *grpc.Server
	ProxyListenerPort   int
	initialToken        string // initial token is sent to STS server for token exchange
	tokenExchangePlugin *google.Plugin
}

// TearDown shuts down all the components.
func (e *Env) TearDown() {
	// Stop proxy first, otherwise XDS stream is still alive and server's graceful
	// stop will be blocked.
	e.ProxySetup.TearDown()
	_ = e.AuthServer.Stop()
	e.xdsServer.GracefulStop()
	e.stsServer.Stop()
}

func getDataFromFile(filePath string, t *testing.T) string {
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read %q", filePath)
	}
	return string(data)
}

// WriteDataToFile writes data into file
func WriteDataToFile(path string, content string) error {
	if path == "" {
		return errors.New("empty file path")
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err = f.WriteString(content); err != nil {
		return err
	}
	_ = f.Sync()
	return nil
}

// SetupTest starts Envoy, XDS server, STS server, token manager, and a token service backend.
// Envoy loads a test config that requires token credential to access XDS server.
// That token credential is provisioned by STS server.
// enableCache indicates whether to enable token cache at STS server side.
// Here is a map between ports and servers
// auth server            : ExtraPort
// STS server             : STSPort
// Dynamic proxy listener : ClientProxyPort
// Static proxy listener  : TCPProxyPort
// XDS server             : DiscoveryPort
// test backend           : BackendPort
// proxy admin            : AdminPort
func SetupTest(t *testing.T, cb *xdsService.XDSCallbacks, testID uint16, enableCache bool) *Env {
	env := &Env{
		initialToken: jwtToken,
	}
	// Set up test environment for Proxy
	proxySetup := istioEnv.NewTestSetup(testID, t)
	proxySetup.EnvoyTemplate = getDataFromFile(istioEnv.IstioSrc+"/security/pkg/stsservice/test/testdata/bootstrap.yaml", t)
	// Set up credential files for bootstrap config
	if err := WriteDataToFile(proxySetup.JWTTokenPath(), jwtToken); err != nil {
		t.Fatalf("failed to set up token file %s: %v", proxySetup.JWTTokenPath(), err)
	}
	caCert := getDataFromFile(istioEnv.IstioSrc+"/security/pkg/stsservice/test/testdata/ca-certificate.crt", t)
	if err := WriteDataToFile(proxySetup.CACertPath(), caCert); err != nil {
		t.Fatalf("failed to set up ca certificate file %s: %v", proxySetup.CACertPath(), err)
	}

	env.ProxySetup = proxySetup
	env.DumpPortMap(t)
	// Set up auth server that provides token service
	backend, err := tokenBackend.StartNewServer(t, tokenBackend.Config{
		SubjectToken: jwtToken,
		Port:         int(proxySetup.Ports().ExtraPort),
		AccessToken:  cb.ExpectedToken(),
	})
	if err != nil {
		t.Fatalf("failed to start a auth backend: %v", err)
	}
	env.AuthServer = backend

	// Set up STS server
	stsServer, plugin, err := setupSTS(int(proxySetup.Ports().STSPort), backend.URL, enableCache)
	if err != nil {
		t.Fatalf("failed to start a STS server: %v", err)
	}
	env.stsServer = stsServer
	env.tokenExchangePlugin = plugin

	// Make sure STS server and auth backend are running
	env.WaitForStsFlowReady(t)

	// Set up XDS server
	env.ProxyListenerPort = int(proxySetup.Ports().ClientProxyPort)
	ls := &xdsService.DynamicListener{Port: env.ProxyListenerPort}
	xds, err := xdsService.StartXDSServer(
		xdsService.XDSConf{Port: int(proxySetup.Ports().DiscoveryPort),
			CertFile: istioEnv.IstioSrc + "/security/pkg/stsservice/test/testdata/server-certificate.crt",
			KeyFile:  istioEnv.IstioSrc + "/security/pkg/stsservice/test/testdata/server-key.key"}, cb, ls, true)
	if err != nil {
		t.Fatalf("failed to start XDS server: %v", err)
	}
	env.xdsServer = xds

	return env
}

// DumpPortMap dumps port allocation status
// auth server            : ExtraPort
// STS server             : STSPort
// Dynamic proxy listener : ClientProxyPort
// Static proxy listener  : TCPProxyPort
// XDS server             : DiscoveryPort
// test backend           : BackendPort
// proxy admin            : AdminPort
func (e *Env) DumpPortMap(t *testing.T) {
	log.Printf("\n\tport allocation status\t\t\t\n"+
		"auth server\t\t:\t%d\n"+
		"STS server\t\t:\t%d\n"+
		"dynamic listener port\t:\t%d\n"+
		"static listener port\t:\t%d\n"+
		"XDS server\t\t:\t%d\n"+
		"test backend\t\t:\t%d\n"+
		"proxy admin\t\t:\t%d", e.ProxySetup.Ports().ExtraPort,
		e.ProxySetup.Ports().STSPort, e.ProxySetup.Ports().ClientProxyPort,
		e.ProxySetup.Ports().TCPProxyPort, e.ProxySetup.Ports().DiscoveryPort,
		e.ProxySetup.Ports().BackendPort, e.ProxySetup.Ports().AdminPort)
}

// ClearTokenCache removes cached token in token exchange plugin.
func (e *Env) ClearTokenCache() {
	e.tokenExchangePlugin.ClearCache()
}

// StartProxy starts proxy.
func (e *Env) StartProxy(t *testing.T) {
	if err := e.ProxySetup.SetUp(); err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	log.Println("proxy is running...")
}

// WaitForStsFlowReady sends STS requests to STS server using HTTP client, and
// verifies that the STS flow is ready.
func (e *Env) WaitForStsFlowReady(t *testing.T) {
	t.Logf("%s check if all servers in the STS flow are up and ready", time.Now().String())
	addr, _ := net.ResolveTCPAddr("tcp", fmt.Sprintf("127.0.0.1:%d", e.ProxySetup.Ports().STSPort))
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
	req := e.genStsReq(stsServerAddress)
	for i := 0; i < 20; i++ {
		resp, err := hTTPClient.Do(req)
		if err == nil {
			if resp.StatusCode == http.StatusOK && resp.Header.Get("Content-Type") == "application/json" {
				t.Logf("%s all servers in the STS flow are up and ready", time.Now().String())
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("STS flow is not ready")
}

func (e *Env) genStsReq(stsAddr string) (req *http.Request) {
	stsQuery := url.Values{}
	stsQuery.Set("grant_type", stsServer.TokenExchangeGrantType)
	stsQuery.Set("resource", "https//:backend.example.com")
	stsQuery.Set("audience", "audience")
	stsQuery.Set("scope", "https://www.googleapis.com/auth/cloud-platform")
	stsQuery.Set("requested_token_type", "urn:ietf:params:oauth:token-type:access_token")
	stsQuery.Set("subject_token", e.initialToken)
	stsQuery.Set("subject_token_type", stsServer.SubjectTokenType)
	stsQuery.Set("actor_token", "")
	stsQuery.Set("actor_token_type", "")
	stsURL := "http://" + stsAddr + stsServer.TokenPath
	req, _ = http.NewRequest("POST", stsURL, strings.NewReader(stsQuery.Encode()))
	req.Header.Set("Content-Type", stsServer.URLEncodedForm)
	return req
}

func setupSTS(stsPort int, backendURL string, enableCache bool) (*stsServer.Server, *google.Plugin, error) {
	// Create token exchange Google plugin
	tokenExchangePlugin, _ := google.CreateTokenManagerPlugin(nil, tokenBackend.FakeTrustDomain,
		tokenBackend.FakeProjectNum, tokenBackend.FakeGKEClusterURL, enableCache)
	federatedTokenTestingEndpoint := backendURL + "/v1/identitybindingtoken"
	accessTokenTestingEndpoint := backendURL + "/v1/projects/-/serviceAccounts/service-%s@gcp-sa-meshdataplane.iam.gserviceaccount.com:generateAccessToken"
	tokenExchangePlugin.SetEndpoints(federatedTokenTestingEndpoint, accessTokenTestingEndpoint)
	// Create token manager
	tm := tokenmanager.CreateTokenManager(tokenmanager.GoogleTokenExchange,
		tokenmanager.Config{TrustDomain: tokenBackend.FakeTrustDomain})
	tm.(*tokenmanager.TokenManager).SetPlugin(tokenExchangePlugin)
	// Create STS server
	addr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("127.0.0.1:%d", stsPort))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create address %v", err)
	}
	server, err := stsServer.NewServer(stsServer.Config{LocalHostAddr: addr.IP.String(), LocalPort: addr.Port}, tm)
	return server, tokenExchangePlugin, err
}
