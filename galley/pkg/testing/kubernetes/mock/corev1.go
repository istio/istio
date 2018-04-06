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

package mock

import (
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"

	"istio.io/istio/galley/pkg/testing/common"
)

type coreV1 struct {
	e              *common.MockLog
	MockNamespaces *namespaces
}

var _ corev1.CoreV1Interface = &coreV1{}

// Namespaces interface method implementation
func (c *coreV1) Namespaces() corev1.NamespaceInterface {
	return c.MockNamespaces
}

// ComponentStatuses interface method implementation
func (c *coreV1) ComponentStatuses() corev1.ComponentStatusInterface {
	panic("Not implemented")
}

// ConfigMaps interface method implementation
func (c *coreV1) ConfigMaps(namespace string) corev1.ConfigMapInterface {
	panic("Not implemented")
}

// Endpoints interface method implementation
func (c *coreV1) Endpoints(namespace string) corev1.EndpointsInterface {
	panic("Not implemented")
}

// Events interface method implementation
func (c *coreV1) Events(namespace string) corev1.EventInterface {
	panic("Not implemented")
}

// LimitRanges interface method implementation
func (c *coreV1) LimitRanges(namespace string) corev1.LimitRangeInterface {
	panic("Not implemented")
}

// Nodes interface method implementation
func (c *coreV1) Nodes() corev1.NodeInterface {
	panic("Not implemented")
}

// PersistentVolumes interface method implementation
func (c *coreV1) PersistentVolumes() corev1.PersistentVolumeInterface {
	panic("Not implemented")
}

// PersistentVolumeClaims interface method implementation
func (c *coreV1) PersistentVolumeClaims(namespace string) corev1.PersistentVolumeClaimInterface {
	panic("Not implemented")
}

// Pods interface method implementation
func (c *coreV1) Pods(namespace string) corev1.PodInterface {
	panic("Not implemented")
}

// PodTemplates interface method implementation
func (c *coreV1) PodTemplates(namespace string) corev1.PodTemplateInterface {
	panic("Not implemented")
}

// ReplicationControllers interface method implementation
func (c *coreV1) ReplicationControllers(namespace string) corev1.ReplicationControllerInterface {
	panic("Not implemented")
}

// ResourceQuotas interface method implementation
func (c *coreV1) ResourceQuotas(namespace string) corev1.ResourceQuotaInterface {
	panic("Not implemented")
}

// Secrets interface method implementation
func (c *coreV1) Secrets(namespace string) corev1.SecretInterface {
	panic("Not implemented")
}

// Services interface method implementation
func (c *coreV1) Services(namespace string) corev1.ServiceInterface {
	panic("Not implemented")
}

// ServiceAccounts interface method implementation
func (c *coreV1) ServiceAccounts(namespace string) corev1.ServiceAccountInterface {
	panic("Not implemented")
}

// RESTClient interface method implementation
func (c *coreV1) RESTClient() rest.Interface {
	panic("Not implemented")
}
