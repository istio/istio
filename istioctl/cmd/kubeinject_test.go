// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package cmd

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"istio.io/istio/pilot/test/util"
)

func TestKubeInject(t *testing.T) {
	cases := []testCase{
		{ // case 0
			args:           strings.Split("kube-inject", " "),
			expectedRegexp: regexp.MustCompile(`filename not specified \(see --filename or -f\)`),
			wantException:  true,
		},
		{ // case 1
			args:           strings.Split("kube-inject -f missing.yaml", " "),
			expectedRegexp: regexp.MustCompile(`open missing.yaml: no such file or directory`),
			wantException:  true,
		},
		{ // case 2
			args: strings.Split(
				"kube-inject --meshConfigFile testdata/mesh-config.yaml"+
					" --injectConfigFile testdata/inject-config.yaml -f testdata/deployment/hello.yaml"+
					" --valuesFile testdata/inject-values.yaml",
				" "),
			goldenFilename: "testdata/deployment/hello.yaml.injected",
		},
		{ // case 3
			args: strings.Split(
				"kube-inject --meshConfigFile testdata/mesh-config.yaml"+
					" --injectConfigFile testdata/inject-config-inline.yaml -f testdata/deployment/hello.yaml"+
					" --valuesFile testdata/inject-values.yaml",
				" "),
			goldenFilename: "testdata/deployment/hello.yaml.injected",
		},
		{ // case 4 with only iop files
			args: strings.Split(
				"kube-inject --operatorFileName testdata/istio-operator.yaml"+
					" --injectConfigFile testdata/inject-config-iop.yaml -f testdata/deployment/hello.yaml",
				" "),
			goldenFilename: "testdata/deployment/hello.yaml.iop.injected",
		},
		{ // case 5 with only iop files
			args: strings.Split(
				"kube-inject --operatorFileName testdata/istio-operator.yaml"+
					" --injectConfigFile testdata/inject-config-inline-iop.yaml -f testdata/deployment/hello.yaml",
				" "),
			goldenFilename: "testdata/deployment/hello.yaml.iop.injected",
		},
		{ // case 6 with iops and values override
			args: strings.Split(
				"kube-inject --operatorFileName testdata/istio-operator.yaml"+
					" --injectConfigFile testdata/inject-config-iop.yaml -f testdata/deployment/hello.yaml"+
					" -f testdata/deployment/hello.yaml"+
					" --valuesFile testdata/inject-values.yaml",
				" "),
			goldenFilename: "testdata/deployment/hello.yaml.iop.injected",
		},
		{ // case 7
			args: strings.Split(
				"kube-inject --meshConfigFile testdata/mesh-config.yaml"+
					" --injectConfigFile testdata/inject-config.yaml -f testdata/deployment/hello-with-proxyconfig-anno.yaml"+
					" --valuesFile testdata/inject-values.yaml",
				" "),
			goldenFilename: "testdata/deployment/hello-with-proxyconfig-anno.yaml.injected",
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyOutput(t, c)
		})
	}
}

func TestKubeInjectWithoutWebhook(t *testing.T) {
	t.Helper()

	args := strings.Split("kube-inject -f testdata/deployment/hello.yaml", " ")

	interfaceFactory = func(kubeconfig string) (kubernetes.Interface, error) {
		cli := fake.NewSimpleClientset(&v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      defaultInjectWebhookConfigName,
				Namespace: istioNamespace,
				Labels: map[string]string{
					"app":   "sidecar-injector",
					"istio": "sidecar-injector",
				},
			},
			Data: map[string]string{
				"values": `global:
  suffix: test
`,
				"config": `templates:
  sidecar: |-
    spec:
      initContainers:
      - name: istio-init
        image: docker.io/istio/proxy_init:unittest-{{.Values.global.suffix}}
      containers:
      - name: istio-proxy
        image: docker.io/istio/proxy_debug:unittest
`,
			},
		}, &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "istio",
				Namespace: istioNamespace,
			},
			Data: map[string]string{
				"mesh": "",
			},
		})
		return cli, nil
	}

	var out bytes.Buffer
	rootCmd := GetRootCmd(args)
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	// shouldn't have errors
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	output := out.String()

	util.CompareContent(t, []byte(output), "testdata/deployment/hello.yaml.injected")
}
