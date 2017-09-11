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

// Package crd provides an implementation of the config store and cache
// using Kubernetes Custom Resources and the informer framework from Kubernetes
package crd

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/wait"
	// import GKE cluster authentication plugin
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	// import OIDC cluster authentication plugin, e.g. for Tectonic
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/pilot/model"
	"istio.io/pilot/platform/kube"
)

// IstioObject is a k8s wrapper interface for config objects
type IstioObject interface {
	runtime.Object
	GetSpec() map[string]interface{}
	SetSpec(map[string]interface{})
	GetObjectMeta() meta_v1.ObjectMeta
	SetObjectMeta(meta_v1.ObjectMeta)
}

// IstioObjectList is a k8s wrapper interface for config lists
type IstioObjectList interface {
	runtime.Object
	GetItems() []IstioObject
}

// Client is a basic REST client for CRDs implementing config store
type Client struct {
	descriptor model.ConfigDescriptor

	// restconfig for REST type descriptors
	restconfig *rest.Config

	// dynamic REST client for accessing config CRDs
	dynamic *rest.RESTClient

	// domainSuffix for the config metadata
	domainSuffix string
}

// CreateRESTConfig for cluster API server, pass empty config file for in-cluster
func CreateRESTConfig(kubeconfig string) (config *rest.Config, err error) {
	if kubeconfig == "" {
		config, err = rest.InClusterConfig()
	} else {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	}

	if err != nil {
		return
	}

	version := schema.GroupVersion{
		Group:   model.IstioAPIGroup,
		Version: model.IstioAPIVersion,
	}

	config.GroupVersion = &version
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON

	types := runtime.NewScheme()
	schemeBuilder := runtime.NewSchemeBuilder(
		func(scheme *runtime.Scheme) error {
			for _, kind := range knownTypes {
				scheme.AddKnownTypes(version, kind.object, kind.collection)
			}
			meta_v1.AddToGroupVersion(scheme, version)
			return nil
		})
	err = schemeBuilder.AddToScheme(types)
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: serializer.NewCodecFactory(types)}

	return
}

// NewClient creates a client to Kubernetes API using a kubeconfig file.
// Use an empty value for `kubeconfig` to use the in-cluster config.
// If the kubeconfig file is empty, defaults to in-cluster config as well.
func NewClient(config string, descriptor model.ConfigDescriptor, domainSuffix string) (*Client, error) {
	for _, typ := range descriptor {
		if _, exists := knownTypes[typ.Type]; !exists {
			return nil, fmt.Errorf("missing known type for %q", typ.Type)
		}
	}

	kubeconfig, err := kube.ResolveConfig(config)
	if err != nil {
		return nil, err
	}

	restconfig, err := CreateRESTConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	dynamic, err := rest.RESTClientFor(restconfig)
	if err != nil {
		return nil, err
	}

	out := &Client{
		descriptor:   descriptor,
		restconfig:   restconfig,
		dynamic:      dynamic,
		domainSuffix: domainSuffix,
	}

	return out, nil
}

// RegisterResources sends a request to create CRDs and waits for them to initialize
func (cl *Client) RegisterResources() error {
	clientset, err := apiextensionsclient.NewForConfig(cl.restconfig)
	if err != nil {
		return err
	}

	for _, schema := range cl.descriptor {
		name := ResourceName(schema.Plural) + "." + model.IstioAPIGroup
		crd := &apiextensionsv1beta1.CustomResourceDefinition{
			ObjectMeta: meta_v1.ObjectMeta{
				Name: name,
			},
			Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
				Group:   model.IstioAPIGroup,
				Version: model.IstioAPIVersion,
				Scope:   apiextensionsv1beta1.NamespaceScoped,
				Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
					Plural: ResourceName(schema.Plural),
					Kind:   kabobCaseToCamelCase(schema.Type),
				},
			},
		}
		glog.V(2).Infof("registering CRD %q", name)
		_, err = clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Create(crd)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
	}

	// wait for CRD being established
	errPoll := wait.Poll(500*time.Millisecond, 60*time.Second, func() (bool, error) {
	descriptor:
		for _, schema := range cl.descriptor {
			name := ResourceName(schema.Plural) + "." + model.IstioAPIGroup
			crd, errGet := clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Get(name, meta_v1.GetOptions{})
			if errGet != nil {
				return false, errGet
			}
			for _, cond := range crd.Status.Conditions {
				switch cond.Type {
				case apiextensionsv1beta1.Established:
					if cond.Status == apiextensionsv1beta1.ConditionTrue {
						glog.V(2).Infof("established CRD %q", name)
						continue descriptor
					}
				case apiextensionsv1beta1.NamesAccepted:
					if cond.Status == apiextensionsv1beta1.ConditionFalse {
						glog.Warningf("name conflict: %v", cond.Reason)
					}
				}
			}
			glog.V(2).Infof("missing status condition for %q", name)
			return false, nil
		}
		return true, nil
	})

	if errPoll != nil {
		deleteErr := cl.DeregisterResources()
		if deleteErr != nil {
			return multierror.Append(errPoll, deleteErr)
		}
		return errPoll
	}

	return nil
}

