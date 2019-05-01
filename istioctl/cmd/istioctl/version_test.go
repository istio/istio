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

package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/ghodss/yaml"
	v1 "k8s.io/api/core/v1"

	"istio.io/istio/istioctl/pkg/kubernetes"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/version"
)

var meshInfo = version.MeshInfo{
	{"Pilot", version.BuildInfo{"1.0.0", "gitSHA123", "user1", "host1", "go1.10", "hub.docker.com", "Clean", "Tag"}},
	{"Injector", version.BuildInfo{"1.0.1", "gitSHAabc", "user2", "host2", "go1.10.1", "hub.docker.com", "Modified", "OtherTag"}},
	{"Citadel", version.BuildInfo{"1.2", "gitSHA321", "user3", "host3", "go1.11.0", "hub.docker.com", "Clean", "Tag"}},
}

type outputKind int

const (
	rawOutputMock outputKind = iota
	shortOutputMock
	jsonOutputMock
	yamlOutputMock
)

func printMeshVersion(kind outputKind) string {
	switch kind {
	case yamlOutputMock:
		ver := &version.Version{MeshVersion: &meshInfo}
		res, _ := yaml.Marshal(ver)
		return string(res)
	case jsonOutputMock:
		res, _ := json.MarshalIndent(&meshInfo, "", "  ")
		return string(res)
	}

	res := ""
	for _, info := range meshInfo {
		switch kind {
		case rawOutputMock:
			res += fmt.Sprintf("%s version: %#v\n", info.Component, info.Info)
		case shortOutputMock:
			res += fmt.Sprintf("%s version: %s\n", info.Component, info.Info.Version)
		}
	}
	return res
}

