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

package client_test

import (
	"fmt"
	"testing"

	"istio.io/istio/mixer/test/client/env"
)

func TestGlobalDictionary(t *testing.T) {
	s := env.NewTestSetup(env.GlobalDictionaryTest, t)
	// let mixer server check dictionary size.
	s.SetCheckDict(true)
	env.SetNetworPolicy(s.MfConfig().HTTPServerConf, false)

	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	url := fmt.Sprintf("http://localhost:%d/echo", s.Ports().ClientProxyPort)

	tag := "CheckDictionary"
	// trigger a mixer report, and verify that report succeeds.
	code, _, err := env.HTTPGet(url)
	if err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}
	if code == 500 {
		t.Error("Proxy global dictionary is ahead of the one in mixer.")
	}
}
