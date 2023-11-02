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

package webhooks

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"time"

	v1 "k8s.io/api/admissionregistration/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/keycertbundle"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/webhooks/util"
)

var (
	errWrongRevision     = errors.New("webhook does not belong to target revision")
	errNotFound          = errors.New("webhook not found")
	errNoWebhookWithName = errors.New("webhook configuration did not contain webhook with target name")
)

// WebhookCertPatcher listens for webhooks on specified revision and patches their CA bundles
type WebhookCertPatcher struct {
	// revision to patch webhooks for
	revision    string
	webhookName string

	queue controllers.Queue

	// File path to the x509 certificate bundle used by the webhook server
	// and patched into the webhook config.
	CABundleWatcher *keycertbundle.Watcher

	webhooks kclient.Client[*v1.MutatingWebhookConfiguration]
}

// NewWebhookCertPatcher creates a WebhookCertPatcher
func NewWebhookCertPatcher(
	client kubelib.Client,
	revision, webhookName string, caBundleWatcher *keycertbundle.Watcher,
) (*WebhookCertPatcher, error) {
	p := &WebhookCertPatcher{
		revision:        revision,
		webhookName:     webhookName,
		CABundleWatcher: caBundleWatcher,
	}
	p.queue = newWebhookPatcherQueue(p.webhookPatchTask)

	p.webhooks = kclient.New[*v1.MutatingWebhookConfiguration](client)
	p.webhooks.AddEventHandler(controllers.ObjectHandler(p.queue.AddObject))

	return p, nil
}

func newWebhookPatcherQueue(reconciler controllers.ReconcilerFn) controllers.Queue {
	return controllers.NewQueue("webhook patcher",
		controllers.WithReconciler(reconciler),
		// Try first few(5) retries quickly so that we can detect true conflicts by multiple Istiod instances fast.
		// If there is a conflict beyond this, it means Istiods are seeing different ca certs and are in inconsistent
		// state for longer duration. Slowdown the retries, so that we do not overload kube api server and etcd.
		controllers.WithRateLimiter(workqueue.NewItemFastSlowRateLimiter(100*time.Millisecond, 1*time.Minute, 5)),
		// Webhook patching has to be retried forever. But the retries would be rate limited.
		controllers.WithMaxAttempts(math.MaxInt))
}

// Run runs the WebhookCertPatcher
func (w *WebhookCertPatcher) Run(stopChan <-chan struct{}) {
	go w.startCaBundleWatcher(stopChan)
	w.webhooks.Start(stopChan)
	kubelib.WaitForCacheSync("webhook patcher", stopChan, w.webhooks.HasSynced)
	w.queue.Run(stopChan)
}

func (w *WebhookCertPatcher) HasSynced() bool {
	return w.queue.HasSynced()
}

// webhookPatchTask takes the result of patchMutatingWebhookConfig and modifies the result for use in task queue
func (w *WebhookCertPatcher) webhookPatchTask(o types.NamespacedName) error {
	err := w.patchMutatingWebhookConfig(o.Name)

	// do not want to retry the task if these errors occur, they indicate that
	// we should no longer be patching the given webhook
	if kerrors.IsNotFound(err) || errors.Is(err, errWrongRevision) || errors.Is(err, errNoWebhookWithName) || errors.Is(err, errNotFound) {
		return nil
	}

	if err != nil {
		log.Errorf("patching webhook %s failed: %v", o.Name, err)
		reportWebhookPatchRetry(o.Name)
	}

	return err
}

// patchMutatingWebhookConfig takes a webhookConfigName and patches the CA bundle for that webhook configuration
func (w *WebhookCertPatcher) patchMutatingWebhookConfig(webhookConfigName string) error {
	config := w.webhooks.Get(webhookConfigName, "")
	if config == nil {
		reportWebhookPatchFailure(webhookConfigName, reasonWebhookConfigNotFound)
		return errNotFound
	}
	// prevents a race condition between multiple istiods when the revision is changed or modified
	v, ok := config.Labels[label.IoIstioRev.Name]
	if !ok {
		log.Debugf("webhook config %q does not have revision label. It is not a Istio webhook. Skipping patching", webhookConfigName)
		return nil
	}
	if v != w.revision {
		reportWebhookPatchFailure(webhookConfigName, reasonWrongRevision)
		return errWrongRevision
	}

	found := false
	updated := false
	caCertPem, err := util.LoadCABundle(w.CABundleWatcher)
	if err != nil {
		log.Errorf("Failed to load CA bundle: %v", err)
		reportWebhookPatchFailure(webhookConfigName, reasonLoadCABundleFailure)
		return err
	}
	for i, wh := range config.Webhooks {
		if strings.HasSuffix(wh.Name, w.webhookName) {
			if !bytes.Equal(caCertPem, config.Webhooks[i].ClientConfig.CABundle) {
				updated = true
			}
			config.Webhooks[i].ClientConfig.CABundle = caCertPem
			found = true
		}
	}
	if !found {
		reportWebhookPatchFailure(webhookConfigName, reasonWebhookEntryNotFound)
		return errNoWebhookWithName
	}

	if updated {
		reportWebhookPatchAttempts(w.webhookName)
		_, err := w.webhooks.Update(config)
		if err != nil {
			reportWebhookPatchFailure(webhookConfigName, reasonWebhookUpdateFailure)
		}
	}

	return err
}

// startCaBundleWatcher listens for updates to the CA bundle and patches the webhooks.
func (w *WebhookCertPatcher) startCaBundleWatcher(stop <-chan struct{}) {
	id, watchCh := w.CABundleWatcher.AddWatcher()
	defer w.CABundleWatcher.RemoveWatcher(id)
	for {
		select {
		case <-watchCh:
			for _, whc := range w.webhooks.List("", klabels.Everything()) {
				log.Debugf("updating caBundle for webhook %q", whc.Name)
				w.queue.AddObject(whc)
			}
		case <-stop:
			return
		}
	}
}
