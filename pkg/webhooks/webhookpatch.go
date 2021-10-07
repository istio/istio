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
	"time"

	v1 "k8s.io/api/admissionregistration/v1"
	kubeErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	admissioninformer "k8s.io/client-go/informers/admissionregistration/v1"
	"k8s.io/client-go/kubernetes"
	admissionregistrationv1client "k8s.io/client-go/kubernetes/typed/admissionregistration/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/keycertbundle"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/webhooks/util"
	"istio.io/pkg/log"
)

var (
	errWrongRevision     = errors.New("webhook does not belong to target revision")
	errNoWebhookWithName = errors.New("webhook configuration did not contain webhook with target name")
)

// WebhookCertPatcher listens for webhooks on specified revision and patches their CA bundles
type WebhookCertPatcher struct {
	client kubernetes.Interface

	// revision to patch webhooks for
	revision    string
	webhookName string

	queue workqueue.RateLimitingInterface

	// File path to the x509 certificate bundle used by the webhook server
	// and patched into the webhook config.
	CABundleWatcher *keycertbundle.Watcher

	informer cache.SharedIndexInformer
}

// NewWebhookCertPatcher creates a WebhookCertPatcher
func NewWebhookCertPatcher(
	client kubelib.Client,
	revision, webhookName string, caBundleWatcher *keycertbundle.Watcher) (*WebhookCertPatcher, error) {
	p := &WebhookCertPatcher{
		client:          client,
		revision:        revision,
		webhookName:     webhookName,
		CABundleWatcher: caBundleWatcher,
		queue:           workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "mutatingwebhookconfiguration"),
	}
	informer := admissioninformer.NewFilteredMutatingWebhookConfigurationInformer(client, 0, cache.Indexers{}, func(options *metav1.ListOptions) {
		options.LabelSelector = fmt.Sprintf("%s=%s", label.IoIstioRev.Name, revision)
	})
	p.informer = informer
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldConfig := oldObj.(*v1.MutatingWebhookConfiguration)
			newConfig := newObj.(*v1.MutatingWebhookConfiguration)
			p.updateWebhookHandler(oldConfig, newConfig)
		},
		AddFunc: func(obj interface{}) {
			config := obj.(*v1.MutatingWebhookConfiguration)
			p.addWebhookHandler(config)
		},
	})

	return p, nil
}

// Run runs the WebhookCertPatcher
func (w *WebhookCertPatcher) Run(stopChan <-chan struct{}) {
	go w.runWebhookController(stopChan)
	go w.startCaBundleWatcher(stopChan)
	go wait.Until(w.worker, time.Second, stopChan)
}

func (w *WebhookCertPatcher) worker() {
	for w.processNextItem() {
	}
}

func (w *WebhookCertPatcher) processNextItem() bool {
	item, shutdown := w.queue.Get()
	if shutdown {
		return false
	}
	defer w.queue.Done(item)

	key := item.(string)
	err := w.webhookPatchTask(key)
	if err != nil {
		log.Errorf("patching webhook %s failed: %v", key, err)
		w.queue.AddRateLimited(item)
		return true
	}
	w.queue.Forget(item)
	return true
}

func (w *WebhookCertPatcher) runWebhookController(stopChan <-chan struct{}) {
	w.informer.Run(stopChan)
}

func (w *WebhookCertPatcher) updateWebhookHandler(oldConfig, newConfig *v1.MutatingWebhookConfiguration) {
	caCertPem, err := util.LoadCABundle(w.CABundleWatcher)
	if err != nil {
		log.Errorf("Failed to load CA bundle: %v", err)
		return
	}
	if oldConfig.ResourceVersion != newConfig.ResourceVersion {
		for i, wh := range newConfig.Webhooks {
			if strings.HasSuffix(wh.Name, w.webhookName) && !bytes.Equal(newConfig.Webhooks[i].ClientConfig.CABundle, caCertPem) {
				w.queue.Add(newConfig.Name)
				break
			}
		}
	}
}

func (w *WebhookCertPatcher) addWebhookHandler(config *v1.MutatingWebhookConfiguration) {
	for _, wh := range config.Webhooks {
		if strings.HasSuffix(wh.Name, w.webhookName) {
			log.Infof("New webhook config added, patching MutatingWebhookConfiguration for %s", config.Name)
			w.queue.Add(config.Name)
			break
		}
	}
}

// webhookPatchTask takes the result of patchMutatingWebhookConfig and modifies the result for use in task queue
func (w *WebhookCertPatcher) webhookPatchTask(webhookConfigName string) error {
	reportWebhookPatchAttempts(webhookConfigName)
	err := w.patchMutatingWebhookConfig(
		w.client.AdmissionregistrationV1().MutatingWebhookConfigurations(),
		webhookConfigName)

	// do not want to retry the task if these errors occur, they indicate that
	// we should no longer be patching the given webhook
	if kubeErrors.IsNotFound(err) || errors.Is(err, errWrongRevision) || errors.Is(err, errNoWebhookWithName) {
		return nil
	}

	if err != nil {
		reportWebhookPatchRetry(webhookConfigName)
	}

	return err
}

// patchMutatingWebhookConfig takes a webhookConfigName and patches the CA bundle for that webhook configuration
func (w *WebhookCertPatcher) patchMutatingWebhookConfig(
	client admissionregistrationv1client.MutatingWebhookConfigurationInterface,
	webhookConfigName string) error {
	config, err := client.Get(context.TODO(), webhookConfigName, metav1.GetOptions{})
	if err != nil {
		reportWebhookPatchFailure(webhookConfigName, reasonWebhookConfigNotFound)
		return err
	}
	// prevents a race condition between multiple istiods when the revision is changed or modified
	v, ok := config.Labels[label.IoIstioRev.Name]
	if v != w.revision || !ok {
		reportWebhookPatchFailure(webhookConfigName, reasonWrongRevision)
		return errWrongRevision
	}

	found := false
	caCertPem, err := util.LoadCABundle(w.CABundleWatcher)
	if err != nil {
		log.Errorf("Failed to load CA bundle: %v", err)
		reportWebhookPatchFailure(webhookConfigName, reasonLoadCABundleFailure)
		return err
	}
	for i, wh := range config.Webhooks {
		if strings.HasSuffix(wh.Name, w.webhookName) {
			config.Webhooks[i].ClientConfig.CABundle = caCertPem
			found = true
		}
	}
	if !found {
		reportWebhookPatchFailure(webhookConfigName, reasonWebhookEntryNotFound)
		return errNoWebhookWithName
	}

	_, err = client.Update(context.TODO(), config, metav1.UpdateOptions{})
	if err != nil {
		reportWebhookPatchFailure(webhookConfigName, reasonWebhookUpdateFailure)
	}

	return err
}

// startCaBundleWatcher listens for updates to the CA bundle and patches the webhooks.
func (w *WebhookCertPatcher) startCaBundleWatcher(stop <-chan struct{}) {
	watchCh := w.CABundleWatcher.AddWatcher()
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
				w.queue.Add(mutatingWebhookConfig.Name)
			}
		case <-stop:
			return
		}
	}
}
