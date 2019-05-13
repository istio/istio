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

package cmd

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/fake"

	"istio.io/api/authentication/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
)

var (
	testMesh = []model.Config{
		{
			ConfigMeta: model.ConfigMeta{
				Name:      "default",
				Namespace: "istio-system",
				Type:      model.AuthenticationMeshPolicy.Type,
				Group:     model.AuthenticationMeshPolicy.Group,
				Version:   model.AuthenticationMeshPolicy.Version,
			},
			Spec: &v1alpha1.Policy{
				Peers: []*v1alpha1.PeerAuthenticationMethod{{
					Params: &v1alpha1.PeerAuthenticationMethod_Mtls{Mtls: &v1alpha1.MutualTls{}},
				}},
			},
		},
	}
)

func TestGenerate(t *testing.T) {
	cases := []testCase{
		{
			args: strings.Split("experimental generate -f bogus.yaml", " "),
			expectedOutput: `Error: open bogus.yaml: no such file or directory
`,
			wantException: true,
		},
		{
			args:           strings.Split("experimental generate -f testdata/generate/dr-template.yaml", " "),
			expectedRegexp: regexp.MustCompile("A valid .service required, use --set service="),
			wantException:  true,
		},
		{
			args:           strings.Split("experimental generate -f testdata/generate/expose-template.yaml --set service=bookstore", " "),
			goldenFilename: "testdata/generate/exposed-service.yaml.golden",
		},
		{ // case 3
			configs:        testMesh,
			args:           strings.Split("experimental generate -f testdata/generate/dr-template.yaml --set service=reviews", " "),
			goldenFilename: "testdata/generate/dr-123.yaml.golden",
		},
	}

	mockK8sObjs := []runtime.Object{
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Service",
				"metadata": map[string]interface{}{
					"name":      "bookstore",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"ports": []interface{}{
						map[string]interface{}{
							"name": "http",
							"port": "9080",
						},
					},
				},
			},
		},

		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "extensions/v1beta1",
				"kind":       "Deployment",
				"metadata": map[string]interface{}{
					"name":      "reviews-v2",
					"namespace": "default",
					"labels": map[string]interface{}{
						"app":     "reviews",
						"version": "v2",
					},
				},
				"spec": map[string]interface{}{},
			},
		},
	}

	for i, c := range cases {
		dynamicClientFactory = mockDynamicFactoryGenerator(runtime.NewScheme(), mockK8sObjs...)

		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyOutput(t, c)
		})
	}
}

func mockDynamicFactoryGenerator(scheme *runtime.Scheme, objects ...runtime.Object) func() (dynamic.Interface, error) {
	outFactory := func() (dynamic.Interface, error) {
		return fake.NewSimpleDynamicClient(scheme, objects...), nil
	}
	return outFactory
}
