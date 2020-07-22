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

package gateway

import (
	"fmt"
	"io/ioutil"
	"testing"

	"github.com/d4l3k/messagediff"
	"github.com/ghodss/yaml"

	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/util"
)

func TestConvertResources(t *testing.T) {
	cases := []string{"simple", "mismatch", "tls"}
	for _, tt := range cases {
		t.Run(tt, func(t *testing.T) {
			input := readConfig(t, fmt.Sprintf("testdata/%s.yaml", tt))
			output := convertResources(splitInput(input))

			goldenFile := fmt.Sprintf("testdata/%s.yaml.golden", tt)
			if util.Refresh() {
				res := append(output.Gateway, output.VirtualService...)
				if err := ioutil.WriteFile(goldenFile, marshalYaml(t, res), 0644); err != nil {
					t.Fatal(err)
				}
			}
			golden := splitOutput(readConfig(t, goldenFile))
			if diff, eq := messagediff.PrettyDiff(golden, output); !eq {
				o, _ := messagediff.PrettyDiff(output, golden)
				t.Fatalf("Diff:\n%s\nReverse:\n%s", diff, o)
			}
		})
	}
}

func splitOutput(configs []model.Config) IstioResources {
	out := IstioResources{
		Gateway:        []model.Config{},
		VirtualService: []model.Config{},
	}
	for _, c := range configs {
		switch c.GroupVersionKind {
		case gvk.Gateway:
			out.Gateway = append(out.Gateway, c)
		case gvk.VirtualService:
			out.VirtualService = append(out.VirtualService, c)
		}
	}
	return out
}

func splitInput(configs []model.Config) *KubernetesResources {
	out := &KubernetesResources{}
	for _, c := range configs {
		switch c.GroupVersionKind {
		case collections.K8SServiceApisV1Alpha1Gatewayclasses.Resource().GroupVersionKind():
			out.GatewayClass = append(out.GatewayClass, c)
		case collections.K8SServiceApisV1Alpha1Gateways.Resource().GroupVersionKind():
			out.Gateway = append(out.Gateway, c)
		case collections.K8SServiceApisV1Alpha1Httproutes.Resource().GroupVersionKind():
			out.HTTPRoute = append(out.HTTPRoute, c)
		case collections.K8SServiceApisV1Alpha1Tcproutes.Resource().GroupVersionKind():
			out.TCPRoute = append(out.TCPRoute, c)
		case collections.K8SServiceApisV1Alpha1Trafficsplits.Resource().GroupVersionKind():
			out.TrafficSplit = append(out.TrafficSplit, c)
		}
	}
	return out
}

func readConfig(t *testing.T, filename string) []model.Config {
	t.Helper()

	data, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatalf("failed to read input yaml file: %v", err)
	}
	c, _, err := crd.ParseInputsWithoutValidation(string(data))
	if err != nil {
		t.Fatalf("failed to parse CRD: %v", err)
	}
	return c
}

// Print as YAML
func marshalYaml(t *testing.T, cl []model.Config) []byte {
	t.Helper()
	result := []byte{}
	separator := []byte("---\n")
	for _, config := range cl {
		obj, err := crd.ConvertConfig(config)
		if err != nil {
			t.Fatalf("Could not decode %v: %v", config.Name, err)
		}
		bytes, err := yaml.Marshal(obj)
		if err != nil {
			t.Fatalf("Could not convert %v to YAML: %v", config, err)
		}
		result = append(result, bytes...)
		result = append(result, separator...)
	}
	return result
}
