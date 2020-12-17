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
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/fake"

	"istio.io/istio/pilot/pkg/xds"
)

func TestWaitCmd(t *testing.T) {
	cannedResponseObj := []xds.SyncedVersions{
		{
			ProxyID:         "foo",
			ClusterVersion:  "1",
			ListenerVersion: "1",
			RouteVersion:    "1",
		},
	}
	cannedResponse, _ := json.Marshal(cannedResponseObj)
	cannedResponseMap := map[string][]byte{"onlyonepilot": cannedResponse}

	cases := []execTestCase{
		{
			execClientConfig: cannedResponseMap,
			args:             strings.Split("x wait --resource-version=2 --timeout=2s virtual-service foo.default", " "),
			wantException:    true,
		},
		{
			execClientConfig: cannedResponseMap,
			args:             strings.Split("x wait --resource-version=1 virtual-service foo.default", " "),
			wantException:    false,
		},
		{
			execClientConfig: cannedResponseMap,
			args:             strings.Split("x wait --resource-version=1 VirtualService foo.default", " "),
			wantException:    false,
		},
		{
			execClientConfig: cannedResponseMap,
			args:             strings.Split("x wait --resource-version=1 not-service foo.default", " "),
			wantException:    true,
		},
		{
			execClientConfig: cannedResponseMap,
			args:             strings.Split("x wait --timeout 2s virtual-service bar.default", " "),
			wantException:    true,
			expectedOutput:   "Error: timeout expired before resource VirtualService/default/bar became effective on all sidecars\n",
		},
		{
			execClientConfig: cannedResponseMap,
			args:             strings.Split("x wait --timeout 2s virtualservice foo.default", " "),
			wantException:    false,
		},
		{
			execClientConfig: cannedResponseMap,
			args:             strings.Split("x wait --revision canary virtualservice foo.default", " "),
			wantException:    false,
		},
	}

	_ = setupK8Sfake()

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyExecTestOutput(t, c)
		})
	}
}

func setupK8Sfake() *fake.FakeDynamicClient {
	objs := []runtime.Object{
		newUnstructured("networking.istio.io/v1alpha3", "virtualservice", "default", "foo", "1"),
		newUnstructured("networking.istio.io/v1alpha3", "virtualservice", "default", "bar", "3"),
	}
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), objs...)
	clientGetter = func(_, _ string) (dynamic.Interface, error) {
		return client, nil
	}
	return client
}

func newUnstructured(apiVersion, kind, namespace, name, resourceVersion string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata": map[string]interface{}{
				"namespace":       namespace,
				"name":            name,
				"resourceVersion": resourceVersion,
			},
		},
	}
}

func TestContainsVersion(t *testing.T) {
	var tests = []struct {
		Name             string
		AcceptedVersions []string
		Version          string
		Result           bool
	}{
		{
			Name: "all accepted version length less than 8 and contains",
			AcceptedVersions: []string{
				"1234",
				"5678",
			},
			Version: "5678",
			Result:  true,
		},
		{
			Name: "all accepted version length less than 8 and not contains",
			AcceptedVersions: []string{
				"1234",
				"5678",
			},
			Version: "9999",
			Result:  false,
		},
		{
			Name: "accepted version length more than 8 and contains",
			AcceptedVersions: []string{
				"12345678",
				"5678",
			},
			Version: "123456789",
			Result:  true,
		},
		{
			Name: "accepted version length more than 8 and not contains",
			AcceptedVersions: []string{
				"12345678",
				"56781023",
			},
			Version: "123456779",
			Result:  false,
		},
	}
	for _, i := range tests {
		t.Run(i.Name, func(t *testing.T) {
			if containsVersion(i.AcceptedVersions, i.Version) != i.Result {
				if i.Result {
					t.Errorf("excepted contains %s", i.Version)
				} else {
					t.Errorf("excepted not contains %s", i.Version)
				}
			}
		})
	}
}
