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
	"bytes"
	"fmt"
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"

	"istio.io/istio/istioctl/pkg/kubernetes"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/security/pkg/nodeagent/sds"
	"istio.io/pkg/version"
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

// mockExecConfig lets us mock calls to remote Envoy and Istio instances
type mockExecConfig struct {
	// results is a map of pod to the results of the expected test on the pod
	results map[string][]byte
}

func TestProxyConfig(t *testing.T) {
	cannedConfig := map[string][]byte{
		"details-v1-5b7f94f9bc-wp5tb": util.ReadFile("../pkg/writer/compare/testdata/envoyconfigdump.json", t),
	}
	endpointConfig := map[string][]byte{
		"details-v1-5b7f94f9bc-wp5tb": util.ReadFile("../pkg/writer/envoy/clusters/testdata/clusters.json", t),
	}
	loggingConfig := map[string][]byte{
		"details-v1-5b7f94f9bc-wp5tb": util.ReadFile("../pkg/writer/envoy/logging/testdata/logging.txt", t),
	}
	cases := []execTestCase{
		{
			args:           strings.Split("proxy-config", " "),
			expectedString: "A group of commands used to retrieve information about",
		},
		{ // short name 'pc'
			args:           strings.Split("pc", " "),
			expectedString: "A group of commands used to retrieve information about",
		},
		{ // clusters invalid
			args:           strings.Split("proxy-config clusters invalid", " "),
			expectedString: "unable to retrieve Pod: pods \"invalid\" not found",
			wantException:  true, // "istioctl proxy-config clusters invalid" should fail
		},
		{ // listeners invalid
			args:           strings.Split("proxy-config listeners invalid", " "),
			expectedString: "unable to retrieve Pod: pods \"invalid\" not found",
			wantException:  true, // "istioctl proxy-config listeners invalid" should fail
		},
		{ // logging invalid
			args:           strings.Split("proxy-config log invalid", " "),
			expectedString: "unable to retrieve Pod: pods \"invalid\" not found",
			wantException:  true, // "istioctl proxy-config logging invalid" should fail
		},
		{ // logging level invalid
			execClientConfig: loggingConfig,
			args:             strings.Split("proxy-config log details-v1-5b7f94f9bc-wp5tb --level xxx", " "),
			expectedString:   "unrecognized logging level: xxx",
			wantException:    true,
		},
		{ // logger name invalid
			execClientConfig: loggingConfig,
			args:             strings.Split("proxy-config log details-v1-5b7f94f9bc-wp5tb --level xxx:debug", " "),
			expectedString:   "unrecognized logger name: xxx",
			wantException:    true,
		},
		{ // logger name valid, but logging level invalid
			execClientConfig: loggingConfig,
			args:             strings.Split("proxy-config log details-v1-5b7f94f9bc-wp5tb --level http:yyy", " "),
			expectedString:   "unrecognized logging level: yyy",
			wantException:    true,
		},
		{ // both logger name and logging level invalid
			execClientConfig: loggingConfig,
			args:             strings.Split("proxy-config log details-v1-5b7f94f9bc-wp5tb --level xxx:yyy", " "),
			expectedString:   "unrecognized logger name: xxx",
			wantException:    true,
		},
		{ // routes invalid
			args:           strings.Split("proxy-config routes invalid", " "),
			expectedString: "unable to retrieve Pod: pods \"invalid\" not found",
			wantException:  true, // "istioctl proxy-config routes invalid" should fail
		},
		{ // bootstrap invalid
			args:           strings.Split("proxy-config bootstrap invalid", " "),
			expectedString: "unable to retrieve Pod: pods \"invalid\" not found",
			wantException:  true, // "istioctl proxy-config bootstrap invalid" should fail
		},
		{ // secret invalid
			args:           strings.Split("proxy-config secret invalid", " "),
			expectedString: "unable to retrieve Pod: pods \"invalid\" not found",
			wantException:  true, // "istioctl proxy-config secret invalid" should fail
		},
		{ // clusters valid
			execClientConfig: cannedConfig,
			args:             strings.Split("proxy-config clusters details-v1-5b7f94f9bc-wp5tb", " "),
			expectedOutput: `SERVICE FQDN                                    PORT      SUBSET     DIRECTION     TYPE
istio-policy.istio-system.svc.cluster.local     15004     -          outbound      EDS
xds-grpc                                        -         -          -             STRICT_DNS
`,
		},
		{ // listeners valid
			execClientConfig: cannedConfig,
			args:             strings.Split("proxy-config listeners details-v1-5b7f94f9bc-wp5tb", " "),
			expectedOutput: `ADDRESS            PORT     TYPE
172.21.134.116     443      TCP
0.0.0.0            8080     HTTP
`,
		},
		{ // routes valid
			execClientConfig: cannedConfig,
			args:             strings.Split("proxy-config routes details-v1-5b7f94f9bc-wp5tb", " "),
			expectedOutput: `NOTE: This output only contains routes loaded via RDS.
NAME                                                    VIRTUAL HOSTS
15004                                                   2
inbound|9080||productpage.default.svc.cluster.local     1
`,
		},
		{ // secret valid
			execClientConfig: cannedConfig,
			args:             strings.Split("proxy-config secret details-v1-5b7f94f9bc-wp5tb", " "),
			expectedOutput: `RESOURCE NAME     TYPE           STATUS      VALID CERT     SERIAL NUMBER                               NOT AFTER                NOT BEFORE
default           Cert Chain     WARMING     true           102248101821513494474081488414108563796     2019-09-05T21:18:20Z     2019-09-04T21:18:20Z
default           Cert Chain     ACTIVE      true           172326788211665918318952701714288464978     2019-08-28T17:19:57Z     2019-08-27T17:19:57Z
`,
		},
		{ // endpoint invalid
			args:           strings.Split("proxy-config endpoint invalid", " "),
			expectedString: "unable to retrieve Pod: pods \"invalid\" not found",
			wantException:  true, // "istioctl proxy-config endpoint invalid" should fail
		},
		{ // endpoint valid
			execClientConfig: endpointConfig,
			args:             strings.Split("proxy-config endpoint details-v1-5b7f94f9bc-wp5tb --port=15014", " "),
			expectedOutput: `ENDPOINT              STATUS        OUTLIER CHECK     CLUSTER
172.17.0.14:15014     UNHEALTHY     OK                outbound|15014||istio-policy.istio-system.svc.cluster.local
`,
		},
		{ // endpoint status filter
			execClientConfig: endpointConfig,
			args:             strings.Split("proxy-config endpoint details-v1-5b7f94f9bc-wp5tb --status=unhealthy", " "),
			expectedOutput: `ENDPOINT              STATUS        OUTLIER CHECK     CLUSTER
172.17.0.14:15014     UNHEALTHY     OK                outbound|15014||istio-policy.istio-system.svc.cluster.local
`,
		},
		{ // bootstrap no args
			execClientConfig: endpointConfig,
			args:             strings.Split("proxy-config bootstrap", " "),
			expectedString:   `Error: bootstrap requires pod name or --file parameter`,
			wantException:    true,
		},
		{ // cluster no args
			execClientConfig: endpointConfig,
			args:             strings.Split("proxy-config cluster", " "),
			expectedString:   `Error: cluster requires pod name or --file parameter`,
			wantException:    true,
		},
		{ // endpoint no args
			execClientConfig: endpointConfig,
			args:             strings.Split("proxy-config endpoint", " "),
			expectedString:   `Error: endpoints requires pod name or --file parameter`,
			wantException:    true,
		},
		{ // listener no args
			execClientConfig: endpointConfig,
			args:             strings.Split("proxy-config listener", " "),
			expectedString:   `Error: listener requires pod name or --file parameter`,
			wantException:    true,
		},
		{ // logging no args
			execClientConfig: endpointConfig,
			args:             strings.Split("proxy-config log", " "),
			expectedString:   `Error: log requires pod name`,
			wantException:    true,
		},
		{ // route no args
			execClientConfig: endpointConfig,
			args:             strings.Split("proxy-config route", " "),
			expectedString:   `Error: route requires pod name or --file parameter`,
			wantException:    true,
		},
		{ // secret no args
			execClientConfig: endpointConfig,
			args:             strings.Split("proxy-config secret", " "),
			expectedString:   `Error: secret requires pod name or --file parameter`,
			wantException:    true,
		},
		{ // clusters using --file
			args: strings.Split("proxy-config clusters --file ../pkg/writer/compare/testdata/envoyconfigdump.json", " "),
			expectedOutput: `SERVICE FQDN                                    PORT      SUBSET     DIRECTION     TYPE
istio-policy.istio-system.svc.cluster.local     15004     -          outbound      EDS
xds-grpc                                        -         -          -             STRICT_DNS
`,
		},
		{ // listeners using --file
			args: strings.Split("proxy-config listeners --file ../pkg/writer/compare/testdata/envoyconfigdump.json", " "),
			expectedOutput: `ADDRESS            PORT     TYPE
172.21.134.116     443      TCP
0.0.0.0            8080     HTTP
`,
		},
		{ // routes using --file
			args: strings.Split("proxy-config routes --file ../pkg/writer/compare/testdata/envoyconfigdump.json", " "),
			expectedOutput: `NOTE: This output only contains routes loaded via RDS.
NAME                                                    VIRTUAL HOSTS
15004                                                   2
inbound|9080||productpage.default.svc.cluster.local     1
`,
		},
		{ // secret using --file
			args: strings.Split("proxy-config secret --file ../pkg/writer/compare/testdata/envoyconfigdump.json", " "),
			expectedOutput: `RESOURCE NAME     TYPE           STATUS      VALID CERT     SERIAL NUMBER                               NOT AFTER                NOT BEFORE
default           Cert Chain     WARMING     true           102248101821513494474081488414108563796     2019-09-05T21:18:20Z     2019-09-04T21:18:20Z
default           Cert Chain     ACTIVE      true           172326788211665918318952701714288464978     2019-08-28T17:19:57Z     2019-08-27T17:19:57Z
`,
		},
		{ // endpoint using --file
			args: strings.Split("proxy-config endpoint --file ../pkg/writer/envoy/clusters/testdata/clusters.json --port=15014", " "),
			expectedOutput: `ENDPOINT              STATUS        OUTLIER CHECK     CLUSTER
172.17.0.14:15014     UNHEALTHY     OK                outbound|15014||istio-policy.istio-system.svc.cluster.local
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
	rootCmd := GetRootCmd(c.args)
	rootCmd.SetOutput(&out)

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

func (client mockExecConfig) PodsForSelector(namespace, labelSelector string) (*v1.PodList, error) {
	return &v1.PodList{}, nil
}

func (client mockExecConfig) BuildPortForwarder(podName string, ns string, localPort int, podPort int) (*kubernetes.PortForward, error) {
	return nil, fmt.Errorf("mock k8s does not forward")
}

func (client mockExecConfig) GetPodNodeAgentSecrets(podName, ns, istioNamespace string) (map[string]sds.Debug, error) {
	return map[string]sds.Debug{}, nil
}

func (client mockExecConfig) NodeAgentDebugEndpointOutput(podName, ns, secretType, container string) (sds.Debug, error) {
	return sds.Debug{}, nil
}
