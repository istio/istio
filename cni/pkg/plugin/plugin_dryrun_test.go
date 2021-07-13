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

package plugin

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/containernetworking/plugins/pkg/testutils"
	"k8s.io/client-go/kubernetes"

	"istio.io/api/annotation"
	"istio.io/istio/pilot/cmd/pilot-agent/options"
	istioenv "istio.io/istio/pkg/test/env"
	istioenv "istio.io/istio/pkg/test/env"
)

type k8sPodInfoFunc func(*kubernetes.Clientset, string, string) (*PodInfo, error)

func generateMockK8sPodInfoFunc(pi *PodInfo) k8sPodInfoFunc {
	return func(_ *kubernetes.Clientset, _, _ string) (*PodInfo, error) {
		return pi, nil
	}
}

func TestIPTablesRuleGeneration(t *testing.T) {
	cniConf := fmt.Sprintf(conf, currentVersion, ifname, sandboxDirectory, "iptables")
	args := testSetArgs(cniConf)
	newKubeClient = mocknewK8sClient

	tests := []struct {
		name   string
		input  *PodInfo
		golden string
	}{
		{
			name: "basic",
			input: &PodInfo{
				Containers:        []string{"test", "istio-proxy"},
				InitContainers:    map[string]struct{}{"istio-validate": {}},
				Annotations:       map[string]string{annotation.SidecarStatus.Name: "true"},
				ProxyEnvironments: map[string]string{},
			},
			golden: filepath.Join(istioenv.IstioSrc, "cni/pkg/plugin/testdata/basic.txt.golden"),
		},
		{
			name: "include-exclude-ip",
			input: &PodInfo{
				Containers:     []string{"test", "istio-proxy"},
				InitContainers: map[string]struct{}{"istio-validate": {}},
				Annotations: map[string]string{
					annotation.SidecarStatus.Name:                         "true",
					annotation.SidecarTrafficIncludeOutboundIPRanges.Name: "127.0.0.0/8",
					annotation.SidecarTrafficExcludeOutboundIPRanges.Name: "10.0.0.0/8",
				},
				ProxyEnvironments: map[string]string{},
			},
			golden: filepath.Join(env.IstioSrc, "cni/pkg/plugin/testdata/include-exclude-ip.txt.golden"),
		},
		{
			name: "include-exclude-ports",
			input: &PodInfo{
				Containers:     []string{"test", "istio-proxy"},
				InitContainers: map[string]struct{}{"istio-validate": {}},
				Annotations: map[string]string{
					annotation.SidecarStatus.Name:                      "true",
					annotation.SidecarTrafficIncludeInboundPorts.Name:  "1111,2222",
					annotation.SidecarTrafficExcludeInboundPorts.Name:  "3333,4444",
					annotation.SidecarTrafficExcludeOutboundPorts.Name: "5555,6666",
				},
				ProxyEnvironments: map[string]string{},
			},
			golden: filepath.Join(env.IstioSrc, "cni/pkg/plugin/testdata/include-exclude-ports.txt.golden"),
		},
		{
			name: "tproxy",
			input: &PodInfo{
				Containers:     []string{"test", "istio-proxy"},
				InitContainers: map[string]struct{}{"istio-validate": {}},
				Annotations: map[string]string{
					annotation.SidecarStatus.Name:           "true",
					annotation.SidecarInterceptionMode.Name: redirectModeTPROXY,
				},
				ProxyEnvironments: map[string]string{},
			},
			golden: filepath.Join(env.IstioSrc, "cni/pkg/plugin/testdata/tproxy.txt.golden"),
		},
		{
			name: "DNS",
			input: &PodInfo{
				Containers:        []string{"test", "istio-proxy"},
				InitContainers:    map[string]struct{}{"istio-validate": {}},
				Annotations:       map[string]string{annotation.SidecarStatus.Name: "true"},
				ProxyEnvironments: map[string]string{options.DNSCaptureByAgent.Name: "true"},
			},
			golden: filepath.Join(env.IstioSrc, "cni/pkg/plugin/testdata/dns.txt.golden"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// TODO(bianpengyuan): How do we test ipv6 rules?
			getKubePodInfo = generateMockK8sPodInfoFunc(tt.input)
			tmpDir := t.TempDir()
			outputFilePath := filepath.Join(tmpDir, "output.txt")
			if _, err := os.Create(outputFilePath); err != nil {
				t.Fatalf("Failed to create temp file for IPTables rule output: %v", err)
			}
			os.Setenv(dryRunFilePath.Name, outputFilePath)
			_, _, err := testutils.CmdAddWithResult(
				sandboxDirectory, ifname, []byte(cniConf), func() error { return CmdAdd(args) })
			if err != nil {
				t.Fatalf("CNI cmdAdd failed with error: %v", err)
			}

			generated, err := ioutil.ReadFile(outputFilePath)
			if err != nil {
				log.Fatalf("Cannot read generated IPTables rule file: %v", err)
			}

			if os.Getenv("REFRESH_GOLDEN") == "true" {
				// Refresh iptables golden file.
				err = ioutil.WriteFile(tt.golden, generated, 0755)
				if err != nil {
					t.Fatalf("Failed to update iptables rule golden file %v", err)
				}
				return
			}

			// Compare generated iptables rule with golden files.
			golden, err := ioutil.ReadFile(tt.golden)
			if err != nil {
				log.Fatalf("Cannot read golden rule file: %v", err)
			}

			if !bytes.Equal(generated, golden) {
				t.Errorf("Unexpected IPtables rules generated, want \n%v \ngot \n%v", string(golden), string(generated))
			}
		})
	}
}
