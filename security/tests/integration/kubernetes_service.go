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

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/integration_old/framework"
)

type (
	// KubernetesService is the test component for K8s service
	KubernetesService struct {
		framework.Component
		clientset *kubernetes.Clientset
		namespace string

		name        string
		serviceType v1.ServiceType
		port        int32
		selector    map[string]string
		annotation  map[string]string

		// internal
		uuid string
	}
)

// NewKubernetesService create a K8s service instance
func NewKubernetesService(clientset *kubernetes.Clientset, namespace string, name string,
	serviceType v1.ServiceType, port int32, selector map[string]string, annotation map[string]string) *KubernetesService {
	return &KubernetesService{
		clientset:   clientset,
		namespace:   namespace,
		name:        name,
		serviceType: serviceType,
		port:        port,
		selector:    selector,
		annotation:  annotation,
	}
}

// GetName return component name
func (c *KubernetesService) GetName() string {
	return c.name
}

// Start is being called in framework.StartUp()
func (c *KubernetesService) Start() (err error) {
	c.uuid = string(uuid.NewUUID())
	_, err = createService(
		c.clientset,
		c.namespace,
		c.name,
		c.port,
		c.serviceType,
		map[string]string{
			"uuid": c.uuid,
		},
		c.selector,
		c.annotation)
	if err != nil {
		log.Errorf("failed to create a service %v", c.name)
	}

	return err
}

// Stop stop this component
// Stop is being called in framework.TearDown()
func (c *KubernetesService) Stop() (err error) {
	log.Infof("deleting the service: %v", c.name)
	return deleteService(c.clientset, c.namespace, c.name)
}

// IsAlive checks if the component is alive/running
func (c *KubernetesService) IsAlive() (bool, error) {
	if c.serviceType == v1.ServiceTypeLoadBalancer {
		log.Infof("waiting for the load balancer to be ready...")
		if err := waitForServiceExternalIPAddress(c.clientset, c.namespace, c.uuid, kubernetesWaitTimeout); err != nil {
			return false, fmt.Errorf("failed to get the external IP adddress of %v service: %v", c.name, err)
		}
	}
	return true, nil
}
