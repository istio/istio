// Copyright 2016 Google Inc.
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
	"bytes"
	"encoding/json"
	"log"
	"reflect"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"

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
	// IstioAPIGroup defines Kubernetes API group for TRP
	IstioAPIGroup = "istio.io"
	// IstioResourceVersion defined Kubernetes API group version
	IstioResourceVersion = "v1"
)

// KubernetesRegistry bindings for the manager:
// - configuration objects are stored as third-party resources
type KubernetesRegistry struct {
	mapping model.KindMap
	client  *kubernetes.Clientset
	dyn     *rest.RESTClient
}

// CreateRESTConfig for cluster API server, pass empty config file for in-cluster
func CreateRESTConfig(kubeconfig string, km model.KindMap) (config *rest.Config, err error) {
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
				&api.ListOptions{},
				&api.DeleteOptions{},
			)
			for kind := range km {
				scheme.AddKnownTypeWithName(schema.GroupVersionKind{
					Group:   IstioAPIGroup,
					Version: IstioResourceVersion,
					Kind:    kind,
				}, &Config{})
				scheme.AddKnownTypeWithName(schema.GroupVersionKind{
					Group:   IstioAPIGroup,
					Version: IstioResourceVersion,
					Kind:    kind + "List",
				}, &ConfigList{})
			}
			return nil
		})
	schemeBuilder.AddToScheme(api.Scheme)

	return
}

// NewKubernetesRegistry creates a client to Kubernetes API using kubeconfig file
func NewKubernetesRegistry(kubeconfig string, km model.KindMap) (*KubernetesRegistry, error) {
	config, err := CreateRESTConfig(kubeconfig, km)
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

	out := &KubernetesRegistry{
		mapping: km,
		client:  cl,
		dyn:     dyn,
	}

	if err = out.RegisterResources(); err != nil {
		return nil, err
	}

	return out, nil
}

// RegisterResources creates third party resources
func (kr *KubernetesRegistry) RegisterResources() error {
	var out error
	for kind, v := range kr.mapping {
		apiName := kindToAPIName(kind)
		res, err := kr.client.Extensions().ThirdPartyResources().
			Get(apiName, meta_v1.GetOptions{})
		if err == nil {
			log.Printf("Resource already exists: %q", res.Name)
		} else if errors.IsNotFound(err) {
			log.Printf("Creating resource: %q", kind)
			tpr := &v1beta1.ThirdPartyResource{
				ObjectMeta:  v1.ObjectMeta{Name: apiName},
				Versions:    []v1beta1.APIVersion{{Name: IstioResourceVersion}},
				Description: v.Description,
			}
			res, err = kr.client.Extensions().ThirdPartyResources().
				Create(tpr)
			if err != nil {
				out = multierror.Append(out, err)
			} else {
				log.Printf("Created resource: %q", res.Name)
			}
		} else {
			out = multierror.Append(out, err)
		}
	}

	return out
}

// DeregisterResources removes third party resources
func (kr *KubernetesRegistry) DeregisterResources() error {
	var out error
	for kind := range kr.mapping {
		apiName := kindToAPIName(kind)
		err := kr.client.Extensions().ThirdPartyResources().
			Delete(apiName, &v1.DeleteOptions{})
		if err != nil {
			out = multierror.Append(out, err)
		}
	}
	return out
}

func (kr *KubernetesRegistry) Get(key model.ConfigKey) (*model.Config, bool) {
	config := &Config{}
	err := kr.dyn.Get().
		Namespace(key.Namespace).
		Resource(key.Kind + "s").
		Name(key.Name).
		Do().Into(config)
	if err != nil {
		log.Printf(err.Error())
		return nil, false
	}
	out, err := kr.convert(key.Kind, config)
	if err != nil {
		log.Printf(err.Error())
		return nil, false
	}
	return out, true
}

func (kr *KubernetesRegistry) Put(obj model.Config) error {
	if err := kr.mapping.ValidateConfig(obj); err != nil {
		return err
	}
	spec, err := protoToMap(obj.Spec.(proto.Message))
	if err != nil {
		return err
	}
	var status map[string]interface{}
	if obj.Status != nil {
		status, err = protoToMap(obj.Status.(proto.Message))
		if err != nil {
			return err
		}
	}

	out := &Config{
		Metadata: api.ObjectMeta{Name: obj.ConfigKey.Name},
		Spec:     spec,
		Status:   status,
	}

	err = kr.dyn.Post().
		Namespace(obj.Namespace).
		Resource(obj.Kind + "s").
		Body(out).
		Do().Error()
	if err != nil {
		return err
	}

	return nil
}

func (kr *KubernetesRegistry) Delete(key model.ConfigKey) {
	err := kr.dyn.Delete().
		Namespace(key.Namespace).
		Resource(key.Kind + "s").
		Name(key.Name).
		Do().Error()
	if err != nil {
		log.Printf(err.Error())
	}
}

func (kr *KubernetesRegistry) List(kind string, ns string) []*model.Config {
	if _, ok := kr.mapping[kind]; !ok {
		return nil
	}

	list := &ConfigList{}
	err := kr.dyn.Get().
		Namespace(ns).
		Resource(kind + "s").
		Do().Into(list)
	if err != nil {
		log.Printf(err.Error())
		return nil
	}

	var out []*model.Config
	for _, item := range list.Items {
		elt, err := kr.convert(kind, &item)
		if err != nil {
			log.Printf(err.Error())
		}
		out = append(out, elt)
	}
	return out
}

// camelCaseToKabobCase converts "MyName" to "my-name"
func camelCaseToKabobCase(s string) string {
	var out bytes.Buffer
	for i := range s {
		if 'A' <= s[i] && s[i] <= 'Z' {
			if i > 0 {
				out.WriteByte('-')
			}
			out.WriteByte(s[i] - 'A' + 'a')
		} else {
			out.WriteByte(s[i])
		}
	}
	return out.String()
}

// kindToAPIName converts Kind name to 3rd party API group
func kindToAPIName(s string) string {
	return camelCaseToKabobCase(s) + "." + IstioAPIGroup
}

func (kr *KubernetesRegistry) convert(kind string, config *Config) (*model.Config, error) {
	spec, err := mapToProto(kr.mapping[kind].MessageName, config.Spec)
	if err != nil {
		return nil, err
	}
	var status proto.Message
	if config.Status != nil {
		status, err = mapToProto(kr.mapping[kind].StatusMessageName, config.Status)
		if err != nil {
			return nil, err
		}
	}

	out := model.Config{
		ConfigKey: model.ConfigKey{
			Kind:      kind,
			Name:      config.GetObjectMeta().GetName(),
			Namespace: config.GetObjectMeta().GetNamespace(),
		},
		Spec:   spec,
		Status: status,
	}

	return &out, nil
}

func protoToMap(msg proto.Message) (map[string]interface{}, error) {
	// Marshal from proto to json bytes
	m := jsonpb.Marshaler{}
	bytes, err := m.MarshalToString(msg)
	if err != nil {
		return nil, err
	}

	// Unmarshal from json bytes to go map
	var data map[string]interface{}
	err = json.Unmarshal([]byte(bytes), &data)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func mapToProto(message string, data map[string]interface{}) (proto.Message, error) {
	// Marshal to json bytes
	str, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	// Unmarshal from bytes to proto
	pbt := proto.MessageType(message)
	pb := reflect.New(pbt.Elem()).Interface().(proto.Message)
	err = jsonpb.UnmarshalString(string(str), pb)
	if err != nil {
		return nil, err
	}

	return pb, nil
}
