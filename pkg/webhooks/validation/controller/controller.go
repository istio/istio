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
	kubeApiApp "k8s.io/api/apps/v1"
	kubeApiCore "k8s.io/api/core/v1"
	kubeApiRbac "k8s.io/api/rbac/v1"
	kubeErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	// File path to the validatingwebhookconfiguration template.
	WebhookConfigPath string

	// Name of the service running the webhook server.
	ServiceName string

	// When non-empty the controller will defer reconciling config
	// until the named deployment no longer exists.
	DeferToDeploymentName string

	// Name of the ClusterRole that the controller should assign
	// cluster-scoped ownership to. The webhook config will be GC'd
	// when this ClusterRole is deleted.
	ClusterRoleName string

	// If true, the controller will run but actively try to remove the
	// validatingwebhookconfiguration instead of creating it. This is
	// useful in cases where validation was previously enabled and
	// subsequently disabled. The controller can clean up after itself
	// without relying on the user to manually delete configs.
	UnregisterValidationWebhook bool
}

func DefaultArgs() Options {
	return Options{
		WatchedNamespace:  "istio-system",
		CAPath:            constants.DefaultRootCert,
		WebhookConfigName: "istio-galley",
		WebhookConfigPath: "/etc/config/validatingwebhookconfiguration.yaml",
		ServiceName:       "istio-galley",
		ClusterRoleName:   "istio-galley-istio-system",
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
	if o.DeferToDeploymentName != "" && !labels.IsDNS1123Label(o.DeferToDeploymentName) {
		errs = multierror.Append(errs, fmt.Errorf("invalid deployment name: %q", o.DeferToDeploymentName))
	}
	if o.ServiceName == "" || !labels.IsDNS1123Label(o.ServiceName) {
		errs = multierror.Append(errs, fmt.Errorf("invalid service name: %q", o.ServiceName))
	}
	if o.CAPath == "" {
		errs = multierror.Append(errs, errors.New("CA cert file not specified"))
	}
	if o.WebhookConfigPath == "" {
		errs = multierror.Append(errs, errors.New("webhook config file not specified"))
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
	_, _ = fmt.Fprintf(buf, "WebhookConfigPath: %v\n", o.WebhookConfigPath)
	_, _ = fmt.Fprintf(buf, "ServiceName: %v\n", o.ServiceName)
	_, _ = fmt.Fprintf(buf, "DeferToDeploymentName: %v\n", o.DeferToDeploymentName)
	_, _ = fmt.Fprintf(buf, "ClusterRoleName: %v\n", o.ClusterRoleName)
	_, _ = fmt.Fprintf(buf, "UnregisterValidationWebhook: %v\n", o.UnregisterValidationWebhook)
	return buf.String()
}

type readFileFunc func(filename string) ([]byte, error)

type Controller struct {
	o                 Options
	client            kubernetes.Interface
	ownerRefs         []kubeApiMeta.OwnerReference
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

func filterWatchedObject(in interface{}, name string) (skip bool, key string) {
	obj, err := meta.Accessor(in)
	if err != nil {
		return true, ""
	}
	if obj.GetName() != name {
		return true, ""
	}
	key, err = cache.DeletionHandlingMetaNamespaceKeyFunc(in)
	if err != nil {
		return true, ""
	}
	return false, key
}

func makeHandler(queue workqueue.Interface, gvk schema.GroupVersionKind, name string) *cache.ResourceEventHandlerFuncs {
	return &cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			skip, key := filterWatchedObject(obj, name)
			scope.Debugf("HandlerAdd: key=%v skip=%v", key, skip)
			if skip {
				return
			}
			req := &reconcileRequest{fmt.Sprintf("add event (%v, Kind=%v) %v", gvk.GroupVersion(), gvk.Kind, key)}
			queue.Add(req)
		},
		UpdateFunc: func(prev, curr interface{}) {
			skip, key := filterWatchedObject(curr, name)
			scope.Debugf("HandlerUpdate: key=%v skip=%v", key, skip)
			if skip {
				return
			}
			if !reflect.DeepEqual(prev, curr) {
				req := &reconcileRequest{fmt.Sprintf("update event (%v, Kind=%v) %v", gvk.GroupVersion(), gvk.Kind, key)}
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
			skip, key := filterWatchedObject(obj, name)
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
	configGVK     = kubeApiAdmission.SchemeGroupVersion.WithKind(reflect.TypeOf(kubeApiAdmission.ValidatingWebhookConfiguration{}).Name())
	endpointGVK   = kubeApiCore.SchemeGroupVersion.WithKind(reflect.TypeOf(kubeApiCore.Endpoints{}).Name())
	deploymentGVK = kubeApiApp.SchemeGroupVersion.WithKind(reflect.TypeOf(kubeApiApp.Deployment{}).Name())
)

func findClusterRoleOwnerRefs(client kubernetes.Interface, clusterRoleName string) []kubeApiMeta.OwnerReference {
	if clusterRoleName == "" {
		scope.Warn("No clusterrole specified to set ownerRef. " +
			"The webhook configuration must be deleted manually.")
		return nil
	}

	clusterRole, err := client.RbacV1().ClusterRoles().Get(clusterRoleName, kubeApiMeta.GetOptions{})
	if err != nil {
		scope.Warnf("Could not find clusterrole: %s to set ownerRef. "+
			"The webhook configuration must be deleted manually.",
			clusterRoleName)
		return nil
	}

	return []kubeApiMeta.OwnerReference{
		*kubeApiMeta.NewControllerRef(
			clusterRole,
			kubeApiRbac.SchemeGroupVersion.WithKind("ClusterRole"),
		),
	}
}

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
	if err := caFileWatcher.Add(o.WebhookConfigPath); err != nil {
		return nil, err
	}
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
		ownerRefs:     findClusterRoleOwnerRefs(client, o.ClusterRoleName),
	}

	c.sharedInformers = informers.NewSharedInformerFactoryWithOptions(client, o.ResyncPeriod,
		informers.WithNamespace(o.WatchedNamespace))

	webhookInformer := c.sharedInformers.Admissionregistration().V1beta1().ValidatingWebhookConfigurations().Informer()
	webhookInformer.AddEventHandler(makeHandler(c.queue, configGVK, o.WebhookConfigName))

	endpointInformer := c.sharedInformers.Core().V1().Endpoints().Informer()
	endpointInformer.AddEventHandler(makeHandler(c.queue, endpointGVK, o.ServiceName))

	if o.DeferToDeploymentName != "" {
		log.Infof("Deferring reconciliation to controller %v when present", o.DeferToDeploymentName)
		deploymentInformer := c.sharedInformers.Apps().V1().Deployments().Informer()
		deploymentInformer.AddEventHandler(makeHandler(c.queue, deploymentGVK, o.DeferToDeploymentName))
	}

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
		case ev := <-c.fw.Events(c.o.WebhookConfigPath):
			req := &reconcileRequest{fmt.Sprintf(
				"validatingwebhookconfiguration file changed: %v", ev)}
			c.queue.Add(req)
		case ev := <-c.fw.Events(c.o.CAPath):
			req := &reconcileRequest{fmt.Sprintf("CA file changed: %v", ev)}
			c.queue.Add(req)
		case err := <-c.fw.Errors(c.o.WebhookConfigPath):
			scope.Warnf("error watching local validatingwebhookconfiguration file: %v", err)
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
			scope.Infof("Endpoint not ready: %v", reason)
			return nil
		}
		c.endpointReadyOnce = true
	}

	// don't update the webhook config if its already managed by an existing galley deployment.
	if c.o.DeferToDeploymentName != "" {
		running, err := c.isGalleyDeploymentRunning()
		if err != nil {
			scope.Errorf("Error checking galley deployment: %v", err)
			return err
		}
		if running {
			scope.Info("Galley deployment detected")
			return nil
		}
	}

	// actively remove the webhook configuration if the controller is running but the webhook
	if c.o.UnregisterValidationWebhook {
		return c.deleteValidatingWebhookConfiguration()
	}

	desired, err := c.buildValidatingWebhookConfiguration()
	if err != nil {
		scope.Errorf("Failed to build validatingwebhookconfiguration: %v", err)
		reportValidationConfigLoadError(err.(*configError).Reason())
		// no point in retrying unless a local config or cert file changes.
		return nil
	}

	return c.updateValidatingWebhookConfiguration(desired)
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

func (c *Controller) isGalleyDeploymentRunning() (running bool, err error) {
	galley, err := c.sharedInformers.Apps().V1().
		Deployments().Lister().Deployments(c.o.WatchedNamespace).Get(c.o.DeferToDeploymentName)

	// galley does/doesn't exist
	if err != nil {
		if kubeErrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	// galley is scaled down to zero replicas. This is useful for debugging
	// to force the istiod controller to run.
	if galley.Spec.Replicas == nil || *galley.Spec.Replicas == 0 {
		return false, nil
	}

	return true, nil
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

func (c *Controller) updateValidatingWebhookConfiguration(desired *kubeApiAdmission.ValidatingWebhookConfiguration) error {
	current, err := c.sharedInformers.Admissionregistration().V1beta1().
		ValidatingWebhookConfigurations().Lister().Get(c.o.WebhookConfigName)

	if kubeErrors.IsNotFound(err) {
		created, err := c.client.AdmissionregistrationV1beta1().
			ValidatingWebhookConfigurations().Create(desired)
		if err != nil {
			scope.Errorf("Failed to create validatingwebhookconfiguration %v: %v",
				c.o.WebhookConfigName, err)
			reportValidationConfigUpdateError(kubeErrors.ReasonForError(err))
			return err
		}
		scope.Infof("Successfully created validatingwebhookconfiguration %v (resourceVersion=%v)",
			c.o.WebhookConfigName, created.ResourceVersion)
		reportValidationConfigUpdate()
		return nil
	}

	updated := current.DeepCopyObject().(*kubeApiAdmission.ValidatingWebhookConfiguration)
	updated.Webhooks = desired.Webhooks
	updated.OwnerReferences = desired.OwnerReferences

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
	defaultFailurePolicy     = kubeApiAdmission.Fail
	defaultSideEffects       = kubeApiAdmission.SideEffectClassUnknown
	defaultTimeout           = int32(30)
	defaultNamespaceSelector = &kubeApiMeta.LabelSelector{}
	defaultObjectSelector    = &kubeApiMeta.LabelSelector{}
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

func (c *Controller) buildValidatingWebhookConfiguration() (*kubeApiAdmission.ValidatingWebhookConfiguration, error) {
	webhook, err := c.readFile(c.o.WebhookConfigPath)
	if err != nil {
		return nil, &configError{err, "could not read validatingwebhookconfiguration file"}
	}
	caBundle, err := c.readFile(c.o.CAPath)
	if err != nil {
		return nil, &configError{err, "could not read caBundle file"}
	}

	var config kubeApiAdmission.ValidatingWebhookConfiguration
	if _, _, err := codec.Decode(webhook, nil, &config); err != nil {
		return nil, &configError{err, "could not decode validatingwebhookconfiguration file"}
	}
	if err := verifyCABundle(caBundle); err != nil {
		return nil, &configError{err, "could not verify caBundle"}
	}

	// update runtime fields
	config.OwnerReferences = c.ownerRefs
	for i := range config.Webhooks {
		config.Webhooks[i].ClientConfig.CABundle = caBundle
	}

	applyDefaultsAndOverrides(&config, c.o.WebhookConfigName)

	return &config, nil
}

func applyDefaultsAndOverrides(in *kubeApiAdmission.ValidatingWebhookConfiguration, webhookConfigName string) {
	// Override the resource name from the file with the name we're watching and
	// updating. This mirrors the behavior of Galley pre-1.5.
	in.Name = webhookConfigName

	// Fill in missing defaults to minimize desired vs. actual diffs later.
	for i := 0; i < len(in.Webhooks); i++ {
		wh := &in.Webhooks[i]
		if wh.FailurePolicy == nil {
			wh.FailurePolicy = &defaultFailurePolicy
		}
		if wh.NamespaceSelector == nil {
			wh.NamespaceSelector = defaultNamespaceSelector
		}
		if wh.ObjectSelector == nil {
			wh.ObjectSelector = defaultObjectSelector
		}
		if wh.SideEffects == nil {
			wh.SideEffects = &defaultSideEffects
		}
		if wh.TimeoutSeconds == nil {
			wh.TimeoutSeconds = &defaultTimeout
		}
	}
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
