// Copyright Istio Authors.
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

package convert

import (
	"context"
	"errors"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sLabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	listerv1 "k8s.io/client-go/listers/core/v1"
)

type podListerWrapper struct {
	client kubernetes.Interface
}

type podListerWrapperWithNamespace struct {
	client    kubernetes.Interface
	namespace string
}

func (p *podListerWrapper) List(selector k8sLabels.Selector) (ret []*v1.Pod, err error) {
	return nil, errors.New("unimplemented")
}

func (p *podListerWrapper) Pods(namespace string) listerv1.PodNamespaceLister {
	return &podListerWrapperWithNamespace{client: p.client, namespace: namespace}
}

func (p *podListerWrapperWithNamespace) List(selector k8sLabels.Selector) (ret []*v1.Pod, err error) {
	set, err := k8sLabels.ConvertSelectorToLabelsMap(selector.String())
	if err != nil {
		return nil, err
	}
	pods, err := p.client.CoreV1().Pods(p.namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: set.String()})
	if err != nil {
		return nil, err
	}
	var list []*v1.Pod
	for _, item := range pods.Items {
		list = append(list, &item)
	}
	return list, nil
}

func (p *podListerWrapperWithNamespace) Get(name string) (*v1.Pod, error) {
	return nil, errors.New("unimplemented")
}

type serviceListerWrapper struct {
	client kubernetes.Interface
}

type serviceListerWrapperWithNamespace struct {
	client    kubernetes.Interface
	namespace string
}

func (s *serviceListerWrapper) List(selector k8sLabels.Selector) (ret []*v1.Service, err error) {
	return nil, errors.New("unimplemented")
}

func (s *serviceListerWrapper) Services(namespace string) listerv1.ServiceNamespaceLister {
	return &serviceListerWrapperWithNamespace{client: s.client, namespace: namespace}
}

func (p *serviceListerWrapperWithNamespace) List(selector k8sLabels.Selector) (ret []*v1.Service, err error) {
	return nil, errors.New("unimplemented")
}

func (p *serviceListerWrapperWithNamespace) Get(name string) (*v1.Service, error) {
	return p.client.CoreV1().Services(p.namespace).Get(context.TODO(), name, metav1.GetOptions{})
}
