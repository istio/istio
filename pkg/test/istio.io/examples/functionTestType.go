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
	"testing"

	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/scopes"
)

type functionTestType struct {
	testFunction testFunc
}

func newStepFunction(testFunction testFunc) testStep {
	return functionTestType{
		testFunction: testFunction,
	}
}

func (test functionTestType) Run(env *kube.Environment, t *testing.T) (string, error) {
	scopes.CI.Infof(fmt.Sprintf("Executing function\n"))
	err := test.testFunction(t)

	//TODO: Fix this. Should function allow output?
	return "", err
}

func (test functionTestType) Copy(destination string) error {
	return nil
}

func (test functionTestType) String() string {
	return fmt.Sprintf("test function")
}
