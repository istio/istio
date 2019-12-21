// Copyright 2019 Istio Authors
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

package webhook

import (
	"fmt"
	"io/ioutil"
	"reflect"
	"time"

	kubeApiAdmission "k8s.io/api/admissionregistration/v1beta1"
	kubeApiApp "k8s.io/api/apps/v1"
	kubeApiCore "k8s.io/api/core/v1"
	kubeErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"istio.io/pkg/filewatcher"
	"istio.io/pkg/log"
)

var scope = log.RegisterScope("webhook controller", "webhook controller", 0)

type Options struct {
	WatchedNamespace  string
	ResyncPeriod      time.Duration
	CAPath            string
	ConfigPath        string
	WebhookConfigName string
	ServiceName       string
	Client            kubernetes.Interface
	GalleyDeployment  string
	ClusterRoleName   string
}

type Controller struct {
	o                 Options
	ownerRefs         []kubeApiMeta.OwnerReference
	queue             workqueue.RateLimitingInterface
	sharedInformers   informers.SharedInformerFactory
	caFileWatcher     filewatcher.FileWatcher
	readFile          func(filename string) ([]byte, error) // test stub
	reconcileDone     func()
	endpointReadyOnce bool
}

type reconcileRequest struct {
	description string
}

func (rr reconcileRequest) String() string {
	return rr.description
}

func filter(in interface{}, wantName, wantNamespace string) (skip bool, key string) {
	obj, err := meta.Accessor(in)
	if err != nil {
		skip = true
		return
	}
	if wantNamespace != "" && obj.GetNamespace() != wantNamespace {
		skip = true
		return
	}
	if wantName != "" && obj.GetName() != wantName {
		skip = true
		return
	}

	// ignore the error because there's nothing to do if this fails.
	key, _ = cache.DeletionHandlingMetaNamespaceKeyFunc(in)
	return
}

func makeHandler(queue workqueue.Interface, gvk schema.GroupVersionKind, nameMatch, namespaceMatch string) *cache.ResourceEventHandlerFuncs {
	return &cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			skip, key := filter(obj, nameMatch, namespaceMatch)
			if skip {
				return
			}

			req := &reconcileRequest{fmt.Sprintf("adding (%v, Kind=%v) %v", gvk.GroupVersion(), gvk.Kind, key)}
			queue.Add(req)
		},
		UpdateFunc: func(prev, curr interface{}) {
			skip, key := filter(curr, nameMatch, namespaceMatch)
			if skip {
				return
			}
			if !reflect.DeepEqual(prev, curr) {
				req := &reconcileRequest{fmt.Sprintf("update (%v, Kind=%v) %v", gvk.GroupVersion(), gvk.Kind, key)}
				queue.Add(req)
			}
		},
		DeleteFunc: func(obj interface{}) {
			if _, ok := obj.(kubeApiMeta.Object); !ok {
				// If the object doesn't have Metadata, assume it is a tombstone object
				// of type DeletedFinalStateUnknown
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					return
				}
				obj = tombstone.Obj
			}
			skip, key := filter(obj, nameMatch, namespaceMatch)
			if skip {
				return
			}
			req := &reconcileRequest{fmt.Sprintf("delete (%v, Kind=%v) %v", gvk.GroupVersion(), gvk.Kind, key)}
			queue.Add(req)
		},
	}
}

func New(o Options) *Controller {
	return newController(o, filewatcher.NewWatcher, ioutil.ReadFile, nil)
}

type readFileFunc func(filename string) ([]byte, error)

// precompute GVK for known types for the purposes of logging.
var (
	configGVK     = kubeApiAdmission.SchemeGroupVersion.WithKind(reflect.TypeOf(kubeApiAdmission.ValidatingWebhookConfiguration{}).Name())
	endpointGVK   = kubeApiCore.SchemeGroupVersion.WithKind(reflect.TypeOf(kubeApiCore.Endpoints{}).Name())
	deploymentGVK = kubeApiApp.SchemeGroupVersion.WithKind(reflect.TypeOf(kubeApiApp.Deployment{}).Name())
)

func newController(
	o Options,
	newFileWatcher filewatcher.NewFileWatcherFunc,
	readFile readFileFunc,
	reconcileDone func(),
) *Controller {
	c := &Controller{
		o:             o,
		queue:         workqueue.NewRateLimitingQueue(workqueue.DefaultItemBasedRateLimiter()),
		caFileWatcher: newFileWatcher(),
		readFile:      readFile,
		reconcileDone: reconcileDone,
		ownerRefs:     findClusterRoleOwnerRefs(o.Client, o.ClusterRoleName),
	}

	c.sharedInformers = informers.NewSharedInformerFactoryWithOptions(o.Client, o.ResyncPeriod,
		informers.WithNamespace(o.WatchedNamespace))

	webhookInformer := c.sharedInformers.Admissionregistration().V1beta1().ValidatingWebhookConfigurations().Informer()
	webhookInformer.AddEventHandler(makeHandler(c.queue, configGVK, o.WebhookConfigName, ""))

	endpointInformer := c.sharedInformers.Core().V1().Endpoints().Informer()
	endpointInformer.AddEventHandler(makeHandler(c.queue, endpointGVK, o.ServiceName, o.WatchedNamespace))

	deploymentInformer := c.sharedInformers.Apps().V1().Deployments().Informer()
	deploymentInformer.AddEventHandler(makeHandler(c.queue, deploymentGVK, o.GalleyDeployment, o.WatchedNamespace))

	return c
}

