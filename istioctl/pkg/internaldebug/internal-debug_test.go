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

package internaldebug

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/anypb"
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
	// revision should default to "default" if not set
	revision string
	noIstiod bool

	// Typically use one of the three
	expectedOutput string // Expected constant output
	expectedString string // String output is expected to contain

	wantException bool
}

func TestInternalDebug(t *testing.T) {
	cases := []execTestCase{
		{ // case 0, no args
			args:           []string{},
			noIstiod:       true,
			expectedOutput: "Error: debug type is required, you can specify --list flag to list supported debug types\n",
			wantException:  true,
		},
		{ // case 1, no istiod
			args:           []string{"adsz"},
			noIstiod:       true,
			expectedOutput: "Error: no running Istio pods in \"istio-system\"\n",
			wantException:  true,
		},
		{ // case 2, with Istiod instance
			args:           []string{"adsz"},
			expectedString: "",
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
		if c.revision == "" {
			c.revision = "default"
		}
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
			verifyExecTestOutput(t, DebugCommand(ctx), c)
		})
	}
}

func TestInternalDebugWithMultiIstiod(t *testing.T) {
	t.Cleanup(func() {
		multixds.GetXdsResponse = xds.GetXdsResponse
	})

	getXdsResponseCall := 0
	cases := []struct {
		name           string
		testcase       execTestCase
		getXdsResponse func(_ *discovery.DiscoveryRequest, _ string, _ string,
			_ clioptions.CentralControlPlaneOptions, _ []grpc.DialOption) (*discovery.DiscoveryResponse, error)
		ExceptGetXdsResponseCall int
	}{
		{
			name: "proxyID not found",
			testcase: execTestCase{
				args:           []string{"config_dump?proxyID=sleep-77ccf74cb-m6jcb"},
				expectedOutput: "Proxy not connected to this Pilot instance. It may be connected to another instance.\n",
			},
			getXdsResponse: func(_ *discovery.DiscoveryRequest, _ string, _ string, _ clioptions.CentralControlPlaneOptions, _ []grpc.DialOption,
			) (*discovery.DiscoveryResponse, error) {
				getXdsResponseCall++
				return &discovery.DiscoveryResponse{
					Resources: []*anypb.Any{
						{
							Value: []byte("Proxy not connected to this Pilot instance. It may be connected to another instance."),
						},
					},
				}, nil
			},
			ExceptGetXdsResponseCall: 2,
		},
		{
			name: "proxyID found when first call",
			testcase: execTestCase{
				args:           []string{"config_dump?proxyID=sleep-77ccf74cb-m6jcb"},
				expectedOutput: "fake config_dump for sleep-77ccf74cb-m6jcb\n",
			},
			getXdsResponse: func(_ *discovery.DiscoveryRequest, _ string, _ string, _ clioptions.CentralControlPlaneOptions, _ []grpc.DialOption,
			) (*discovery.DiscoveryResponse, error) {
				getXdsResponseCall++
				return &discovery.DiscoveryResponse{
					Resources: []*anypb.Any{
						{
							Value: []byte("fake config_dump for sleep-77ccf74cb-m6jcb"),
						},
					},
				}, nil
			},
			ExceptGetXdsResponseCall: 1,
		},
		{
			name: "proxyID found when second call",
			testcase: execTestCase{
				args:           []string{"config_dump?proxyID=sleep-77ccf74cb-m6jcb"},
				expectedOutput: "fake config_dump for sleep-77ccf74cb-m6jcb in second call\n",
			},
			getXdsResponse: func(_ *discovery.DiscoveryRequest, _ string, _ string, _ clioptions.CentralControlPlaneOptions, _ []grpc.DialOption,
			) (*discovery.DiscoveryResponse, error) {
				getXdsResponseCall++
				if getXdsResponseCall == 1 {
					return &discovery.DiscoveryResponse{
						Resources: []*anypb.Any{
							{
								Value: []byte("Proxy not connected to this Pilot instance. It may be connected to another instance."),
							},
						},
					}, nil
				}
				return &discovery.DiscoveryResponse{
					Resources: []*anypb.Any{
						{
							Value: []byte("fake config_dump for sleep-77ccf74cb-m6jcb in second call"),
						},
					},
				}, nil
			},
			ExceptGetXdsResponseCall: 2,
		},
	}

	for _, c := range cases {
		if c.testcase.revision == "" {
			c.testcase.revision = "default"
		}
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				getXdsResponseCall = 0
			}()
			multixds.GetXdsResponse = c.getXdsResponse

			ctx := cli.NewFakeContext(&cli.NewFakeContextOption{
				IstioNamespace: "istio-system",
			})
			client, err := ctx.CLIClientWithRevision(c.testcase.revision)
			require.NoError(t, err)

			_, err = client.Kube().CoreV1().Pods("istio-system").Create(context.TODO(), &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "istiod-test-1",
					Namespace: "istio-system",
					Labels: map[string]string{
						"app": "istiod",
						label.IoIstioRev.Name: c.testcase.revision,
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			}, metav1.CreateOptions{})
			require.NoError(t, err)

			_, err = client.Kube().CoreV1().Pods("istio-system").Create(context.TODO(), &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "istiod-test-2",
					Namespace: "istio-system",
					Labels: map[string]string{
						"app":                 "istiod",
						label.IoIstioRev.Name: c.testcase.revision,
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			}, metav1.CreateOptions{})
			require.NoError(t, err)

			verifyExecTestOutput(t, DebugCommand(ctx), c.testcase)
			require.Equal(t, c.ExceptGetXdsResponseCall, getXdsResponseCall)
		})
	}
}

