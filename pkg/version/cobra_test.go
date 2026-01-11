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

package version

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

func TestOpts(t *testing.T) {
	ordinaryCmd := CobraCommand()
	remoteCmd := CobraCommandWithOptions(
		CobraOptions{GetRemoteVersion: mockRemoteMesh(&meshInfoMultiVersion, nil)})

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
			var out bytes.Buffer
			v.cmd.SetOut(&out)
			v.cmd.SetErr(&out)
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

var meshEmptyVersion = MeshInfo{}

var meshInfoSingleVersion = MeshInfo{
	{
		Component: "Pilot",
		Revision:  "default",
		Info:      BuildInfo{"1.2.0", "gitSHA123", "go1.10", "Clean", "tag"},
	},
	{
		Component: "Injector",
		Revision:  "default",
		Info:      BuildInfo{"1.2.0", "gitSHAabc", "go1.10.1", "Modified", "tag"},
	},
	{
		Component: "Citadel",
		Revision:  "default",
		Info:      BuildInfo{"1.2.0", "gitSHA321", "go1.11.0", "Clean", "tag"},
	},
}

var meshInfoMultiVersion = MeshInfo{
	{
		Component: "Pilot",
		Revision:  "default",
		Info:      BuildInfo{"1.0.0", "gitSHA123", "go1.10", "Clean", "1.0.0"},
	},
	{
		Component: "Injector",
		Revision:  "default",
		Info:      BuildInfo{"1.0.1", "gitSHAabc", "go1.10.1", "Modified", "1.0.1"},
	},
	{
		Component: "Citadel",
		Revision:  "default",
		Info:      BuildInfo{"1.2", "gitSHA321", "go1.11.0", "Clean", "1.2"},
	},
}

func mockRemoteMesh(meshInfo *MeshInfo, err error) GetRemoteVersionFunc {
	return func() (*MeshInfo, error) {
		return meshInfo, err
	}
}

type outputKind int

const (
	rawOutputMock outputKind = iota
	shortOutputMock
	jsonOutputMock
	yamlOutputMock
)

func printMeshVersion(meshInfo *MeshInfo, kind outputKind) string {
	switch kind {
	case yamlOutputMock:
		ver := &Version{MeshVersion: meshInfo}
		res, _ := yaml.Marshal(ver)
		return string(res)
	case jsonOutputMock:
		res, _ := json.MarshalIndent(meshInfo, "", "  ")
		return string(res)
	}

	var res strings.Builder
	for _, info := range *meshInfo {
		switch kind {
		case rawOutputMock:
			res.WriteString(fmt.Sprintf("%s version: %#v\n", info.Component, info.Info))
		case shortOutputMock:
			res.WriteString(fmt.Sprintf("%s version: %s\n", info.Component, info.Info.Version))
		}
	}
	return res.String()
}

func TestVersion(t *testing.T) {
	cases := []struct {
		args           []string
		remoteMesh     *MeshInfo
		err            error
		expectFail     bool
		expectedOutput string         // Expected constant output
		expectedRegexp *regexp.Regexp // Expected regexp output
	}{
		{ // case 0 client-side only, normal output
			args: strings.Split("version --remote=false --short=false", " "),
			expectedRegexp: regexp.MustCompile("version.BuildInfo{Version:\"unknown\", GitRevision:\"unknown\", " +
				"GolangVersion:\"go1.([0-9+?(\\.)?]+).*\", " +
				"BuildStatus:\"unknown\", GitTag:\"unknown\"}"),
		},
		{ // case 1 client-side only, short output
			args:           strings.Split("version -s --remote=false", " "),
			expectedOutput: "client version: unknown\n",
		},
		{ // case 2 client-side only, yaml output
			args: strings.Split("version --remote=false -o yaml", " "),
			expectedRegexp: regexp.MustCompile("clientVersion:\n" +
				"  golang_version: go1.([0-9+?(\\.)?]+).*\n" +
				"  revision: unknown\n" +
				"  status: unknown\n" +
				"  tag: unknown\n" +
				"  version: unknown\n\n"),
		},
		{ // case 3 client-side only, json output
			args: strings.Split("version --remote=false -o json", " "),
			expectedRegexp: regexp.MustCompile("{\n" +
				"  \"clientVersion\": {\n" +
				"    \"version\": \"unknown\",\n" +
				"    \"revision\": \"unknown\",\n" +
				"    \"golang_version\": \"go1.([0-9+?(\\.)?]+).*\",\n" +
				"    \"status\": \"unknown\",\n" +
				"    \"tag\": \"unknown\"\n" +
				"  }\n" +
				"}\n"),
		},

		{ // case 4 remote, normal output
			args:       strings.Split("version --remote=true --short=false --output=", " "),
			remoteMesh: &meshInfoMultiVersion,
			expectedRegexp: regexp.MustCompile("client version: version.BuildInfo{Version:\"unknown\", GitRevision:\"unknown\", " +
				"GolangVersion:\"go1.([0-9+?(\\.)?]+).*\", " +
				"BuildStatus:\"unknown\", GitTag:\"unknown\"}\n" +
				printMeshVersion(&meshInfoMultiVersion, rawOutputMock)),
		},
		{ // case 5 remote, short output
			args:           strings.Split("version --short=true --remote=true --output=", " "),
			remoteMesh:     &meshInfoMultiVersion,
			expectedOutput: "client version: unknown\n" + printMeshVersion(&meshInfoMultiVersion, shortOutputMock),
		},
		{ // case 6 remote, yaml output
			args:       strings.Split("version --remote=true -o yaml", " "),
			remoteMesh: &meshInfoMultiVersion,
			expectedRegexp: regexp.MustCompile("clientVersion:\n" +
				"  golang_version: go1.([0-9+?(\\.)?]+).*\n" +
				"  revision: unknown\n" +
				"  status: unknown\n" +
				"  tag: unknown\n" +
				"  version: unknown\n" + printMeshVersion(&meshInfoMultiVersion, yamlOutputMock)),
		},
		{ // case 7 remote, json output
			args:       strings.Split("version --remote=true -o json", " "),
			remoteMesh: &meshInfoMultiVersion,
			expectedRegexp: regexp.MustCompile("{\n" +
				"  \"clientVersion\": {\n" +
				"    \"version\": \"unknown\",\n" +
				"    \"revision\": \"unknown\",\n" +
				"    \"golang_version\": \"go1.([0-9+?(\\.)?]+).*\",\n" +
				"    \"status\": \"unknown\",\n" +
				"    \"tag\": \"unknown\"\n" +
				"  },\n" +
				printMeshVersion(&meshInfoMultiVersion, jsonOutputMock)),
		},

		{ // case 8 bogus arg
			args:           strings.Split("version --typo", " "),
			expectedRegexp: regexp.MustCompile("Error: unknown flag: --typo\n"),
			expectFail:     true,
		},

		{ // case 9 bogus output arg
			args:           strings.Split("version --output xyz", " "),
			expectedRegexp: regexp.MustCompile("Error: --output must be 'yaml' or 'json'\n"),
			expectFail:     true,
		},
		{ // case 10 remote, coalesced version output
			args:       strings.Split("version --short=true --remote=true --output=", " "),
			remoteMesh: &meshInfoSingleVersion,
			expectedOutput: `client version: unknown
control plane version: 1.2.0
`,
		},
		{ // case 11 remote, GetRemoteVersion returns a server error, skip
			args:       strings.Split("version --remote=true", " "),
			remoteMesh: &meshEmptyVersion,
			err:        fmt.Errorf("server error"),
			expectedRegexp: regexp.MustCompile("version.BuildInfo{Version:\"unknown\", GitRevision:\"unknown\", " +
				"GolangVersion:\"go1.([0-9+?(\\.)?]+).*\", " +
				"BuildStatus:\"unknown\", GitTag:\"unknown\"}"),
		},
	}

	for i, v := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(v.args, " ")), func(t *testing.T) {
			cmd := CobraCommandWithOptions(CobraOptions{GetRemoteVersion: mockRemoteMesh(v.remoteMesh, v.err)})
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(v.args)
			err := cmd.Execute()
			output := out.String()

			if v.expectedOutput != "" && v.expectedOutput != output {
				t.Fatalf("Unexpected output for 'istioctl %s'\n got: %q\nwant: %q",
					strings.Join(v.args, " "), output, v.expectedOutput)
			}

			if v.expectedRegexp != nil && !v.expectedRegexp.MatchString(output) {
				t.Fatalf("Output didn't match for 'istioctl %s'\n got %v\nwant: %v",
					strings.Join(v.args, " "), output, v.expectedRegexp)
			}

			if !v.expectFail && err != nil {
				t.Errorf("Got %v, expecting success", err)
			}
			if v.expectFail && err == nil {
				t.Errorf("Expected failure, got success")
			}
		})
	}
}
