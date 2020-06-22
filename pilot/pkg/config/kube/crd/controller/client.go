// Copyright Istio Authors
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

// Package controller provides an implementation of the config store and cache
// using Kubernetes Custom Resources and the informer framework from Kubernetes
package controller

import (
	"context"
	"fmt"

	"github.com/hashicorp/go-multierror"

	"istio.io/api/label"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeSchema "k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"  // import GKE cluster authentication plugin
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc" // import OIDC cluster authentication plugin, e.g. for Tectonic
	"k8s.io/client-go/rest"

	"istio.io/pkg/ledger"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/resource"
	kubecfg "istio.io/istio/pkg/kube"
)

// Client is a basic REST client for CRDs implementing config store
type Client struct {
	// Map of apiVersion to restClient.
	clientset map[string]*restClient

	crdClient *apiextensionsclient.Clientset

	schemas collection.Schemas

	// domainSuffix for the config metadata
	domainSuffix string

	// Ledger for tracking config distribution
	configLedger ledger.Ledger

	// revision for this control plane instance. We will only read configs that match this revision.
	revision string
}

type restClient struct {
	apiVersion kubeSchema.GroupVersion

	// schemas from the same apiVersion.
	schemas collection.Schemas

	// types of the schema and objects in the schemas.
	types []*crd.SchemaType

	// dynamic REST client for accessing config CRDs
	dynamic *rest.RESTClient
}

type restClientBuilder struct {
	apiVersion     kubeSchema.GroupVersion
	schemasBuilder *collection.SchemasBuilder
	types          []*crd.SchemaType
}

var scope = log.RegisterScope("kube", "Kubernetes client messages", 0)

func (b *restClientBuilder) build() *restClient {
	return &restClient{
		apiVersion: b.apiVersion,
		schemas:    b.schemasBuilder.Build(),
		types:      b.types,
	}
}

func newClientSet(schemas collection.Schemas) (map[string]*restClient, error) {
	builderMap := make(map[string]*restClientBuilder)

	for _, s := range schemas.All() {
		knownType, exists := crd.SupportedTypes[s.Resource().GroupVersionKind()]
		if !exists {
			return nil, fmt.Errorf("missing known kind for %q", s.Resource().Kind())
		}

		rc, ok := builderMap[s.Resource().APIVersion()]
		if !ok {
			// create a new client if one doesn't already exist
			rc = &restClientBuilder{
				apiVersion: kubeSchema.GroupVersion{
					Group:   s.Resource().Group(),
					Version: s.Resource().Version(),
				},
				schemasBuilder: collection.NewSchemasBuilder(),
			}
			builderMap[s.Resource().APIVersion()] = rc
		}
		if err := rc.schemasBuilder.Add(s); err != nil {
			return nil, err
		}
		rc.types = append(rc.types, &knownType)
	}

	cs := make(map[string]*restClient, len(builderMap))
	for key, builder := range builderMap {
		cs[key] = builder.build()
	}
	return cs, nil
}

func (rc *restClient) init(cfg *rest.Config) error {
	cfg, err := rc.updateRESTConfig(cfg)
	if err != nil {
		return err
	}

	dynamic, err := rest.RESTClientFor(cfg)
	if err != nil {
		return err
	}

	rc.dynamic = dynamic
	return nil
}

// createRESTConfig for cluster API server, pass empty config file for in-cluster
func (rc *restClient) updateRESTConfig(cfg *rest.Config) (config *rest.Config, err error) {
	config = cfg
	config.GroupVersion = &rc.apiVersion
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON

	types := runtime.NewScheme()
	schemeBuilder := runtime.NewSchemeBuilder(
		func(scheme *runtime.Scheme) error {
			for _, kind := range rc.types {
				scheme.AddKnownTypeWithName(rc.apiVersion.WithKind(kind.Schema.Resource().Kind()), kind.Object)
				scheme.AddKnownTypeWithName(rc.apiVersion.WithKind(kind.Schema.Resource().Kind()+"List"), kind.Collection)
			}
			meta_v1.AddToGroupVersion(scheme, rc.apiVersion)
			return nil
		})
	err = schemeBuilder.AddToScheme(types)
	config.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: serializer.NewCodecFactory(types)}

	return
}

