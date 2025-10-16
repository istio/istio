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

// Package crdclient provides an implementation of the config store and cache
// using Kubernetes Custom Resources and the informer framework from Kubernetes
//
// This code relies heavily on code generation for performance reasons; to implement the
// Istio store interface, we need to take dynamic inputs. Using the dynamic informers results in poor
// performance, as the cache will store unstructured objects which need to be marshaled on each Get/List call.
// Using istio/client-go directly will cache objects marshaled, allowing us to have cheap Get/List calls,
// at the expense of some code gen.
package crdclient

import (
	"fmt"
	"sync"
	"time"

	jsonmerge "github.com/evanphx/json-patch/v5"
	"go.uber.org/atomic"
	"gomodules.xyz/jsonpatch/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"  // import GKE cluster authentication plugin
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc" // import OIDC cluster authentication plugin, e.g. for Tectonic

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
)

var scope = log.RegisterScope("kube", "Kubernetes client messages")

// Client is a client for Istio CRDs, implementing config store cache
// This is used for CRUD operators on Istio configuration, as well as handling of events on config changes
type Client struct {
	// schemas defines the set of schemas used by this client.
	// Note: this must be a subset of the schemas defined in the codegen
	schemas collection.Schemas

	// domainSuffix for the config metadata
	domainSuffix string

	// revision for this control plane instance. We will only read configs that match this revision.
	revision string

	// kinds keeps track of all cache handlers for known types
	kinds   map[config.GroupVersionKind]nsStore
	kindsMu sync.RWMutex
	// a flag indicates whether this client has been run, it is to prevent run queue twice
	started *atomic.Bool

	schemasByCRDName map[string]resource.Schema
	client           kube.Client
	logger           *log.Scope

	// namespacesFilter is only used to initiate filtered informer.
	filtersByGVK map[config.GroupVersionKind]kubetypes.Filter
	stop         chan struct{}
}

type nsStore struct {
	collection krt.Collection[config.Config]
	index      krt.Index[string, config.Config]
	handlers   []krt.HandlerRegistration
}

type Option struct {
	Revision     string
	DomainSuffix string
	Identifier   string
	FiltersByGVK map[config.GroupVersionKind]kubetypes.Filter
	KrtDebugger  *krt.DebugHandler
}

var _ model.ConfigStoreController = &Client{}

func New(client kube.Client, opts Option) *Client {
	schemas := collections.Pilot
	if features.EnableGatewayAPI {
		schemas = collections.PilotGatewayAPI()
	}
	return NewForSchemas(client, opts, schemas)
}

func NewForSchemas(client kube.Client, opts Option, schemas collection.Schemas) *Client {
	schemasByCRDName := map[string]resource.Schema{}
	for _, s := range schemas.All() {
		// From the spec: "Its name MUST be in the format <.spec.name>.<.spec.group>."
		name := fmt.Sprintf("%s.%s", s.Plural(), s.Group())
		schemasByCRDName[name] = s
	}

	stop := make(chan struct{})

	out := &Client{
		domainSuffix:     opts.DomainSuffix,
		schemas:          schemas,
		schemasByCRDName: schemasByCRDName,
		revision:         opts.Revision,
		started:          atomic.NewBool(false),
		kinds:            map[config.GroupVersionKind]nsStore{},
		client:           client,
		logger:           scope.WithLabels("controller", opts.Identifier),
		filtersByGVK:     opts.FiltersByGVK,
		stop:             stop,
	}

	kopts := krt.NewOptionsBuilder(stop, "crdclient", opts.KrtDebugger)
	for _, s := range out.schemas.All() {
		// From the spec: "Its name MUST be in the format <.spec.name>.<.spec.group>."
		name := fmt.Sprintf("%s.%s", s.Plural(), s.Group())
		out.addCRD(name, kopts)
	}

	return out
}

func (cl *Client) RegisterEventHandler(kind config.GroupVersionKind, handler model.EventHandler) {
	if c, ok := cl.kind(kind); ok {
		c.handlers = append(c.handlers, c.collection.RegisterBatch(func(o []krt.Event[config.Config]) {
			for _, event := range o {
				switch event.Event {
				case controllers.EventAdd:
					handler(config.Config{}, *event.New, model.Event(event.Event))
				case controllers.EventUpdate:
					handler(*event.Old, *event.New, model.Event(event.Event))
				case controllers.EventDelete:
					handler(config.Config{}, *event.Old, model.Event(event.Event))
				}
			}
		}, false))
		return
	}

	cl.logger.Warnf("unknown type: %s", kind)
}

