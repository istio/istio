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
	"os/exec"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

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
			podName, err := getPodName(ztunnelPods)
			if err != nil {
				t.Fatalf("Failed to get pod ID: %v", err)
			}

			// get raw config dump from the Ztunnel instance
			var configDump []byte
			if configDump, err = getConfigDumpFromEndpoint(istioCfg, podName); err != nil {
				t.Fatalf("Failed to get raw config dump from Ztunnel instance: %v", err)
			}
			var rawZtunnelDump map[string]any
			if err = json.Unmarshal(configDump, &rawZtunnelDump); err != nil {
				t.Fatalf("Failed to unmarshal JSON output for %s: %v", err)
			}

			// get the config dump generated when running the istioctl zc all command
			var istioctOutputConfigDump string
			var args []string
			g := NewWithT(t)
			args = []string{
				"--namespace=dummy",
				"zc", "all", podName, "-o", "json",
			}
			istioctOutputConfigDump, _ = istioCtl.InvokeOrFail(t, args)
			var istioctlAllOutput map[string]any
			if err = json.Unmarshal([]byte(istioctOutputConfigDump), &istioctlAllOutput); err != nil {
				t.Fatalf("Failed to unmarshal JSON output for %s: %v", strings.Join(args, " "), err)
			}

			// compare the raw config dump to the istioctl output
			g.Expect(rawZtunnelDump).To(Equal(istioctlAllOutput))
		})
}

func getConfigDumpFromEndpoint(istioCfg istio.Config, podName string) ([]byte, error) {
	path := "config_dump"
	url := fmt.Sprintf("http://localhost:15000/%s", path)
	cmd := exec.Command("kubectl", "exec", podName, "-n", istioCfg.SystemNamespace, "--", "curl", "-X", "GET", url)
	return cmd.Output()
}

func getPodName(zPods []corev1.Pod) (string, error) {
	for _, ztunnel := range zPods {
		return ztunnel.GetName(), nil
	}

	return "", fmt.Errorf("no ztunnel pod")
}
