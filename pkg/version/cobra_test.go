// Copyright 2018 Istio Authors
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

package version

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestOpts(t *testing.T) {
	ordinaryCmd := CobraCommand()
	remoteCmd := CobraCommandWithOptions(CobraOptions{GetRemoteVersion: getRemoteInfo})

	cases := []struct {
		args       string
		cmd        *cobra.Command
		expectFail bool
	}{
		{
			"version",
			ordinaryCmd,
			false,
		},
		{
			"version --short",
			ordinaryCmd,
			false,
		},
		{
			"version --output yaml",
			ordinaryCmd,
			false,
		},
		{
			"version --output json",
			ordinaryCmd,
			false,
		},
		{
			"version --output xuxa",
			ordinaryCmd,
			true,
		},
		{
			"version --remote",
			ordinaryCmd,
			true,
		},

		{
			"version --remote",
			remoteCmd,
			false,
		},
		{
			"version --remote --short",
			remoteCmd,
			false,
		},
		{
			"version --remote --output yaml",
			remoteCmd,
			false,
		},
		{
			"version --remote --output json",
			remoteCmd,
			false,
		},
	}

	for _, v := range cases {
		t.Run(v.args, func(t *testing.T) {
			v.cmd.SetArgs(strings.Split(v.args, " "))
			err := v.cmd.Execute()

			if !v.expectFail && err != nil {
				t.Errorf("Got %v, expecting success", err)
			}
			if v.expectFail && err == nil {
				t.Errorf("Expected failure, got success")
			}
		})
	}
}

var meshInfo = MeshInfo{
	{"Pilot", BuildInfo{"1.0.0", "gitSHA123", "user1", "host1", "go1.10", "hub.docker.com", "Clean", "tag"}},
	{"Injector", BuildInfo{"1.0.1", "gitSHAabc", "user2", "host2", "go1.10.1", "hub.docker.com", "Modified", "tag"}},
	{"Citadel", BuildInfo{"1.2", "gitSHA321", "user3", "host3", "go1.11.0", "hub.docker.com", "Clean", "tag"}},
}

func getRemoteInfo() (*MeshInfo, error) {
	return &meshInfo, nil
}
