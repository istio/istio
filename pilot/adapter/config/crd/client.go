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
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
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

const (
	// IstioAPIGroup defines Kubernetes API group for CRD
	IstioAPIGroup = "config.istio.io"

	// IstioResourceVersion defines Kubernetes API group version
	IstioResourceVersion = "v1alpha1"

	// IstioKindName defines the shared CRD kind to avoid boilerplate
	// code for each custom kind
	IstioKindName = "IstioKind"
)

// Client is a basic REST client for CRDs implementing config store
type Client struct {
	descriptor model.ConfigDescriptor

	// restconfig for REST type descriptors
	restconfig *rest.Config

	// dynamic REST client for accessing config CRDs
	dynamic *rest.RESTClient

	// namespace is the namespace for storing CRDs
	namespace string
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
		Group:   IstioAPIGroup,
		Version: IstioResourceVersion,
	}

	config.GroupVersion = &version
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON

	types := runtime.NewScheme()
	schemeBuilder := runtime.NewSchemeBuilder(
		func(scheme *runtime.Scheme) error {
			scheme.AddKnownTypes(
				version, &IstioKind{}, &IstioKindList{},
			)
			meta_v1.AddToGroupVersion(scheme, version)
			return nil
		})
	err = schemeBuilder.AddToScheme(types)
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: serializer.NewCodecFactory(types)}

	return
}

// NewClient creates a client to Kubernetes API using a kubeconfig file.
// namespace argument provides the namespace to store CRDs
// Use an empty value for `kubeconfig` to use the in-cluster config.
// If the kubeconfig file is empty, defaults to in-cluster config as well.
func NewClient(config string, descriptor model.ConfigDescriptor, namespace string) (*Client, error) {
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
		descriptor: descriptor,
		restconfig: restconfig,
		dynamic:    dynamic,
		namespace:  namespace,
	}

	return out, nil
}

// RegisterResources sends a request to create CRDs and waits for them to initialize
func (cl *Client) RegisterResources() error {
	clientset, err := apiextensionsclient.NewForConfig(cl.restconfig)
	if err != nil {
		return err
	}

	name := strings.ToLower(IstioKindName) + "s." + IstioAPIGroup

	crd := &apiextensionsv1beta1.CustomResourceDefinition{
		ObjectMeta: meta_v1.ObjectMeta{
			Name: name,
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group:   IstioAPIGroup,
			Version: IstioResourceVersion,
			Scope:   apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural: strings.ToLower(IstioKindName) + "s",
				Kind:   IstioKindName,
			},
		},
	}
	_, err = clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Create(crd)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	// wait for CRD being established
	err = wait.Poll(500*time.Millisecond, 60*time.Second, func() (bool, error) {
		crd, err = clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Get(name, meta_v1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, cond := range crd.Status.Conditions {
			switch cond.Type {
			case apiextensionsv1beta1.Established:
				if cond.Status == apiextensionsv1beta1.ConditionTrue {
					return true, err
				}
			case apiextensionsv1beta1.NamesAccepted:
				if cond.Status == apiextensionsv1beta1.ConditionFalse {
					glog.Warningf("name conflict: %v", cond.Reason)
				}
			}
		}
		return false, err
	})

	if err != nil {
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

	name := strings.ToLower(IstioKindName) + "s." + IstioAPIGroup
	return clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Delete(name, nil)
}

// ConfigDescriptor for the store
func (cl *Client) ConfigDescriptor() model.ConfigDescriptor {
	return cl.descriptor
}

// Get implements store interface
func (cl *Client) Get(typ, key string) (proto.Message, bool, string) {
	schema, exists := cl.descriptor.GetByType(typ)
	if !exists {
		return nil, false, ""
	}

	config := &IstioKind{}
	err := cl.dynamic.Get().
		Namespace(cl.namespace).
		Resource(IstioKindName + "s").
		Name(configKey(typ, key)).
		Do().Into(config)

	if err != nil {
		glog.Warning(err)
		return nil, false, ""
	}

	out, err := schema.FromJSONMap(config.Spec)
	if err != nil {
		glog.Warningf("%v for %#v", err, config.Spec)
		return nil, false, ""
	}
	return out, true, config.ObjectMeta.ResourceVersion
}

// Post implements store interface
func (cl *Client) Post(v proto.Message) (string, error) {
	messageName := proto.MessageName(v)
	schema, exists := cl.descriptor.GetByMessageName(messageName)
	if !exists {
		return "", fmt.Errorf("unrecognized message name %q", messageName)
	}

	if err := schema.Validate(v); err != nil {
		return "", multierror.Prefix(err, "validation error:")
	}

	out, err := modelToKube(schema, cl.namespace, v)
	if err != nil {
		return "", err
	}

	config := &IstioKind{}
	err = cl.dynamic.Post().
		Namespace(out.ObjectMeta.Namespace).
		Resource(IstioKindName + "s").
		Body(out).
		Do().Into(config)
	if err != nil {
		return "", err
	}

	return config.ObjectMeta.ResourceVersion, nil
}

// Put implements store interface
func (cl *Client) Put(v proto.Message, revision string) (string, error) {
	messageName := proto.MessageName(v)
	schema, exists := cl.descriptor.GetByMessageName(messageName)
	if !exists {
		return "", fmt.Errorf("unrecognized message name %q", messageName)
	}

	if err := schema.Validate(v); err != nil {
		return "", multierror.Prefix(err, "validation error:")
	}

	if revision == "" {
		return "", fmt.Errorf("revision is required")
	}

	out, err := modelToKube(schema, cl.namespace, v)
	if err != nil {
		return "", err
	}

	out.ObjectMeta.ResourceVersion = revision

	config := &IstioKind{}
	err = cl.dynamic.Put().
		Namespace(out.ObjectMeta.Namespace).
		Resource(IstioKindName + "s").
		Name(out.ObjectMeta.Name).
		Body(out).
		Do().Into(config)
	if err != nil {
		return "", err
	}

	return config.ObjectMeta.ResourceVersion, nil
}

// Delete implements store interface
func (cl *Client) Delete(typ, key string) error {
	_, exists := cl.descriptor.GetByType(typ)
	if !exists {
		return fmt.Errorf("missing type %q", typ)
	}

	return cl.dynamic.Delete().
		Namespace(cl.namespace).
		Resource(IstioKindName + "s").
		Name(configKey(typ, key)).
		Do().Error()
}

// List implements store interface
func (cl *Client) List(typ string) ([]model.Config, error) {
	_, exists := cl.descriptor.GetByType(typ)
	if !exists {
		return nil, fmt.Errorf("missing type %q", typ)
	}

	list := &IstioKindList{}
	errs := cl.dynamic.Get().
		Namespace(cl.namespace).
		Resource(IstioKindName + "s").
		Do().Into(list)

	out := make([]model.Config, 0)
	for _, item := range list.Items {
		config, err := cl.convertConfig(&item)
		if typ == config.Type {
			if err != nil {
				errs = multierror.Append(errs, err)
			} else {
				out = append(out, config)
			}
		}
	}
	return out, errs
}
