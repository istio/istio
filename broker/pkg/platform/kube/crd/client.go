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
// This implementation is adopted from github.com/istio/pilot/adapter/config/crd/
package crd

import (
	"fmt"
	"os"
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

	"istio.io/istio/broker/pkg/model/config"
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

// resourceNames creates the k8s crd resource names from the schema.
// Returns Kind, Singular name, Plural name, and CRD resource name.
func resourceNames(s config.Schema) (string, string, string, string) {
	p := resourceName(s.Plural)
	return kabobCaseToCamelCase(s.Type), resourceName(s.Type), p,
		p + "." + config.IstioAPIGroup
}

// Client is a basic REST client for CRDs implementing config store
type Client struct {
	descriptor config.Descriptor

	// restconfig for REST type descriptors
	restconfig *rest.Config

	// dynamic REST client for accessing config CRDs
	dynamic *rest.RESTClient
}

// resolveConfig checks whether to use the in-cluster or out-of-cluster config.
func resolveConfig(kubeconfig string) (string, error) {
	if kubeconfig != "" {
		info, err := os.Stat(kubeconfig)
		if err != nil {
			if os.IsNotExist(err) {
				err = fmt.Errorf("kubernetes configuration file %q does not exist", kubeconfig)
			} else {
				err = multierror.Append(err, fmt.Errorf("kubernetes configuration file %q", kubeconfig))
			}
			return "", err
		}

		// if it's an empty file, switch to in-cluster config
		if info.Size() == 0 {
			glog.Info("using in-cluster configuration")
			return "", nil
		}
	}
	return kubeconfig, nil
}

// CreateRESTConfig for cluster API server, pass empty config file for in-cluster
func CreateRESTConfig(kubeconfig string) (restconfig *rest.Config, err error) {
	if kubeconfig == "" {
		restconfig, err = rest.InClusterConfig()
	} else {
		restconfig, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	}

	if err != nil {
		return
	}

	version := schema.GroupVersion{
		Group:   config.IstioAPIGroup,
		Version: config.IstioAPIVersion,
	}

	restconfig.GroupVersion = &version
	restconfig.APIPath = "/apis"
	restconfig.ContentType = runtime.ContentTypeJSON

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
	restconfig.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: serializer.NewCodecFactory(types)}

	return
}

// NewClient creates a client to Kubernetes API using a kubeconfig file.
// Use an empty value for `kubeconfig` to use the in-cluster config.
// If the kubeconfig file is empty, defaults to in-cluster config as well.
func NewClient(config string, descriptor config.Descriptor) (*Client, error) {
	for _, typ := range descriptor {
		if _, exists := knownTypes[typ.Type]; !exists {
			return nil, fmt.Errorf("missing known type for %q", typ.Type)
		}
	}

	kubeconfig, err := resolveConfig(config)
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
		descriptor: descriptor,
		restconfig: restconfig,
		dynamic:    dynamic,
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
		k, s, p, name := resourceNames(schema)
		rd := &apiextensionsv1beta1.CustomResourceDefinition{
			ObjectMeta: meta_v1.ObjectMeta{
				Name: name,
			},
			Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
				Group:   config.IstioAPIGroup,
				Version: config.IstioAPIVersion,
				Scope:   apiextensionsv1beta1.NamespaceScoped,
				Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
					Singular: s,
					Plural:   p,
					Kind:     k,
				},
			},
		}
		glog.V(2).Infof("registering CRD %q", rd)
		_, err = clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Create(rd)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
	}

	// wait for CRD being established
	errPoll := wait.Poll(500*time.Millisecond, 60*time.Second, func() (bool, error) {
	descriptor:
		for _, schema := range cl.descriptor {
			_, _, _, name := resourceNames(schema)
			rd, errGet := clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Get(name, meta_v1.GetOptions{})
			if errGet != nil {
				return false, errGet
			}
			for _, cond := range rd.Status.Conditions {
				switch cond.Type {
				case apiextensionsv1beta1.Established:
					if cond.Status == apiextensionsv1beta1.ConditionTrue {
						glog.V(2).Infof("established CRD %q", name)
						continue descriptor
					}
				case apiextensionsv1beta1.NamesAccepted:
					if cond.Status == apiextensionsv1beta1.ConditionFalse {
						errGet = multierror.Append(errGet, fmt.Errorf("name conflict: %v", cond.Reason))
					}
				}
			}
			errGet = multierror.Append(errGet, fmt.Errorf("missing status condition for %q", name))
			return false, errGet
		}
		return true, nil
	})

	if errPoll != nil {
		deleteErr := cl.DeregisterResources()
		if deleteErr != nil {
			return multierror.Append(err, deleteErr)
		}
		return err
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
		_, _, _, name := resourceNames(schema)
		err := clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Delete(name, nil)
		errs = multierror.Append(errs, err)
	}
	return errs
}

