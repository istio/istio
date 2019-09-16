// Copyright 2019 Istio Authors
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
package examples

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"testing"

	testEnv "istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/scopes"
)

type scriptTestType struct {
	script   string
	output   outputType
	verifier VerificationFunction
}

func newStepScript(script string, output outputType, verifier VerificationFunction) testStep {
	return scriptTestType{
		script:   script,
		output:   output,
		verifier: verifier,
	}
}

func (test scriptTestType) Run(env *kube.Environment, t *testing.T) (string, error) {
	scopes.CI.Infof(fmt.Sprintf("Executing %s\n", test.script))

	script, err := ioutil.ReadFile(test.script)
	if err != nil {
		return "", fmt.Errorf("test framework failed to read script: %s\n", err)
	}

	//replace @.*@ with the correct paths
	atMatch := regexp.MustCompile("@.*@")
	script = atMatch.ReplaceAllFunc(script, func(input []byte) []byte {
		trimmed := input[1 : len(input)-1]
		return []byte(path.Join(testEnv.IstioSrc, string(trimmed)))
	})

	cmd := exec.Command("bash")
	cmd.Stdin = strings.NewReader(string(script))
	cmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", env.Settings().KubeConfig))
	output, err := cmd.CombinedOutput()

	test.output.Write(test.script, output)

	//if a verification function is provided, execute that and use errors from that instead.
	//if a verification function is not provided, return errors from execution instead.
	scopes.CI.Infof("Verifying %s\n", test.script)
	if test.verifier != nil {
		return string(output), test.verifier(string(output), err)
	}

	return string(output), err
}
func (test scriptTestType) Copy(destination string) error {
	_, filename := path.Split(test.script)
	scopes.CI.Infof("Copying %s to %s\n", test.script, path.Join(destination, filename))
	return copyFile(test.script, path.Join(destination, filename))
}

func (test scriptTestType) String() string {
	return fmt.Sprintf("executing %s", test.script)
}

func checkCurlResponse(returnCode string, expectedReturnCode string) error {
	if returnCode != expectedReturnCode {
		return fmt.Errorf("expected return code to be %s; actual: %s\n", expectedReturnCode, returnCode)
	}
	return nil
}

func VerifyCurlRequests(output string, responses []string) error {
	lines := strings.Split(output, "\n")

	i := 0
	for ; i < len(lines); i++ {
		line := lines[i]
		if line == "" {
			continue
		}

		if i > len(responses) {
			return fmt.Errorf("expected %d responses; got: %d", len(responses), len(lines))
		}

		//get the http return code from end of the curl output.
		returnCodeRegex := regexp.MustCompile("[0-9]*$")
		returnCode := returnCodeRegex.FindString(line)

		//for 000, compare 000 and the next line
		if returnCode == "000" {
			scopes.CI.Infof("Curl response was 000. Processing next line as string.\n")
			if err := checkCurlResponse(returnCode, responses[i]); err != nil {
				return err
			}

			if len(lines) < i+1 || lines[i+1] != responses[i+1] {
				return fmt.Errorf("expected output to be: %s; actual was: %s", responses[i+1], lines[i+1])
			}

			i++
			continue
		}

		if err := checkCurlResponse(returnCode, responses[i]); err != nil {
			return err
		}
	}
	if i < len(responses) {
		return fmt.Errorf("expected %d responses; got: %d", len(responses), len(lines))
	}

	return nil
}

func GetCurlVerifier(responses []string) func(string, error) error {
	return func(output string, err error) error {
		return VerifyCurlRequests(output, responses)
	}
}
