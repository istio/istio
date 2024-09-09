//go:build integ
// +build integ

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

package ambient

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	"istio.io/istio/istioctl/pkg/writer/ztunnel/configdump"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/assert"
)

func TestZtunnelConfig(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			istioCtl := istioctl.NewOrFail(t, istioctl.Config{})
			istioCfg := istio.DefaultConfigOrFail(t, t)
			ztunnelPods, err := kubetest.NewPodFetch(t.AllClusters()[0], istioCfg.SystemNamespace, "app=ztunnel")()
			assert.NoError(t, err)
			podName, err := getPodName(ztunnelPods)
			if err != nil {
				t.Fatalf("Failed to get pod ID: %v", err)
			}

			// get the raw config dump generated when running the istioctl zc all command
			var rawZtunnelDump string
			var args []string
			g := NewWithT(t)
			args = []string{
				"--namespace=dummy",
				"zc", "all", podName, "-o", "json",
			}
			rawZtunnelDump, _ = istioCtl.InvokeOrFail(t, args)
			var istioctlAllOutput map[string]any
			if err = json.Unmarshal([]byte(rawZtunnelDump), &istioctlAllOutput); err != nil {
				t.Fatalf("Failed to unmarshal JSON output for %s: %v", strings.Join(args, " "), err)
			}
			fp := "istioct_all_output.json"
			_ = os.WriteFile(fp, []byte(rawZtunnelDump), 0644)

			// get output from istioctl zc service
			args = []string{
				"--namespace=dummy",
				"zc", "service", podName, "-o", "json",
			}
			ztunnelService, _ := istioCtl.InvokeOrFail(t, args)
			// var istioctlServiceOutput configdump.ZtunnelService
			// if err = json.Unmarshal([]byte(ztunnelService), &istioctlServiceOutput); err != nil {
			// 	t.Fatalf("Failed to unmarshal JSON output for %s: %v", strings.Join(args, " "), err)
			// }
			rawDump := configdump.ZtunnelDump{}
			jsonUnmarshalListOrMap([]byte(ztunnelService), &rawDump.Services)

			// get the parsed service output from istioctl zc service
			parsedDump := configdump.ZtunnelDump{}
			svcs := []*configdump.ZtunnelService{}
			val := istioctlAllOutput["services"]
			switch v := val.(type) {
			case []interface{}:
				for _, item := range v {
					svc := &configdump.ZtunnelService{}
					itemBytes, err := json.Marshal(item)
					if err != nil {
						t.Fatalf("Failed to marshal item: %v", err)
					}
					if err = json.Unmarshal(itemBytes, svc); err != nil {
						t.Fatalf("Failed to unmarshal JSON output for %s: %v", strings.Join(args, " "), err)
					}
					svcs = append(svcs, svc)
				}
			default:
				t.Fatalf("Unexpected type for 'services' key: %T", v)
			}
			parsedDump.Services = svcs

			mapParsedDumpp := make(map[string]*configdump.ZtunnelService)
			for _, svc := range parsedDump.Services {
				mapParsedDumpp[svc.Name] = svc
			}

			mapRawDumpp := make(map[string]*configdump.ZtunnelService)
			for _, svc := range rawDump.Services {
				mapRawDumpp[svc.Name] = svc
			}

			// ensure they are sorted before comparison because sorting isn't guaranteed in the output
			// sort.Slice(parsedDump.Services, func(i, j int) bool {
			// 	return parsedDump.Services[i].Name < parsedDump.Services[j].Name
			// })
			// sort.Slice(dumpp.Services, func(i, j int) bool {
			// 	return dumpp.Services[i].Name < dumpp.Services[j].Name
			// })
			// for i, svc := range parsedDump.Services {
			// 	g.Expect(svc).To(Equal(dumpp.Services[i]))
			// 	fmt.Printf("DIFF: %v\n", cmp.Diff(svc, dumpp.Services[i]))
			// 	break
			// }
			for k, v := range mapParsedDumpp {
				// g.Expect(v).To(Equal(mapRawDumpp[k]))
				fmt.Println(cmp.Diff(v, mapRawDumpp[k]))
			}

			g.Expect(rawDump.Services).To(Equal(rawDump.Services))
		})
}

func jsonUnmarshalListOrMap[T any](input json.RawMessage, i *[]T) error {
	if len(input) == 0 {
		return nil
	}
	if input[0] == '[' {
		return json.Unmarshal(input, i)
	}
	m := make(map[string]T)
	if err := json.Unmarshal(input, &m); err != nil {
		return err
	}
	*i = maps.Values(m)
	return nil
}

// getStructFieldNames returns the field names of a struct as a slice of strings
func getStructFieldNames(v any) []string {
	val := reflect.ValueOf(v)
	typ := val.Type()

	var fieldNames []string
	for i := 0; i < val.NumField(); i++ {
		fieldNames = append(fieldNames, typ.Field(i).Tag.Get("json"))
	}
	return fieldNames
}

func getStructFieldValues(v any) []string {
	val := reflect.ValueOf(v)

	var fieldValues []string
	for i := 0; i < val.NumField(); i++ {
		fieldValues = append(fieldValues, val.Field(i).String())
	}
	return fieldValues
}

func getPodName(zPods []corev1.Pod) (string, error) {
	for _, ztunnel := range zPods {
		return ztunnel.GetName(), nil
	}

	return "", fmt.Errorf("no ztunnel pod")
}