// Descriptor for the store
func (cl *Client) Descriptor() config.Descriptor {
	return cl.descriptor
}

// Get implements store interface
func (cl *Client) Get(typ, name, namespace string) (*config.Entry, bool) {
	schema, exists := cl.descriptor.GetByType(typ)
	if !exists {
		return nil, false
	}
	_, _, p, _ := resourceNames(schema)

	entry := knownTypes[typ].object.DeepCopyObject().(IstioObject)
	err := cl.dynamic.Get().
		Namespace(namespace).
		Resource(p).
		Name(name).
		Do().Into(entry)

	if err != nil {
		glog.Warning(err)
		return nil, false
	}

	out, err := convertObject(schema, entry)
	if err != nil {
		glog.Warning(err)
		return nil, false
	}
	return out, true
}

// Create implements store interface
func (cl *Client) Create(entry config.Entry) (string, error) {
	schema, exists := cl.descriptor.GetByType(entry.Type)
	if !exists {
		return "", fmt.Errorf("unrecognized type %q", entry.Type)
	}

	if err := schema.Validate(entry.Spec); err != nil {
		return "", multierror.Prefix(err, "validation error:")
	}

	out, err := convertConfig(schema, entry)
	if err != nil {
		return "", err
	}

	_, _, p, _ := resourceNames(schema)
	obj := knownTypes[schema.Type].object.DeepCopyObject().(IstioObject)
	err = cl.dynamic.Post().
		Namespace(out.GetObjectMeta().Namespace).
		Resource(p).
		Body(out).
		Do().Into(obj)
	if err != nil {
		return "", err
	}

	return obj.GetObjectMeta().ResourceVersion, nil
}

// Update implements store interface
func (cl *Client) Update(entry config.Entry) (string, error) {
	schema, exists := cl.descriptor.GetByType(entry.Type)
	if !exists {
		return "", fmt.Errorf("unrecognized type %q", entry.Type)
	}

	if err := schema.Validate(entry.Spec); err != nil {
		return "", multierror.Prefix(err, "validation error:")
	}

	if entry.ResourceVersion == "" {
		return "", fmt.Errorf("revision is required")
	}

	out, err := convertConfig(schema, entry)
	if err != nil {
		return "", err
	}

	_, _, p, _ := resourceNames(schema)
	obj := knownTypes[schema.Type].object.DeepCopyObject().(IstioObject)
	err = cl.dynamic.Put().
		Namespace(out.GetObjectMeta().Namespace).
		Resource(p).
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

	_, _, p, _ := resourceNames(schema)
	return cl.dynamic.Delete().
		Namespace(namespace).
		Resource(p).
		Name(name).
		Do().Error()
}

// List implements store interface
func (cl *Client) List(typ, namespace string) ([]config.Entry, error) {
	schema, exists := cl.descriptor.GetByType(typ)
	if !exists {
		return nil, fmt.Errorf("missing type %q", typ)
	}
	list := knownTypes[schema.Type].collection.DeepCopyObject().(IstioObjectList) // nolint
	_, _, p, _ := resourceNames(schema)
	errs := cl.dynamic.Get().
		Namespace(namespace).
		Resource(p).
		Do().Into(list)

	out := make([]config.Entry, 0)
	for _, item := range list.GetItems() { // nolint
		obj, err := convertObject(schema, item)
		if err != nil {
			errs = multierror.Append(errs, err)
		} else {
			out = append(out, *obj)
		}
	}
	return out, errs
}
