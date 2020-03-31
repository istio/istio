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
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"testing"
	"time"

	proxyEnv "istio.io/istio/mixer/test/client/env"
	"istio.io/istio/pkg/spiffe"
	istioEnv "istio.io/istio/pkg/test/env"
	"istio.io/istio/security/pkg/nodeagent/cache"
	citadel "istio.io/istio/security/pkg/nodeagent/caclient/providers/citadel"
	"istio.io/istio/security/pkg/nodeagent/sds"
	"istio.io/istio/security/pkg/nodeagent/secretfetcher"
	caserver "istio.io/istio/security/pkg/nodeagent/test/mock"
)

const (
	proxyTokenPath = "/tmp/sts-envoy-token.jwt"
	sdsPath        = "/tmp/sdstestudspath"
)

// Env manages test setup and teardown.
type Env struct {
	ProxySetup           *proxyEnv.TestSetup
	OutboundListenerPort int
	// SDS server
	SDSServer *sds.Server
	// CA server
	CAServer *caserver.CAServer
}

// TearDown tears down all components.
func (e *Env) TearDown() {
	// Stop proxy first, otherwise XDS stream is still alive and server's graceful
	// stop will be blocked.
	e.ProxySetup.TearDown()
	e.SDSServer.Stop()
	e.CAServer.GRPCServer.GracefulStop()
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

// SetupTest starts Envoy, SDS server, CA server and a dummy backend.
// The test allow HTTP request flow.
//                                                     CA server
//                                                         |
//                                           +---------SDS server-------+
//                                           |                          |
// HTTP request->outbound listener->outbound TLS cluster<-mTLS->inbound TLS listener->inbound cluster->backend
func SetupTest(t *testing.T, testID uint16) *Env {
	// Set up credential files for bootstrap config
	jwtToken := getDataFromFile(istioEnv.IstioSrc+"/security/pkg/nodeagent/test/testdata/jwt-token.jwt", t)
	if err := WriteDataToFile(proxyTokenPath, jwtToken); err != nil {
		t.Fatalf("failed to set up token file %s: %v", proxyTokenPath, err)
	}

	env := &Env{}
	// Set up test environment for Proxy
	proxySetup := proxyEnv.NewTestSetup(testID, t)
	proxySetup.SetNoMixer(true)
	proxySetup.EnvoyTemplate = getDataFromFile(istioEnv.IstioSrc+"/security/pkg/nodeagent/test/testdata/bootstrap.yaml", t)
	env.ProxySetup = proxySetup
	env.OutboundListenerPort = int(proxySetup.Ports().ClientProxyPort)

	env.DumpPortMap(t)
	ca, err := caserver.NewCAServer(int(proxySetup.Ports().MixerPort))
	if err != nil {
		t.Fatalf("failed to start CA server: %+v", err)
	}
	env.CAServer = ca
	env.StartSDSServer(t)
	return env
}

// DumpPortMap dumps port allocation status
// outbound listener      : ClientProxyPort
// inbound listener       : ServerProxyPort
// test backend           : BackendPort
// proxy admin            : AdminPort
// CSR server             : MixerPort
func (e *Env) DumpPortMap(t *testing.T) {
	log.Printf("\n\tport allocation status\t\t\t\n"+
		"outbound listener\t\t:\t%d\n"+
		"inbound listener\t\t:\t%d\n"+
		"test backend\t\t\t:\t%d\n"+
		"proxy admin\t\t\t:\t%d\n"+
		"CSR server\t\t\t:\t%d\n", e.ProxySetup.Ports().ClientProxyPort,
		e.ProxySetup.Ports().ServerProxyPort, e.ProxySetup.Ports().BackendPort,
		e.ProxySetup.Ports().AdminPort, e.ProxySetup.Ports().MixerPort)
}

// StartProxy starts proxy.
func (e *Env) StartProxy(t *testing.T) {
	if err := e.ProxySetup.SetUp(); err != nil {
		t.Fatalf("failed to start proxy: %v", err)
	}
	log.Println("proxy is running...")
}

// StartSDSServer starts SDS server
func (e *Env) StartSDSServer(t *testing.T) {
	serverOptions := sds.Options{
		WorkloadUDSPath:   sdsPath,
		UseLocalJWT:       true,
		JWTPath:           proxyTokenPath,
		CAEndpoint:        fmt.Sprintf("127.0.0.1:%d", e.ProxySetup.Ports().MixerPort),
		EnableWorkloadSDS: true,
		RecycleInterval:   5 * time.Minute,
	}

	caClient, err := citadel.NewCitadelClient(serverOptions.CAEndpoint, false, nil)
	if err != nil {
		log.Fatalf("failed to create CA client: %+v", err)
	}
	secretFetcher := &secretfetcher.SecretFetcher{
		UseCaClient: true,
		CaClient:    caClient,
	}
	opt := cache.Options{
		TrustDomain:      spiffe.GetTrustDomain(),
		RotationInterval: 5 * time.Minute,
	}
	workloadSecretCache := cache.NewSecretCache(secretFetcher, sds.NotifyProxy, opt)
	sdsServer, err := sds.NewServer(serverOptions, workloadSecretCache, nil)
	if err != nil {
		t.Fatalf("failed to start SDS server: %+v", err)
	}
	e.SDSServer = sdsServer
}
