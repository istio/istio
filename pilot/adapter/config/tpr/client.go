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

// Package tpr provides an implementation of the config store and cache
// using Kubernetes Third-Party Resources and the informer framework from Kubernetes
package tpr

import (
	"fmt"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	multierror "github.com/hashicorp/go-multierror"

	"k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
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
	// IstioAPIGroup defines Kubernetes API group for TPR
	IstioAPIGroup = "istio.io"

	// IstioResourceVersion defines Kubernetes API group version
	IstioResourceVersion = "v1alpha1"

	// IstioKind defines the shared TPR kind to avoid boilerplate
	// code for each custom kind
	IstioKind = "IstioConfig"
)

// Client is a basic REST client for TPRs implementing config store
type Client struct {
	descriptor model.ConfigDescriptor
	client     kubernetes.Interface
	dynamic    *rest.RESTClient
	namespace  string
}

// createRESTConfig for cluster API server, pass empty config file for in-cluster
func createRESTConfig(kubeconfig string) (config *rest.Config, err error) {
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
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: api.Codecs}

	schemeBuilder := runtime.NewSchemeBuilder(
		func(scheme *runtime.Scheme) error {
			scheme.AddKnownTypes(
				version,
			)
			scheme.AddKnownTypeWithName(schema.GroupVersionKind{
				Group:   IstioAPIGroup,
				Version: IstioResourceVersion,
				Kind:    IstioKind,
			}, &Config{})
			scheme.AddKnownTypeWithName(schema.GroupVersionKind{
				Group:   IstioAPIGroup,
				Version: IstioResourceVersion,
				Kind:    IstioKind + "List",
			}, &ConfigList{})

			return nil
		})
	meta_v1.AddToGroupVersion(api.Scheme, version)
	err = schemeBuilder.AddToScheme(api.Scheme)

	return
}

// NewClient creates a client to Kubernetes API using a kubeconfig file.
// namespace argument provides the namespace to store TPRs
// Use an empty value for `kubeconfig` to use the in-cluster config.
// If the kubeconfig file is empty, defaults to in-cluster config as well.
func NewClient(config string, descriptor model.ConfigDescriptor, namespace string) (*Client, error) {
	kubeconfig, err := kube.ResolveConfig(config)
	if err != nil {
		return nil, err
	}
	restconfig, err := createRESTConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(restconfig)
	if err != nil {
		return nil, err
	}

	dynamic, err := rest.RESTClientFor(restconfig)
	if err != nil {
		return nil, err
	}

	out := &Client{
		descriptor: descriptor,
		client:     client,
		dynamic:    dynamic,
		namespace:  namespace,
	}

	return out, nil
}

// RegisterResources creates third party resources
func (cl *Client) RegisterResources() error {
	var out error
	kinds := []string{IstioKind}
	for _, kind := range kinds {
		apiName := kindToAPIName(kind)
		res, err := cl.client.
			Extensions().
			ThirdPartyResources().
			Get(apiName, meta_v1.GetOptions{})
		if err == nil {
			glog.V(2).Infof("Resource already exists: %q", res.Name)
		} else if errors.IsNotFound(err) {
			glog.V(1).Infof("Creating resource: %q", kind)
			tpr := &v1beta1.ThirdPartyResource{
				ObjectMeta:  meta_v1.ObjectMeta{Name: apiName},
				Versions:    []v1beta1.APIVersion{{Name: IstioResourceVersion}},
				Description: "Istio configuration",
			}
			res, err = cl.client.
				Extensions().
				ThirdPartyResources().
				Create(tpr)
			if err != nil {
				out = multierror.Append(out, err)
			} else {
				glog.V(2).Infof("Created resource: %q", res.Name)
			}
		} else {
			out = multierror.Append(out, err)
		}
	}

	// validate that the resources exist or fail with an error after 30s
	ready := true
	glog.V(2).Infof("Checking for TPR resources")
	for i := 0; i < 30; i++ {
		ready = true
		for _, kind := range kinds {
			list := &ConfigList{}
			err := cl.dynamic.Get().
				Namespace(api.NamespaceAll).
				Resource(IstioKind + "s").
				Do().Into(list)
			if err != nil {
				glog.V(2).Infof("TPR %q is not ready (%v). Waiting...", kind, err)
				ready = false
				break
			}
		}
		if ready {
			break
		}
		time.Sleep(1 * time.Second)
	}

	if !ready {
		out = multierror.Append(out, fmt.Errorf("Failed to create all TPRs"))
	}

	return out
}

