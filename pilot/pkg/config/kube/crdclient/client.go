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
	"golang.org/x/exp/maps"
	"gomodules.xyz/jsonpatch/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
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
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/queue"
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
	kinds   map[config.GroupVersionKind]kclient.Untyped
	kindsMu sync.RWMutex
	queue   queue.Instance

	// handlers defines a list of event handlers per-type
	handlers map[config.GroupVersionKind][]model.EventHandler

	schemasByCRDName map[string]resource.Schema
	client           kube.Client
	logger           *log.Scope

	// namespacesFilter is only used to initiate filtered informer.
	namespacesFilter func(obj interface{}) bool
	filtersByGVK     map[config.GroupVersionKind]kubetypes.Filter
}

type Option struct {
	Revision         string
	DomainSuffix     string
	Identifier       string
	NamespacesFilter func(obj interface{}) bool
	FiltersByGVK     map[config.GroupVersionKind]kubetypes.Filter
}

var _ model.ConfigStoreController = &Client{}

func New(client kube.Client, opts Option) (*Client, error) {
	schemas := collections.Pilot
	if features.EnableGatewayAPI {
		schemas = collections.PilotGatewayAPI()
	}
	return NewForSchemas(client, opts, schemas)
}

func NewForSchemas(client kube.Client, opts Option, schemas collection.Schemas) (*Client, error) {
	schemasByCRDName := map[string]resource.Schema{}
	for _, s := range schemas.All() {
		// From the spec: "Its name MUST be in the format <.spec.name>.<.spec.group>."
		name := fmt.Sprintf("%s.%s", s.Plural(), s.Group())
		schemasByCRDName[name] = s
	}
	out := &Client{
		domainSuffix:     opts.DomainSuffix,
		schemas:          schemas,
		schemasByCRDName: schemasByCRDName,
		revision:         opts.Revision,
		queue:            queue.NewQueue(1 * time.Second),
		kinds:            map[config.GroupVersionKind]kclient.Untyped{},
		handlers:         map[config.GroupVersionKind][]model.EventHandler{},
		client:           client,
		logger:           scope.WithLabels("controller", opts.Identifier),
		namespacesFilter: opts.NamespacesFilter,
		filtersByGVK:     opts.FiltersByGVK,
	}

	for _, s := range out.schemas.All() {
		// From the spec: "Its name MUST be in the format <.spec.name>.<.spec.group>."
		name := fmt.Sprintf("%s.%s", s.Plural(), s.Group())
		out.addCRD(name)
	}

	return out, nil
}

func (cl *Client) RegisterEventHandler(kind config.GroupVersionKind, handler model.EventHandler) {
	cl.handlers[kind] = append(cl.handlers[kind], handler)
}

// Run the queue and all informers. Callers should  wait for HasSynced() before depending on results.
func (cl *Client) Run(stop <-chan struct{}) {
	t0 := time.Now()
	cl.logger.Infof("Starting Pilot K8S CRD controller")

	if !kube.WaitForCacheSync("crdclient", stop, cl.informerSynced) {
		cl.logger.Errorf("Failed to sync Pilot K8S CRD controller cache")
		return
	}
	cl.logger.Infof("Pilot K8S CRD controller synced in %v", time.Since(t0))

	cl.queue.Run(stop)
	cl.logger.Infof("controller terminated")
}

func (cl *Client) informerSynced() bool {
	for gk, ctl := range cl.allKinds() {
		if !ctl.HasSynced() {
			cl.logger.Infof("controller %q is syncing...", gk)
			return false
		}
	}
	return true
}

func (cl *Client) HasSynced() bool {
	return cl.queue.HasSynced()
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
	obj := h.Get(name, namespace)
	if obj == nil {
		cl.logger.Debugf("couldn't find %s/%s in informer index", namespace, name)
		return nil
	}

	cfg := TranslateObject(obj, typ, cl.domainSuffix)
	return &cfg
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

// Patch applies only the modifications made in the PatchFunc rather than doing a full replace. Useful to avoid
// read-modify-write conflicts when there are many concurrent-writers to the same resource.
func (cl *Client) Patch(orig config.Config, patchFn config.PatchFunc) (string, error) {
	modified, patchType := patchFn(orig.DeepCopy())

	meta, err := patch(cl.client, orig, getObjectMetadata(orig), modified, getObjectMetadata(modified), patchType)
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

	list := h.List(namespace, klabels.Everything())

	out := make([]config.Config, 0, len(list))
	for _, item := range list {
		cfg := TranslateObject(item, kind, cl.domainSuffix)
		out = append(out, cfg)
	}

	return out
}

func (cl *Client) allKinds() map[config.GroupVersionKind]kclient.Untyped {
	cl.kindsMu.RLock()
	defer cl.kindsMu.RUnlock()
	return maps.Clone(cl.kinds)
}

func (cl *Client) kind(r config.GroupVersionKind) (kclient.Untyped, bool) {
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

func (cl *Client) addCRD(name string) {
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
	filter := cl.filtersByGVK[resourceGVK]
	objectFilter := filter.ObjectFilter
	filter.ObjectFilter = func(t any) bool {
		if objectFilter != nil && !objectFilter(t) {
			return false
		}
		if cl.namespacesFilter != nil && !cl.namespacesFilter(t) {
			return false
		}
		return config.LabelsInRevision(t.(controllers.Object).GetLabels(), cl.revision)
	}
	var kc kclient.Untyped
	if s.IsBuiltin() {
		kc = kclient.NewUntypedInformer(cl.client, gvr, filter)
	} else {
		kc = kclient.NewDelayedInformer(
			cl.client,
			gvr,
			kubetypes.StandardInformer,
			filter,
		)
	}

	kind := s.Kind()
	kc.AddEventHandler(controllers.EventHandler[controllers.Object]{
		AddFunc: func(obj controllers.Object) {
			incrementEvent(kind, "add")
			cl.queue.Push(func() error {
				cl.onEvent(resourceGVK, nil, obj, model.EventAdd)
				return nil
			})
		},
		UpdateFunc: func(old, cur controllers.Object) {
			incrementEvent(kind, "update")
			cl.queue.Push(func() error {
				cl.onEvent(resourceGVK, old, cur, model.EventUpdate)
				return nil
			})
		},
		DeleteFunc: func(obj controllers.Object) {
			incrementEvent(kind, "delete")
			cl.queue.Push(func() error {
				cl.onEvent(resourceGVK, nil, obj, model.EventDelete)
				return nil
			})
		},
	})

	cl.kinds[resourceGVK] = kc
}

func (cl *Client) onEvent(resourceGVK config.GroupVersionKind, old controllers.Object, curr controllers.Object, event model.Event) {
	currItem := controllers.ExtractObject(curr)
	if currItem == nil {
		return
	}

	currConfig := TranslateObject(currItem, resourceGVK, cl.domainSuffix)

	var oldConfig config.Config
	if old != nil {
		oldConfig = TranslateObject(old, resourceGVK, cl.domainSuffix)
	}

	for _, f := range cl.handlers[resourceGVK] {
		f(oldConfig, currConfig, event)
	}
}
