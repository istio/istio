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
	"net/http"
	"os"
	"strings"
	"testing"

	"log"

	"istio.io/istio/tests/integration/framework"
	env "istio.io/istio/tests/integration/framework/environment"
)

const (
	appOnlyEnv      = "app_only_env"
	mixerEnvoyEnv   = "mixer_envoy_env"
	serverEndpoint  = "http://localhost:8080/"
	sidecarEndpoint = "http://localhost:9090/echo"
	testID          = "sample1_test"
)

var (
	testFW *framework.IstioTestFramework
)

func TestSample1(t *testing.T) {
	log.Printf("Running %s", testFW.TestID)

	var url string
	if testFW.TestEnv.GetName() == appOnlyEnv {
		url = serverEndpoint
	} else if testFW.TestEnv.GetName() == mixerEnvoyEnv {
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
	buf.ReadFrom(resp.Body)
	bodyReceived := buf.String()
	if bodyReceived != testID {
		t.Fatalf("Echo server, [%s] sent, [%s] received", testID, bodyReceived)
	} else {
		log.Printf("%s succeeded!", testFW.TestID)
	}
}

func TestMain(m *testing.M) {
	flag.Parse()

	testFW = framework.NewIstioTestFramework(env.NewAppOnlyEnv(appOnlyEnv), testID)
	res1 := testFW.RunTest(m)
	log.Printf("Test result %d in env %s", res1, appOnlyEnv)

	testFW = framework.NewIstioTestFramework(env.NewMixerEnvoyEnv(mixerEnvoyEnv), testID)
	res2 := testFW.RunTest(m)
	log.Printf("Test result %d in env %s", res2, mixerEnvoyEnv)

	if res1 == 0 && res2 == 0 {
		os.Exit(0)
	} else {
		os.Exit(1)
	}
}
