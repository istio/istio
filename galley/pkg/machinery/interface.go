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

package machinery

import (
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

// Interface is a common interface for accessing Kubernetes machinery.
type Interface interface {
	CustomResourceDefinitionInterface() v1beta1.CustomResourceDefinitionInterface
	KubernetesInterface() kubernetes.Interface
	DynamicInterface(gv schema.GroupVersion, kind string, listKind string) (dynamic.Interface, error)
}

type client struct {
	config    rest.Config
	crdIface  v1beta1.CustomResourceDefinitionInterface
	dynIface  dynamic.Interface
	kubeIface kubernetes.Interface
}

var _ Interface = &client{}

// NewInterface returns a new instance of Interface
func NewInterface(config *rest.Config) (Interface, error) {

	clientset, err := v1beta1.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	dyn, err := dynamic.NewClient(config)
	if err != nil {
		return nil, err
	}

	k, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &client{
		config:    *config,
		crdIface:  clientset.CustomResourceDefinitions(),
		dynIface:  dyn,
		kubeIface: k,
	}, nil
}

// CustomResourceDefinitionInterface returns a new instance of CustomResourceDefinitionInterface
func (c *client) CustomResourceDefinitionInterface() v1beta1.CustomResourceDefinitionInterface {
	return c.crdIface
}

// DynamicInterface returns a new instance of DynamicInterface
func (c *client) DynamicInterface(gv schema.GroupVersion, kind string, listKind string) (dynamic.Interface, error) {
	configShallowCopy := c.config
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

	var client dynamic.Interface
	if client, err = dynamic.NewClient(&configShallowCopy); err != nil {
		return nil, err
	}

	return client, nil
}

// KubernetesInterface returns a new instance of KubernetesInterface
func (c *client) KubernetesInterface() kubernetes.Interface {
	return c.kubeIface
}

func addTypeToScheme(s *runtime.Scheme, gv schema.GroupVersion, kind string, listkind string) error {
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
			Kind:    listkind,
		}

		c := &unstructured.UnstructuredList{}
		o.SetAPIVersion(gv.Group)
		o.SetKind(listkind)
		s.AddKnownTypeWithName(gvk, c)

		return nil
	})

	return builder.AddToScheme(s)
}
