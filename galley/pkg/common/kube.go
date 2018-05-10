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

package common

import (
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Kube interface allows access to the Kubernetes API Service methods. It is mainly used for test/injection
// purposes
type Kube interface {
	CustomResourceDefinitionInterface() (v1beta1.CustomResourceDefinitionInterface, error)
	DynamicInterface(gv schema.GroupVersion, kind string, listKind string) (dynamic.Interface, error)
	KubernetesInterface() (kubernetes.Interface, error)
}

type kube struct {
	cfg *rest.Config
}

var _ Kube = &kube{}

// NewKube returns a new instance of Kube.
func NewKube(cfg *rest.Config) Kube {
	return &kube{
		cfg: cfg,
	}
}

// CustomResourceDefinitionInterface returns a new instnace of v1beta1.CustomResourceDefinitionInterface.
func (k *kube) CustomResourceDefinitionInterface() (v1beta1.CustomResourceDefinitionInterface, error) {
	c, err := clientset.NewForConfig(k.cfg)
	if err != nil {
		return nil, err
	}
	return c.ApiextensionsV1beta1().CustomResourceDefinitions(), nil
}

// DynamicInterface returns a new dynamic.Interface for the specified API Group/Version.
func (k *kube) DynamicInterface(gv schema.GroupVersion, kind, listKind string) (dynamic.Interface, error) {
	configShallowCopy := *k.cfg

	configShallowCopy.GroupVersion = &gv
	configShallowCopy.APIPath = "/apis"
	configShallowCopy.ContentType = runtime.ContentTypeJSON

	scheme := runtime.NewScheme()
	metav1.AddToGroupVersion(scheme, schema.GroupVersion{Version: "v1"})

	err := addTypeToScheme(scheme, gv, kind, listKind)
	if err != nil {
		return nil, err
	}
	configShallowCopy.NegotiatedSerializer = serializer.DirectCodecFactory{
		CodecFactory: serializer.NewCodecFactory(scheme),
	}

	return dynamic.NewClient(&configShallowCopy)
}

// KubernetesInterface returns a new kubernetes.Interface.
func (k *kube) KubernetesInterface() (kubernetes.Interface, error) {
	return kubernetes.NewForConfig(k.cfg)
}

func addTypeToScheme(s *runtime.Scheme, gv schema.GroupVersion, kind, listKind string) error {
	builder := runtime.NewSchemeBuilder(func(s *runtime.Scheme) error {
		// Add the object itself
		gvk := schema.GroupVersionKind{
			Group:   gv.Group,
			Version: gv.Version,
			Kind:    kind,
		}

		o := &unstructured.Unstructured{}
		o.SetAPIVersion(gv.Version)
		o.SetKind(kind)
		s.AddKnownTypeWithName(gvk, o)

		// Add the collection object.
		gvk = schema.GroupVersionKind{
			Group:   gv.Group,
			Version: gv.Version,
			Kind:    listKind,
		}

		c := &unstructured.UnstructuredList{}
		o.SetAPIVersion(gv.Group)
		o.SetKind(listKind)
		s.AddKnownTypeWithName(gvk, c)

		return nil
	})

	return builder.AddToScheme(s)
}