func (c *Controller) Start(stop <-chan struct{}) {
	go c.startFileWatcher(stop)
	go c.sharedInformers.Start(stop)

	for _, ready := range c.sharedInformers.WaitForCacheSync(stop) {
		if !ready {
			return
		}
	}

	req := &reconcileRequest{"initial request to kickstart reconcilation"}
	c.queue.Add(req)

	go c.runWorker()
}

func (c *Controller) startFileWatcher(stop <-chan struct{}) {
	for {
		select {
		case ev := <-c.caFileWatcher.Events(c.o.CAPath):
			req := &reconcileRequest{fmt.Sprintf("CA file changed: %v", ev)}
			c.queue.Add(req)
		case <-c.caFileWatcher.Errors(c.o.CAPath):
			// log only
		case <-stop:
			return
		}
	}
}

func (c *Controller) processDeployments() (stop bool, err error) {
	galley, err := c.sharedInformers.Apps().V1().
		Deployments().Lister().Deployments(c.o.WatchedNamespace).Get(c.o.GalleyDeployment)

	// galley does/doesn't exist
	if err != nil {
		if kubeErrors.IsNotFound(err) {
			return false, nil
		}
		return true, err
	}

	// galley is scaled down to zero replicas. This is useful for debugging
	// to force the istiod controller to run.
	if galley.Spec.Replicas != nil && *galley.Spec.Replicas == 0 {
		return false, nil
	}
	return true, nil
}

func (c *Controller) processEndpoints() (stop bool, err error) {
	if c.endpointReadyOnce {
		return true, nil
	}
	endpoint, err := c.sharedInformers.Core().V1().
		Endpoints().Lister().Endpoints(c.o.WatchedNamespace).Get(c.o.ServiceName)
	if err != nil {
		if kubeErrors.IsNotFound(err) {
			return true, nil
		}
		return true, err
	}
	ready, _ := endpointReady(endpoint)
	return !ready, nil
}

// reconcile the desired state with the kube-apiserver.
func (c *Controller) reconcile(req *reconcileRequest) error {
	defer func() {
		if c.reconcileDone != nil {
			c.reconcileDone()
		}
	}()

	scope.Infof("Reconcile: %v", req)

	// skip reconciliation if our endpoint isn't ready ...
	if stop, err := c.processEndpoints(); stop || err != nil {
		return err
	}

	// ... or another galley deployment is already managed the webhook.
	if stop, err := c.processDeployments(); stop || err != nil {
		return err
	}

	desired, err := c.buildValidatingWebhookConfiguration()
	if err != nil {
		// no point in retrying unless a local config or cert file changes.
		return nil
	}

	current, err := c.sharedInformers.Admissionregistration().V1beta1().
		ValidatingWebhookConfigurations().Lister().Get(c.o.WebhookConfigName)
	if kubeErrors.IsNotFound(err) {
		_, err := c.o.Client.AdmissionregistrationV1beta1().
			ValidatingWebhookConfigurations().Create(desired)
		if err != nil {
			return err
		}
		return nil
	}

	updated := current.DeepCopyObject().(*kubeApiAdmission.ValidatingWebhookConfiguration)
	updated.Webhooks = desired.Webhooks
	updated.OwnerReferences = desired.OwnerReferences

	if !reflect.DeepEqual(updated, current) {
		_, err := c.o.Client.AdmissionregistrationV1beta1().
			ValidatingWebhookConfigurations().Update(updated)
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Controller) processNextWorkItem() (cont bool) {
	obj, shutdown := c.queue.Get()
	if shutdown {
		return false
	}
	defer c.queue.Done(obj)

	req, ok := obj.(*reconcileRequest)
	if !ok {
		// don't retry an invalid reconcileRequest item
		c.queue.Forget(req)
		return true
	}

	if err := c.reconcile(req); err != nil {
		c.queue.AddRateLimited(obj)
		utilruntime.HandleError(err)
	} else {
		c.queue.Forget(obj)
	}
	return true
}

func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *Controller) buildValidatingWebhookConfiguration() (*kubeApiAdmission.ValidatingWebhookConfiguration, error) {
	webhook, err := c.readFile(c.o.ConfigPath)
	if err != nil {
		return nil, err
	}
	caBundle, err := c.readFile(c.o.CAPath)
	if err != nil {
		return nil, err
	}
	return buildValidatingWebhookConfiguration(caBundle, webhook, c.ownerRefs)
}
