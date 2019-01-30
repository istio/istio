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

package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"istio.io/istio/istioctl/pkg/kubernetes"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/version"
)

type execTestCase struct {
	execClientConfig map[string][]byte
	args             []string

	// Typically use one of the three
	expectedOutput string // Expected constant output
	expectedString string // String output is expected to contain
	goldenFilename string // Expected output stored in golden file

	wantException bool
}

// mockExecFactory lets us mock calls to remote Envoy and Istio instances
type mockExecConfig struct {
	// results is a map of pod to the results of the expected test on the pod
	results map[string][]byte
}

func TestProxyConfig(t *testing.T) {
	cannedConfig := map[string][]byte{
		"details-v1-5b7f94f9bc-wp5tb": util.ReadFile("../../pkg/writer/compare/testdata/envoyconfigdump.json", t),
	}
	endpointConfig := map[string][]byte{
		"details-v1-5b7f94f9bc-wp5tb": util.ReadFile("../../pkg/writer/envoy/clusters/testdata/clusters.json", t),
	}
	cases := []execTestCase{
		{ // case 0
			args:           strings.Split("proxy-config", " "),
			expectedString: "A group of commands used to retrieve information about",
		},
		{ // case 1 short name 'pc'
			args:           strings.Split("pc", " "),
			expectedString: "A group of commands used to retrieve information about",
		},
		{ // case 2 clusters invalid
			args:           strings.Split("proxy-config clusters invalid", " "),
			expectedString: "unable to retrieve Pod: pods \"invalid\" not found",
			wantException:  true, // "istioctl proxy-config clusters invalid" should fail
		},
		{ // case 3 listeners invalid
			args:           strings.Split("proxy-config listeners invalid", " "),
			expectedString: "unable to retrieve Pod: pods \"invalid\" not found",
			wantException:  true, // "istioctl proxy-config listeners invalid" should fail
		},
		{ // case 4 routes invalid
			args:           strings.Split("proxy-config routes invalid", " "),
			expectedString: "unable to retrieve Pod: pods \"invalid\" not found",
			wantException:  true, // "istioctl proxy-config routes invalid" should fail
		},
		{ // case 5 bootstrap invalid
			args:           strings.Split("proxy-config bootstrap invalid", " "),
			expectedString: "unable to retrieve Pod: pods \"invalid\" not found",
			wantException:  true, // "istioctl proxy-config bootstrap invalid" should fail
		},
		{ // case 6 clusters valid
			execClientConfig: cannedConfig,
			args:             strings.Split("proxy-config clusters details-v1-5b7f94f9bc-wp5tb", " "),
			expectedOutput: `SERVICE FQDN                                    PORT      SUBSET     DIRECTION     TYPE
istio-policy.istio-system.svc.cluster.local     15004     -          outbound      EDS
xds-grpc                                        -         -          -             STRICT_DNS
`,
		},
		{ // case 7 listeners valid
			execClientConfig: cannedConfig,
			args:             strings.Split("proxy-config listeners details-v1-5b7f94f9bc-wp5tb", " "),
			expectedOutput: `ADDRESS            PORT     TYPE
172.21.134.116     443      TCP
0.0.0.0            8080     HTTP
`,
		},
		{ // case 8 routes valid
			execClientConfig: cannedConfig,
			args:             strings.Split("proxy-config routes details-v1-5b7f94f9bc-wp5tb", " "),
			expectedOutput: `NOTE: This output only contains routes loaded via RDS.
NAME                                                    VIRTUAL HOSTS
15004                                                   2
inbound|9080||productpage.default.svc.cluster.local     1
`,
		},
		{ // case 9 endpoint invalid
			args:           strings.Split("proxy-config endpoint invalid", " "),
			expectedString: "unable to retrieve Pod: pods \"invalid\" not found",
			wantException:  true, // "istioctl proxy-config endpoint invalid" should fail
		},
		{ // case 10 endpoint valid
			execClientConfig: endpointConfig,
			args:             strings.Split("proxy-config endpoint details-v1-5b7f94f9bc-wp5tb --port=15014", " "),
			expectedOutput: `ENDPOINT              STATUS        CLUSTER
172.17.0.14:15014     UNHEALTHY     outbound|15014||istio-policy.istio-system.svc.cluster.local
`,
		},
		{ // case 11 endpoint status filter
			execClientConfig: endpointConfig,
			args:             strings.Split("proxy-config endpoint details-v1-5b7f94f9bc-wp5tb --status=unhealthy", " "),
			expectedOutput: `ENDPOINT              STATUS        CLUSTER
172.17.0.14:15014     UNHEALTHY     outbound|15014||istio-policy.istio-system.svc.cluster.local
`,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyExecTestOutput(t, c)
		})
	}
}

func verifyExecTestOutput(t *testing.T, c execTestCase) {
	t.Helper()

	// Override the exec client factory used by proxyconfig.go and proxystatus.go
	clientExecFactory = mockClientExecFactoryGenerator(c.execClientConfig)

	var out bytes.Buffer
	rootCmd.SetOutput(&out)
	rootCmd.SetArgs(c.args)

	file = "" // Clear, because we re-use

	fErr := rootCmd.Execute()
	output := out.String()

	if c.expectedOutput != "" && c.expectedOutput != output {
		t.Fatalf("Unexpected output for 'istioctl %s'\n got: %q\nwant: %q", strings.Join(c.args, " "), output, c.expectedOutput)
	}

	if c.expectedString != "" && !strings.Contains(output, c.expectedString) {
		t.Fatalf("Output didn't match for 'istioctl %s'\n got %v\nwant: %v", strings.Join(c.args, " "), output, c.expectedString)
	}

	if c.goldenFilename != "" {
		util.CompareContent([]byte(output), c.goldenFilename, t)
	}

	if c.wantException {
		if fErr == nil {
			t.Fatalf("Wanted an exception for 'istioctl %s', didn't get one, output was %q",
				strings.Join(c.args, " "), output)
		}
	} else {
		if fErr != nil {
			t.Fatalf("Unwanted exception for 'istioctl %s': %v", strings.Join(c.args, " "), fErr)
		}
	}
}

// mockClientExecFactoryGenerator generates a function with the same signature as
// kubernetes.NewExecClient() that returns a mock client.
func mockClientExecFactoryGenerator(testResults map[string][]byte) func(kubeconfig, configContext string) (kubernetes.ExecClient, error) {
	outFactory := func(kubeconfig, configContext string) (kubernetes.ExecClient, error) {
		return mockExecConfig{
			results: testResults,
		}, nil
	}

	return outFactory
}

// nolint: unparam
func (client mockExecConfig) AllPilotsDiscoveryDo(pilotNamespace, method, path string, body []byte) (map[string][]byte, error) {
	return client.results, nil
}

// nolint: unparam
func (client mockExecConfig) EnvoyDo(podName, podNamespace, method, path string, body []byte) ([]byte, error) {
	results, ok := client.results[podName]
	if !ok {
		return nil, fmt.Errorf("unable to retrieve Pod: pods %q not found", podName)
	}
	return results, nil
}

// nolint: unparam
func (client mockExecConfig) PilotDiscoveryDo(pilotNamespace, method, path string, body []byte) ([]byte, error) {
	for _, results := range client.results {
		return results, nil
	}
	return nil, fmt.Errorf("unable to find any Pilot instances")
}

func (client mockExecConfig) GetIstioVersions(namespace string) (*version.MeshInfo, error) {
	return nil, nil
}