// DeregisterResources removes third party resources
func (cl *Client) DeregisterResources() error {
	clientset, err := apiextensionsclient.NewForConfig(cl.restconfig)
	if err != nil {
		return err
	}

	var errs error
	for _, schema := range cl.descriptor {
		name := ResourceName(schema.Plural) + "." + model.IstioAPIGroup
		err := clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Delete(name, nil)
		errs = multierror.Append(errs, err)
	}
	return errs
}

// ConfigDescriptor for the store
func (cl *Client) ConfigDescriptor() model.ConfigDescriptor {
	return cl.descriptor
}

// Get implements store interface
func (cl *Client) Get(typ, name, namespace string) (*model.Config, bool) {
	schema, exists := cl.descriptor.GetByType(typ)
	if !exists {
		return nil, false
	}

	config := knownTypes[typ].object.DeepCopyObject().(IstioObject)
	err := cl.dynamic.Get().
		Namespace(namespace).
		Resource(ResourceName(schema.Plural)).
		Name(name).
		Do().Into(config)

	if err != nil {
		glog.Warning(err)
		return nil, false
	}

	out, err := ConvertObject(schema, config, cl.domainSuffix)
	if err != nil {
		glog.Warning(err)
		return nil, false
	}
	return out, true
}

// Create implements store interface
func (cl *Client) Create(config model.Config) (string, error) {
	schema, exists := cl.descriptor.GetByType(config.Type)
	if !exists {
		return "", fmt.Errorf("unrecognized type %q", config.Type)
	}

	if err := schema.Validate(config.Spec); err != nil {
		return "", multierror.Prefix(err, "validation error:")
	}

	out, err := ConvertConfig(schema, config)
	if err != nil {
		return "", err
	}

	obj := knownTypes[schema.Type].object.DeepCopyObject().(IstioObject)
	err = cl.dynamic.Post().
		Namespace(out.GetObjectMeta().Namespace).
		Resource(ResourceName(schema.Plural)).
		Body(out).
		Do().Into(obj)
	if err != nil {
		return "", err
	}

	return obj.GetObjectMeta().ResourceVersion, nil
}

// Update implements store interface
func (cl *Client) Update(config model.Config) (string, error) {
	schema, exists := cl.descriptor.GetByType(config.Type)
	if !exists {
		return "", fmt.Errorf("unrecognized type %q", config.Type)
	}

	if err := schema.Validate(config.Spec); err != nil {
		return "", multierror.Prefix(err, "validation error:")
	}

	if config.ResourceVersion == "" {
		return "", fmt.Errorf("revision is required")
	}

	out, err := ConvertConfig(schema, config)
	if err != nil {
		return "", err
	}

	obj := knownTypes[schema.Type].object.DeepCopyObject().(IstioObject)
	err = cl.dynamic.Put().
		Namespace(out.GetObjectMeta().Namespace).
		Resource(ResourceName(schema.Plural)).
		Name(out.GetObjectMeta().Name).
		Body(out).
		Do().Into(obj)
	if err != nil {
		return "", err
	}

	return obj.GetObjectMeta().ResourceVersion, nil
}

// Delete implements store interface
func (cl *Client) Delete(typ, name, namespace string) error {
	schema, exists := cl.descriptor.GetByType(typ)
	if !exists {
		return fmt.Errorf("missing type %q", typ)
	}

	return cl.dynamic.Delete().
		Namespace(namespace).
		Resource(ResourceName(schema.Plural)).
		Name(name).
		Do().Error()
}

// List implements store interface
func (cl *Client) List(typ, namespace string) ([]model.Config, error) {
	schema, exists := cl.descriptor.GetByType(typ)
	if !exists {
		return nil, fmt.Errorf("missing type %q", typ)
	}

	list := knownTypes[schema.Type].collection.DeepCopyObject().(IstioObjectList)
	errs := cl.dynamic.Get().
		Namespace(namespace).
		Resource(ResourceName(schema.Plural)).
		Do().Into(list)

	out := make([]model.Config, 0)
	for _, item := range list.GetItems() {
		obj, err := ConvertObject(schema, item, cl.domainSuffix)
		if err != nil {
			errs = multierror.Append(errs, err)
		} else {
			out = append(out, *obj)
		}
	}
	return out, errs
}
