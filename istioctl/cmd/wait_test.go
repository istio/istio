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

package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	v2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
)

func TestWaitCmd(t *testing.T) {
	cannedResponseObj := []v2.SyncedVersions{
		v2.SyncedVersions{
			ProxyID:         "foo",
			ClusterVersion:  "1",
			ListenerVersion: "1",
			RouteVersion:    "1",
		},
	}
	cannedResponse, _ := json.Marshal(cannedResponseObj)
	cannedResponseMap := map[string][]byte{"onlyonepilot": cannedResponse}

	cases := []execTestCase{
		{
			execClientConfig: cannedResponseMap,
			args:             strings.Split("experimental wait virtual-service/foo/bar", " "),
			wantException:    true,
		},
		{
			execClientConfig: cannedResponseMap,
			args:             strings.Split("experimental wait --for-distribution --resource-version=1 virtual-service/foo/bar", " "),
			wantException:    false,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyExecTestOutput(t, c)
		})
	}
}
