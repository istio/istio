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
	"io"
	"os"
	"os/exec"
	"testing"

	"istio.io/istio/pkg/test/framework/components/environment/kube"
)

type testStep interface {
	Run(*kube.Environment, *testing.T) (string, error)
	Copy(path string) error
}

type fileTestType struct {
	path      string
	namespace string
}

func newStepFile(namespace string, path string) testStep {
	return fileTestType{
		path:      path,
		namespace: namespace,
	}
}

func (test fileTestType) Run(env *kube.Environment, t *testing.T) (string, error) {
	t.Logf(fmt.Sprintf("Executing %s\n", test.path))
	if err := env.Apply(test.namespace, test.path); err != nil {
		return "", err
	}

	return "", nil
}

func (test fileTestType) Copy(path string) error {
	return copyFile(test.path, path)
}

func newStepFunction(testFunction testFunc) testStep {
	return functionTestType{
		testFunction: testFunction,
	}
}

type functionTestType struct {
	testFunction testFunc
}

func (test functionTestType) Run(env *kube.Environment, t *testing.T) (string, error) {
	t.Logf(fmt.Sprintf("Executing function\n"))
	test.testFunction(t)
	return "", nil
}

func (test functionTestType) Copy(path string) error {
	return nil
}

func newStepScript(script string, output outputType) testStep {
	return scriptTestType{
		script: script,
		output: output,
	}

	//todo: what to do for different output types?
}

type scriptTestType struct {
	script string
	output outputType
}

func (test scriptTestType) Run(env *kube.Environment, t *testing.T) (string, error) {
	t.Logf(fmt.Sprintf("Executing %s\n", test.script))
	cmd := exec.Command(test.script)

	output, err := cmd.CombinedOutput()
	return string(output), err
}
func (test scriptTestType) Copy(path string) error {
	return copyFile(test.script, path)
}

func copyFile(src string, dest string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}
	return nil
}
