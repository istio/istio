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

// Package arbitraryclient provides an implementation of the config store and cache
// using Kubernetes Resources and the informer framework from Kubernetes
//
// To implement the Istio store interface, we need to take dynamic inputs. Using the dynamic informers results in poor
// performance, as the cache will store unstructured objects which need to be marshaled on each Get/List call.
// Therefore this store is appropriate only for applications where low performance is acceptable.
package arbitraryclient

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.uber.org/atomic"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"

	//  import GKE cluster authentication plugin
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	//  import OIDC cluster authentication plugin, e.g. for Tectonic
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/tools/cache"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/queue"
	"istio.io/pkg/log"

	"istio.io/pkg/monitoring"
)

var scope = log.RegisterScope("kube", "Kubernetes client messages", 0)

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
	kinds   map[config.GroupVersionKind]*cacheHandler
	kindsMu sync.RWMutex
	queue   queue.Instance

	// handlers defines a list of event handlers per-type
	handlers map[config.GroupVersionKind][]model.EventHandler

	// beginSync is set to true when calling SyncAll, it indicates the controller has began sync resources.
	beginSync *atomic.Bool
	// initialSync is set to true after performing an initial processing of all objects.
	initialSync *atomic.Bool
}

var _ model.ConfigStoreCache = &Client{}

func New(client kube.Client, revision, domainSuffix string) (model.ConfigStoreCache, error) {
	schemas := collections.Kube
	if features.EnableGatewayAPI {
		schemas = collections.PilotGatewayAPI
	}
	return NewForSchemas(context.Background(), client, revision, domainSuffix, schemas)
}

func NewForSchemas(ctx context.Context, client kube.Client, revision, domainSuffix string, schemas collection.Schemas) (model.ConfigStoreCache, error) {
	out := &Client{
		domainSuffix: domainSuffix,
		schemas:      schemas,
		revision:     revision,
		queue:        queue.NewQueue(1 * time.Second),
		kinds:        map[config.GroupVersionKind]*cacheHandler{},
		handlers:     map[config.GroupVersionKind][]model.EventHandler{},
		beginSync:    atomic.NewBool(false),
		initialSync:  atomic.NewBool(false),
	}

	for _, s := range schemas.All() {
		i := client.DynamicInformer().ForResource(s.Resource().GroupVersionResource())
		out.kinds[s.Resource().GroupVersionKind()] = createCacheHandler(out, s, i)
	}

	return out, nil
}

// Validate we are ready to handle events. Until the informers are synced, we will block the queue
func (cl *Client) checkReadyForEvents(curr interface{}) error {
	if !cl.informerSynced() {
		return errors.New("waiting till full synchronization")
	}
	_, err := cache.DeletionHandlingMetaNamespaceKeyFunc(curr)
	if err != nil {
		scope.Infof("Error retrieving key: %v", err)
	}
	return nil
}

func (cl *Client) RegisterEventHandler(kind config.GroupVersionKind, handler model.EventHandler) {
	cl.handlers[kind] = append(cl.handlers[kind], handler)
}

func (cl *Client) SetWatchErrorHandler(handler func(r *cache.Reflector, err error)) error {
	panic("not implemented")
}

// Run the queue and all informers. Callers should  wait for HasSynced() before depending on results.
func (cl *Client) Run(stop <-chan struct{}) {
	t0 := time.Now()
	scope.Info("Starting Pilot K8S CRD controller")

	if !cache.WaitForCacheSync(stop, cl.informerSynced) {
		scope.Error("Failed to sync Pilot K8S CRD controller cache")
		return
	}
	cl.SyncAll()
	cl.initialSync.Store(true)
	scope.Info("Pilot K8S CRD controller synced ", time.Since(t0))

	cl.queue.Run(stop)
	scope.Info("controller terminated")
}

func (cl *Client) informerSynced() bool {
	for _, ctl := range cl.allKinds() {
		if !ctl.informer.HasSynced() {
			scope.Infof("controller %q is syncing...", ctl.schema.Resource().GroupVersionKind())
			return false
		}
	}
	return true
}

func (cl *Client) HasSynced() bool {
	return cl.initialSync.Load()
}

