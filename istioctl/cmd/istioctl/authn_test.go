// Copyright 2019 Istio Authors
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
	"fmt"
	"regexp"
	"strings"
	"testing"

	"istio.io/istio/istioctl/pkg/kubernetes"
	"istio.io/istio/pilot/pkg/model"
)

func TestAuthnTlsCheck(t *testing.T) {
	clientExecFactory = mockExecClientAuth

	cases := []testCase{
		{ // case 0
			configs:        []model.Config{},
			args:           strings.Split("authn tls-check", " "),
			expectedOutput: "Error: requires at least 1 arg(s), only received 0\n",
			wantException:  true,
		},
		{ // case 1
			configs: []model.Config{},
			args:    strings.Split("authn tls-check foo-123456-7890", " "),
			expectedOutput: `HOST:PORT                                  STATUS     SERVER        CLIENT     AUTHN POLICY     DESTINATION RULE
details.default.svc.cluster.local:8080     OK         HTTP/mTLS     mTLS       default/         details/default
`,
		},
		{ // case 2
			configs: []model.Config{},
			args:    strings.Split("authn tls-check foo-123456-7890 bar", " "),
			expectedOutput: `HOST:PORT     STATUS     SERVER     CLIENT     AUTHN POLICY     DESTINATION RULE
`,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyOutput(t, c)
		})
	}
}

func TestAuthnTlsCheckNoPilot(t *testing.T) {
	clientExecFactory = mockExecClientAuthNoPilot

	cases := []testCase{
		{ // case 0
			configs:        []model.Config{},
			args:           strings.Split("authn tls-check badpod-123456-7890", " "),
			expectedRegexp: regexp.MustCompile("Error: "),
			wantException:  true,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyOutput(t, c)
		})
	}
}

func mockExecClientAuth(_, _ string) (kubernetes.ExecClient, error) {
	return &mockExecConfig{
		results: map[string][]byte{
			"istio-pilot-123456-7890": []byte(`
[
{
  "host": "details.default.svc.cluster.local",
  "port": 8080,
  "authentication_policy_name": "default/",
  "destination_rule_name": "details/default",
  "server_protocol": "HTTP/mTLS",
  "client_protocol": "mTLS",
  "TLS_conflict_status": "OK"
}]`),
		},
	}, nil
}

func mockExecClientAuthNoPilot(_, _ string) (kubernetes.ExecClient, error) {
	return &mockExecConfig{}, nil
}
