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
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	"istio.io/istio/istioctl/pkg/writer/ztunnel/configdump"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/assert"
)

func TestZtunnelConfig(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			// Test setup
			istioCtl := istioctl.NewOrFail(t, istioctl.Config{})
			g := NewWithT(t)
			ztunnelNS, err := locateDaemonsetNS(t, "ztunnel")
			if err != nil {
				t.Fatalf("Failed to locate ztunnel daemonset namespace: %v", err)
			}
			ztunnelPods, err := kubetest.NewPodFetch(t.AllClusters()[0], ztunnelNS, "app=ztunnel")()
			assert.NoError(t, err)
			podName, err := getPodName(ztunnelPods)
			if err != nil {
				t.Fatalf("Failed to get pod ID: %v", err)
			}

			args := []string{
				"zc", "all", podName, "--namespace", ztunnelNS, "-o", "json",
			}
			var zDumpAll configdump.ZtunnelDump
			out, _ := istioCtl.InvokeOrFail(t, args)
			err = json.Unmarshal([]byte(out), &zDumpAll)
			g.Expect(err).To(BeNil())
			g.Expect(zDumpAll).To(Not(BeNil()))
			g.Expect(zDumpAll.Services).To(Not(BeNil()))
			g.Expect(zDumpAll.Workloads).To(Not(BeNil()))
			g.Expect(zDumpAll.Policies).To(Not(BeNil()))
			g.Expect(zDumpAll.Certificates).To(Not(BeNil()))

			var zDump configdump.ZtunnelDump
			args = []string{
				"zc", "services", podName, "--namespace", ztunnelNS, "-o", "json",
			}
			out, _ = istioCtl.InvokeOrFail(t, args)
			err = unmarshalListOrMap([]byte(out), &zDump.Services)
			g.Expect(err).To(BeNil())
			g.Expect(zDump.Services).To(Not(BeNil()))

			args = []string{
				"zc", "workloads", podName, "--namespace", ztunnelNS, "-o", "json",
			}
			out, _ = istioCtl.InvokeOrFail(t, args)
			err = unmarshalListOrMap([]byte(out), &zDump.Workloads)
			g.Expect(err).To(BeNil())
			g.Expect(zDump.Workloads).To(Not(BeNil()))

			args = []string{
				"zc", "policies", podName, "--namespace", ztunnelNS, "-o", "json",
			}
			out, _ = istioCtl.InvokeOrFail(t, args)
			err = unmarshalListOrMap([]byte(out), &zDump.Policies)
			g.Expect(err).To(BeNil())
			g.Expect(zDump.Policies).To(Not(BeNil()))

			args = []string{
				"zc", "certificates", podName, "--namespace", ztunnelNS, "-o", "json",
			}
			out, _ = istioCtl.InvokeOrFail(t, args)
			err = unmarshalListOrMap([]byte(out), &zDump.Certificates)
			g.Expect(err).To(BeNil())
			g.Expect(zDump.Certificates).To(Not(BeNil()))
		})
}

func unmarshalListOrMap[T any](input json.RawMessage, i *[]T) error {
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

func getPodName(zPods []corev1.Pod) (string, error) {
	for _, ztunnel := range zPods {
		return ztunnel.GetName(), nil
	}

	return "", fmt.Errorf("no ztunnel pod")
}
