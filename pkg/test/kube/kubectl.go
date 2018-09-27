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

	"istio.io/istio/pkg/test/shell"
)

// ApplyContents applies the given config contents using kubectl.
func ApplyContents(kubeconfig string, namespace string, contents string) error {
	f, err := ioutil.TempFile(os.TempDir(), "kubectl")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()

	_, err = f.WriteString(contents)
	if err != nil {
		return err
	}

	return Apply(kubeconfig, namespace, f.Name())
}

// Apply the config in the given filename using kubectl.
func Apply(kubeconfig string, namespace string, filename string) error {
	namespacePart := ""
	if namespace != "" {
		namespacePart = fmt.Sprintf(" -n %s", namespace)
	}

	s, err := shell.Execute("kubectl apply --kubeconfig=%s%s -f %s", kubeconfig, namespacePart, filename)
	if err == nil {
		return nil
	}

	return fmt.Errorf("%v: %s", err, s)
}

// Delete the config in the given filename using kubectl.
func Delete(kubeconfig string, namespace string, filename string) error {
	namespacePart := ""
	if namespace != "" {
		namespacePart = fmt.Sprintf(" -n %s", namespace)
	}

	s, err := shell.Execute("kubectl delete --kubeconfig=%s%s -f %s", kubeconfig, namespacePart, filename)
	if err == nil {
		return nil
	}

	return fmt.Errorf("%v: %s", err, s)
}

// Logs calls the logs command for the specified pod, with -c, if container is specified.
func Logs(kubeConfig string, namespace string, pod string, container string) (string, error) {
	containerFlag := ""
	if container != "" {
		containerFlag += " -c " + container
	}

	cmd := fmt.Sprintf("kubectl logs --kubeconfig=%s -n %s %s%s",
		kubeConfig, namespace, pod, containerFlag)

	s, err := shell.Execute(cmd)

	if err == nil {
		return s, nil
	}

	return "", fmt.Errorf("%v: %s", err, s)
}

// GetPods calls "kubectl get pods" and returns the text.
func GetPods(kubeConfig string, namespace string) (string, error) {
	s, err := shell.Execute("kubectl get pods --kubeconfig=%s -n %s", kubeConfig, namespace)

	if err == nil {
		return s, nil
	}

	return "", fmt.Errorf("%v: %s", err, s)
}
