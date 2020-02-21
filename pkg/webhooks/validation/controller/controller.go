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

// Package controller implements a k8s controller for managing the lifecycle of a validating webhook.
package controller

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"reflect"
	"time"

	"github.com/hashicorp/go-multierror"
	kubeApiAdmission "k8s.io/api/admissionregistration/v1beta1"
	kubeApiCore "k8s.io/api/core/v1"
	kubeErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeLabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/runtime/serializer/versioning"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"istio.io/pkg/filewatcher"
	"istio.io/pkg/log"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
)

var scope = log.RegisterScope("validationController", "validation webhook controller", 0)

type Options struct {
	// Istio system namespace in which galley and istiod reside.
	WatchedNamespace string

	// Periodically resync with the kube-apiserver. Set to zero to disable.
	ResyncPeriod time.Duration

	// File path to the x509 certificate bundle used by the webhook server
	// and patched into the webhook config.
	CAPath string

	// Name of the k8s validatingwebhookconfiguration resource. This should
	// match the name in the config template.
	WebhookConfigName string

	// Name of the service running the webhook server.
	ServiceName string

	// If true, the controller will run but actively try to remove the
	// validatingwebhookconfiguration instead of creating it. This is
	// useful in cases where validation was previously enabled and
	// subsequently disabled. The controller can clean up after itself
	// without relying on the user to manually delete configs.
	// Deprecated: istiod webhook controller shouldn't use this.
	UnregisterValidationWebhook bool
}

func DefaultArgs() Options {
	return Options{
		WatchedNamespace:  "istio-system",
		CAPath:            constants.DefaultRootCert,
		WebhookConfigName: "istio-galley",
		ServiceName:       "istio-galley",
	}
}

// Validate the options that exposed to end users
func (o Options) Validate() error {
	var errs *multierror.Error
	if o.WebhookConfigName == "" || !labels.IsDNS1123Label(o.WebhookConfigName) {
		errs = multierror.Append(errs, fmt.Errorf("invalid webhook name: %q", o.WebhookConfigName)) // nolint: lll
	}
	if o.WatchedNamespace == "" || !labels.IsDNS1123Label(o.WatchedNamespace) {
		errs = multierror.Append(errs, fmt.Errorf("invalid namespace: %q", o.WatchedNamespace)) // nolint: lll
	}
	if o.ServiceName == "" || !labels.IsDNS1123Label(o.ServiceName) {
		errs = multierror.Append(errs, fmt.Errorf("invalid service name: %q", o.ServiceName))
	}
	if o.CAPath == "" {
		errs = multierror.Append(errs, errors.New("CA cert file not specified"))
	}
	return errs.ErrorOrNil()
}

// String produces a stringified version of the arguments for debugging.
func (o Options) String() string {
	buf := &bytes.Buffer{}
	_, _ = fmt.Fprintf(buf, "WatchedNamespace: %v\n", o.WatchedNamespace)
	_, _ = fmt.Fprintf(buf, "ResyncPeriod: %v\n", o.ResyncPeriod)
	_, _ = fmt.Fprintf(buf, "CAPath: %v\n", o.CAPath)
	_, _ = fmt.Fprintf(buf, "WebhookConfigName: %v\n", o.WebhookConfigName)
	_, _ = fmt.Fprintf(buf, "ServiceName: %v\n", o.ServiceName)
	_, _ = fmt.Fprintf(buf, "UnregisterValidationWebhook: %v\n", o.UnregisterValidationWebhook)
	return buf.String()
}

type readFileFunc func(filename string) ([]byte, error)

type Controller struct {
	o                 Options
	client            kubernetes.Interface
	queue             workqueue.RateLimitingInterface
	sharedInformers   informers.SharedInformerFactory
	endpointReadyOnce bool
	fw                filewatcher.FileWatcher

	// unittest hooks
	readFile      readFileFunc
	reconcileDone func()
}

type reconcileRequest struct {
	description string
}

func (rr reconcileRequest) String() string {
	return rr.description
}

