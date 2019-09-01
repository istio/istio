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

package cmd

import (
	"fmt"
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"

	"istio.io/istio/istioctl/pkg/kubernetes"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/pkg/version"
)

var meshInfo = version.MeshInfo{
	{"Pilot", version.BuildInfo{"1.0.0", "gitSHA123", "go1.10", "Clean", "Tag"}},
	{"Injector", version.BuildInfo{"1.0.1", "gitSHAabc", "go1.10.1", "Modified", "OtherTag"}},
	{"Citadel", version.BuildInfo{"1.2", "gitSHA321", "go1.11.0", "Clean", "Tag"}},
}

func TestVersion(t *testing.T) {
	clientExecFactory = mockExecClientVersionTest

	cases := []testCase{
		{ // case 0 client-side only, normal output
			configs: []model.Config{},
			args:    strings.Split("version --remote=false --short=false", " "),
			// ignore the output, all output checks are now in istio/pkg
		},
		{ // case 1 remote, normal output
			configs: []model.Config{},
			args:    strings.Split("version --remote=true --short=false --output=", " "),
			// ignore the output, all output checks are now in istio/pkg
		},
		{ // case 2 bogus arg
			configs:        []model.Config{},
			args:           strings.Split("version --typo", " "),
			expectedOutput: "Error: unknown flag: --typo\n",
			wantException:  true,
		},
		{ // case 3 bogus output arg
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
