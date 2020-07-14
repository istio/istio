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
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/galley/pkg/config/source/kube"
)

var _ kube.Interfaces = &Kube{}

// Kube is a mock implementation of galley/pkg/common/Kube
type Kube struct {
	response1 []interface{}
	response2 []error

	client          kubernetes.Interface
	APIExtClientSet *fake.Clientset
}

// NewKube returns a new instance of mock Kube.
func NewKube() *Kube {
	return &Kube{
		client:          newKubeInterface(),
		APIExtClientSet: fake.NewSimpleClientset(),
	}
}

// DynamicInterface implementation.
func (k *Kube) DynamicInterface() (dynamic.Interface, error) {
	if len(k.response1) == 0 {
		panic("No more responses left")
	}

	r1 := k.response1[0]
	err := k.response2[0]
	k.response1 = k.response1[1:]
	k.response2 = k.response2[1:]

	var iface dynamic.Interface
	if r1 != nil {
		iface = r1.(dynamic.Interface)
	}
	return iface, err
}

// AddResponse adds a new response to this mock.
func (k *Kube) AddResponse(r1 interface{}, r2 error) {
	k.response1 = append(k.response1, r1)
	k.response2 = append(k.response2, r2)
}

// APIExtensionsClientset implements client.Interfaces
func (k *Kube) APIExtensionsClientset() (clientset.Interface, error) {
	return k.APIExtClientSet, nil
}

// KubeClient implements client.Interfaces
func (k *Kube) KubeClient() (kubernetes.Interface, error) {
	return k.client, nil
}
