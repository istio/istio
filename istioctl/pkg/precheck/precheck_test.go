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
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/rest/fake"
	cmdtesting "k8s.io/kubectl/pkg/cmd/testing"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/pkg/config/analysis/diag"
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
