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
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	kubeApiAdmission "k8s.io/api/admissionregistration/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"

	"istio.io/api/label"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/keycertbundle"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/webhooks/util"
)

var scope = log.RegisterScope("validationController", "validation webhook controller")

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
		errs = multierror.Append(errs, fmt.Errorf("invalid namespace: %q", o.WatchedNamespace))
	}
	if o.ServiceName == "" || !labels.IsDNS1123Label(o.ServiceName) {
		errs = multierror.Append(errs, fmt.Errorf("invalid service name: %q", o.ServiceName))
	}
	if o.CABundleWatcher == nil {
		errs = multierror.Append(errs, errors.New("CA bundle watcher not specified"))
	}
	return errs.ErrorOrNil()
}

// String produces a string field version of the arguments for debugging.
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

	queue                         controllers.Queue
	dryRunOfInvalidConfigRejected bool
	webhooks                      kclient.Client[*kubeApiAdmission.ValidatingWebhookConfiguration]
}

// NewValidatingWebhookController creates a new Controller.
func NewValidatingWebhookController(client kube.Client,
	revision, ns string, caBundleWatcher *keycertbundle.Watcher,
) *Controller {
	o := Options{
		WatchedNamespace: ns,
		CABundleWatcher:  caBundleWatcher,
		Revision:         revision,
		ServiceName:      "istiod",
	}
	return newController(o, client)
}

func newController(o Options, client kube.Client) *Controller {
	c := &Controller{
		o:      o,
		client: client,
	}

	c.queue = controllers.NewQueue("validation",
		controllers.WithReconciler(c.Reconcile),
		// Webhook patching has to be retried forever. But the retries would be rate limited.
		controllers.WithMaxAttempts(math.MaxInt),
		// Retry with backoff. Failures could be from conflicts of other instances (quick retry helps), or
		// longer lasting concerns which will eventually be retried on 1min interval.
		// Unlike the mutating webhook controller, we do not use NewItemFastSlowRateLimiter. This is because
		// the validation controller waits for its own service to be ready, so typically this takes a few seconds
		// before we are ready; using FastSlow means we tend to always take the Slow time (1min).
		controllers.WithRateLimiter(workqueue.NewItemExponentialFailureRateLimiter(100*time.Millisecond, 1*time.Minute)))

	c.webhooks = kclient.NewFiltered[*kubeApiAdmission.ValidatingWebhookConfiguration](client, kclient.Filter{
		LabelSelector: fmt.Sprintf("%s=%s", label.IoIstioRev.Name, o.Revision),
	})
	c.webhooks.AddEventHandler(controllers.ObjectHandler(c.queue.AddObject))

	return c
}

func (c *Controller) Reconcile(key types.NamespacedName) error {
	name := key.Name
	whc := c.webhooks.Get(name, "")
	scope := scope.WithLabels("webhook", name)
	// Stop early if webhook is not present, rather than attempting (and failing) to reconcile permanently
	// If the webhook is later added a new reconciliation request will trigger it to update
	if whc == nil {
		scope.Infof("Skip patching webhook, not found")
		return nil
	}

	scope.Debugf("Reconcile(enter)")
	defer func() { scope.Debugf("Reconcile(exit)") }()

	caBundle, err := util.LoadCABundle(c.o.CABundleWatcher)
	if err != nil {
		scope.Errorf("Failed to load CA bundle: %v", err)
		reportValidationConfigLoadError(err.(*util.ConfigError).Reason())
		// no point in retrying unless cert file changes.
		return nil
	}
	ready := c.readyForFailClose()
	if err := c.updateValidatingWebhookConfiguration(whc, caBundle, ready); err != nil {
		return fmt.Errorf("fail to update webhook: %v", err)
	}
	if !ready {
		return fmt.Errorf("webhook is not ready, retry")
	}
	return nil
}

func (c *Controller) Run(stop <-chan struct{}) {
	kube.WaitForCacheSync("validation", stop, c.webhooks.HasSynced)
	go c.startCaBundleWatcher(stop)
	c.queue.Run(stop)
}

