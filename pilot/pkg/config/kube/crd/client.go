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

	multierror "github.com/hashicorp/go-multierror"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/wait"             // import GKE cluster authentication plugin
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp" // import OIDC cluster authentication plugin, e.g. for Tectonic
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/rest"

	"istio.io/istio/pilot/pkg/model"
	kubecfg "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
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
	// Map of apiVersion to restClient.
	clientset map[string]*restClient

	// domainSuffix for the config metadata
	domainSuffix string
}

type restClient struct {
	apiVersion schema.GroupVersion

	// descriptor from the same apiVersion.
	descriptor model.ConfigDescriptor

	// types of the schema and objects in the descriptor.
	types []*schemaType

	// restconfig for REST type descriptors
	restconfig *rest.Config

	// dynamic REST client for accessing config CRDs
	dynamic *rest.RESTClient
}

func apiVersion(schema *model.ProtoSchema) string {
	return ResourceGroup(schema) + "/" + schema.Version
}

func apiVersionFromConfig(config *model.Config) string {
	return config.Group + "/" + config.Version
}

func newClientSet(descriptor model.ConfigDescriptor) (map[string]*restClient, error) {
	cs := make(map[string]*restClient)
	for _, typ := range descriptor {
		s, exists := knownTypes[typ.Type]
		if !exists {
			return nil, fmt.Errorf("missing known type for %q", typ.Type)
		}

		rc, ok := cs[apiVersion(&typ)]
		if !ok {
			// create a new client if one doesn't already exist
			rc = &restClient{
				apiVersion: schema.GroupVersion{
					Group:   ResourceGroup(&typ),
					Version: typ.Version,
				},
			}
			cs[apiVersion(&typ)] = rc
		}
		rc.descriptor = append(rc.descriptor, typ)
		rc.types = append(rc.types, &s)
	}
	return cs, nil
}

func (rc *restClient) init(kubeconfig string, context string) error {
	cfg, err := rc.createRESTConfig(kubeconfig, context)
	if err != nil {
		return err
	}

	dynamic, err := rest.RESTClientFor(cfg)
	if err != nil {
		return err
	}

	rc.restconfig = cfg
	rc.dynamic = dynamic
	return nil
}

// createRESTConfig for cluster API server, pass empty config file for in-cluster
func (rc *restClient) createRESTConfig(kubeconfig string, context string) (config *rest.Config, err error) {
	config, err = kubecfg.BuildClientConfig(kubeconfig, context)

	if err != nil {
		return nil, err
	}

	config.GroupVersion = &rc.apiVersion
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON

	types := runtime.NewScheme()
	schemeBuilder := runtime.NewSchemeBuilder(
		func(scheme *runtime.Scheme) error {
			for _, kind := range rc.types {
				scheme.AddKnownTypes(rc.apiVersion, kind.object, kind.collection)
			}
			meta_v1.AddToGroupVersion(scheme, rc.apiVersion)
			return nil
		})
	err = schemeBuilder.AddToScheme(types)
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: serializer.NewCodecFactory(types)}

	return
}

// NewClient creates a client to Kubernetes API using a kubeconfig file.
// Use an empty value for `kubeconfig` to use the in-cluster config.
// If the kubeconfig file is empty, defaults to in-cluster config as well.
// You can also choose a config context by providing the desired context name.
func NewClient(config string, context string, descriptor model.ConfigDescriptor, domainSuffix string) (*Client, error) {
	cs, err := newClientSet(descriptor)
	if err != nil {
		return nil, err
	}

	out := &Client{
		clientset:    cs,
		domainSuffix: domainSuffix,
	}

	for _, v := range out.clientset {
		if err := v.init(config, context); err != nil {
			return nil, err
		}
	}

	return out, nil
}