// SyncAll syncs all the objects during bootstrap to make the configs updated to caches
func (cl *Client) SyncAll() {
	cl.beginSync.Store(true)
	wg := sync.WaitGroup{}
	for _, h := range cl.allKinds() {
		handlers := cl.handlers[h.schema.Resource().GroupVersionKind()]
		if len(handlers) == 0 {
			continue
		}
		h := h
		wg.Add(1)
		go func() {
			defer wg.Done()
			objects := h.informer.GetIndexer().List()
			for _, object := range objects {
				currItem, ok := object.(*unstructured.Unstructured)
				if !ok {
					scope.Warnf("New Object can not be converted to runtime Object %v, is type %T", object, object)
					return
				}
				currConfig := *TranslateObject(currItem, h.client.domainSuffix, h.schema)
				for _, f := range handlers {
					f(config.Config{}, currConfig, model.EventAdd)
				}
			}
		}()
	}
	wg.Wait()
}

func TranslateObject(obj *unstructured.Unstructured, domainSuffix string, schema collection.Schema) *config.Config {
	mv2, err := schema.Resource().NewInstance()
	if err != nil {
		panic(err)
	}
	if spec, ok := obj.UnstructuredContent()["spec"]; ok {
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(spec.(map[string]interface{}), mv2)
	} else {
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), mv2)
	}
	if err != nil {
		panic(err)
	}

	m := obj
	return &config.Config{
		Meta: config.Meta{
			GroupVersionKind: config.GroupVersionKind{
				Group:   m.GetObjectKind().GroupVersionKind().Group,
				Version: m.GetObjectKind().GroupVersionKind().Version,
				Kind:    m.GetObjectKind().GroupVersionKind().Kind,
			},
			UID:               string(m.GetUID()),
			Name:              m.GetName(),
			Namespace:         m.GetNamespace(),
			Labels:            m.GetLabels(),
			Annotations:       m.GetAnnotations(),
			ResourceVersion:   m.GetResourceVersion(),
			CreationTimestamp: m.GetCreationTimestamp().Time,
			OwnerReferences:   m.GetOwnerReferences(),
			Generation:        m.GetGeneration(),
			Domain:            domainSuffix,
		},
		Spec: mv2,
	}
}

// Schemas for the store
func (cl *Client) Schemas() collection.Schemas {
	return cl.schemas
}

// Get implements store interface
func (cl *Client) Get(typ config.GroupVersionKind, name, namespace string) *config.Config {
	h, f := cl.kind(typ)
	if !f {
		scope.Warnf("unknown type: %s", typ)
		return nil
	}

	obj, err := h.lister(namespace).Get(name)
	if err != nil {
		// TODO we should be returning errors not logging
		scope.Warnf("error on get %v/%v: %v", name, namespace, err)
		return nil
	}

	cfg := TranslateObject(obj.(*unstructured.Unstructured), cl.domainSuffix, h.schema)
	if !cl.objectInRevision(cfg) {
		return nil
	}
	return cfg
}

// Create implements store interface
func (cl *Client) Create(cfg config.Config) (string, error) {
	panic("Create not implemented: this cache is read-only")
}

// Update implements store interface
func (cl *Client) Update(cfg config.Config) (string, error) {
	panic("Update not implemented: this cache is read-only")
}

func (cl *Client) UpdateStatus(cfg config.Config) (string, error) {
	panic("UpdateStatus not implemented: this cache is read-only")
}

// Patch applies only the modifications made in the PatchFunc rather than doing a full replace. Useful to avoid
// read-modify-write conflicts when there are many concurrent-writers to the same resource.
func (cl *Client) Patch(orig config.Config, patchFn config.PatchFunc) (string, error) {
	panic("Patch not implemented: this cache is read-only")
}

// Delete implements store interface
// `resourceVersion` must be matched before deletion is carried out. If not possible, a 409 Conflict status will be
func (cl *Client) Delete(typ config.GroupVersionKind, name, namespace string, resourceVersion *string) error {
	panic("Delete not implemented: this cache is read-only")
}

// List implements store interface
func (cl *Client) List(kind config.GroupVersionKind, namespace string) ([]config.Config, error) {
	h, f := cl.kind(kind)
	if !f {
		return nil, nil
	}

	list, err := h.lister(namespace).List(klabels.Everything())
	if err != nil {
		return nil, err
	}
	out := make([]config.Config, 0, len(list))
	for _, item := range list {
		cfg := TranslateObject(item.(*unstructured.Unstructured), cl.domainSuffix, h.schema)
		if cl.objectInRevision(cfg) {
			out = append(out, *cfg)
		}
	}

	return out, err
}

