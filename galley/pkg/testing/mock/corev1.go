// Copyright Istio Authors
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

package mock

import (
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
)

var _ corev1.CoreV1Interface = &corev1Impl{}

type corev1Impl struct {
	nodes      corev1.NodeInterface
	pods       corev1.PodInterface
	services   corev1.ServiceInterface
	endpoints  corev1.EndpointsInterface
	namespaces corev1.NamespaceInterface
	configmaps corev1.ConfigMapInterface
}

func (c *corev1Impl) Nodes() corev1.NodeInterface {
	return c.nodes
}

func (c *corev1Impl) Pods(namespace string) corev1.PodInterface {
	return c.pods
}

func (c *corev1Impl) Services(namespace string) corev1.ServiceInterface {
	return c.services
}

func (c *corev1Impl) Endpoints(namespace string) corev1.EndpointsInterface {
	return c.endpoints
}

func (c *corev1Impl) RESTClient() rest.Interface {
	panic("not implemented")
}

func (c *corev1Impl) ComponentStatuses() corev1.ComponentStatusInterface {
	panic("not implemented")
}

func (c *corev1Impl) ConfigMaps(namespace string) corev1.ConfigMapInterface {
	return c.configmaps
}

func (c *corev1Impl) Events(namespace string) corev1.EventInterface {
	panic("not implemented")
}

func (c *corev1Impl) LimitRanges(namespace string) corev1.LimitRangeInterface {
	panic("not implemented")
}

func (c *corev1Impl) Namespaces() corev1.NamespaceInterface {
	return c.namespaces
}

func (c *corev1Impl) PersistentVolumes() corev1.PersistentVolumeInterface {
	panic("not implemented")
}

func (c *corev1Impl) PersistentVolumeClaims(namespace string) corev1.PersistentVolumeClaimInterface {
	panic("not implemented")
}

func (c *corev1Impl) PodTemplates(namespace string) corev1.PodTemplateInterface {
	panic("not implemented")
}

func (c *corev1Impl) ReplicationControllers(namespace string) corev1.ReplicationControllerInterface {
	panic("not implemented")
}

func (c *corev1Impl) ResourceQuotas(namespace string) corev1.ResourceQuotaInterface {
	panic("not implemented")
}

func (c *corev1Impl) Secrets(namespace string) corev1.SecretInterface {
	panic("not implemented")
}

func (c *corev1Impl) ServiceAccounts(namespace string) corev1.ServiceAccountInterface {
	panic("not implemented")
}