// RegisterResources sends a request to create CRDs and waits for them to initialize
func (cl *Client) RegisterResources() error {
	for k, rc := range cl.clientset {
		log.Infof("registering for apiVersion %v", k)
		if err := rc.registerResources(); err != nil {
			return err
		}
	}
	return nil
}

func (rc *restClient) registerResources() error {
	cs, err := apiextensionsclient.NewForConfig(rc.restconfig)
	if err != nil {
		return err
	}

	skipCreate := true
	for _, schema := range rc.descriptor {
		name := ResourceName(schema.Plural) + "." + ResourceGroup(&schema)
		crd, errGet := cs.ApiextensionsV1beta1().CustomResourceDefinitions().Get(name, meta_v1.GetOptions{})
		if errGet != nil {
			skipCreate = false
			break // create the resources
		}
		for _, cond := range crd.Status.Conditions {
			if cond.Type == apiextensionsv1beta1.Established &&
				cond.Status == apiextensionsv1beta1.ConditionTrue {
				continue
			}

			if cond.Type == apiextensionsv1beta1.NamesAccepted &&
				cond.Status == apiextensionsv1beta1.ConditionTrue {
				continue
			}

			log.Warnf("Not established: %v", name)
			skipCreate = false
			break
		}
	}

	if skipCreate {
		return nil
	}

	for _, schema := range rc.descriptor {
		g := ResourceGroup(&schema)
		name := ResourceName(schema.Plural) + "." + g
		crdScope := apiextensionsv1beta1.NamespaceScoped
		if schema.ClusterScoped {
			crdScope = apiextensionsv1beta1.ClusterScoped
		}
		crd := &apiextensionsv1beta1.CustomResourceDefinition{
			ObjectMeta: meta_v1.ObjectMeta{
				Name: name,
			},
			Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
				Group:   g,
				Version: schema.Version,
				Scope:   crdScope,
				Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
					Plural: ResourceName(schema.Plural),
					Kind:   KebabCaseToCamelCase(schema.Type),
				},
			},
		}
		log.Infof("registering CRD %q", name)
		_, err = cs.ApiextensionsV1beta1().CustomResourceDefinitions().Create(crd)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
	}

	// wait for CRD being established
	errPoll := wait.Poll(500*time.Millisecond, 60*time.Second, func() (bool, error) {
	descriptor:
		for _, schema := range rc.descriptor {
			name := ResourceName(schema.Plural) + "." + ResourceGroup(&schema)
			crd, errGet := cs.ApiextensionsV1beta1().CustomResourceDefinitions().Get(name, meta_v1.GetOptions{})
			if errGet != nil {
				return false, errGet
			}
			for _, cond := range crd.Status.Conditions {
				switch cond.Type {
				case apiextensionsv1beta1.Established:
					if cond.Status == apiextensionsv1beta1.ConditionTrue {
						log.Infof("established CRD %q", name)
						continue descriptor
					}
				case apiextensionsv1beta1.NamesAccepted:
					if cond.Status == apiextensionsv1beta1.ConditionFalse {
						log.Warnf("name conflict: %v", cond.Reason)
					}
				}
			}
			log.Infof("missing status condition for %q", name)
			return false, nil
		}
		return true, nil
	})

	if errPoll != nil {
		log.Error("failed to verify CRD creation")
		return errPoll
	}

	return nil
}

// DeregisterResources removes third party resources
func (cl *Client) DeregisterResources() error {
	for k, rc := range cl.clientset {
		log.Infof("deregistering for apiVersion %s", k)
		if err := rc.deregisterResources(); err != nil {
			return err
		}
	}
	return nil
}

func (rc *restClient) deregisterResources() error {
	cs, err := apiextensionsclient.NewForConfig(rc.restconfig)
	if err != nil {
		return err
	}

	var errs error
	for _, schema := range rc.descriptor {
		name := ResourceName(schema.Plural) + "." + ResourceGroup(&schema)
		err := cs.ApiextensionsV1beta1().CustomResourceDefinitions().Delete(name, nil)
		errs = multierror.Append(errs, err)
	}
	return errs
}

