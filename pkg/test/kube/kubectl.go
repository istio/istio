//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package kube

import (
	"fmt"
	"io/ioutil"
	"os"
)

// ApplyContents applies the given config contents using kubectl.
func ApplyContents(kubeconfig string, ns string, contents string) error {
	f, err := ioutil.TempFile(os.TempDir(), "kubectl")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()

	_, err = f.WriteString(contents)
	if err != nil {
		return err
	}

	return Apply(kubeconfig, ns, f.Name())
}

// Apply the config in the given filename using kubectl.
func Apply(kubeconfig string, ns string, filename string) error {
	s, err := execute("kubectl apply --kubeconfig=%s -n %s -f %s", kubeconfig, ns, filename)
	if err == nil {
		return nil
	}
	return fmt.Errorf("%v: %s", err, s)
}