// Run the queue and all informers. Callers should  wait for HasSynced() before depending on results.
func (cl *Client) Run(stop <-chan struct{}) {
	if cl.started.Swap(true) {
		// was already started by other thread
		return
	}

	t0 := time.Now()
	cl.logger.Infof("Starting Pilot K8S CRD controller")
	if !kube.WaitForCacheSync("crdclient", stop, cl.informerSynced) {
		cl.logger.Errorf("Failed to sync Pilot K8S CRD controller cache")
	} else {
		cl.logger.Infof("Pilot K8S CRD controller synced in %v", time.Since(t0))
	}
	<-stop
	close(cl.stop)
	// Cleanup handlers
	for _, h := range cl.allKinds() {
		for _, reg := range h.handlers {
			reg.UnregisterHandler()
		}
	}
	cl.logger.Infof("controller terminated")
}

func (cl *Client) informerSynced() bool {
	for gk, ctl := range cl.allKinds() {
		if !ctl.collection.HasSynced() {
			cl.logger.Infof("controller %q is syncing...", gk)
			return false
		}
	}
	return true
}

func (cl *Client) HasSynced() bool {
	for _, ctl := range cl.allKinds() {
		if !ctl.collection.HasSynced() {
			return false
		}

		for _, h := range ctl.handlers {
			if !h.HasSynced() {
				return false
			}
		}
	}

	return true
}

// Schemas for the store
func (cl *Client) Schemas() collection.Schemas {
	return cl.schemas
}

// Get implements store interface
func (cl *Client) Get(typ config.GroupVersionKind, name, namespace string) *config.Config {
	h, f := cl.kind(typ)
	if !f {
		cl.logger.Warnf("unknown type: %s", typ)
		return nil
	}

	var key string
	if namespace == "" {
		key = name
	} else {
		key = namespace + "/" + name
	}

	obj := h.collection.GetKey(key)
	if obj == nil {
		cl.logger.Debugf("couldn't find %s/%s in informer index", namespace, name)
		return nil
	}

	return obj
}

// Create implements store interface
func (cl *Client) Create(cfg config.Config) (string, error) {
	if cfg.Spec == nil {
		return "", fmt.Errorf("nil spec for %v/%v", cfg.Name, cfg.Namespace)
	}

	meta, err := create(cl.client, cfg, getObjectMetadata(cfg))
	if err != nil {
		return "", err
	}
	return meta.GetResourceVersion(), nil
}

// Update implements store interface
func (cl *Client) Update(cfg config.Config) (string, error) {
	if cfg.Spec == nil {
		return "", fmt.Errorf("nil spec for %v/%v", cfg.Name, cfg.Namespace)
	}

	meta, err := update(cl.client, cfg, getObjectMetadata(cfg))
	if err != nil {
		return "", err
	}
	return meta.GetResourceVersion(), nil
}

func (cl *Client) UpdateStatus(cfg config.Config) (string, error) {
	if cfg.Status == nil {
		return "", fmt.Errorf("nil status for %v/%v on updateStatus()", cfg.Name, cfg.Namespace)
	}

	meta, err := updateStatus(cl.client, cfg, getObjectMetadata(cfg))
	if err != nil {
		return "", err
	}
	return meta.GetResourceVersion(), nil
}

// Delete implements store interface
// `resourceVersion` must be matched before deletion is carried out. If not possible, a 409 Conflict status will be
func (cl *Client) Delete(typ config.GroupVersionKind, name, namespace string, resourceVersion *string) error {
	return delete(cl.client, typ, name, namespace, resourceVersion)
}

// List implements store interface
func (cl *Client) List(kind config.GroupVersionKind, namespace string) []config.Config {
	h, f := cl.kind(kind)
	if !f {
		return nil
	}

	if namespace == metav1.NamespaceAll {
		return h.collection.List()
	}

	return h.index.Lookup(namespace)
}

func (cl *Client) allKinds() map[config.GroupVersionKind]nsStore {
	cl.kindsMu.RLock()
	defer cl.kindsMu.RUnlock()
	return maps.Clone(cl.kinds)
}

func (cl *Client) kind(r config.GroupVersionKind) (nsStore, bool) {
	cl.kindsMu.RLock()
	defer cl.kindsMu.RUnlock()
	ch, ok := cl.kinds[r]
	return ch, ok
}

func TranslateObject(r runtime.Object, gvk config.GroupVersionKind, domainSuffix string) config.Config {
	translateFunc, f := translationMap[gvk]
	if !f {
		scope.Errorf("unknown type %v", gvk)
		return config.Config{}
	}
	c := translateFunc(r)
	c.Domain = domainSuffix
	return c
}