func TestPrintAll(t *testing.T) {
	testCases := []struct {
		name     string
		input    map[string]*discovery.DiscoveryResponse
		expected string
	}{
		{
			name: "valid JSON struct",
			input: map[string]*discovery.DiscoveryResponse{
				"user": {
					Resources: []*anypb.Any{
						{Value: []byte(`{"name": "Alice", "age": 30}`)},
					},
				},
			},
			expected: `{
  "user": {
    "name": "Alice",
    "age": 30
  }
}
`,
		},
		{
			name: "valid JSON slice",
			input: map[string]*discovery.DiscoveryResponse{
				"hobbies": {
					Resources: []*anypb.Any{
						{Value: []byte(`["reading", "coding"]`)},
					},
				},
			},
			expected: `{
  "hobbies": [
    "reading",
    "coding"
  ]
}
`,
		},
		{
			name: "invalid JSON string",
			input: map[string]*discovery.DiscoveryResponse{
				"invalid": {
					Resources: []*anypb.Any{
						{Value: []byte(`invalid json data`)},
					},
				},
			},
			expected: `{
  "invalid": "invalid json data"
}
`,
		},
		{
			name: "empty key",
			input: map[string]*discovery.DiscoveryResponse{
				"": {
					Resources: []*anypb.Any{
						{Value: []byte(`empty key`)},
					},
				},
			},
			expected: `{
  "": "empty key"
}
`,
		},
		{
			name: "empty value",
			input: map[string]*discovery.DiscoveryResponse{
				"empty": {
					Resources: []*anypb.Any{
						{Value: []byte(`{}`)},
					},
				},
			},
			expected: `{
  "empty": {}
}
`,
		},
		{
			name: "the value is nil",
			input: map[string]*discovery.DiscoveryResponse{
				"nilValue": {
					Resources: []*anypb.Any{
						{Value: nil},
					},
				},
			},
			expected: `{
  "nilValue": ""
}
`,
		},
		{
			name: "special characters",
			input: map[string]*discovery.DiscoveryResponse{
				"special": {
					Resources: []*anypb.Any{
						{Value: []byte(`"Hello \"World\"!"`)},
					},
				},
			},
			expected: `{
  "special": "Hello \"World\"!"
}
`,
		},
		{
			name: "binary data",
			input: map[string]*discovery.DiscoveryResponse{
				"binary": {
					Resources: []*anypb.Any{
						{Value: []byte{0x00, 0x01, 0x02}},
					},
				},
			},
			expected: `{
  "binary": "\u0000\u0001\u0002"
}
`,
		},
		{
			name: "valid nested structures",
			input: map[string]*discovery.DiscoveryResponse{
				"nested": {
					Resources: []*anypb.Any{
						{Value: []byte(`{"user": {"name": "Bob", "tags": ["dev", "ops"]}}`)},
					},
				},
			},
			expected: `{
  "nested": {
    "user": {
      "name": "Bob",
      "tags": [
        "dev",
        "ops"
      ]
    }
  }
}
`,
		},
		{
			name: "invalid nested structures, missing a \" at the end",
			input: map[string]*discovery.DiscoveryResponse{
				"nested": {
					Resources: []*anypb.Any{
						{Value: []byte(`{"user": {"name": "Bob", "tags": ["dev", "ops]}}`)},
					},
				},
			},
			expected: `{
  "nested": "{\"user\": {\"name\": \"Bob\", \"tags\": [\"dev\", \"ops]}}"
}
`,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			dw := &DebugWriter{
				Writer:                 &buf,
				InternalDebugAllIstiod: true,
			}

			if err := dw.PrintAll(tt.input); err != nil {
				t.Fatalf("PrintAll() returned error: %v", err)
			}
			got := buf.String()
			if got != tt.expected {
				t.Errorf("Output mismatch.\nWant:\n%s\nGot:\n%s", tt.expected, got)
			}
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
