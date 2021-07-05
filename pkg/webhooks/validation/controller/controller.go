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

// Package controller implements a k8s controller for managing the lifecycle of a validating webhook.
package controller

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	kubeApiAdmission "k8s.io/api/admissionregistration/v1"
	kubeErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/runtime/serializer/versioning"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"istio.io/api/label"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/keycertbundle"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

var scope = log.RegisterScope("validationController", "validation webhook controller", 0)

type Options struct {
	// Istio system namespace where istiod resides.
	WatchedNamespace string

	// File path to the x509 certificate bundle used by the webhook server
	// and patched into the webhook config.
	CABundleWatcher *keycertbundle.Watcher

	// Revision for control plane performing patching on the validating webhook.
	Revision string

	// Name of the service running the webhook server.
	ServiceName string
}

// Validate the options that exposed to end users
func (o Options) Validate() error {
	var errs *multierror.Error
	if o.WatchedNamespace == "" || !labels.IsDNS1123Label(o.WatchedNamespace) {
		errs = multierror.Append(errs, fmt.Errorf("invalid namespace: %q", o.WatchedNamespace)) // nolint: lll
	}
	if o.ServiceName == "" || !labels.IsDNS1123Label(o.ServiceName) {
		errs = multierror.Append(errs, fmt.Errorf("invalid service name: %q", o.ServiceName))
	}
	if o.CABundleWatcher == nil {
		errs = multierror.Append(errs, errors.New("CA bundle watcher not specified"))
	}
	return errs.ErrorOrNil()
}

// String produces a stringified version of the arguments for debugging.
func (o Options) String() string {
	buf := &bytes.Buffer{}
	_, _ = fmt.Fprintf(buf, "WatchedNamespace: %v\n", o.WatchedNamespace)
	_, _ = fmt.Fprintf(buf, "Revision: %v\n", o.Revision)
	_, _ = fmt.Fprintf(buf, "ServiceName: %v\n", o.ServiceName)
	return buf.String()
}

type Controller struct {
	o      Options
	client kube.Client

	webhookInformer               cache.SharedInformer
	webhookName                   string
	queue                         workqueue.RateLimitingInterface
	dryRunOfInvalidConfigRejected bool
}

// NewValidatingWebhookController creates a new Controller.
func NewValidatingWebhookController(client kube.Client,
	revision, ns string, caBundleWatcher *keycertbundle.Watcher) *Controller {
	o := Options{
		WatchedNamespace: ns,
		CABundleWatcher:  caBundleWatcher,
		Revision:         revision,
		ServiceName:      "istiod",
	}
	return newController(o, client)
}

type eventType string

const (
	retryEvent  eventType = "retryEvent"
	updateEvent eventType = "updateEvent"
)

type reconcileRequest struct {
	event       eventType
	description string
}

func (rr reconcileRequest) String() string {
	return fmt.Sprintf("[description] %s, [eventType] %s", rr.description, rr.event)
}

func filterWatchedObject(obj metav1.Object) (skip bool, key string) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		return true, ""
	}
	return false, key
}