// NewForConfig creates a client to the Kubernetes API using a rest config.
func NewForConfig(cfg *rest.Config, schemas collection.Schemas, domainSuffix string, configLedger ledger.Ledger, revision string) (*Client, error) {
	cs, err := newClientSet(schemas)
	if err != nil {
		return nil, err
	}

	crdClient, err := apiextensionsclient.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	out := &Client{
		clientset:    cs,
		crdClient:    crdClient,
		domainSuffix: domainSuffix,
		configLedger: configLedger,
		schemas:      schemas,
		revision:     revision,
	}

	for _, v := range out.clientset {
		if err := v.init(cfg); err != nil {
			return nil, err
		}
	}

	return out, nil
}

// NewClient creates a client to Kubernetes API using a kubeconfig file.
// Use an empty value for `kubeconfig` to use the in-cluster config.
// If the kubeconfig file is empty, defaults to in-cluster config as well.
// You can also choose a config context by providing the desired context name.
func NewClient(config string, context string, schemas collection.Schemas, domainSuffix string, configLedger ledger.Ledger, revision string) (*Client, error) {
	cfg, err := kubecfg.BuildClientConfig(config, context)
	if err != nil {
		return nil, err
	}

	return NewForConfig(cfg, schemas, domainSuffix, configLedger, revision)
}

// Schemas for the store
func (cl *Client) Schemas() collection.Schemas {
	return cl.schemas
}

// Get implements store interface
func (cl *Client) Get(typ resource.GroupVersionKind, name, namespace string) *model.Config {
	t, ok := crd.SupportedTypes[typ]
	if !ok {
		scope.Warnf("unknown type: %s", typ)
		return nil
	}
	rc, ok := cl.clientset[t.Schema.Resource().APIVersion()]
	if !ok {
		scope.Warnf("cannot find client for type: %s", typ)
		return nil
	}

	s, exists := rc.schemas.FindByGroupVersionKind(typ)
	if !exists {
		scope.Warnf("cannot find proto schema for type: %s", typ)
		return nil
	}

	config := t.Object.DeepCopyObject().(crd.IstioObject)
	err := rc.dynamic.Get().
		NamespaceIfScoped(namespace, !s.Resource().IsClusterScoped()).
		Resource(s.Resource().Plural()).
		Name(name).
		Do(context.TODO()).Into(config)

	if err != nil {
		scope.Warna(err)
		return nil
	}

	out, err := crd.ConvertObject(s, config, cl.domainSuffix)
	if err != nil {
		scope.Warna(err)
		return nil
	}
	if cl.objectInRevision(out) {
		return out
	}
	return nil
}

// Create implements store interface
func (cl *Client) Create(config model.Config) (string, error) {
	rc, ok := cl.clientset[crd.APIVersionFromConfig(&config)]
	if !ok {
		return "", fmt.Errorf("unrecognized apiVersion %q", config)
	}

	s, exists := rc.schemas.FindByGroupVersionKind(config.GroupVersionKind)
	if !exists {
		return "", fmt.Errorf("unrecognized type: %s", config.GroupVersionKind)
	}

	if err := s.Resource().ValidateProto(config.Name, config.Namespace, config.Spec); err != nil {
		return "", multierror.Prefix(err, "validation error:")
	}

	out, err := crd.ConvertConfig(s, config)
	if err != nil {
		return "", err
	}

	obj := crd.SupportedTypes[s.Resource().GroupVersionKind()].Object.DeepCopyObject().(crd.IstioObject)
	err = rc.dynamic.Post().
		NamespaceIfScoped(out.GetObjectMeta().Namespace, !s.Resource().IsClusterScoped()).
		Resource(s.Resource().Plural()).
		Body(out).
		Do(context.TODO()).Into(obj)
	if err != nil {
		return "", err
	}

	return obj.GetObjectMeta().ResourceVersion, nil
}

