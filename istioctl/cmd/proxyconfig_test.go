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

package cmd

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/kube"
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

func TestProxyConfig(t *testing.T) {
	loggingConfig := map[string][]byte{
		"details-v1-5b7f94f9bc-wp5tb": util.ReadFile(t, "../pkg/writer/envoy/logging/testdata/logging.txt"),
		"httpbin-794b576b6c-qx6pf":    []byte("{}"),
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
		{ // logging empty
			args:           strings.Split("proxy-config log", " "),
			expectedString: "Error: log requires pod name or --selector",
			wantException:  true, // "istioctl proxy-config logging empty" should fail
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
		{ // endpoint invalid
			args:           strings.Split("proxy-config endpoint invalid", " "),
			expectedString: "unable to retrieve Pod: pods \"invalid\" not found",
			wantException:  true, // "istioctl proxy-config endpoint invalid" should fail
		},
		{ // supplying nonexistent deployment name should result in error
			args:           strings.Split("proxy-config clusters deployment/random-gibberish", " "),
			expectedString: `"deployment/random-gibberish" does not refer to a pod`,
			wantException:  true,
		},
		{ // supplying nonexistent deployment name in nonexistent namespace
			args:           strings.Split("proxy-config endpoint deployment/random-gibberish.bogus", " "),
			expectedString: `"deployment/random-gibberish" does not refer to a pod`,
			wantException:  true,
		},
		{ // supplying type that doesn't select pods should fail
			args:           strings.Split("proxy-config listeners serviceaccount/sleep", " "),
			expectedString: `"serviceaccount/sleep" does not refer to a pod`,
			wantException:  true,
		},
		{ // supplying valid pod name retrieves Envoy config (fails because we don't check in Envoy config unit tests)
			execClientConfig: loggingConfig,
			args:             strings.Split("pc clusters httpbin-794b576b6c-qx6pf", " "),
			expectedString:   `config dump has no configuration type`,
			wantException:    true,
		},
		{ // supplying valid pod name retrieves Envoy config (fails because we don't check in Envoy config unit tests)
			execClientConfig: loggingConfig,
			args:             strings.Split("pc bootstrap httpbin-794b576b6c-qx6pf", " "),
			expectedString:   `config dump has no configuration type`,
			wantException:    true,
		},
		{ // supplying valid pod name retrieves Envoy config (fails because we don't check in Envoy config unit tests)
			execClientConfig: loggingConfig,
			args:             strings.Split("pc endpoint httpbin-794b576b6c-qx6pf", " "),
			expectedString:   `ENDPOINT     STATUS     OUTLIER CHECK     CLUSTER`,
			wantException:    false,
		},
		{ // supplying valid pod name retrieves Envoy config (fails because we don't check in Envoy config unit tests)
			execClientConfig: loggingConfig,
			args:             strings.Split("pc listener httpbin-794b576b6c-qx6pf", " "),
			expectedString:   `config dump has no configuration type`,
			wantException:    true,
		},
		{ // supplying valid pod name retrieves Envoy config (fails because we don't check in Envoy config unit tests)
			execClientConfig: loggingConfig,
			args:             strings.Split("pc route httpbin-794b576b6c-qx6pf", " "),
			expectedString:   `config dump has no configuration type`,
			wantException:    true,
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
	kubeClientWithRevision = mockClientExecFactoryGenerator(c.execClientConfig)
	kubeClient = mockEnvoyClientFactoryGenerator(c.execClientConfig)

	var out bytes.Buffer
	rootCmd := GetRootCmd(c.args)
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	fErr := rootCmd.Execute()
	output := out.String()

	if c.expectedOutput != "" && c.expectedOutput != output {
		t.Fatalf("Unexpected output for 'istioctl %s'\n got: %q\nwant: %q", strings.Join(c.args, " "), output, c.expectedOutput)
	}

	if c.expectedString != "" && !strings.Contains(output, c.expectedString) {
		t.Fatalf("Output didn't match for 'istioctl %s'\n got %v\nwant: %v", strings.Join(c.args, " "), output, c.expectedString)
	}

	if c.goldenFilename != "" {
		util.CompareContent(t, []byte(output), c.goldenFilename)
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
// nolint: lll
func mockClientExecFactoryGenerator(testResults map[string][]byte) func(kubeconfig, configContext string, _ string) (kube.CLIClient, error) {
	outFactory := func(_, _ string, _ string) (kube.CLIClient, error) {
		return kube.MockClient{
			Results: testResults,
		}, nil
	}

	return outFactory
}

func mockEnvoyClientFactoryGenerator(testResults map[string][]byte) func(kubeconfig, configContext string) (kube.CLIClient, error) {
	outFactory := func(_, _ string) (kube.CLIClient, error) {
		return kube.MockClient{
			Results: testResults,
		}, nil
	}

	return outFactory
}