func TestVersion(t *testing.T) {
	clientExecFactory = mockExecClientVersionTest

	cases := []testCase{
		{ // case 0 client-side only, normal output
			configs: []model.Config{},
			args:    strings.Split("version --remote=false --short=false", " "),
			expectedRegexp: regexp.MustCompile("version.BuildInfo{Version:\"unknown\", GitRevision:\"unknown\", " +
				"User:\"unknown\", Host:\"unknown\", GolangVersion:\"go1.([0-9+?(\\.)?]+)(rc[0-9]?)?\", " +
				"DockerHub:\"unknown\", BuildStatus:\"unknown\", GitTag:\"unknown\"}"),
		},
		{ // case 1 client-side only, short output
			configs:        []model.Config{},
			args:           strings.Split("version -s --remote=false", " "),
			expectedOutput: "unknown\n",
		},
		{ // case 2 client-side only, yaml output
			configs: []model.Config{},
			args:    strings.Split("version --remote=false -o yaml", " "),
			expectedRegexp: regexp.MustCompile("clientVersion:\n" +
				"  golang_version: go1.([0-9+?(\\.)?]+)(rc[0-9]?)?\n" +
				"  host: unknown\n" +
				"  hub: unknown\n" +
				"  revision: unknown\n" +
				"  status: unknown\n" +
				"  tag: unknown\n" +
				"  user: unknown\n" +
				"  version: unknown\n\n"),
		},
		{ // case 3 client-side only, json output
			configs: []model.Config{},
			args:    strings.Split("version --remote=false -o json", " "),
			expectedRegexp: regexp.MustCompile("{\n" +
				"  \"clientVersion\": {\n" +
				"    \"version\": \"unknown\",\n" +
				"    \"revision\": \"unknown\",\n" +
				"    \"user\": \"unknown\",\n" +
				"    \"host\": \"unknown\",\n" +
				"    \"golang_version\": \"go1.([0-9+?(\\.)?]+)(rc[0-9]?)?\",\n" +
				"    \"hub\": \"unknown\",\n" +
				"    \"status\": \"unknown\",\n" +
				"    \"tag\": \"unknown\"\n" +
				"  }\n" +
				"}\n"),
		},

		{ // case 4 remote, normal output
			configs: []model.Config{},
			args:    strings.Split("version --remote=true --short=false --output=", " "),
			expectedRegexp: regexp.MustCompile("client version: version.BuildInfo{Version:\"unknown\", GitRevision:\"unknown\", " +
				"User:\"unknown\", Host:\"unknown\", GolangVersion:\"go1.([0-9+?(\\.)?]+)(rc[0-9]?)?\", " +
				"DockerHub:\"unknown\", BuildStatus:\"unknown\", GitTag:\"unknown\"}\n" +
				printMeshVersion(rawOutputMock)),
		},
		{ // case 5 remote, short output
			configs:        []model.Config{},
			args:           strings.Split("version --short=true --remote=true --output=", " "),
			expectedOutput: "client version: unknown\n" + printMeshVersion(shortOutputMock),
		},
		{ // case 6 remote, yaml output
			configs: []model.Config{},
			args:    strings.Split("version --remote=true -o yaml", " "),
			expectedRegexp: regexp.MustCompile("clientVersion:\n" +
				"  golang_version: go1.([0-9+?(\\.)?]+)(rc[0-9]?)?\n" +
				"  host: unknown\n" +
				"  hub: unknown\n" +
				"  revision: unknown\n" +
				"  status: unknown\n" +
				"  tag: unknown\n" +
				"  user: unknown\n" +
				"  version: unknown\n" + printMeshVersion(yamlOutputMock)),
		},
		{ // case 7 remote, json output
			configs: []model.Config{},
			args:    strings.Split("version --remote=true -o json", " "),
			expectedRegexp: regexp.MustCompile("{\n" +
				"  \"clientVersion\": {\n" +
				"    \"version\": \"unknown\",\n" +
				"    \"revision\": \"unknown\",\n" +
				"    \"user\": \"unknown\",\n" +
				"    \"host\": \"unknown\",\n" +
				"    \"golang_version\": \"go1.([0-9+?(\\.)?]+)(rc[0-9]?)?\",\n" +
				"    \"hub\": \"unknown\",\n" +
				"    \"status\": \"unknown\",\n" +
				"    \"tag\": \"unknown\"\n" +
				"  },\n" +
				printMeshVersion(jsonOutputMock)),
		},

		{ // case 8 bogus arg
			configs:        []model.Config{},
			args:           strings.Split("version --typo", " "),
			expectedOutput: "Error: unknown flag: --typo\n",
			wantException:  true,
		},

		{ // case 9 bogus output arg
			configs:        []model.Config{},
			args:           strings.Split("version --output xyz", " "),
			expectedOutput: "Error: --output must be 'yaml' or 'json'\n",
			wantException:  true,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyOutput(t, c)
		})
	}
}

type mockExecVersionConfig struct {
}

func (client mockExecVersionConfig) AllPilotsDiscoveryDo(pilotNamespace, method, path string, body []byte) (map[string][]byte, error) {
	return nil, nil
}

func (client mockExecVersionConfig) EnvoyDo(podName, podNamespace, method, path string, body []byte) ([]byte, error) {
	return nil, nil
}

func (client mockExecVersionConfig) PilotDiscoveryDo(pilotNamespace, method, path string, body []byte) ([]byte, error) {
	return nil, nil
}

// nolint: unparam
func (client mockExecVersionConfig) GetIstioVersions(namespace string) (*version.MeshInfo, error) {
	return &meshInfo, nil
}

func mockExecClientVersionTest(_, _ string) (kubernetes.ExecClient, error) {
	return &mockExecVersionConfig{}, nil
}

func (client mockExecVersionConfig) PodsForSelector(namespace, labelSelector string) (*v1.PodList, error) {
	return &v1.PodList{}, nil
}

func (client mockExecVersionConfig) BuildPortForwarder(podName string, ns string, localPort int, podPort int) (*kubernetes.PortForward, error) {
	return nil, fmt.Errorf("mock k8s does not forward")
}
