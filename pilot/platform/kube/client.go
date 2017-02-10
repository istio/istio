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

package kube

import (
	"fmt"
	"strings"
	"time"

	"github.com/golang/protobuf/proto"

	"github.com/golang/glog"

	multierror "github.com/hashicorp/go-multierror"

	"istio.io/manager/model"

	meta_v1 "k8s.io/client-go/pkg/apis/meta/v1"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/errors"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
	"k8s.io/client-go/pkg/runtime"
	"k8s.io/client-go/pkg/runtime/schema"
	"k8s.io/client-go/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
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

// Client provides state-less Kubernetes bindings for the manager:
// - configuration objects are stored as third-party resources
// - dynamic REST client is configured to use third-party resources
// - static client exposes Kubernetes API
type Client struct {
	mapping model.KindMap
	client  *kubernetes.Clientset
	dyn     *rest.RESTClient
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
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: api.Codecs}

	schemeBuilder := runtime.NewSchemeBuilder(
		func(scheme *runtime.Scheme) error {
			scheme.AddKnownTypes(
				version,
				&v1.ListOptions{},
				&v1.DeleteOptions{},
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
	err = schemeBuilder.AddToScheme(api.Scheme)

	return
}

// NewClient creates a client to Kubernetes API using a kubeconfig file.
// Use an empty value for `kubeconfig` to use the in-cluster config.
func NewClient(kubeconfig string, km model.KindMap) (*Client, error) {
	config, err := CreateRESTConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	cl, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	dyn, err := rest.RESTClientFor(config)
	if err != nil {
		return nil, err
	}

	out := &Client{
		mapping: km,
		client:  cl,
		dyn:     dyn,
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
				ObjectMeta:  v1.ObjectMeta{Name: apiName},
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
			err := cl.dyn.Get().
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
			Delete(apiName, &v1.DeleteOptions{})
		if err != nil {
			out = multierror.Append(out, err)
		}
	}
	return out
}

// Get implements registry operation
func (cl *Client) Get(key model.Key) (proto.Message, bool) {
	if err := cl.mapping.ValidateKey(&key); err != nil {
		glog.Warning(err)
		return nil, false
	}

	config := &Config{}
	err := cl.dyn.Get().
		Namespace(key.Namespace).
		Resource(IstioKind + "s").
		Name(configKey(&key)).
		Do().Into(config)

	if err != nil {
		glog.Warning(err)
		return nil, false
	}

	kind := cl.mapping[key.Kind]
	out, err := kind.FromJSONMap(config.Spec)
	if err != nil {
		glog.Warning(err)
		return nil, false
	}
	return out, true
}

// Put implements registry operation
func (cl *Client) Put(k model.Key, v proto.Message) error {
	out, err := modelToKube(cl.mapping, &k, v)
	if err != nil {
		return err
	}
	return cl.dyn.Post().
		Namespace(k.Namespace).
		Resource(IstioKind + "s").
		Body(out).
		Do().Error()
}

// Delete implements registry operation
func (cl *Client) Delete(key model.Key) error {
	if err := cl.mapping.ValidateKey(&key); err != nil {
		return err
	}

	return cl.dyn.Delete().
		Namespace(key.Namespace).
		Resource(IstioKind + "s").
		Name(configKey(&key)).
		Do().Error()
}

// List implements registry operation
func (cl *Client) List(kind, namespace string) (map[model.Key]proto.Message, error) {
	if _, ok := cl.mapping[kind]; !ok {
		return nil, fmt.Errorf("Missing kind %q", kind)
	}

	list := &ConfigList{}
	errs := cl.dyn.Get().
		Namespace(namespace).
		Resource(IstioKind + "s").
		Do().Into(list)

	out := make(map[model.Key]proto.Message, 0)
	for _, item := range list.Items {
		name, ns, kind, data, err := cl.convertConfig(&item)
		if kind != "" {
			if err != nil {
				errs = multierror.Append(errs, err)
			} else {
				out[model.Key{
					Name:      name,
					Namespace: ns,
					Kind:      kind,
				}] = data
			}
		}
	}
	return out, errs
}

// configKey assigns k8s TPR name to Istio config
func configKey(k *model.Key) string {
	return k.Kind + "-" + k.Name
}

// convertConfig extracts Istio config data from k8s TPRs and leaves kind empty if no kind matches
func (cl *Client) convertConfig(item *Config) (name, namespace, kind string, data proto.Message, err error) {
	for k, v := range cl.mapping {
		if strings.HasPrefix(item.Metadata.Name, k) {
			kind = k
			name = strings.TrimPrefix(item.Metadata.Name, kind+"-")
			namespace = item.Metadata.Namespace
			data, err = v.FromJSONMap(item.Spec)
			return
		}
	}
	return
}
