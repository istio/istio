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

type kubectl struct {
	kubeConfig string
}

// applyContents applies the given config contents using kubectl.
func (c *kubectl) applyContents(namespace string, contents string) error {
	f, err := writeContentsToTempFile(contents)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f) }()

	return c.apply(namespace, f)
}

// apply the config in the given filename using kubectl.
func (c *kubectl) apply(namespace string, filename string) error {
	s, err := shell.Execute("kubectl apply %s %s -f %s", c.configArg(), namespaceArg(namespace), filename)
	if err == nil {
		return nil
	}

	return fmt.Errorf("%v: %s", err, s)
}

// deleteContents deletes the given config contents using kubectl.
func (c *kubectl) deleteContents(namespace, contents string) error {
	f, err := writeContentsToTempFile(contents)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f) }()

	return c.delete(namespace, f)
}

// delete the config in the given filename using kubectl.
func (c *kubectl) delete(namespace string, filename string) error {
	s, err := shell.Execute("kubectl delete %s %s -f %s", c.configArg(), namespaceArg(namespace), filename)
	if err == nil {
		return nil
	}

	return fmt.Errorf("%v: %s", err, s)
}

// logs calls the logs command for the specified pod, with -c, if container is specified.
func (c *kubectl) logs(namespace string, pod string, container string) (string, error) {
	cmd := fmt.Sprintf("kubectl logs %s %s %s %s",
		c.configArg(), namespaceArg(namespace), pod, containerArg(container))

	s, err := shell.Execute(cmd)

	if err == nil {
		return s, nil
	}

	return "", fmt.Errorf("%v: %s", err, s)
}

func (c *kubectl) exec(namespace, pod, container, command string) (string, error) {
	return shell.Execute("kubectl exec %s %s %s %s -- %s ", pod, namespaceArg(namespace), containerArg(container), c.configArg(), command)
}

func (c *kubectl) configArg() string {
	return configArg(c.kubeConfig)
}

func configArg(kubeConfig string) string {
	if kubeConfig != "" {
		return fmt.Sprintf("--kubeconfig=%s", kubeConfig)
	}
	return ""
}

func namespaceArg(namespace string) string {
	if namespace != "" {
		return fmt.Sprintf("-n %s", namespace)
	}
	return ""
}

func containerArg(container string) string {
	if container != "" {
		return fmt.Sprintf("-c %s", container)
	}
	return ""
}

func writeContentsToTempFile(contents string) (string, error) {
	f, err := ioutil.TempFile(os.TempDir(), "kubectl")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(f.Name()) }()

	_, err = f.WriteString(contents)
	if err != nil {
		return "", err
	}
	return f.Name(), nil
}
