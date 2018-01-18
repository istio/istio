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

package sample1

import (
	"bytes"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"
	"testing"

	appOnlyEnv "istio.io/istio/tests/integration/example/environment/appOnlyEnv"
	mixerEnvoyEnv "istio.io/istio/tests/integration/example/environment/mixerEnvoyEnv"
	"istio.io/istio/tests/integration/framework"
)

const (
	appOnlyEnvName    = "app_only_env"
	mixerEnvoyEnvName = "mixer_envoy_env"
	serverEndpoint    = "http://localhost:8080/"
	sidecarEndpoint   = "http://localhost:9090/echo"
	testID            = "sample1_test"
)

var (
	testEM *framework.TestEnvManager
)

func TestSample1(t *testing.T) {
	log.Printf("Running %s", testEM.TestID)

	var url string
	if testEM.TestEnv.GetName() == appOnlyEnvName {
		url = serverEndpoint
	} else if testEM.TestEnv.GetName() == mixerEnvoyEnvName {
		url = sidecarEndpoint
	}

	body := strings.NewReader(testID)
	client := &http.Client{}
	req, _ := http.NewRequest(http.MethodPost, url, body)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("error when do request: %s", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("response code is not 200: %d", resp.StatusCode)
	}

	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	bodyReceived := buf.String()
	if bodyReceived != testID {
		t.Fatalf("Echo server, [%s] sent, [%s] received", testID, bodyReceived)
	} else {
		log.Printf("%s succeeded!", testEM.TestID)
	}
}

func TestMain(m *testing.M) {
	flag.Parse()

	testEM = framework.NewTestEnvManager(appOnlyEnv.NewAppOnlyEnv(appOnlyEnvName), testID)
	res1 := testEM.RunTest(m)
	log.Printf("Test result %d in env %s", res1, appOnlyEnvName)

	testEM = framework.NewTestEnvManager(mixerEnvoyEnv.NewMixerEnvoyEnv(mixerEnvoyEnvName), testID)
	res2 := testEM.RunTest(m)
	log.Printf("Test result %d in env %s", res2, mixerEnvoyEnvName)

	if res1 == 0 && res2 == 0 {
		os.Exit(0)
	} else {
		os.Exit(1)
	}
}
