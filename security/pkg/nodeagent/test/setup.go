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
	"io/ioutil"
	"os"
	"testing"

	proxyEnv "istio.io/istio/mixer/test/client/env"
	istioEnv "istio.io/istio/pkg/test/env"
)

const (
	proxyTokenPath = "/tmp/sts-envoy-token.jwt"
)

type Env struct {
	ProxySetup *proxyEnv.TestSetup

	// SDS server

	// CSR server
}

func (e *Env) TearDown() {
	// Stop proxy first, otherwise XDS stream is still alive and server's graceful
	// stop will be blocked.
	e.ProxySetup.TearDown()
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

// SetupTest starts Envoy, a dummy backend
func SetupTest(t *testing.T, testID uint16) *Env {
	// Set up credential files for bootstrap config
	jwtToken := getDataFromFile(istioEnv.IstioSrc+"/security/pkg/stsservice/test/testdata/trustworthy-jwt.jwt", t)
	if err := WriteDataToFile(proxyTokenPath, jwtToken); err != nil {
		t.Fatalf("failed to set up token file %s: %v", proxyTokenPath, err)
	}

	env := &Env{}
	// Set up test environment for Proxy
	proxySetup := proxyEnv.NewTestSetup(testID, t)
	proxySetup.SetNoMixer(true)
	proxySetup.EnvoyTemplate = getDataFromFile(istioEnv.IstioSrc+"/security/pkg/stsservice/test/testdata/bootstrap.yaml", t)
	env.ProxySetup = proxySetup

	return env
}