func makeHandler(queue workqueue.Interface, gvk schema.GroupVersionKind) *cache.ResourceEventHandlerFuncs {
	return &cache.ResourceEventHandlerFuncs{
		AddFunc: func(curr interface{}) {
			obj, err := meta.Accessor(curr)
			if err != nil {
				return
			}
			skip, key := filterWatchedObject(obj)
			scope.Debugf("HandlerAdd: key=%v skip=%v", key, skip)
			if skip {
				return
			}
			req := &reconcileRequest{
				event:       updateEvent,
				description: fmt.Sprintf("add event (%v, Kind=%v) %v", gvk.GroupVersion(), gvk.Kind, key),
			}
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
			skip, key := filterWatchedObject(currObj)
			scope.Debugf("HandlerUpdate: key=%v skip=%v", key, skip)
			if skip {
				return
			}
			req := &reconcileRequest{
				event:       updateEvent,
				description: fmt.Sprintf("update event (%v, Kind=%v) %v", gvk.GroupVersion(), gvk.Kind, key),
			}
			queue.Add(req)
		},
		DeleteFunc: func(curr interface{}) {
			if _, ok := curr.(metav1.Object); !ok {
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
			skip, key := filterWatchedObject(currObj)
			scope.Debugf("HandlerDelete: key=%v skip=%v", key, skip)
			if skip {
				return
			}
			req := &reconcileRequest{
				event:       updateEvent,
				description: fmt.Sprintf("delete event (%v, Kind=%v) %v", gvk.GroupVersion(), gvk.Kind, key),
			}
			queue.Add(req)
		},
	}
}

// precompute GVK for known types.
var (
	configGVK = kubeApiAdmission.SchemeGroupVersion.WithKind(reflect.TypeOf(kubeApiAdmission.ValidatingWebhookConfiguration{}).Name())
)

func newController(
	o Options,
	client kube.Client,
) *Controller {
	c := &Controller{
		o:           o,
		client:      client,
		queue:       workqueue.NewRateLimitingQueue(workqueue.NewItemExponentialFailureRateLimiter(1*time.Second, 1*time.Minute)),
		webhookName: o.validatingWebhookName(),
	}

	webhookInformer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
				opts.LabelSelector = fmt.Sprintf("%s=%s", label.IoIstioRev.Name, o.Revision)
				return client.AdmissionregistrationV1().ValidatingWebhookConfigurations().List(context.TODO(), opts)
			},
			WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
				opts.LabelSelector = fmt.Sprintf("%s=%s", label.IoIstioRev.Name, o.Revision)
				return client.AdmissionregistrationV1().ValidatingWebhookConfigurations().Watch(context.TODO(), opts)
			},
		},
		&kubeApiAdmission.ValidatingWebhookConfiguration{}, 0, cache.Indexers{},
	)
	webhookInformer.AddEventHandler(makeHandler(c.queue, configGVK))
	c.webhookInformer = webhookInformer

	return c
}

func (c *Controller) Run(stop <-chan struct{}) {
	defer c.queue.ShutDown()
	go c.webhookInformer.Run(stop)
	if !cache.WaitForCacheSync(stop, c.webhookInformer.HasSynced) {
		return
	}
	go c.startCaBundleWatcher(stop)
	go c.runWorker()
	<-stop
}

