// Copyright 2019 Istio Authors. All Rights Reserved.
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

package agent

import (
	"flag"
	"strings"
	"testing"
	"time"

	"istio.io/istio/security/pkg/testing/sdsc"
)

var (
	sdsUdsPath = flag.String("istio.testing.citadelagent.uds", "", "The agent server uds path.")
	skip       = flag.Bool("istio.testing.citadelagent.skip", true, "Whether skipping the citadel agent integration test.")
)

// TODO(incfly): refactor node_agent_k8s/main.go to be able to start a server within the test.
func TestAgentFailsRequestWithoutToken(t *testing.T) {
	if *skip {
		t.Skip("Test is skipped until Citadel agent is setup in test.")
	}
	client, err := sdsc.NewClient(sdsc.ClientOptions{
		ServerAddress: *sdsUdsPath,
	})
	if err != nil {
		t.Errorf("failed to create sds client")
	}
	client.Start()
	defer client.Stop()
	if err := client.Send(); err != nil {
		t.Errorf("failed to send request to sds server %v", err)
	}
	errmsg := "no credential token"
	_, err = client.WaitForUpdate(3 * time.Second)
	if err == nil || strings.Contains(err.Error(), errmsg) {
		t.Errorf("got [%v], want error with substring [%v]", err, errmsg)
	}
}