func getObjectMetadata(config config.Config) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:            config.Name,
		Namespace:       config.Namespace,
		Labels:          config.Labels,
		Annotations:     config.Annotations,
		ResourceVersion: config.ResourceVersion,
		OwnerReferences: config.OwnerReferences,
		UID:             types.UID(config.UID),
	}
}

func genPatchBytes(oldRes, modRes runtime.Object, patchType types.PatchType) ([]byte, error) {
	oldJSON, err := json.Marshal(oldRes)
	if err != nil {
		return nil, fmt.Errorf("failed marhsalling original resource: %v", err)
	}
	newJSON, err := json.Marshal(modRes)
	if err != nil {
		return nil, fmt.Errorf("failed marhsalling modified resource: %v", err)
	}
	switch patchType {
	case types.JSONPatchType:
		ops, err := jsonpatch.CreatePatch(oldJSON, newJSON)
		if err != nil {
			return nil, err
		}
		return json.Marshal(ops)
	case types.MergePatchType:
		return jsonmerge.CreateMergePatch(oldJSON, newJSON)
	default:
		return nil, fmt.Errorf("unsupported patch type: %v. must be one of JSONPatchType or MergePatchType", patchType)
	}
}

func (cl *Client) addCRD(name string, opts krt.OptionsBuilder) {
	cl.logger.Debugf("adding CRD %q", name)
	s, f := cl.schemasByCRDName[name]
	if !f {
		cl.logger.Debugf("added resource that we are not watching: %v", name)
		return
	}
	resourceGVK := s.GroupVersionKind()
	gvr := s.GroupVersionResource()

	cl.kindsMu.Lock()
	defer cl.kindsMu.Unlock()
	if _, f := cl.kinds[resourceGVK]; f {
		cl.logger.Debugf("added resource that already exists: %v", resourceGVK)
		return
	}
	translateFunc, f := translationMap[resourceGVK]
	if !f {
		if !s.IsSynthetic() {
			cl.logger.Errorf("translation function for %v not found", resourceGVK)
		}
		return
	}

	// We need multiple filters:
	// 1. Is it in this revision?
	// 2. Does it match the discovery selector?
	// 3. Does it have a special per-type object filter?
	var extraFilter func(obj any) bool
	var transform func(obj any) (any, error)
	var fieldSelector string
	if of, f := cl.filtersByGVK[resourceGVK]; f {
		if of.ObjectFilter != nil {
			extraFilter = of.ObjectFilter.Filter
		}
		if of.ObjectTransform != nil {
			transform = of.ObjectTransform
		}
		fieldSelector = of.FieldSelector
	}

	var namespaceFilter kubetypes.DynamicObjectFilter
	if !s.IsClusterScoped() {
		namespaceFilter = cl.client.ObjectFilter()
	}
	filter := kubetypes.Filter{
		ObjectFilter:    kubetypes.ComposeFilters(namespaceFilter, cl.inRevision, extraFilter),
		ObjectTransform: transform,
		FieldSelector:   fieldSelector,
	}
	if resourceGVK == gvk.KubernetesGateway {
		filter.ObjectFilter = kubetypes.ComposeFilters(namespaceFilter, extraFilter)
	}

	var kc kclient.Untyped
	if s.IsBuiltin() {
		kc = kclient.NewUntypedInformer(cl.client, gvr, filter)
	} else {
		kc = kclient.NewDelayedInformer[controllers.Object](
			cl.client,
			gvr,
			kubetypes.StandardInformer,
			filter,
		)
	}

	wrappedClient := krt.WrapClient(kc, opts.WithName("informer/"+resourceGVK.Kind)...)
	collection := krt.MapCollection(wrappedClient, func(obj controllers.Object) config.Config {
		cfg := translateFunc(obj)
		cfg.Domain = cl.domainSuffix
		return cfg
	}, opts.WithName("collection/"+resourceGVK.Kind)...)
	index := krt.NewNamespaceIndex(collection)
	cl.kinds[resourceGVK] = nsStore{
		collection: collection,
		index:      index,
		handlers: []krt.HandlerRegistration{
			collection.RegisterBatch(func(o []krt.Event[config.Config]) {
				for _, event := range o {
					incrementEvent(resourceGVK.Kind, event.Event.String())
				}
			}, false),
		},
	}
}

func (cl *Client) inRevision(obj any) bool {
	object := controllers.ExtractObject(obj)
	if object == nil {
		return false
	}
	return config.LabelsInRevision(object.GetLabels(), cl.revision)
}