// startCaBundleWatcher listens for updates to the CA bundle and patches the webhooks.
// shouldn't we be doing this for both validating and mutating webhooks...?
func (c *Controller) startCaBundleWatcher(stop <-chan struct{}) {
	watchCh := c.o.CABundleWatcher.AddWatcher()
	for {
		select {
		case <-watchCh:
			c.queue.AddRateLimited(&reconcileRequest{
				updateEvent,
				"CA bundle update",
			})
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

	if retry, err := c.reconcileRequest(req); retry || err != nil {
		c.queue.AddRateLimited(retryRequest)
		utilruntime.HandleError(err)
	} else {
		c.queue.Forget(obj)
	}
	return true
}

var retryRequest = &reconcileRequest{
	event:       retryEvent,
	description: "retry reconcile request",
}

// reconcile the desired state with the kube-apiserver.
// the returned results indicate if the reconciliation should be retried and/or
// if there was an error.
func (c *Controller) reconcileRequest(req *reconcileRequest) (bool, error) {
	// Stop early if webhook is not present, rather than attempting (and failing) to reconcile permanently
	// If the webhook is later added a new reconciliation request will trigger it to update
	configuration, err := c.client.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(context.Background(), c.webhookName, metav1.GetOptions{})
	if err != nil {
		if kubeErrors.IsNotFound(err) {
			scope.Infof("Skip patching webhook, webhook %q not found", c.webhookName)
			return false, nil
		}
		return false, err
	}

	scope.Infof("Reconcile(enter): %v", req)
	defer func() { scope.Debugf("Reconcile(exit)") }()

	caBundle, err := c.loadCABundle()
	if err != nil {
		scope.Errorf("Failed to load CA bundle: %v", err)
		reportValidationConfigLoadError(err.(*configError).Reason())
		// no point in retrying unless cert file changes.
		return false, nil
	}
	failurePolicy := kubeApiAdmission.Ignore
	ready := c.readyForFailClose()
	if ready {
		failurePolicy = kubeApiAdmission.Fail
	}
	return !ready, c.updateValidatingWebhookConfiguration(configuration, caBundle, failurePolicy)
}

func (c *Controller) readyForFailClose() bool {
	if !c.dryRunOfInvalidConfigRejected {
		if rejected, reason := c.isDryRunOfInvalidConfigRejected(); !rejected {
			scope.Infof("Not ready to switch validation to fail-closed: %v", reason)
			return false
		}
		scope.Info("Endpoint successfully rejected invalid config. Switching to fail-close.")
		c.dryRunOfInvalidConfigRejected = true
	}
	return true
}

const (
	deniedRequestMessageFragment   = `denied the request`
	missingResourceMessageFragment = `the server could not find the requested resource`
)

// Confirm invalid configuration is successfully rejected before switching to FAIL-CLOSE.
func (c *Controller) isDryRunOfInvalidConfigRejected() (rejected bool, reason string) {
	invalidGateway := &v1alpha3.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "invalid-gateway",
			Namespace: c.o.WatchedNamespace,
			// Must ensure that this is the revision validating the known-bad config
			Labels: map[string]string{
				label.IoIstioRev.Name: c.o.Revision,
			},
		},
		Spec: networking.Gateway{},
	}

	createOptions := metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}}
	istioClient := c.client.Istio().NetworkingV1alpha3()
	_, err := istioClient.Gateways(c.o.WatchedNamespace).Create(context.TODO(), invalidGateway, createOptions)
	if kubeErrors.IsAlreadyExists(err) {
		updateOptions := metav1.UpdateOptions{DryRun: []string{metav1.DryRunAll}}
		_, err = istioClient.Gateways(c.o.WatchedNamespace).Update(context.TODO(), invalidGateway, updateOptions)
	}
	if err == nil {
		return false, "dummy invalid config not rejected"
	}
	// We expect to get deniedRequestMessageFragment (the config was rejected, as expected)
	if strings.Contains(err.Error(), deniedRequestMessageFragment) {
		return true, ""
	}
	// If the CRD does not exist, we will get this error. This is to handle when Pilot is run
	// without CRDs - in this case, this check will not be possible.
	if strings.Contains(err.Error(), missingResourceMessageFragment) {
		scope.Warnf("Missing Gateway CRD, cannot perform validation check. Assuming validation is ready")
		return true, ""
	}
	return false, fmt.Sprintf("dummy invalid rejected for the wrong reason: %v", err)
}

func (c *Controller) updateValidatingWebhookConfiguration(current *kubeApiAdmission.ValidatingWebhookConfiguration,
	caBundle []byte, failurePolicy kubeApiAdmission.FailurePolicyType) error {
	updated := current.DeepCopy()
	for i := range updated.Webhooks {
		updated.Webhooks[i].ClientConfig.CABundle = caBundle
		updated.Webhooks[i].FailurePolicy = &failurePolicy
	}

	if !reflect.DeepEqual(updated, current) {
		latest, err := c.client.AdmissionregistrationV1().
			ValidatingWebhookConfigurations().Update(context.TODO(), updated, metav1.UpdateOptions{})
		if err != nil {
			scope.Errorf("Failed to update validatingwebhookconfiguration %v (failurePolicy=%v, resourceVersion=%v): %v",
				updated.Name, failurePolicy, updated.ResourceVersion, err)
			reportValidationConfigUpdateError(kubeErrors.ReasonForError(err))
			return err
		}

		scope.Infof("Successfully updated validatingwebhookconfiguration %v (failurePolicy=%v,resourceVersion=%v)",
			updated.Name, failurePolicy, latest.ResourceVersion)
		reportValidationConfigUpdate()
		return nil
	}

	scope.Infof("validatingwebhookconfiguration %v (failurePolicy=%v, resourceVersion=%v) is up-to-date. No change required.",
		current.Name, failurePolicy, current.ResourceVersion)

	return nil
}

func (o *Options) validatingWebhookName() string {
	name := "istio-validator"
	if o.Revision != "default" {
		name = fmt.Sprintf("%s-%s", name, o.Revision)
	}
	name = fmt.Sprintf("%s-%s", name, o.WatchedNamespace)
	return name
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
	caBundle := c.o.CABundleWatcher.GetCABundle()
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
