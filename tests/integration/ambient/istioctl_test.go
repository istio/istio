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
	"slices"
	"strings"
	"testing"

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
			g := NewWithT(t)
			ztunnelPods, err := kubetest.NewPodFetch(t.AllClusters()[0], istioCfg.SystemNamespace, "app=ztunnel")()
			assert.NoError(t, err)
			podName, err := getPodName(ztunnelPods)
			if err != nil {
				t.Fatalf("Failed to get pod ID: %v", err)
			}

			var (
				args       []string
				dumpAll    configdump.ZtunnelDump
				dumpParsed configdump.ZtunnelDump
			)

			// get the raw config dump generated when running the istioctl zc all command and unmarshal it into ZtunnelDump struct
			// for test comparison
			args = []string{
				"--namespace=dummy",
				"zc", "all", podName, "-o", "json",
			}
			zcAllOutput, _ := istioCtl.InvokeOrFail(t, args)
			if err = json.Unmarshal([]byte(zcAllOutput), &dumpAll); err != nil {
				t.Fatalf("Failed to unmarshal zc all output: %v", err)
			}

			// get the config dump generated when running the istioctl zc svc command and unmarshal it into ZtunnelDump struct
			// for test comparison
			args = []string{
				"--namespace=dummy",
				"zc", "svc", podName, "-o", "json",
			}
			zcSvcOutput, _ := istioCtl.InvokeOrFail(t, args)
			if err = jsonUnmarshalListOrMap([]byte(zcSvcOutput), &dumpParsed.Services); err != nil {
				t.Fatalf("Failed to unmarshal zc svc output: %v", err)
			}
			// need to initialize the SubjectAltNames field to an empty slice to avoid nil / slice comparison that fails the test
			for _, svc := range dumpParsed.Services {
				if svc.SubjectAltNames == nil {
					svc.SubjectAltNames = []string{}
				}
			}

			// get the config dump generated when running the istioctl policies command and unmarshal it into ZtunnelDump struct
			// for test comparison
			args = []string{
				"--namespace=dummy",
				"zc", "policies", podName, "-o", "json",
			}
			zcPoliciesOutput, _ := istioCtl.InvokeOrFail(t, args)
			if err = jsonUnmarshalListOrMap([]byte(zcPoliciesOutput), &dumpParsed.Policies); err != nil {
				t.Fatalf("Failed to unmarshal zc policies output: %v", err)
			}

			// get the config dump generated when running the istioctl zc workloads command and unmarshal it into ZtunnelDump struct
			// for test comparison
			args = []string{
				"--namespace=dummy",
				"zc", "workloads", podName, "-o", "json",
			}
			zcWorkloadsOutput, _ := istioCtl.InvokeOrFail(t, args)
			if err = jsonUnmarshalListOrMap([]byte(zcWorkloadsOutput), &dumpParsed.Workloads); err != nil {
				t.Fatalf("Failed to unmarshal zc workloads output: %v", err)
			}

			// get the config dump generated when running the istioctl zc certs command and unmarshal it into ZtunnelDump struct
			// for test comparison
			args = []string{
				"--namespace=dummy",
				"zc", "certs", podName, "-o", "json",
			}
			zcCertsOutput, _ := istioCtl.InvokeOrFail(t, args)
			if err = jsonUnmarshalListOrMap([]byte(zcCertsOutput), &dumpParsed.Certificates); err != nil {
				t.Fatalf("Failed to unmarshal zc certs output: %v", err)
			}

			// sort the slices to avoid false negative comparison between the raw dump and the parsed dump
			//
			// sort certificates by identity
			slices.SortStableFunc(dumpAll.Certificates, func(a, b *configdump.CertsDump) int {
				return strings.Compare(a.Identity, b.Identity)
			})
			slices.SortStableFunc(dumpParsed.Certificates, func(a, b *configdump.CertsDump) int {
				return strings.Compare(a.Identity, b.Identity)
			})
			// sort workloads by UID
			slices.SortStableFunc(dumpAll.Workloads, func(a, b *configdump.ZtunnelWorkload) int {
				return strings.Compare(a.UID, b.UID)
			})
			slices.SortStableFunc(dumpParsed.Workloads, func(a, b *configdump.ZtunnelWorkload) int {
				return strings.Compare(a.UID, b.UID)
			})
			// sort services by hostname
			slices.SortStableFunc(dumpAll.Services, func(a, b *configdump.ZtunnelService) int {
				return strings.Compare(a.Hostname, b.Hostname)
			})
			slices.SortStableFunc(dumpParsed.Services, func(a, b *configdump.ZtunnelService) int {
				return strings.Compare(a.Hostname, b.Hostname)
			})
			// sort policies by name
			slices.SortStableFunc(dumpAll.Policies, func(a, b *configdump.ZtunnelPolicy) int {
				return strings.Compare(a.Name, b.Name)
			})
			slices.SortStableFunc(dumpParsed.Policies, func(a, b *configdump.ZtunnelPolicy) int {
				return strings.Compare(a.Name, b.Name)
			})

			// test that the config dump generated by the zc all command is the same as the config dump
			// generated by the commands zc svc, zc policies, zc workloads, and zc certs
			g.Expect(dumpAll.Services).To(Equal(dumpParsed.Services))
			g.Expect(dumpAll.Policies).To(Equal(dumpParsed.Policies))
			g.Expect(dumpAll.Workloads).To(Equal(dumpParsed.Workloads))
			g.Expect(dumpAll.Certificates).To(Equal(dumpParsed.Certificates))
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

func getPodName(zPods []corev1.Pod) (string, error) {
	for _, ztunnel := range zPods {
		return ztunnel.GetName(), nil
	}

	return "", fmt.Errorf("no ztunnel pod")
}
