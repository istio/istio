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
	"context"
	"errors"
	"fmt"
	"strings"

	v1 "k8s.io/api/admissionregistration/v1"
	kubeErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	admissioninformer "k8s.io/client-go/informers/admissionregistration/v1"
	"k8s.io/client-go/kubernetes"
	admissionregistrationv1client "k8s.io/client-go/kubernetes/typed/admissionregistration/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/keycertbundle"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/webhooks/util"
	"istio.io/pkg/log"
)

var (
	errWrongRevision     = errors.New("webhook does not belong to target revision")
	errNotFound          = errors.New("webhook not found")
	errNoWebhookWithName = errors.New("webhook configuration did not contain webhook with target name")
)

// WebhookCertPatcher listens for webhooks on specified revision and patches their CA bundles
type WebhookCertPatcher struct {
	client kubernetes.Interface

	// revision to patch webhooks for
	revision    string
	webhookName string

	queue controllers.Queue

	// File path to the x509 certificate bundle used by the webhook server
	// and patched into the webhook config.
	CABundleWatcher *keycertbundle.Watcher

	informer cache.SharedIndexInformer
}

// NewWebhookCertPatcher creates a WebhookCertPatcher
func NewWebhookCertPatcher(
	client kubelib.Client,
	revision, webhookName string, caBundleWatcher *keycertbundle.Watcher,
) (*WebhookCertPatcher, error) {
	p := &WebhookCertPatcher{
		client:          client.Kube(),
		revision:        revision,
		webhookName:     webhookName,
		CABundleWatcher: caBundleWatcher,
	}
	p.queue = controllers.NewQueue("webhook patcher",
		controllers.WithReconciler(p.webhookPatchTask),
		controllers.WithMaxAttempts(5))
	informer := admissioninformer.NewFilteredMutatingWebhookConfigurationInformer(client.Kube(), 0, cache.Indexers{}, func(options *metav1.ListOptions) {
		options.LabelSelector = fmt.Sprintf("%s=%s", label.IoIstioRev.Name, revision)
	})
	p.informer = informer
	informer.AddEventHandler(controllers.ObjectHandler(p.queue.AddObject))

	return p, nil
}

// Run runs the WebhookCertPatcher
func (w *WebhookCertPatcher) Run(stopChan <-chan struct{}) {
	go w.informer.Run(stopChan)
	go w.startCaBundleWatcher(stopChan)
	w.queue.Run(stopChan)
}

func (w *WebhookCertPatcher) HasSynced() bool {
	return w.informer.HasSynced() && w.queue.HasSynced()
}

// webhookPatchTask takes the result of patchMutatingWebhookConfig and modifies the result for use in task queue
func (w *WebhookCertPatcher) webhookPatchTask(o types.NamespacedName) error {
	reportWebhookPatchAttempts(o.Name)
	err := w.patchMutatingWebhookConfig(
		w.client.AdmissionregistrationV1().MutatingWebhookConfigurations(),
		o.Name)

	// do not want to retry the task if these errors occur, they indicate that
	// we should no longer be patching the given webhook
	if kubeErrors.IsNotFound(err) || errors.Is(err, errWrongRevision) || errors.Is(err, errNoWebhookWithName) || errors.Is(err, errNotFound) {
		return nil
	}

	if err != nil {
		log.Errorf("patching webhook %s failed: %v", o.Name, err)
		reportWebhookPatchRetry(o.Name)
	}

	return err
}

// patchMutatingWebhookConfig takes a webhookConfigName and patches the CA bundle for that webhook configuration
func (w *WebhookCertPatcher) patchMutatingWebhookConfig(
	client admissionregistrationv1client.MutatingWebhookConfigurationInterface,
	webhookConfigName string,
) error {
	raw, _, err := w.informer.GetIndexer().GetByKey(webhookConfigName)
	if raw == nil || err != nil {
		reportWebhookPatchFailure(webhookConfigName, reasonWebhookConfigNotFound)
		return errNotFound
	}
	config := raw.(*v1.MutatingWebhookConfiguration)
	// prevents a race condition between multiple istiods when the revision is changed or modified
	v, ok := config.Labels[label.IoIstioRev.Name]
	if v != w.revision || !ok {
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
		_, err = client.Update(context.Background(), config, metav1.UpdateOptions{})
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
			lists := w.informer.GetStore().List()
			for _, list := range lists {
				mutatingWebhookConfig := list.(*v1.MutatingWebhookConfiguration)
				if mutatingWebhookConfig == nil {
					continue
				}
				log.Debugf("updating caBundle for webhook %q", mutatingWebhookConfig.Name)
				w.queue.Add(types.NamespacedName{Name: mutatingWebhookConfig.Name})
			}
		case <-stop:
			return
		}
	}
}
