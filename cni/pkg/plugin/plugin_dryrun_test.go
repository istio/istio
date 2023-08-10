//go:build linux
// +build linux

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
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/pkg/testutils"
	"k8s.io/client-go/kubernetes"

	"istio.io/api/annotation"
	"istio.io/istio/pilot/cmd/pilot-agent/options"
	diff "istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/tools/istio-iptables/pkg/cmd"
	"istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

type k8sPodInfoFunc func(*kubernetes.Clientset, string, string) (*PodInfo, error)

func generateMockK8sPodInfoFunc(pi *PodInfo) k8sPodInfoFunc {
	return func(_ *kubernetes.Clientset, _, _ string) (*PodInfo, error) {
		return pi, nil
	}
}

type mockNetNs struct {
	path string
}

func (ns *mockNetNs) Do(toRun func(ns.NetNS) error) error {
	return toRun(ns)
}

func (*mockNetNs) Set() error {
	return nil
}

func (ns *mockNetNs) Path() string {
	return ns.path
}

func (*mockNetNs) Fd() uintptr {
	return 0
}

func (*mockNetNs) Close() error {
	return nil
}

type netNsFunc func(nspath string) (ns.NetNS, error)

func generateMockGetNsFunc(netNs string) netNsFunc {
	return func(nspath string) (ns.NetNS, error) {
		return &mockNetNs{path: netNs}, nil
	}
}

func TestIPTablesRuleGeneration(t *testing.T) {
	cniConf := fmt.Sprintf(conf, currentVersion, currentVersion, ifname, sandboxDirectory, "iptables")
	args := testSetArgs(cniConf)
	newKubeClient = mocknewK8sClient

	customUID := int64(1000670000)
	customGID := int64(1000670001)
	zero := int64(0)

	tests := []struct {
		name   string
		input  *PodInfo
		golden string
	}{
		{
			name: "basic",
			input: &PodInfo{
				Containers:        sets.New("test", "istio-proxy", "istio-validate"),
				Annotations:       map[string]string{annotation.SidecarStatus.Name: "true"},
				ProxyEnvironments: map[string]string{},
			},
			golden: filepath.Join(env.IstioSrc, "cni/pkg/plugin/testdata/basic.txt.golden"),
		},
		{
			name: "include-exclude-ip",
			input: &PodInfo{
				Containers: sets.New("test", "istio-proxy", "istio-validate"),
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
				Containers: sets.New("test", "istio-proxy", "istio-validate"),
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
				Containers: sets.New("test", "istio-proxy", "istio-validate"),
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
				Containers:        sets.New("test", "istio-proxy", "istio-validate"),
				Annotations:       map[string]string{annotation.SidecarStatus.Name: "true"},
				ProxyEnvironments: map[string]string{options.DNSCaptureByAgent.Name: "true"},
			},
			golden: filepath.Join(env.IstioSrc, "cni/pkg/plugin/testdata/dns.txt.golden"),
		},
		{
			name: "invalid-drop",
			input: &PodInfo{
				Containers:        sets.New("test", "istio-proxy", "istio-validate"),
				Annotations:       map[string]string{annotation.SidecarStatus.Name: "true"},
				ProxyEnvironments: map[string]string{cmd.InvalidDropByIptables.Name: "true"},
			},
			golden: filepath.Join(env.IstioSrc, "cni/pkg/plugin/testdata/invalid-drop.txt.golden"),
		},
		{
			name: "custom-uid",
			input: &PodInfo{
				Containers:  sets.New("test", "istio-proxy", "istio-validate"),
				Annotations: map[string]string{annotation.SidecarStatus.Name: "true"},
				ProxyUID:    &customUID,
				ProxyGID:    &customGID,
			},
			golden: filepath.Join(env.IstioSrc, "cni/pkg/plugin/testdata/custom-uid.txt.golden"),
		},
		{
			name: "custom-uid-zero",
			input: &PodInfo{
				Containers:  sets.New("test", "istio-proxy", "istio-validate"),
				Annotations: map[string]string{annotation.SidecarStatus.Name: "true"},
				ProxyUID:    &zero,
			},
			golden: filepath.Join(env.IstioSrc, "cni/pkg/plugin/testdata/basic.txt.golden"),
		},
		{
			name: "custom-uid-tproxy",
			input: &PodInfo{
				Containers: sets.New("test", "istio-proxy", "istio-validate"),
				Annotations: map[string]string{
					annotation.SidecarStatus.Name:           "true",
					annotation.SidecarInterceptionMode.Name: redirectModeTPROXY,
				},
				ProxyUID: &customUID,
				ProxyGID: &customGID,
			},
			golden: filepath.Join(env.IstioSrc, "cni/pkg/plugin/testdata/custom-uid-tproxy.txt.golden"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// TODO(bianpengyuan): How do we test ipv6 rules?
			getKubePodInfo = generateMockK8sPodInfoFunc(tt.input)
			getNs = generateMockGetNsFunc(sandboxDirectory)
			tmpDir := t.TempDir()
			outputFilePath := filepath.Join(tmpDir, "output.txt")
			if _, err := os.Create(outputFilePath); err != nil {
				t.Fatalf("Failed to create temp file for IPTables rule output: %v", err)
			}
			t.Setenv(dependencies.DryRunFilePath.Name, outputFilePath)
			_, _, err := testutils.CmdAddWithArgs(
				&skel.CmdArgs{
					Netns:     sandboxDirectory,
					IfName:    ifname,
					StdinData: []byte(cniConf),
				}, func() error { return CmdAdd(args) })
			if err != nil {
				t.Fatalf("CNI cmdAdd failed with error: %v", err)
			}

			generated, err := os.ReadFile(outputFilePath)
			if err != nil {
				log.Fatalf("Cannot read generated IPTables rule file: %v", err)
			}
			generatedRules := getRules(generated)

			refreshGoldens(t, tt.golden, generatedRules)

			// Compare generated iptables rule with golden files.
			golden, err := os.ReadFile(tt.golden)
			if err != nil {
				log.Fatalf("Cannot read golden rule file: %v", err)
			}
			goldenRules := getRules(golden)

			if len(generatedRules) == 0 {
				t.Error("Got empty generated rules")
			}
			if !reflect.DeepEqual(generatedRules, goldenRules) {
				t.Errorf("Unexpected IPtables rules generated, want \n%v \ngot \n%v", goldenRules, generatedRules)
			}
		})
	}
}

func getRules(b []byte) map[string]string {
	// Separate content with "COMMIT"
	parts := strings.Split(string(b), "COMMIT")
	tables := make(map[string]string)
	for _, table := range parts {
		// If table is not empty, get table name from the first line
		lines := strings.Split(strings.Trim(table, "\n"), "\n")
		if len(lines) >= 1 && strings.HasPrefix(lines[0], "* ") {
			tableName := lines[0][2:]
			lines = append(lines, "COMMIT")
			tables[tableName] = strings.Join(lines, "\n")
		}
	}
	return tables
}

func refreshGoldens(t *testing.T, goldenFileName string, generatedRules map[string]string) {
	tables := slices.Sort(maps.Keys(generatedRules))
	goldenFileContent := ""
	for _, t := range tables {
		goldenFileContent += generatedRules[t] + "\n"
	}
	diff.RefreshGoldenFile(t, []byte(goldenFileContent), goldenFileName)
}
