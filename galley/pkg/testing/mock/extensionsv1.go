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
	extensionsv1 "k8s.io/client-go/kubernetes/typed/extensions/v1beta1"
	"k8s.io/client-go/rest"
)

type extensionsv1Impl struct {
	ingresses extensionsv1.IngressInterface
}

var _ extensionsv1.ExtensionsV1beta1Interface = &extensionsv1Impl{}

func (e *extensionsv1Impl) Ingresses(namespace string) extensionsv1.IngressInterface {
	return e.ingresses
}

func (e *extensionsv1Impl) RESTClient() rest.Interface {
	panic("not implemented")
}

func (e *extensionsv1Impl) DaemonSets(namespace string) extensionsv1.DaemonSetInterface {
	panic("not implemented")
}

func (e *extensionsv1Impl) Deployments(namespace string) extensionsv1.DeploymentInterface {
	panic("not implemented")
}
func (e *extensionsv1Impl) PodSecurityPolicies() extensionsv1.PodSecurityPolicyInterface {
	panic("not implemented")
}

func (e *extensionsv1Impl) ReplicaSets(namespace string) extensionsv1.ReplicaSetInterface {
	panic("not implemented")
}

func (e *extensionsv1Impl) NetworkPolicies(namespace string) extensionsv1.NetworkPolicyInterface {
	panic("not implemented")
}
