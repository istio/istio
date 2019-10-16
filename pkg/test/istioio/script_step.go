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

package istioio

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	testEnv "istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/file"
)

var (
	atMatch = regexp.MustCompile("@.*@")
)

// Verifier is a function used to verify output and any errors returned.
type Verifier func(stdOut string, err error) error

var _ step = &scriptStep{}

type scriptStep struct {
	script   FileSelector
	verifier Verifier
}

func newScriptStep(script FileSelector, verifier Verifier) step {
	return &scriptStep{
		script:   script,
		verifier: verifier,
	}
}

func (s *scriptStep) Run(ctx Context) {
	// Copy the script to the output directory.
	script := s.script.SelectFile(ctx)
	scriptSourceDir, scriptFileName := path.Split(script)
	if err := file.Copy(script, path.Join(ctx.OutputDir, scriptFileName)); err != nil {
		ctx.Fatalf("failed copying input files for step: %v", err)
	}

	scopes.CI.Infof("Executing %s", s.script)

	content, err := ioutil.ReadFile(script)
	if err != nil {
		ctx.Fatal("test framework failed to read script: %s", err)
	}

	// Replace @.*@ with the correct paths
	content = atMatch.ReplaceAllFunc(content, func(input []byte) []byte {
		trimmed := input[1 : len(input)-1]
		return []byte(path.Join(testEnv.IstioSrc, string(trimmed)))
	})

	cmd := exec.Command("bash")
	// Run the command from the scripts dir in case one script calls another.
	cmd.Dir = scriptSourceDir
	cmd.Stdin = strings.NewReader(string(content))
	cmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", ctx.Env.Settings().KubeConfig))
	output, err := cmd.CombinedOutput()

	// Copy the result of the command to the output directory.
	outputFileName := filepath.Join(ctx.OutputDir, scriptFileName+"_output.txt")
	if err := ioutil.WriteFile(outputFileName, output, 0644); err != nil {
		ctx.Fatalf("failed copying script result: %v", err)
	}

	// If a verification function is provided, execute that and use errors from that instead.
	// If a verification function is not provided, return errors from execution instead.
	scopes.CI.Infof("Verifying %s", s.script)
	if s.verifier != nil {
		if err := s.verifier(string(output), err); err != nil {
			ctx.Fatalf("script verification failed: %v", err)
		}
	}
}

// CurlVerifier creates a new Verifier for the set of responses.
func CurlVerifier(responses ...string) func(string, error) error {
	return func(output string, err error) error {
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
}

func checkCurlResponse(returnCode string, expectedReturnCode string) error {
	if returnCode != expectedReturnCode {
		return fmt.Errorf("expected return code to be %s; actual: %s", expectedReturnCode, returnCode)
	}
	return nil
}