func filterWatchedObject(obj kubeApiMeta.Object, name string) (skip bool, key string) {
	if name != "" && obj.GetName() != name {
		return true, ""
	}
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		return true, ""
	}
	return false, key
}

func makeHandler(queue workqueue.Interface, gvk schema.GroupVersionKind, name string) *cache.ResourceEventHandlerFuncs {
	return &cache.ResourceEventHandlerFuncs{
		AddFunc: func(curr interface{}) {
			obj, err := meta.Accessor(curr)
			if err != nil {
				return
			}
			skip, key := filterWatchedObject(obj, name)
			scope.Debugf("HandlerAdd: key=%v skip=%v", key, skip)
			if skip {
				return
			}
			req := &reconcileRequest{fmt.Sprintf("add event (%v, Kind=%v) %v", gvk.GroupVersion(), gvk.Kind, key)}
			queue.Add(req)
		},
		UpdateFunc: func(prev, curr interface{}) {
			currObj, err := meta.Accessor(curr)
			if err != nil {
				return
			}
			prevObj, err := meta.Accessor(prev)
			if err != nil {
				return
			}
			if currObj.GetResourceVersion() == prevObj.GetResourceVersion() {
				return
			}
			skip, key := filterWatchedObject(currObj, name)
			scope.Debugf("HandlerUpdate: key=%v skip=%v", key, skip)
			if skip {
				return
			}
			req := &reconcileRequest{fmt.Sprintf("update event (%v, Kind=%v) %v", gvk.GroupVersion(), gvk.Kind, key)}
			queue.Add(req)
		},
		DeleteFunc: func(curr interface{}) {
			if _, ok := curr.(kubeApiMeta.Object); !ok {
				// If the object doesn't have Metadata, assume it is a tombstone object
				// of type DeletedFinalStateUnknown
				tombstone, ok := curr.(cache.DeletedFinalStateUnknown)
				if !ok {
					return
				}
				curr = tombstone.Obj
			}
			currObj, err := meta.Accessor(curr)
			if err != nil {
				return
			}
			skip, key := filterWatchedObject(currObj, name)
			scope.Debugf("HandlerDelete: key=%v skip=%v", key, skip)
			if skip {
				return
			}
			req := &reconcileRequest{fmt.Sprintf("delete event (%v, Kind=%v) %v", gvk.GroupVersion(), gvk.Kind, key)}
			queue.Add(req)
		},
	}
}

// precompute GVK for known types.
var (
	configGVK   = kubeApiAdmission.SchemeGroupVersion.WithKind(reflect.TypeOf(kubeApiAdmission.ValidatingWebhookConfiguration{}).Name())
	endpointGVK = kubeApiCore.SchemeGroupVersion.WithKind(reflect.TypeOf(kubeApiCore.Endpoints{}).Name())
)

func New(o Options, client kubernetes.Interface) (*Controller, error) {
	return newController(o, client, filewatcher.NewWatcher, ioutil.ReadFile, nil)
}

func newController(
	o Options,
	client kubernetes.Interface,
	newFileWatcher filewatcher.NewFileWatcherFunc,
	readFile readFileFunc,
	reconcileDone func(),
) (*Controller, error) {
	caFileWatcher := newFileWatcher()
	if err := caFileWatcher.Add(o.CAPath); err != nil {
		return nil, err
	}

	c := &Controller{
		o:             o,
		client:        client,
		queue:         workqueue.NewRateLimitingQueue(workqueue.DefaultItemBasedRateLimiter()),
		fw:            caFileWatcher,
		readFile:      readFile,
		reconcileDone: reconcileDone,
	}

	c.sharedInformers = informers.NewSharedInformerFactoryWithOptions(client, o.ResyncPeriod,
		informers.WithNamespace(o.WatchedNamespace))

	webhookInformer := c.sharedInformers.Admissionregistration().V1beta1().ValidatingWebhookConfigurations().Informer()
	webhookInformer.AddEventHandler(makeHandler(c.queue, configGVK, o.WebhookConfigName))

	endpointInformer := c.sharedInformers.Core().V1().Endpoints().Informer()
	endpointInformer.AddEventHandler(makeHandler(c.queue, endpointGVK, o.ServiceName))

	return c, nil
}

