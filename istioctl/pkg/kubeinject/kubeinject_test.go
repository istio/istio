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
package kubeinject

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/util/testutil"
)

func TestKubeInject(t *testing.T) {
	cases := []testutil.TestCase{
		{ // case 0
			Args:           []string{},
			ExpectedRegexp: regexp.MustCompile(`filename not specified \(see --filename or -f\)`),
			WantException:  true,
		},
		{ // case 1
			Args:           strings.Split("-f missing.yaml", " "),
			ExpectedRegexp: regexp.MustCompile(`open missing.yaml: no such file or directory`),
			WantException:  true,
		},
		{ // case 2
			Args: strings.Split(
				"--meshConfigFile testdata/mesh-config.yaml"+
					" --injectConfigFile testdata/inject-config.yaml -f testdata/deployment/hello.yaml"+
					" --valuesFile testdata/inject-values.yaml",
				" "),
			GoldenFilename: "testdata/deployment/hello.yaml.injected",
		},
		{ // case 3
			Args: strings.Split(
				"--meshConfigFile testdata/mesh-config.yaml"+
					" --injectConfigFile testdata/inject-config-inline.yaml -f testdata/deployment/hello.yaml"+
					" --valuesFile testdata/inject-values.yaml",
				" "),
			GoldenFilename: "testdata/deployment/hello.yaml.injected",
		},
		{ // case 4 with only iop files
			Args: strings.Split(
				"--operatorFileName testdata/istio-operator.yaml"+
					" --injectConfigFile testdata/inject-config-iop.yaml -f testdata/deployment/hello.yaml",
				" "),
			GoldenFilename: "testdata/deployment/hello.yaml.iop.injected",
		},
		{ // case 5 with only iop files
			Args: strings.Split(
				"--operatorFileName testdata/istio-operator.yaml"+
					" --injectConfigFile testdata/inject-config-inline-iop.yaml -f testdata/deployment/hello.yaml",
				" "),
			GoldenFilename: "testdata/deployment/hello.yaml.iop.injected",
		},
		{ // case 6 with iops and values override
			Args: strings.Split(
				"--operatorFileName testdata/istio-operator.yaml"+
					" --injectConfigFile testdata/inject-config-iop.yaml -f testdata/deployment/hello.yaml"+
					" -f testdata/deployment/hello.yaml"+
					" --valuesFile testdata/inject-values.yaml",
				" "),
			GoldenFilename: "testdata/deployment/hello.yaml.iop.injected",
		},
		{ // case 7
			Args: strings.Split(
				"--meshConfigFile testdata/mesh-config.yaml"+
					" --injectConfigFile testdata/inject-config.yaml -f testdata/deployment/hello-with-proxyconfig-anno.yaml"+
					" --valuesFile testdata/inject-values.yaml",
				" "),
			GoldenFilename: "testdata/deployment/hello-with-proxyconfig-anno.yaml.injected",
		},
	}

	kubeInject := InjectCommand(cli.NewFakeContext(nil))
	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.Args, " ")), func(t *testing.T) {
			testutil.VerifyOutput(t, kubeInject, c)
			cleanUpKubeInjectTestEnv()
		})
	}
}

func cleanUpKubeInjectTestEnv() {
	meshConfigFile = ""
	injectConfigFile = ""
	valuesFile = ""
	inFilename = ""
	iopFilename = ""
}
