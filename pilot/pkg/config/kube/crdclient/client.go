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
	"errors"
	"fmt"
	"time"

	jsonmerge "github.com/evanphx/json-patch/v5"
	"gomodules.xyz/jsonpatch/v2"
	crd "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/informers"

	//  import GKE cluster authentication plugin
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	//  import OIDC cluster authentication plugin, e.g. for Tectonic
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/tools/cache"
	gatewayapiclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"

	"istio.io/api/label"
	istioclient "istio.io/client-go/pkg/clientset/versioned"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube"
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
	kinds map[config.GroupVersionKind]*cacheHandler
	queue queue.Instance

	// The istio/client-go client we will use to access objects
	istioClient istioclient.Interface

	// The gateway-api client we will use to access objects
	gatewayAPIClient gatewayapiclient.Interface
}

var _ model.ConfigStoreCache = &Client{}

// Validate we are ready to handle events. Until the informers are synced, we will block the queue
func (cl *Client) checkReadyForEvents(curr interface{}) error {
	if !cl.HasSynced() {
		return errors.New("waiting till full synchronization")
	}
	_, err := cache.DeletionHandlingMetaNamespaceKeyFunc(curr)
	if err != nil {
		scope.Infof("Error retrieving key: %v", err)
	}
	return nil
}

func (cl *Client) RegisterEventHandler(kind config.GroupVersionKind, handler func(config.Config, config.Config, model.Event)) {
	h, exists := cl.kinds[kind]
	if !exists {
		return
	}

	h.handlers = append(h.handlers, handler)
}

// Run the queue and all informers. Callers should  wait for HasSynced() before depending on results.
func (cl *Client) Run(stop <-chan struct{}) {
	t0 := time.Now()
	scope.Info("Starting Pilot K8S CRD controller")

	go func() {
		cache.WaitForCacheSync(stop, cl.HasSynced)
		scope.Info("Pilot K8S CRD controller synced ", time.Since(t0))
		cl.queue.Run(stop)
	}()

	<-stop
	scope.Info("controller terminated")
}

func (cl *Client) HasSynced() bool {
	for kind, ctl := range cl.kinds {
		if !ctl.informer.HasSynced() {
			scope.Infof("controller %q is syncing...", kind)
			return false
		}
	}
	return true
}

func New(client kube.Client, revision, domainSuffix string) (model.ConfigStoreCache, error) {
	schemas := collections.Pilot
	if features.EnableServiceApis {
		schemas = collections.PilotServiceApi
	}
	return NewForSchemas(client, revision, domainSuffix, schemas)
}

func NewForSchemas(client kube.Client, revision, domainSuffix string, schemas collection.Schemas) (model.ConfigStoreCache, error) {
	out := &Client{
		domainSuffix:     domainSuffix,
		schemas:          schemas,
		revision:         revision,
		queue:            queue.NewQueue(1 * time.Second),
		kinds:            map[config.GroupVersionKind]*cacheHandler{},
		istioClient:      client.Istio(),
		gatewayAPIClient: client.GatewayAPI(),
	}
	known := knownCRDs(client.Ext())
	for _, s := range out.schemas.All() {
		// From the spec: "Its name MUST be in the format <.spec.name>.<.spec.group>."
		name := fmt.Sprintf("%s.%s", s.Resource().Plural(), s.Resource().Group())
		if _, f := known[name]; f {
			var i informers.GenericInformer
			var err error
			if s.Resource().Group() == "networking.x-k8s.io" {
				i, err = client.GatewayAPIInformer().ForResource(s.Resource().GroupVersionResource())
			} else {
				i, err = client.IstioInformer().ForResource(s.Resource().GroupVersionResource())
			}
			if err != nil {
				return nil, err
			}
			out.kinds[s.Resource().GroupVersionKind()] = createCacheHandler(out, s, i)
		} else {
			scope.Warnf("Skipping CRD %v as it is not present", s.Resource().GroupVersionKind())
		}
	}

	return out, nil
}