func (c *Controller) Start(stop <-chan struct{}) {
	go c.startFileWatcher(stop)
	go c.sharedInformers.Start(stop)

	for _, ready := range c.sharedInformers.WaitForCacheSync(stop) {
		if !ready {
			return
		}
	}

	req := &reconcileRequest{"initial request to kickstart reconciliation"}
	c.queue.Add(req)

	go c.runWorker()
}

func (c *Controller) startFileWatcher(stop <-chan struct{}) {
	for {
		select {
		case ev := <-c.fw.Events(c.o.CAPath):
			req := &reconcileRequest{fmt.Sprintf("CA file changed: %v", ev)}
			c.queue.Add(req)
		case err := <-c.fw.Errors(c.o.CAPath):
			scope.Warnf("error watching local CA bundle: %v", err)
		case <-stop:
			return
		}
	}
}

func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
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

	if err := c.reconcileRequest(req); err != nil {
		c.queue.AddRateLimited(obj)
		utilruntime.HandleError(err)
	} else {
		c.queue.Forget(obj)
	}
	return true
}

// reconcile the desired state with the kube-apiserver.
func (c *Controller) reconcileRequest(req *reconcileRequest) error {
	defer func() {
		if c.reconcileDone != nil {
			c.reconcileDone()
		}
	}()

	scope.Infof("Reconcile(enter): %v", req)
	defer func() { scope.Debugf("Reconcile(exit)") }()

	// don't create the webhook config before the endpoint is ready
	if !c.endpointReadyOnce {
		ready, reason, err := c.isEndpointReady()
		if err != nil {
			scope.Errorf("Error checking endpoint readiness: %v", err)
			return err
		}
		if !ready {
			scope.Infof("Endpoint %v is not ready: %v", c.o.ServiceName, reason)
			return nil
		}
		c.endpointReadyOnce = true
	}

	// actively remove the webhook configuration if the controller is running but the webhook
	if c.o.UnregisterValidationWebhook {
		return c.deleteValidatingWebhookConfiguration()
	}

	caBundle, err := c.loadCABundle()
	if err != nil {
		scope.Errorf("Failed to load CA bundle: %v", err)
		reportValidationConfigLoadError(err.(*configError).Reason())
		// no point in retrying unless cert file changes.
		return nil
	}

	return c.updateValidatingWebhookConfiguration(caBundle)
}

func (c *Controller) isEndpointReady() (ready bool, reason string, err error) {
	endpoint, err := c.sharedInformers.Core().V1().
		Endpoints().Lister().Endpoints(c.o.WatchedNamespace).Get(c.o.ServiceName)
	if err != nil {
		if kubeErrors.IsNotFound(err) {
			return false, "resource not found", nil
		}
		return false, fmt.Sprintf("error getting resource: %v", err), err
	}
	ready, reason = isEndpointReady(endpoint)
	return ready, reason, nil
}

func isEndpointReady(endpoint *kubeApiCore.Endpoints) (ready bool, reason string) {
	if len(endpoint.Subsets) == 0 {
		return false, "no subsets"
	}
	for _, subset := range endpoint.Subsets {
		if len(subset.Addresses) > 0 {
			return true, ""
		}
	}
	return false, "no subset addresses ready"
}

func (c *Controller) galleyPodsRunning() (running bool, err error) {
	selector := kubeLabels.SelectorFromSet(map[string]string{"istio": "galley"})
	pods, err := c.sharedInformers.Core().V1().Pods().Lister().List(selector)
	if err != nil {
		return true, err
	}
	if len(pods) > 0 {
		return true, nil
	}
	return false, nil
}

