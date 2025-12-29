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

package ztunnelconfig

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/rest/fake"
	cmdtesting "k8s.io/kubectl/pkg/cmd/testing"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/pkg/kube"
)

type execTestCase struct {
	execClientConfig map[string][]byte
	args             []string

	// Typically use one of the three
	expectedOutput string // Expected constant output
	expectedString string // String output is expected to contain

	wantException bool
}

func TestProxyConfig(t *testing.T) {
	loggingConfig := map[string][]byte{
		"ztunnel-9v7nw": []byte("current log level is debug"),
	}
	cases := []execTestCase{
		{
			args:           []string{},
			expectedString: "A group of commands used to update or retrieve Ztunnel",
		},
		{ // logger name invalid when namespacing is used improperly
			execClientConfig: loggingConfig,
			args:             strings.Split("log ztunnel-9v7nw --level ztunnel:::pool:debug", " "),
			expectedString:   "unrecognized logging level: pool:debug",
			wantException:    true,
		},
		{ // logger name valid and logging level valid
			execClientConfig: loggingConfig,
			args:             strings.Split("log ztunnel-9v7nw --level ztunnel::pool:debug", " "),
			expectedString:   "",
			wantException:    false,
		},
		{ // set ztunnel logging level
			execClientConfig: loggingConfig,
			args:             strings.Split("log ztunnel-9v7nw --level debug", " "),
			expectedString:   "current log level is debug",
			wantException:    false,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyExecTestOutput(t, ZtunnelConfig(cli.NewFakeContext(&cli.NewFakeContextOption{
				Results:   c.execClientConfig,
				Namespace: "default",
			})), c)
		})
	}
}

func verifyExecTestOutput(t *testing.T, cmd *cobra.Command, c execTestCase) {
	t.Helper()

	var out bytes.Buffer
	cmd.SetArgs(c.args)
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	fErr := cmd.Execute()
	output := out.String()

	if c.expectedOutput != "" && c.expectedOutput != output {
		t.Fatalf("Unexpected output for 'istioctl %s'\n got: %q\nwant: %q", strings.Join(c.args, " "), output, c.expectedOutput)
	}

	if c.expectedString != "" && !strings.Contains(output, c.expectedString) {
		t.Fatalf("Output didn't match for '%s %s'\n got %v\nwant: %v", cmd.Name(), strings.Join(c.args, " "), output, c.expectedString)
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

func init() {
	cli.MakeKubeFactory = func(k kube.CLIClient) cmdutil.Factory {
		tf := cmdtesting.NewTestFactory()
		_, _, codec := cmdtesting.NewExternalScheme()
		tf.UnstructuredClient = &fake.RESTClient{
			NegotiatedSerializer: resource.UnstructuredPlusDefaultContentConfig().NegotiatedSerializer,
			Resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     cmdtesting.DefaultHeader(),
				Body: cmdtesting.ObjBody(codec,
					cmdtesting.NewInternalType("", "", "foo")),
			},
		}
		return tf
	}
}
