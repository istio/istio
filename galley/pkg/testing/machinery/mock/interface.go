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
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/galley/pkg/machinery"
	crdmock "istio.io/istio/galley/pkg/testing/crd/mock"
	dmock "istio.io/istio/galley/pkg/testing/dynamic/mock"
	kmock "istio.io/istio/galley/pkg/testing/kubernetes/mock"
)

type Interface struct {
	MockCRDI           *crdmock.Interface
	MockKubernetes     *kmock.Client
	MockDynamic        *dmock.Client
	DynamicFn          DynamicFn
	DynamicErrorResult error
}

var _ machinery.Interface = &Interface{}

type DynamicFn func(gv schema.GroupVersion, kind string, listKind string) (dynamic.Interface, error)

func NewInterface() *Interface {
	i := &Interface{
		MockCRDI:       crdmock.NewInterface(),
		MockKubernetes: kmock.NewClient(),
		MockDynamic:    dmock.NewClient(),
	}

	i.DynamicFn = func(gv schema.GroupVersion, kind string, listKind string) (dynamic.Interface, error) {
		return i.MockDynamic, i.DynamicErrorResult
	}

	return i
}

func (c *Interface) CustomResourceDefinitionInterface() v1beta1.CustomResourceDefinitionInterface {
	return c.MockCRDI
}

func (c *Interface) KubernetesInterface() kubernetes.Interface {
	return c.MockKubernetes

}

func (c *Interface) DynamicInterface(gv schema.GroupVersion, kind string, listKind string) (dynamic.Interface, error) {
	return c.DynamicFn(gv, kind, listKind)
}
