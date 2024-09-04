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
	"reflect"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	"istio.io/istio/istioctl/pkg/writer/ztunnel/configdump"
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
			// we do not know which ztunnel instance is located on the node as the workload, so we need to check all of them initially
			ztunnelPods, err := kubetest.NewPodFetch(t.AllClusters()[0], istioCfg.SystemNamespace, "app=ztunnel")()
			assert.NoError(t, err)

			podID, err := getPodID(ztunnelPods)
			if err != nil {
				t.Fatalf("Failed to get pod ID: %v", err)
			}

			var output string
			var args []string
			g := NewWithT(t)

			args = []string{
				"--namespace=dummy",
				"zc", "all", podID, "-o", "json",
			}
			output, _ = istioCtl.InvokeOrFail(t, args)

			var jsonOutput map[string]interface{}
			if err = json.Unmarshal([]byte(output), &jsonOutput); err != nil {
				t.Fatalf("Failed to unmarshal JSON output for %s: %v", strings.Join(args, " "), err)
			}

			// Use reflection to get the expected keys from the configdump.ZtunnelDump struct
			expectedKeys := getStructFieldNames(configdump.ZtunnelDump{})

			// Ensure that the unmarshaled JSON has the expected keys
			for _, key := range expectedKeys {
				g.Expect(jsonOutput).To(HaveKey(key))
			}
		})
}

func getPodID(zPods []corev1.Pod) (string, error) {
	for _, ztunnel := range zPods {
		return fmt.Sprintf("%s.%s", ztunnel.Name, ztunnel.Namespace), nil
	}

	return "", fmt.Errorf("no ztunnel pod")
}

// getStructFieldNames returns the field names of a struct as a slice of strings
func getStructFieldNames(v interface{}) []string {
	val := reflect.ValueOf(v)
	typ := val.Type()

	var fieldNames []string
	for i := 0; i < val.NumField(); i++ {
		fieldNames = append(fieldNames, typ.Field(i).Tag.Get("json"))
	}
	fmt.Println(fieldNames)
	return fieldNames
}
