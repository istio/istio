// Copyright Istio Authors.
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
)

var (
	defaultYAML = `apiVersion: networking.istio.io/v1alpha3
kind: WorkloadGroup
metadata:
  name: foo
  namespace: bar
spec:
  labels: {}
  network: default
  ports: {}
  serviceAccount: default
`

	customYAML = `apiVersion: networking.istio.io/v1alpha3
kind: WorkloadGroup
metadata:
  name: foo
  namespace: bar
spec:
  labels:
    app: foo
    bar: baz
  network: local
  ports:
    grpc: 3550
    http: 8080
  serviceAccount: test
`
)

func TestCreateGroup(t *testing.T) {
	cases := []testcase{
		{
			description:       "Invalid command args - missing service name and namespace",
			args:              strings.Split("experimental sidecar create-group", " "),
			expectedException: true,
			expectedOutput:    "Error: expecting a service name\n",
		},
		{
			description:       "Invalid command args - missing service name",
			args:              strings.Split("experimental sidecar create-group --namespace bar", " "),
			expectedException: true,
			expectedOutput:    "Error: expecting a service name\n",
		},
		{
			description:       "Invalid command args - missing service namespace",
			args:              strings.Split("experimental sidecar create-group --name foo", " "),
			expectedException: true,
			expectedOutput:    "Error: expecting a service namespace\n",
		},
		{
			description:       "valid case - minimal flags, infer defaults",
			args:              strings.Split("experimental sidecar create-group --name foo --namespace bar", " "),
			expectedException: false,
			expectedOutput:    defaultYAML,
		},
		{
			description:       "valid case - create full workload group",
			args:              strings.Split("experimental sidecar create-group --name foo --namespace bar --labels app=foo,bar=baz --ports grpc=3550,http=8080 --network local --serviceAccount test", " "),
			expectedException: false,
			expectedOutput:    customYAML,
		},
		{
			description:       "valid case - create full workload group with shortnames",
			args:              strings.Split("experimental sidecar create-group --name foo -n bar -l app=foo,bar=baz -p grpc=3550,http=8080 --network local --serviceAccount test", " "),
			expectedException: false,
			expectedOutput:    customYAML,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, c.description), func(t *testing.T) {
			verifyAddToMeshOutput(t, c)
		})
	}
}