// Update implements store interface
func (cl *Client) Update(config model.Config) (string, error) {
	rc, ok := cl.clientset[crd.APIVersionFromConfig(&config)]
	if !ok {
		return "", fmt.Errorf("unrecognized apiVersion %q", config)
	}
	s, exists := rc.schemas.FindByGroupVersionKind(config.GroupVersionKind)
	if !exists {
		return "", fmt.Errorf("unrecognized type %q", config.GroupVersionKind)
	}

	if err := s.Resource().ValidateProto(config.Name, config.Namespace, config.Spec); err != nil {
		return "", multierror.Prefix(err, "validation error:")
	}

	if config.ResourceVersion == "" {
		return "", fmt.Errorf("revision is required")
	}

	out, err := crd.ConvertConfig(s, config)
	if err != nil {
		return "", err
	}

	obj := crd.SupportedTypes[s.Resource().GroupVersionKind()].Object.DeepCopyObject().(crd.IstioObject)
	err = rc.dynamic.Put().
		NamespaceIfScoped(out.GetObjectMeta().Namespace, !s.Resource().IsClusterScoped()).
		Resource(s.Resource().Plural()).
		Name(out.GetObjectMeta().Name).
		Body(out).
		Do(context.TODO()).Into(obj)
	if err != nil {
		return "", err
	}

	return obj.GetObjectMeta().ResourceVersion, nil
}

// Delete implements store interface
func (cl *Client) Delete(typ resource.GroupVersionKind, name, namespace string) error {
	t, ok := crd.SupportedTypes[typ]
	if !ok {
		return fmt.Errorf("unrecognized type %q", typ)
	}
	rc, ok := cl.clientset[t.Schema.Resource().APIVersion()]
	if !ok {
		return fmt.Errorf("unrecognized apiVersion %v", t.Schema.Resource().APIVersion())
	}
	s, exists := rc.schemas.FindByGroupVersionKind(typ)
	if !exists {
		return fmt.Errorf("missing type %q", typ)
	}

	return rc.dynamic.Delete().
		NamespaceIfScoped(namespace, !s.Resource().IsClusterScoped()).
		Resource(s.Resource().Plural()).
		Name(name).
		Do(context.TODO()).Error()
}

func (cl *Client) Version() string {
	return cl.configLedger.RootHash()
}

func (cl *Client) GetResourceAtVersion(version string, key string) (resourceVersion string, err error) {
	return cl.configLedger.GetPreviousValue(version, key)
}

func (cl *Client) GetLedger() ledger.Ledger {
	return cl.configLedger
}

func (cl *Client) SetLedger(l ledger.Ledger) error {
	cl.configLedger = l
	return nil
}

// List implements store interface
func (cl *Client) List(kind resource.GroupVersionKind, namespace string) ([]model.Config, error) {
	t, ok := crd.SupportedTypes[kind]
	if !ok {
		return nil, fmt.Errorf("unrecognized type: %s", kind)
	}
	rc, ok := cl.clientset[t.Schema.Resource().APIVersion()]
	if !ok {
		return nil, fmt.Errorf("unrecognized apiVersion: %v", t.Schema.Resource().APIVersion())
	}
	s, exists := rc.schemas.FindByGroupVersionKind(kind)
	if !exists {
		return nil, fmt.Errorf("missing type: %s", kind)
	}

	list := t.Collection.DeepCopyObject().(crd.IstioObjectList)
	errs := rc.dynamic.Get().
		NamespaceIfScoped(namespace, !s.Resource().IsClusterScoped()).
		Resource(s.Resource().Plural()).
		Do(context.TODO()).Into(list)

	out := make([]model.Config, 0)
	for _, item := range list.GetItems() {
		obj, err := crd.ConvertObject(s, item, cl.domainSuffix)
		if err != nil {
			errs = multierror.Append(errs, err)
		} else if cl.objectInRevision(obj) {
			out = append(out, *obj)
		}
	}
	return out, errs
}

func (cl *Client) objectInRevision(o *model.Config) bool {
	configEnv, f := o.Labels[label.IstioRev]
	if !f {
		// This is a global object, and always included
		return true
	}
	// Otherwise, only return if the
	return configEnv == cl.revision
}

// KnownCRDs returns all CRDs present in the cluster
func (cl *Client) KnownCRDs() (map[string]struct{}, error) {
	res, err := cl.crdClient.ApiextensionsV1beta1().CustomResourceDefinitions().List(context.TODO(), meta_v1.ListOptions{})
	if err != nil {
		return nil, err
	}
	mp := map[string]struct{}{}
	for _, r := range res.Items {
		mp[r.Name] = struct{}{}
	}
	return mp, nil
}
