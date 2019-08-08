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
	"os"
	"testing"

	"istio.io/istio/pkg/test/framework/components/environment/kube"

	"istio.io/istio/pkg/test/framework"
)

type outputType string

const (
	// TextOutput describes a test which returns text output
	TextOutput outputType = "text"
	// YamlOutput describes a test which returns yaml output
	YamlOutput outputType = "yaml"
	// JSONOutput describes a test which returns json output
	JSONOutput outputType = "json"
)

const (
	istioPath = "istio.io/istio/"
)

// Example manages the steps in a test, executes them, and records the output
type Example struct {
	name  string
	steps []testStep
	t     *testing.T
}

// New returns an instance of an example test
func New(t *testing.T, name string) Example {
	return Example{
		name:  name,
		steps: make([]testStep, 0),
		t:     t,
	}
}

// AddScript adds a directive to run a script
func (example *Example) AddScript(namespace string, script string, output outputType) {
	//fullPath := getFullPath(istioPath + script)
	example.steps = append(example.steps, newStepScript("./"+script, output))
}

// AddFile adds an existing file
func (example *Example) AddFile(namespace string, path string) {
	fullPath := getFullPath(istioPath + path)
	example.steps = append(example.steps, newStepFile(namespace, fullPath))
}

// todo: get last script run output
type testFunc func(t *testing.T)

// Exec registers a callback to be invoked synchronously. This is typically used for
// validation logic to ensure command-lines worked as intended
func (example *Example) Exec(testFunction testFunc) {
	example.steps = append(example.steps, newStepFunction(testFunction))
}

// getFullPath reads the current gopath from environment variables and appends
// the specified path to it
func getFullPath(path string) string {
	gopath := os.Getenv("GOPATH")
	return gopath + "/src/" + path

}

// Run runs the scripts and capture output
// Note that this overrides os.Stdout/os.Stderr and is not thread-safe
func (example *Example) Run() {

	//override stdout and stderr for test. Is there a better way of doing this?

	/*prevStdOut := os.Stdout
	prevStdErr := os.Stderr
	defer func() {
		os.Stdout = prevStdOut
		os.Stderr = prevStdErr
	}()*/

	//f, err := os.Create(
	//os.StdOut =

	example.t.Log(fmt.Sprintf("Executing test %s (%d steps)", example.name, len(example.steps)))

	//create directory if it doesn't exist
	if _, err := os.Stat(example.name); os.IsNotExist(err) {
		err := os.Mkdir(example.name, os.ModePerm)
		if err != nil {
			example.t.Fatalf("test framework failed to create directory: %s", err)
		}
	}

	framework.
		NewTest(example.t).
		Run(func(ctx framework.TestContext) {
			kubeEnv, ok := ctx.Environment().(*kube.Environment)
			if !ok {
				example.t.Fatalf("test framework unable to get Kubernetes environment")
			}
			for _, step := range example.steps {
				output, err := step.Run(kubeEnv, example.t)
				if err != nil {
					example.t.Log(output)
					example.t.Fatal(output)
				}
			}
		})
}
