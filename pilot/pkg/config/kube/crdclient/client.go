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
	"context"
	"fmt"
	"sync"
	"time"

	jsonmerge "github.com/evanphx/json-patch/v5"
	"go.uber.org/atomic"
	"gomodules.xyz/jsonpatch/v3"
	crd "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/informers"
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
	"istio.io/istio/pkg/kube/watcher/crdwatcher"
	"istio.io/istio/pkg/queue"
	"istio.io/pkg/log"
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
	// a flag indicates whether this client has been run, it is to prevent run queue twice
	started *atomic.Bool

	// handlers defines a list of event handlers per-type
	handlers map[config.GroupVersionKind][]model.EventHandler

	schemasByCRDName map[string]resource.Schema
	client           kube.Client
	crdWatcher       *crdwatcher.Controller
	logger           *log.Scope

	// namespacesFilter is only used to initiate filtered informer.
	namespacesFilter func(obj interface{}) bool

	// crdWatches notifies consumers when a CRD is present
	crdWatches map[config.GroupVersionKind]*waiter
	stop       <-chan struct{}
}

type Option struct {
	Revision         string
	DomainSuffix     string
	Identifier       string
	NamespacesFilter func(obj interface{}) bool
}

var _ model.ConfigStoreController = &Client{}

func New(client kube.Client, opts Option) (*Client, error) {
	schemas := collections.Pilot
	if features.EnableGatewayAPI {
		schemas = collections.PilotGatewayAPI()
	}
	return NewForSchemas(client, opts, schemas)
}

type waiter struct {
	once sync.Once
	stop chan struct{}
}

func newWaiter() *waiter {
	return &waiter{
		once: sync.Once{},
		stop: make(chan struct{}),
	}
}

// WaitForCRD waits until the request CRD exists, and returns true on success. A false return value
// indicates the CRD does not exist but the wait failed or was canceled.
// This is useful to conditionally enable controllers based on CRDs being created.
func (cl *Client) WaitForCRD(k config.GroupVersionKind, stop <-chan struct{}) bool {
	ch, f := cl.crdWatches[k]
	if !f {
		log.Warnf("waiting for CRD %s that is not registered", k.String())
		return false
	}
	select {
	case <-stop:
		return false
	case <-ch.stop:
		return true
	}
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
		started:          atomic.NewBool(false),
		kinds:            map[config.GroupVersionKind]*cacheHandler{},
		handlers:         map[config.GroupVersionKind][]model.EventHandler{},
		client:           client,
		crdWatcher:       crdwatcher.NewController(client),
		logger:           scope.WithLabels("controller", opts.Identifier),
		namespacesFilter: opts.NamespacesFilter,
		crdWatches: map[config.GroupVersionKind]*waiter{
			gvk.KubernetesGateway: newWaiter(),
			gvk.GatewayClass:      newWaiter(),
		},
	}

	out.crdWatcher.AddCallBack(func(name string) {
		handleCRDAdd(out, name)
	})
	known, err := knownCRDs(client.Ext())
	if err != nil {
		return nil, err
	}
	for _, s := range schemas.All() {
		// From the spec: "Its name MUST be in the format <.spec.name>.<.spec.group>."
		name := fmt.Sprintf("%s.%s", s.Plural(), s.Group())
		if s.IsBuiltin() {
			handleCRDAdd(out, name)
		} else {
			if _, f := known[name]; f {
				handleCRDAdd(out, name)
			} else {
				out.logger.Warnf("Skipping CRD %v as it is not present", s.GroupVersionKind())
			}
		}
	}
	return out, nil
}

func (cl *Client) RegisterEventHandler(kind config.GroupVersionKind, handler model.EventHandler) {
	cl.handlers[kind] = append(cl.handlers[kind], handler)
}

