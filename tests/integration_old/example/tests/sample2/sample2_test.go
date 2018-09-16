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
	"log"
	"net/http"
	"os"
	"testing"

	"istio.io/istio/tests/integration_old/component/mixer"
	"istio.io/istio/tests/integration_old/component/proxy"
	"istio.io/istio/tests/integration_old/example/environment/mixerEnvoyEnv"
	"istio.io/istio/tests/integration_old/framework"
)

// This sample shows how to reuse a test environment in different test cases.
// Two test cases are using the same environment.
// TestEcho verifies the proxy routing behavior.
// TestMetrics verifies the metrics endpoint provided by mixer.
// The environment is brought up at the beginning, followed by two test cases
// and will be teared down after both tests finish.

const (
	mixerEnvoyEnvName = "mixer_envoy_env"
	testID            = "sample2_test"
)

var (
	testEM *framework.TestEnvManager
)

func TestEcho(t *testing.T) {
	log.Printf("Running %s: echo test", testEM.GetID())

	sideCarStatus, ok := testEM.GetEnv().GetComponents()[1].GetStatus().(proxy.LocalCompStatus)
	if !ok {
		t.Fatalf("failed to get side car proxy status")
	}
	url := sideCarStatus.SideCarEndpoint

	client := &http.Client{}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("error when do request: %s", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("response code is not 200: %d", resp.StatusCode)
	}
	log.Printf("%s echo test succeeded!", testEM.GetID())
}

func TestMetrics(t *testing.T) {
	log.Printf("Running %s: metrics test", testEM.GetID())

	mixerStatus, ok := testEM.GetEnv().GetComponents()[2].GetStatus().(mixer.LocalCompStatus)
	if !ok {
		t.Fatalf("failed to get status of mixer component")
	}
	req, _ := http.NewRequest(http.MethodGet, mixerStatus.MetricsEndpoint, nil)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("error when do request: %s", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("response code is not 200: %d", resp.StatusCode)
	}

	config := testEM.GetEnv().GetComponents()[2].GetConfig()
	mixerConfig, ok := config.(mixer.LocalCompConfig)
	if !ok {
		t.Fatalf("failed to get config of mixer component")
	}
	log.Printf("mixer configfile Dir is: %s", mixerConfig.ConfigFileDir)
	log.Printf("%s metrics test succeeded!", testEM.GetID())
}

func TestMain(m *testing.M) {
	flag.Parse()
	testEM = framework.NewTestEnvManager(mixerEnvoyEnv.NewMixerEnvoyEnv(mixerEnvoyEnvName), testID)
	res := testEM.RunTest(m)
	log.Printf("Test result %d in env %s", res, mixerEnvoyEnvName)
	os.Exit(res)
}
