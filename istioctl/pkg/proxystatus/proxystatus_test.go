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

package proxystatus

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/rest/fake"
	cmdtesting "k8s.io/kubectl/pkg/cmd/testing"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"

	"istio.io/api/label"
	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/multixds"
	"istio.io/istio/istioctl/pkg/xds"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/util/assert"
)

type execTestCase struct {
	args     []string
	revision string
	noIstiod bool

	// Typically use one of the three
	expectedOutput string // Expected constant output
	expectedString string // String output is expected to contain

	wantException bool
}

func TestProxyStatus(t *testing.T) {
	cases := []execTestCase{
		{ // case 0, with no Isitod instance
			args:           []string{},
			noIstiod:       true,
			expectedOutput: "Error: no running Istio pods in \"istio-system\"\n",
			wantException:  true,
		},
		{ // case 1, with Istiod instance but no proxies
			args:           []string{},
			expectedOutput: "Error: failed to setup status print: no proxies found (checked 1 istiods)\n",
			wantException:  true,
		},
		{ // case 2: supplying nonexistent pod name should result in error with flag
			args:          strings.Split("deployment/random-gibberish", " "),
			wantException: true,
		},
		{ // case 3: supplying nonexistent deployment name
			args:          strings.Split("deployment/random-gibberish.default", " "),
			wantException: true,
		},
		{ // case 4: supplying nonexistent deployment name in nonexistent namespace
			args:          strings.Split("deployment/random-gibberish.bogus", " "),
			wantException: true,
		},
		{ // case 5: supplying nonexistent pod name should result in error
			args:          strings.Split("random-gibberish-podname-61789237418234", " "),
			wantException: true,
		},
		{ // case 6: new --revision argument, but no proxies
			args:           strings.Split("--revision canary", " "),
			expectedOutput: "Error: failed to setup status print: no proxies found (checked 1 istiods)\n",
			revision:       "canary",
			wantException:  true,
		},
		{ // case 7: supplying type that doesn't select pods should fail
			args:          strings.Split("serviceaccount/sleep", " "),
			wantException: true,
		},
	}
	multixds.GetXdsResponse = func(_ *discovery.DiscoveryRequest, _ string, _ string, _ clioptions.CentralControlPlaneOptions, _ []grpc.DialOption,
	) (*discovery.DiscoveryResponse, error) {
		return &discovery.DiscoveryResponse{}, nil
	}
	t.Cleanup(func() {
		multixds.GetXdsResponse = xds.GetXdsResponse
	})

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			ctx := cli.NewFakeContext(&cli.NewFakeContextOption{
				IstioNamespace: "istio-system",
			})
			if !c.noIstiod {
				client, err := ctx.CLIClientWithRevision(c.revision)
				assert.NoError(t, err)
				_, err = client.Kube().CoreV1().Pods("istio-system").Create(context.TODO(), &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "istiod-test",
						Namespace: "istio-system",
						Labels: map[string]string{
							"app":                 "istiod",
							label.IoIstioRev.Name: c.revision,
						},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				}, metav1.CreateOptions{})
				assert.NoError(t, err)
			}
			verifyExecTestOutput(t, XdsStatusCommand(ctx), c)
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
