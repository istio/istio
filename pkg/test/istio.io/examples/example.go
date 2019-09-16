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
package examples

import (
	"os"
	"path"
	"testing"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/environment/kube"

	"istio.io/istio/pkg/test/framework"
)

type outputType outputTypeImpl

var (
	// TextOutput describes a test which returns text output
	TextOutput outputType = textOutput{}
	// YamlOutput describes a test which returns yaml output
	YamlOutput outputType = yamlOutput{}
	// JSONOutput describes a test which returns json output
	JSONOutput outputType = jsonOutput{}
)

const (
	istioPath = "istio.io/istio/"
)

//VerificationFunction is used to define a function that will be used to verify output and any errors returned.
type VerificationFunction func(stdOut string, err error) error

// Example manages the steps in a test, executes them, and records the output
type Example struct {
	name  string
	steps []testStep
	t     *testing.T
}

// New returns an instance of an example test
func New(t *testing.T, name string) *Example {
	return &Example{
		name:  name,
		steps: make([]testStep, 0),
		t:     t,
	}
}

// RunScript adds a directive to run a script
func (example *Example) RunScript(script string, output outputType, verifier VerificationFunction) *Example {
	example.steps = append(example.steps, newStepScript(script, output, verifier))
	return example
}

//WaitForPods adds a directive to wait for pods provided by a PodFunc to deploy
func (example *Example) WaitForPods(fetchFunc KubePodFetchFunc) *Example {
	example.steps = append(example.steps, newWaitForPodTestType(fetchFunc))
	return example
}

// Apply applies an existing file specified by path where the path is relative
// to the root of the Istio source tree.
func (example *Example) Apply(namespace string, path string) *Example {
	fullPath := getFullPath(istioPath + path)
	example.steps = append(example.steps, newStepFile(namespace, fullPath, false))
	return example
}

// Delete deletes an existing file
func (example *Example) Delete(namespace string, path string) *Example {
	fullPath := getFullPath(istioPath + path)
	example.steps = append(example.steps, newStepFile(namespace, fullPath, true))
	return example
}

type testFunc func(t *testing.T) error

// Exec registers a callback to be invoked synchronously. This is typically used for
// validation logic to ensure command-lines worked as intended
func (example *Example) Exec(testFunction testFunc) *Example {
	example.steps = append(example.steps, newStepFunction(testFunction))
	return example
}

// getFullPath reads the current gopath from environment variables and appends
// the specified path to it
func getFullPath(scriptPath string) string {
	return path.Join(env.IstioTop, "src", scriptPath)

}

// Run executes the test and captures the output
func (example *Example) Run() {
	example.t.Logf("Executing test %s (%d steps)", example.name, len(example.steps))
	//create directory if it doesn't exist
	if _, err := os.Stat("output"); os.IsNotExist(err) {
		err := os.Mkdir("output", os.ModePerm)
		if err != nil {
			example.t.Logf("test framework failed to create directory: %s", err)
			example.t.Fail()
		}
	}

	framework.
		NewTest(example.t).
		Run(func(ctx framework.TestContext) {
			kubeEnv, ok := ctx.Environment().(*kube.Environment)
			if !ok {
				example.t.Errorf("test framework unable to get Kubernetes environment")
				example.t.Fail()
			}

			for _, step := range example.steps {
				step.Copy("output/")
				output, err := step.Run(kubeEnv, example.t)

				//let the step state what it's doing.
				example.t.Logf("%s", step)
				if output != "" {
					example.t.Logf("Output: %s", output)
				}

				if err != nil {
					example.t.Errorf("Error: %s", err.Error())
					example.t.Fail()
				}
			}
		})
}
