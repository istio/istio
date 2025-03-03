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

	"github.com/containernetworking/plugins/pkg/ns"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"istio.io/api/annotation"
	"istio.io/istio/pilot/cmd/pilot-agent/options"
	diff "istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/tools/istio-iptables/pkg/cmd"
	"istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

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

func buildDryrunConf() string {
	return fmt.Sprintf(
		mockConfTmpl,
		"1.0.0",
		"1.0.0",
		"eth0",
		testSandboxDirectory,
		filepath.Dir("/tmp"),
		false,
		"iptables",
	)
}

func TestIPTablesRuleGeneration(t *testing.T) {
	cniConf := buildDryrunConf()

	customUID := int64(1000670000)
	customGID := int64(1000670001)
	zero := int64(0)

	tests := []struct {
		name        string
		annotations map[string]string
		proxyEnv    []corev1.EnvVar
		customUID   *int64
		customGID   *int64
		golden      string
	}{
		{
			name:        "basic",
			annotations: map[string]string{annotation.SidecarStatus.Name: "true"},
			proxyEnv:    []corev1.EnvVar{},
			golden:      filepath.Join(env.IstioSrc, "cni/pkg/plugin/testdata/basic.txt.golden"),
		},
		{
			name: "include-exclude-ip",
			annotations: map[string]string{
				annotation.SidecarStatus.Name:                         "true",
				annotation.SidecarTrafficIncludeOutboundIPRanges.Name: "127.0.0.0/8",
				annotation.SidecarTrafficExcludeOutboundIPRanges.Name: "10.0.0.0/8",
			},
			proxyEnv: []corev1.EnvVar{},
			golden:   filepath.Join(env.IstioSrc, "cni/pkg/plugin/testdata/include-exclude-ip.txt.golden"),
		},
		{
			name: "include-exclude-ports",
			annotations: map[string]string{
				annotation.SidecarStatus.Name:                      "true",
				annotation.SidecarTrafficIncludeInboundPorts.Name:  "1111,2222",
				annotation.SidecarTrafficExcludeInboundPorts.Name:  "3333,4444",
				annotation.SidecarTrafficExcludeOutboundPorts.Name: "5555,6666",
			},
			proxyEnv: []corev1.EnvVar{},
			golden:   filepath.Join(env.IstioSrc, "cni/pkg/plugin/testdata/include-exclude-ports.txt.golden"),
		},
		{
			name: "tproxy",
			annotations: map[string]string{
				annotation.SidecarStatus.Name:           "true",
				annotation.SidecarInterceptionMode.Name: redirectModeTPROXY,
			},
			proxyEnv: []corev1.EnvVar{},
			golden:   filepath.Join(env.IstioSrc, "cni/pkg/plugin/testdata/tproxy.txt.golden"),
		},
		{
			name:        "DNS",
			annotations: map[string]string{annotation.SidecarStatus.Name: "true"},
			proxyEnv:    []corev1.EnvVar{{Name: options.DNSCaptureByAgent.Name, Value: "true"}},
			golden:      filepath.Join(env.IstioSrc, "cni/pkg/plugin/testdata/dns.txt.golden"),
		},
		{
			name:        "invalid-drop",
			annotations: map[string]string{annotation.SidecarStatus.Name: "true"},
			proxyEnv:    []corev1.EnvVar{{Name: cmd.InvalidDropByIptables, Value: "true"}},
			golden:      filepath.Join(env.IstioSrc, "cni/pkg/plugin/testdata/invalid-drop.txt.golden"),
		},
		{
			name:        "custom-uid",
			annotations: map[string]string{annotation.SidecarStatus.Name: "true"},
			customUID:   &customUID,
			customGID:   &customGID,
			golden:      filepath.Join(env.IstioSrc, "cni/pkg/plugin/testdata/custom-uid.txt.golden"),
		},
		{
			name:        "custom-uid-zero",
			annotations: map[string]string{annotation.SidecarStatus.Name: "true"},
			customUID:   &zero,
			golden:      filepath.Join(env.IstioSrc, "cni/pkg/plugin/testdata/basic.txt.golden"),
		},
		{
			name: "custom-uid-tproxy",
			annotations: map[string]string{
				annotation.SidecarStatus.Name:           "true",
				annotation.SidecarInterceptionMode.Name: redirectModeTPROXY,
			},
			customUID: &customUID,
			customGID: &customGID,
			golden:    filepath.Join(env.IstioSrc, "cni/pkg/plugin/testdata/custom-uid-tproxy.txt.golden"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// TODO(bianpengyuan): How do we test ipv6 rules?
			getNs = generateMockGetNsFunc(testSandboxDirectory)
			tmpDir := t.TempDir()
			outputFilePath := filepath.Join(tmpDir, "output.txt")
			if _, err := os.Create(outputFilePath); err != nil {
				t.Fatalf("Failed to create temp file for IPTables rule output: %v", err)
			}
			t.Setenv(dependencies.DryRunFilePath.Name, outputFilePath)

			pod := buildFakeDryRunPod()
			pod.ObjectMeta.Annotations = tt.annotations
			pod.Spec.Containers[1].Env = tt.proxyEnv

			pod.Spec.Containers[1].SecurityContext = &corev1.SecurityContext{}

			if tt.customGID != nil {
				pod.Spec.Containers[1].SecurityContext.RunAsGroup = tt.customGID
			}

			if tt.customUID != nil {
				pod.Spec.Containers[1].SecurityContext.RunAsUser = tt.customUID
			}

			testdoAddRunWithIptablesIntercept(t, cniConf, testPodName, testNSName, pod)

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
		lines = slices.Filter(lines, func(line string) bool {
			return line != "iptables-save" && line != "ip6tables-save"
		})

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

func testdoAddRunWithIptablesIntercept(t *testing.T, stdinData, testPodName, testNSName string, objects ...runtime.Object) {
	args := buildCmdArgs(stdinData, testPodName, testNSName)

	conf, err := parseConfig(args.StdinData)
	if err != nil {
		t.Fatalf("config parse failed with error: %v", err)
	}

	// Create a kube client
	client := kube.NewFakeClient(objects...)

	err = doAddRun(args, conf, client.Kube(), IptablesInterceptRuleMgr())
	if err != nil {
		t.Fatalf("failed with error: %v", err)
	}
}

func buildFakeDryRunPod() *corev1.Pod {
	app := corev1.Container{Name: "test"}
	proxy := corev1.Container{Name: "istio-proxy"}
	validate := corev1.Container{Name: "istio-validate"}
	fakePod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "core/v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        testPodName,
			Namespace:   testNSName,
			Annotations: map[string]string{},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{app, proxy, validate},
		},
	}

	return fakePod
}