// ConfigDescriptor for the store
func (cl *Client) ConfigDescriptor() model.ConfigDescriptor {
	d := make(model.ConfigDescriptor, 0, len(cl.clientset))
	for _, rc := range cl.clientset {
		d = append(d, rc.descriptor...)
	}
	return d
}

// Get implements store interface
func (cl *Client) Get(typ, name, namespace string) *model.Config {
	s, ok := knownTypes[typ]
	if !ok {
		log.Warn("unknown type " + typ)
		return nil
	}
	rc, ok := cl.clientset[apiVersion(&s.schema)]
	if !ok {
		log.Warn("cannot find client for type " + typ)
		return nil
	}

	schema, exists := rc.descriptor.GetByType(typ)
	if !exists {
		log.Warn("cannot find proto schema for type " + typ)
		return nil
	}

	config := s.object.DeepCopyObject().(IstioObject)
	err := rc.dynamic.Get().
		Namespace(namespace).
		Resource(ResourceName(schema.Plural)).
		Name(name).
		Do().Into(config)

	if err != nil {
		log.Warna(err)
		return nil
	}

	out, err := ConvertObject(schema, config, cl.domainSuffix)
	if err != nil {
		log.Warna(err)
		return nil
	}
	return out
}

// Create implements store interface
func (cl *Client) Create(config model.Config) (string, error) {
	rc, ok := cl.clientset[apiVersionFromConfig(&config)]
	if !ok {
		return "", fmt.Errorf("unrecognized apiVersion %q", config)
	}

	schema, exists := rc.descriptor.GetByType(config.Type)
	if !exists {
		return "", fmt.Errorf("unrecognized type %q", config.Type)
	}

	if err := schema.Validate(config.Name, config.Namespace, config.Spec); err != nil {
		return "", multierror.Prefix(err, "validation error:")
	}

	out, err := ConvertConfig(schema, config)
	if err != nil {
		return "", err
	}

	obj := knownTypes[schema.Type].object.DeepCopyObject().(IstioObject)
	err = rc.dynamic.Post().
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
	rc, ok := cl.clientset[apiVersionFromConfig(&config)]
	if !ok {
		return "", fmt.Errorf("unrecognized apiVersion %q", config)
	}
	schema, exists := rc.descriptor.GetByType(config.Type)
	if !exists {
		return "", fmt.Errorf("unrecognized type %q", config.Type)
	}

	if err := schema.Validate(config.Name, config.Namespace, config.Spec); err != nil {
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
	err = rc.dynamic.Put().
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
	s, ok := knownTypes[typ]
	if !ok {
		return fmt.Errorf("unrecognized type %q", typ)
	}
	rc, ok := cl.clientset[apiVersion(&s.schema)]
	if !ok {
		return fmt.Errorf("unrecognized apiVersion %v", s.schema)
	}
	schema, exists := rc.descriptor.GetByType(typ)
	if !exists {
		return fmt.Errorf("missing type %q", typ)
	}

	return rc.dynamic.Delete().
		Namespace(namespace).
		Resource(ResourceName(schema.Plural)).
		Name(name).
		Do().Error()
}

// List implements store interface
func (cl *Client) List(typ, namespace string) ([]model.Config, error) {
	s, ok := knownTypes[typ]
	if !ok {
		return nil, fmt.Errorf("unrecognized type %q", typ)
	}
	rc, ok := cl.clientset[apiVersion(&s.schema)]
	if !ok {
		return nil, fmt.Errorf("unrecognized apiVersion %v", s.schema)
	}
	schema, exists := rc.descriptor.GetByType(typ)
	if !exists {
		return nil, fmt.Errorf("missing type %q", typ)
	}

	list := knownTypes[schema.Type].collection.DeepCopyObject().(IstioObjectList)
	errs := rc.dynamic.Get().
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
