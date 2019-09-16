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
	"path"
	"testing"

	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/scopes"
)

type fileTestType struct {
	path      string
	namespace string
	delete    bool
}

func newStepFile(namespace string, path string, delete bool) testStep {
	return fileTestType{
		path:      path,
		namespace: namespace,
		delete:    delete,
	}
}

func (test fileTestType) Run(env *kube.Environment, t *testing.T) (string, error) {
	if test.delete {
		scopes.CI.Infof("Deleting %s\n", test.path)
		if err := env.Delete(test.namespace, test.path); err != nil {
			return "", err
		}
	} else {
		scopes.CI.Infof("Applying %s\n", test.path)
		if err := env.Apply(test.namespace, test.path); err != nil {
			return "", err
		}
	}

	return "", nil
}

func (test fileTestType) Copy(destination string) error {
	_, filename := path.Split(test.path)
	fmt.Printf("Copying %s to %s\n", test.path, path.Join(destination, filename))
	return copyFile(test.path, path.Join(destination, filename))
}

func (test fileTestType) String() string {
	if test.delete {
		return fmt.Sprintf("Deleting %s from %s.", test.path, test.namespace)
	} else {
		return fmt.Sprintf("Applying %s to %s.", test.path, test.namespace)
	}

}
