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

package precheck

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/rest/fake"
	cmdtesting "k8s.io/kubectl/pkg/cmd/testing"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/kube"
)

type testCase struct {
	version        string
	revision       string
	expectedOutput diag.Messages
	expectedError  error
}

func Test_checkFromVersion(t *testing.T) {
	cases := []testCase{
		{
			revision:       "canary",
			version:        "1.21",
			expectedOutput: diag.Messages{},
			expectedError:  nil,
		},
		{
			revision:       "canary",
			version:        "XX",
			expectedOutput: nil,
			expectedError:  fmt.Errorf("invalid version XX, expected format like '1.0'"),
		},
		{
			revision:       "canary",
			version:        "2.0",
			expectedOutput: nil,
			expectedError:  fmt.Errorf("expected major version 1, got 2"),
		},
		{
			revision:       "canary",
			version:        "2.",
			expectedOutput: nil,
			expectedError:  fmt.Errorf("minor version is not a number: X"),
		},
		// TODO: add more test cases
		// minor <= 21 and checkPilot failing
		// minor <= 20 and checkDestinationRuleTLS failing
		// minor <= 20 and checkExternalNameAlias failing
		// minor <= 20 and checkVirtualServiceHostMatching failing
		// minor <= 21 and checkPassthroughTargetPorts failing
		// minor <= 21 and checkTracing failing
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("revision=%s, version=%s", c.revision, c.version), func(t *testing.T) {
			ctx := cli.NewFakeContext(&cli.NewFakeContextOption{
				IstioNamespace: "istio-system",
			})
			output, err := checkFromVersion(ctx, c.revision, c.version)

			assert.Equal(t, output, c.expectedOutput)

			if c.expectedError != nil {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func Test_checkTracing_ZipkinNotFound(t *testing.T) {
	cli := kube.NewFakeClient()
	messages := diag.Messages{}

	cli.Kube().CoreV1().Services("istio-system").Create(context.Background(), nil, metav1.CreateOptions{})

	err := checkTracing(cli, &messages)
	assert.Error(t, err)
}

func Test_checkTracing_ZipkinFound(t *testing.T) {
	cli := kube.NewFakeClient()
	messages := diag.Messages{}

	zipkinSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "zipkin",
			Namespace: "istio-system",
		},
	}
	cli.Kube().CoreV1().Services("istio-system").Create(context.Background(), zipkinSvc, metav1.CreateOptions{})

	err := checkTracing(cli, &messages)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(messages))

	expectedOutput := msg.NewUpdateIncompatibility(ObjectToInstance(zipkinSvc),
		"meshConfig.defaultConfig.tracer", "1.21",
		"tracing is no longer by default enabled to send to 'zipkin.istio-system.svc'; "+
			"follow https://istio.io/latest/docs/tasks/observability/distributed-tracing/telemetry-api/",
		"1.21")

	assert.Equal(t, expectedOutput, messages[0])
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