// startCaBundleWatcher listens for updates to the CA bundle and patches the webhooks.
// shouldn't we be doing this for both validating and mutating webhooks...?
func (c *Controller) startCaBundleWatcher(stop <-chan struct{}) {
	if c.o.CABundleWatcher == nil {
		return
	}
	id, watchCh := c.o.CABundleWatcher.AddWatcher()
	defer c.o.CABundleWatcher.RemoveWatcher(id)

	for {
		select {
		case <-watchCh:
			c.syncAll()
		case <-stop:
			return
		}
	}
}

func (c *Controller) readyForFailClose() bool {
	if !c.dryRunOfInvalidConfigRejected {
		if rejected, reason := c.isDryRunOfInvalidConfigRejected(); !rejected {
			scope.Infof("Not ready to switch validation to fail-closed: %v", reason)
			return false
		}
		scope.Info("Endpoint successfully rejected invalid config. Switching to fail-close.")
		c.dryRunOfInvalidConfigRejected = true
		// Sync all webhooks; this ensures if we have multiple webhooks all of them are updated
		c.syncAll()
	}
	return true
}

const (
	deniedRequestMessageFragment     = `denied the request`
	missingResourceMessageFragment   = `the server could not find the requested resource`
	unsupportedDryRunMessageFragment = `does not support dry run`
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
			Annotations: map[string]string{
				// Add always-reject annotation. For now, we are invalid for two reasons: missing `spec.servers`, and this
				// annotation. In the future, the CRD will reject a missing `spec.servers` before we hit the webhook, so we will
				// only have that annotation. For backwards compatibility, we keep both methods for some time.
				constants.AlwaysReject: "true",
			},
		},
		Spec: networking.Gateway{},
	}

	createOptions := metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}}
	istioClient := c.client.Istio().NetworkingV1alpha3()
	_, err := istioClient.Gateways(c.o.WatchedNamespace).Create(context.TODO(), invalidGateway, createOptions)
	if kerrors.IsAlreadyExists(err) {
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
	// If some validating webhooks does not support dryRun(sideEffects=Unknown or Some), we will get this error.
	// We should assume valdiation is ready because there is no point in retrying this request.
	if strings.Contains(err.Error(), unsupportedDryRunMessageFragment) {
		scope.Warnf("One of the validating webhooks does not support DryRun, cannot perform validation check. Assuming validation is ready. Details: %v", err)
		return true, ""
	}
	return false, fmt.Sprintf("dummy invalid rejected for the wrong reason: %v", err)
}

func (c *Controller) updateValidatingWebhookConfiguration(current *kubeApiAdmission.ValidatingWebhookConfiguration,
	caBundle []byte, ready bool,
) error {
	dirty := false
	for i := range current.Webhooks {
		caNeed := !bytes.Equal(current.Webhooks[i].ClientConfig.CABundle, caBundle)
		failureNeed := ready && (current.Webhooks[i].FailurePolicy != nil && *current.Webhooks[i].FailurePolicy != kubeApiAdmission.Fail)
		if caNeed || failureNeed {
			dirty = true
			break
		}
	}
	scope := scope.WithLabels(
		"name", current.Name,
		"fail closed", ready,
		"resource version", current.ResourceVersion,
	)
	if !dirty {
		scope.Infof("up-to-date, no change required")
		return nil
	}
	updated := current.DeepCopy()
	for i := range updated.Webhooks {
		updated.Webhooks[i].ClientConfig.CABundle = caBundle
		if ready {
			updated.Webhooks[i].FailurePolicy = ptr.Of(kubeApiAdmission.Fail)
		}
	}

	latest, err := c.webhooks.Update(updated)
	if err != nil {
		scope.Errorf("failed to updated: %v", err)
		reportValidationConfigUpdateError(kerrors.ReasonForError(err))
		return err
	}

	scope.WithLabels("resource version", latest.ResourceVersion).Infof("successfully updated")
	reportValidationConfigUpdate()
	return nil
}

func (c *Controller) syncAll() {
	for _, whc := range c.webhooks.List("", klabels.Everything()) {
		c.queue.AddObject(whc)
	}
}
