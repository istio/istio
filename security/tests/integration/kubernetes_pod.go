// Copyright 2017 Istio Authors
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

package integration

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/integration_old/framework"
)

type (
	// KubernetesPod is the test component for K8s pod
	KubernetesPod struct {
		framework.Component
		clientset *kubernetes.Clientset
		namespace string
		name      string
		image     string
		cmds      []string
		args      []string
		uuid      string
	}
)

// NewKubernetesPod create a K8s pod instance
func NewKubernetesPod(clientset *kubernetes.Clientset, namespace string, name string,
	image string, cmds []string, args []string) *KubernetesPod {
	return &KubernetesPod{
		clientset: clientset,
		namespace: namespace,
		name:      name,
		image:     image,
		cmds:      cmds,
		args:      args,
	}
}

// GetName return component name
func (c *KubernetesPod) GetName() string {
	return c.name
}

// Start is being called in framework.StartUp()
func (c *KubernetesPod) Start() (err error) {
	c.uuid = string(uuid.NewUUID())

	labels := map[string]string{
		"uuid":      c.uuid,
		"pod-group": fmt.Sprintf("%v-pod-group", c.name),
	}

	if _, err = createPod(c.clientset, c.namespace, c.image, c.name, labels, c.cmds, c.args); err != nil {
		log.Errorf("failed to create a pod: %v", c.name)
	}
	return err
}

// Stop stop this component
// Stop is being called in framework.TearDown()
func (c *KubernetesPod) Stop() (err error) {
	log.Infof("deleting the pod: %v", c.name)
	return deletePod(c.clientset, c.namespace, c.name)
}

// IsAlive check if component is alive/running
func (c *KubernetesPod) IsAlive() (bool, error) {
	if err := waitForPodRunning(c.clientset, c.namespace, c.uuid, kubernetesWaitTimeout); err != nil {
		log.Errorf("failed to create a pod: %v err: %v", c.name, err)
		return false, err
	}

	return true, nil
}