func (cl *Client) objectInRevision(o *config.Config) bool {
	configEnv, f := o.Labels[label.IoIstioRev.Name]
	if !f {
		// This is a global object, and always included
		return true
	}
	// Otherwise, only return if the
	return configEnv == cl.revision
}

func (cl *Client) allKinds() []*cacheHandler {
	cl.kindsMu.RLock()
	defer cl.kindsMu.RUnlock()
	ret := make([]*cacheHandler, 0, len(cl.kinds))
	for _, k := range cl.kinds {
		ret = append(ret, k)
	}
	return ret
}

func (cl *Client) kind(r config.GroupVersionKind) (*cacheHandler, bool) {
	cl.kindsMu.RLock()
	defer cl.kindsMu.RUnlock()
	ch, ok := cl.kinds[r]
	return ch, ok
}

// cacheHandler abstracts the logic of an informer with a set of handlers. Handlers can be added at runtime
// and will be invoked on each informer event.
type cacheHandler struct {
	client   *Client
	informer cache.SharedIndexInformer
	schema   collection.Schema
	lister   func(namespace string) cache.GenericNamespaceLister
}

func (h *cacheHandler) onEvent(old interface{}, curr interface{}, event model.Event) error {
	if err := h.client.checkReadyForEvents(curr); err != nil {
		return err
	}

	currItem, ok := curr.(*unstructured.Unstructured)
	if !ok {
		scope.Warnf("New Object can not be converted to unstructured %v, is type %T", curr, curr)
		return nil
	}
	currConfig := *TranslateObject(currItem, h.client.domainSuffix, h.schema)

	var oldConfig config.Config
	if old != nil {
		oldItem, ok := old.(*unstructured.Unstructured)
		if !ok {
			log.Warnf("Old Object can not be converted to runtime Object %v, is type %T", old, old)
			return nil
		}
		oldConfig = *TranslateObject(oldItem, h.client.domainSuffix, h.schema)
	}

	// TODO we may consider passing a pointer to handlers instead of the value. While spec is a pointer, the meta will be copied
	for _, f := range h.client.handlers[h.schema.Resource().GroupVersionKind()] {
		f(oldConfig, currConfig, event)
	}
	return nil
}

func createCacheHandler(cl *Client, schema collection.Schema, i informers.GenericInformer) *cacheHandler {
	scope.Debugf("registered CRD %v", schema.Resource().GroupVersionKind())
	h := &cacheHandler{
		client:   cl,
		schema:   schema,
		informer: i.Informer(),
	}
	h.lister = func(namespace string) cache.GenericNamespaceLister {
		if schema.Resource().IsClusterScoped() {
			return i.Lister()
		}
		return i.Lister().ByNamespace(namespace)
	}
	kind := schema.Resource().Kind()
	i.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			incrementEvent(kind, "add")
			if !cl.beginSync.Load() {
				return
			}
			cl.queue.Push(func() error {
				return h.onEvent(nil, obj, model.EventAdd)
			})
		},
		UpdateFunc: func(old, cur interface{}) {
			incrementEvent(kind, "update")
			if !cl.beginSync.Load() {
				return
			}
			cl.queue.Push(func() error {
				return h.onEvent(old, cur, model.EventUpdate)
			})
		},
		DeleteFunc: func(obj interface{}) {
			incrementEvent(kind, "delete")
			if !cl.beginSync.Load() {
				return
			}
			cl.queue.Push(func() error {
				return h.onEvent(nil, obj, model.EventDelete)
			})
		},
	})
	return h
}

var (
	typeTag  = monitoring.MustCreateLabel("type")
	eventTag = monitoring.MustCreateLabel("event")

	k8sEvents = monitoring.NewSum(
		"pilot_k8s_cfg_events",
		"Events from k8s config.",
		monitoring.WithLabels(typeTag, eventTag),
	)
)

func init() {
	monitoring.MustRegister(k8sEvents)
}

func incrementEvent(kind, event string) {
	k8sEvents.With(typeTag.Value(kind), eventTag.Value(event)).Increment()
}
