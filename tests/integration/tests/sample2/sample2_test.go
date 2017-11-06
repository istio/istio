// Copyright 2017 Istio Authors
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

package sample2

import (
	"flag"
	"net/http"
	"os"
	"testing"

	"log"

	"istio.io/istio/tests/integration/framework"
	env "istio.io/istio/tests/integration/framework/environment"
)

const (
	mixerEnvoyEnv   = "mixer_envoy_env"
	sidecarEndpoint = "http://localhost:9090/echo"
	metricsEndpoint = "http://localhost:42422/metrics"
	testID          = "sample2_test"
)

var (
	testFW *framework.IstioTestFramework
)

func TestSample2(t *testing.T) {
	log.Printf("Running %s", testFW.TestID)
	client := &http.Client{}
	req, _ := http.NewRequest(http.MethodGet, sidecarEndpoint, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("error when do request: %s", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("response code is not 200: %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, metricsEndpoint, nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("error when do request: %s", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("response code is not 200: %d", resp.StatusCode)
	}

	log.Printf("%s succeeded!", testFW.TestID)
}

func TestMain(m *testing.M) {
	flag.Parse()
	testFW = framework.NewIstioTestFramework(env.NewMixerEnvoyEnv(mixerEnvoyEnv), testID)
	res := testFW.RunTest(m)
	log.Printf("Test result %d in env %s", res, mixerEnvoyEnv)
	os.Exit(res)
}