// Schemas for the store
func (cl *Client) Schemas() collection.Schemas {
	return cl.schemas
}

// Get implements store interface
func (cl *Client) Get(typ config.GroupVersionKind, name, namespace string) *config.Config {
	h, f := cl.kinds[typ]
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

	cfg := TranslateObject(obj, typ, cl.domainSuffix)
	if !cl.objectInRevision(cfg) {
		return nil
	}
	return cfg
}

// Create implements store interface
func (cl *Client) Create(cfg config.Config) (string, error) {
	if cfg.Spec == nil {
		return "", fmt.Errorf("nil spec for %v/%v", cfg.Name, cfg.Namespace)
	}

	meta, err := create(cl.istioClient, cl.gatewayAPIClient, cfg, getObjectMetadata(cfg))
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

	meta, err := update(cl.istioClient, cl.gatewayAPIClient, cfg, getObjectMetadata(cfg))
	if err != nil {
		return "", err
	}
	return meta.GetResourceVersion(), nil
}

func (cl *Client) UpdateStatus(cfg config.Config) (string, error) {
	if cfg.Status == nil {
		return "", fmt.Errorf("nil status for %v/%v on updateStatus()", cfg.Name, cfg.Namespace)
	}

	meta, err := updateStatus(cl.istioClient, cl.gatewayAPIClient, cfg, getObjectMetadata(cfg))
	if err != nil {
		return "", err
	}
	return meta.GetResourceVersion(), nil
}

// Patch applies only the modifications made in the PatchFunc rather than doing a full replace. Useful to avoid
// read-modify-write conflicts when there are many concurrent-writers to the same resource.
func (cl *Client) Patch(orig config.Config, patchFn config.PatchFunc) (string, error) {
	modified, patchType := patchFn(orig.DeepCopy())

	meta, err := patch(cl.istioClient, cl.gatewayAPIClient, orig, getObjectMetadata(orig), modified, getObjectMetadata(modified), patchType)
	if err != nil {
		return "", err
	}
	return meta.GetResourceVersion(), nil
}

// Delete implements store interface
// `resourceVersion` must be matched before deletion is carried out. If not possible, a 409 Conflict status will be
func (cl *Client) Delete(typ config.GroupVersionKind, name, namespace string, resourceVersion *string) error {
	return delete(cl.istioClient, cl.gatewayAPIClient, typ, name, namespace, resourceVersion)
}

// List implements store interface
func (cl *Client) List(kind config.GroupVersionKind, namespace string) ([]config.Config, error) {
	h, f := cl.kinds[kind]
	if !f {
		return nil, nil
	}

	list, err := h.lister(namespace).List(klabels.Everything())
	if err != nil {
		return nil, err
	}
	out := make([]config.Config, 0, len(list))
	for _, item := range list {
		cfg := TranslateObject(item, kind, cl.domainSuffix)
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

// knownCRDs returns all CRDs present in the cluster, with retries
func knownCRDs(crdClient apiextensionsclient.Interface) map[string]struct{} {
	delay := time.Second
	maxDelay := time.Minute
	var res *crd.CustomResourceDefinitionList
	for {
		var err error
		res, err = crdClient.ApiextensionsV1().CustomResourceDefinitions().List(context.TODO(), metav1.ListOptions{})
		if err == nil {
			break
		}
		scope.Errorf("failed to list CRDs: %v", err)
		time.Sleep(delay)
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}

	mp := map[string]struct{}{}
	for _, r := range res.Items {
		mp[r.Name] = struct{}{}
	}
	return mp
}

func TranslateObject(r runtime.Object, gvk config.GroupVersionKind, domainSuffix string) *config.Config {
	translateFunc, f := translationMap[gvk]
	if !f {
		scope.Errorf("unknown type %v", gvk)
		return nil
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