// Run the queue and all informers. Callers should  wait for HasSynced() before depending on results.
func (cl *Client) Run(stop <-chan struct{}) {
	if cl.started.Swap(true) {
		// was already started by other thread
		return
	}

	t0 := time.Now()
	cl.logger.Infof("Starting Pilot K8S CRD controller")

	cl.stop = stop

	if !kube.WaitForCacheSync(stop, cl.informerSynced) {
		cl.logger.Errorf("Failed to sync Pilot K8S CRD controller cache")
		return
	}
	cl.logger.Infof("Pilot K8S CRD controller synced in %v", time.Since(t0))
	cl.queue.Run(stop)
	cl.logger.Infof("controller terminated")
}

func (cl *Client) informerSynced() bool {
	for _, ctl := range cl.allKinds() {
		if !ctl.informer.HasSynced() {
			cl.logger.Infof("controller %q is syncing...", ctl.schema.GroupVersionKind())
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
	obj := h.informer.Get(name, namespace)
	if obj == nil {
		cl.logger.Debugf("couldn't find %s/%s in informer index", namespace, name)
		return nil
	}

	cfg := TranslateObject(obj, typ, cl.domainSuffix)
	if !cl.objectInRevision(&cfg) {
		return nil
	}
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

	list := h.informer.List(namespace, klabels.Everything())

	out := make([]config.Config, 0, len(list))
	for _, item := range list {
		cfg := TranslateObject(item, kind, cl.domainSuffix)
		if cl.objectInRevision(&cfg) {
			out = append(out, cfg)
		}
	}

	return out
}

func (cl *Client) objectInRevision(o *config.Config) bool {
	return config.ObjectInRevision(o, cl.revision)
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

// knownCRDs returns all CRDs present in the cluster, with timeout and retries.
func knownCRDs(crdClient apiextensionsclient.Interface) (map[string]struct{}, error) {
	var res *crd.CustomResourceDefinitionList
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var err error
	res, err = crdClient.ApiextensionsV1().CustomResourceDefinitions().List(ctx, metav1.ListOptions{})
	if err != nil {
		scope.Errorf("failed to list CRDs: %v", err)
		return nil, err
	}
	mp := map[string]struct{}{}
	for _, r := range res.Items {
		mp[r.Name] = struct{}{}
	}
	return mp, nil
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

func handleCRDAdd(cl *Client, name string) {
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
	var i informers.GenericInformer
	var ifactory starter
	var err error
	switch s.Group() {
	case gvk.KubernetesGateway.Group:
		ifactory = cl.client.GatewayAPIInformer()
		i, err = cl.client.GatewayAPIInformer().ForResource(gvr)
	case gvk.Pod.Group, gvk.Deployment.Group, gvk.MutatingWebhookConfiguration.Group:
		ifactory = cl.client.KubeInformer()
		i, err = cl.client.KubeInformer().ForResource(gvr)
	case gvk.CustomResourceDefinition.Group:
		ifactory = cl.client.ExtInformer()
		i, err = cl.client.ExtInformer().ForResource(gvr)
	default:
		ifactory = cl.client.IstioInformer()
		i, err = cl.client.IstioInformer().ForResource(gvr)
	}
	if err != nil {
		// Shouldn't happen
		cl.logger.Errorf("failed to create informer for %v: %v", resourceGVK, err)
		return
	}
	_ = i.Informer().SetTransform(kube.StripUnusedFields)

	cl.kinds[resourceGVK] = createCacheHandler(cl, s, i)
	if w, f := cl.crdWatches[resourceGVK]; f {
		cl.logger.Infof("notifying watchers %v was created", resourceGVK)
		w.once.Do(func() {
			close(w.stop)
		})
	}
	// Start informer. In startup case, we will not start here as
	// we will start all factories once we are ready to initialize.
	// For dynamically added CRDs, we need to start immediately though
	if cl.stop != nil {
		ifactory.Start(cl.stop)
	}
}

type starter interface {
	Start(stopCh <-chan struct{})
}