func (c *Controller) deleteValidatingWebhookConfiguration() error {
	err := c.client.AdmissionregistrationV1beta1().
		ValidatingWebhookConfigurations().Delete(c.o.WebhookConfigName, &kubeApiMeta.DeleteOptions{})
	if err != nil {
		scope.Errorf("Failed to delete validatingwebhookconfiguration: %v", err)
		reportValidationConfigDeleteError(kubeErrors.ReasonForError(err))
		return err
	}
	scope.Info("Successfully deleted validatingwebhookconfiguration")
	return nil
}

func (c *Controller) updateValidatingWebhookConfiguration(caBundle []byte) error {
	current, err := c.sharedInformers.Admissionregistration().V1beta1().
		ValidatingWebhookConfigurations().Lister().Get(c.o.WebhookConfigName)

	if err != nil {
		if kubeErrors.IsNotFound(err) {
			scope.Warnf("validatingwebhookconfiguration %v not found: %v",
				c.o.WebhookConfigName, err)
			reportValidationConfigUpdateError(kubeErrors.ReasonForError(err))
			return nil
		}

		scope.Warnf("Failed to get validatingwebhookconfiguration %v: %v",
			c.o.WebhookConfigName, err)
		reportValidationConfigUpdateError(kubeErrors.ReasonForError(err))
	}

	updated := current.DeepCopyObject().(*kubeApiAdmission.ValidatingWebhookConfiguration)

	for i := range updated.Webhooks {
		updated.Webhooks[i].ClientConfig.CABundle = caBundle
		updated.Webhooks[i].FailurePolicy = &defaultFailurePolicy
	}

	if !reflect.DeepEqual(updated, current) {
		latest, err := c.client.AdmissionregistrationV1beta1().
			ValidatingWebhookConfigurations().Update(updated)
		if err != nil {
			scope.Errorf("Failed to update validatingwebhookconfiguration %v (resourceVersion=%v): %v",
				c.o.WebhookConfigName, updated.ResourceVersion, err)
			reportValidationConfigUpdateError(kubeErrors.ReasonForError(err))
			return err
		}

		scope.Infof("Successfully updated validatingwebhookconfiguration %v (resourceVersion=%v)",
			c.o.WebhookConfigName, latest.ResourceVersion)
		reportValidationConfigUpdate()
		return nil
	}

	scope.Infof("validatingwebhookconfiguration %v (resourceVersion=%v) is up-to-date. No change required.",
		c.o.WebhookConfigName, current.ResourceVersion)

	return nil
}

type configError struct {
	err    error
	reason string
}

func (e configError) Error() string {
	return e.err.Error()
}

func (e configError) Reason() string {
	return e.reason
}

var (
	codec  runtime.Codec
	scheme *runtime.Scheme

	// defaults per k8s spec
	defaultFailurePolicy = kubeApiAdmission.Fail
	FailurePolicyIgnore  = kubeApiAdmission.Ignore
)

func init() {
	scheme = runtime.NewScheme()
	utilruntime.Must(kubeApiAdmission.AddToScheme(scheme))
	opt := json.SerializerOptions{Yaml: true}
	yamlSerializer := json.NewSerializerWithOptions(json.DefaultMetaFactory, scheme, scheme, opt)
	codec = versioning.NewDefaultingCodecForScheme(
		scheme,
		yamlSerializer,
		yamlSerializer,
		kubeApiAdmission.SchemeGroupVersion,
		runtime.InternalGroupVersioner,
	)
}

func (c *Controller) loadCABundle() ([]byte, error) {
	caBundle, err := c.readFile(c.o.CAPath)
	if err != nil {
		return nil, &configError{err, "could not read caBundle file"}
	}

	if err := verifyCABundle(caBundle); err != nil {
		return nil, &configError{err, "could not verify caBundle"}
	}

	return caBundle, nil
}

func verifyCABundle(caBundle []byte) error {
	block, _ := pem.Decode(caBundle)
	if block == nil {
		return errors.New("could not decode pem")
	}
	if block.Type != "CERTIFICATE" {
		return fmt.Errorf("cert contains wrong pem type: %q", block.Type)
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		return fmt.Errorf("cert contains invalid x509 certificate: %v", err)
	}
	return nil
}