// DeregisterResources removes third party resources
func (cl *Client) DeregisterResources() error {
	var out error
	kinds := []string{IstioKind}
	for _, kind := range kinds {
		apiName := kindToAPIName(kind)
		err := cl.client.Extensions().ThirdPartyResources().
			Delete(apiName, &meta_v1.DeleteOptions{})
		if err != nil {
			out = multierror.Append(out, err)
		}
	}
	return out
}

// GetKubernetesInterface returns a static Kubernetes interface
func (cl *Client) GetKubernetesInterface() kubernetes.Interface {
	return cl.client
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

	config := &Config{}
	err := cl.dynamic.Get().
		Namespace(cl.namespace).
		Resource(IstioKind + "s").
		Name(configKey(typ, key)).
		Do().Into(config)

	if err != nil {
		glog.Warning(err)
		return nil, false, ""
	}

	out, err := schema.FromJSONMap(config.Spec)
	if err != nil {
		glog.Warning(err)
		return nil, false, ""
	}
	return out, true, config.Metadata.ResourceVersion
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

	config := &Config{}
	err = cl.dynamic.Post().
		Namespace(out.Metadata.Namespace).
		Resource(IstioKind + "s").
		Body(out).
		Do().Into(config)
	if err != nil {
		return "", err
	}

	return config.Metadata.ResourceVersion, nil
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

	out.Metadata.ResourceVersion = revision

	config := &Config{}
	err = cl.dynamic.Put().
		Namespace(out.Metadata.Namespace).
		Resource(IstioKind + "s").
		Name(out.Metadata.Name).
		Body(out).
		Do().Into(config)
	if err != nil {
		return "", err
	}

	return config.Metadata.ResourceVersion, nil
}

// Delete implements store interface
func (cl *Client) Delete(typ, key string) error {
	_, exists := cl.descriptor.GetByType(typ)
	if !exists {
		return fmt.Errorf("missing type %q", typ)
	}

	return cl.dynamic.Delete().
		Namespace(cl.namespace).
		Resource(IstioKind + "s").
		Name(configKey(typ, key)).
		Do().Error()
}

// List implements store interface
func (cl *Client) List(typ string) ([]model.Config, error) {
	_, exists := cl.descriptor.GetByType(typ)
	if !exists {
		return nil, fmt.Errorf("missing type %q", typ)
	}

	list := &ConfigList{}
	errs := cl.dynamic.Get().
		Namespace(cl.namespace).
		Resource(IstioKind + "s").
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

// Request sends requests through the Kubernetes apiserver proxy to
// the a Kubernetes service.
// (see https://kubernetes.io/docs/concepts/cluster-administration/access-cluster/#discovering-builtin-services)
func (cl *Client) Request(namespace, service, method, path string, inBody []byte) (int, []byte, error) {
	// Kubernetes apiserver proxy prefix for the specified namespace and service.
	absPath := fmt.Sprintf("api/v1/namespaces/%s/services/%s/proxy", namespace, service)

	// TODO(https://github.com/istio/api/issues/94) - pilot and
	// mixer API server paths are not consistent. Pilot path is
	// prefixed with Istio resource version (i.e. v1alpha1) and mixer
	// path is not. Short term workaround is to special case this
	// behavior. Long term solution is to unify API scheme and server
	// implementations.
	if strings.HasPrefix(path, "config") || strings.HasPrefix(path, "version") {
		absPath += "/" + IstioResourceVersion // pilot api server path
	}

	// API server resource path.
	absPath += "/" + path

	var status int
	outBody, err := cl.dynamic.Verb(method).
		AbsPath(absPath).
		SetHeader("Content-Type", "application/json").
		Body(inBody).
		Do().
		StatusCode(&status).
		Raw()
	return status, outBody, err
}
