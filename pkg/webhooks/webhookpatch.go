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
	"io/ioutil"
	"strings"
	"time"

	v1 "k8s.io/api/admissionregistration/v1"
	kubeErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	admissionregistrationv1client "k8s.io/client-go/kubernetes/typed/admissionregistration/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/api/label"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/queue"
	"istio.io/istio/pkg/webhooks/validation/controller"
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
	caCertPem   []byte

	queue queue.Instance
}

// Run runs the WebhookCertPatcher
func (w *WebhookCertPatcher) Run(stopChan <-chan struct{}) {
	go w.queue.Run(stopChan)
	go w.runWebhookController(stopChan)
}

// NewWebhookCertPatcher creates a WebhookCertPatcher
func NewWebhookCertPatcher(
	client kubernetes.Interface,
	revision, webhookName, caBundlePath string) (*WebhookCertPatcher, error) {
	// ca from k8s
	caCertPem, err := ioutil.ReadFile(caBundlePath)
	if err != nil {
		log.Errorf("Skipping webhook patch, missing CA path %v", caBundlePath)
		return nil, fmt.Errorf("could not read CA path: %v", err)
	}

	return &WebhookCertPatcher{
		client:      client,
		revision:    revision,
		webhookName: webhookName,
		caCertPem:   caCertPem,
		queue:       queue.NewQueue(time.Second * 2),
	}, nil
}

func (w *WebhookCertPatcher) runWebhookController(stopChan <-chan struct{}) {
	watchlist := cache.NewFilteredListWatchFromClient(
		w.client.AdmissionregistrationV1().RESTClient(),
		"mutatingwebhookconfigurations",
		"",
		func(options *metav1.ListOptions) {
			options.LabelSelector = fmt.Sprintf("%s=%s", label.IoIstioRev.Name, w.revision)
		})

	_, c := cache.NewInformer(
		watchlist,
		&v1.MutatingWebhookConfiguration{},
		0,
		cache.ResourceEventHandlerFuncs{
			UpdateFunc: func(oldObj, newObj interface{}) {
				oldConfig := oldObj.(*v1.MutatingWebhookConfiguration)
				newConfig := newObj.(*v1.MutatingWebhookConfiguration)
				w.updateWebhookHandler(oldConfig, newConfig)
			},
			AddFunc: func(obj interface{}) {
				config := obj.(*v1.MutatingWebhookConfiguration)
				w.addWebhookHandler(config)
			},
		},
	)

	c.Run(stopChan)
}

func (w *WebhookCertPatcher) updateWebhookHandler(oldConfig, newConfig *v1.MutatingWebhookConfiguration) {
	if oldConfig.ResourceVersion != newConfig.ResourceVersion {
		for i, wh := range newConfig.Webhooks {
			if strings.HasSuffix(wh.Name, w.webhookName) && !bytes.Equal(newConfig.Webhooks[i].ClientConfig.CABundle, w.caCertPem) {
				w.queue.Push(func() error {
					return w.webhookPatchTask(newConfig.Name)
				})
				break
			}
		}
	}
}

func (w *WebhookCertPatcher) addWebhookHandler(config *v1.MutatingWebhookConfiguration) {
	for i, wh := range config.Webhooks {
		if strings.HasSuffix(wh.Name, w.webhookName) && !bytes.Equal(config.Webhooks[i].ClientConfig.CABundle, w.caCertPem) {
			log.Infof("New webhook config added, patching MutatingWebhookConfiguration for %s", config.Name)
			w.queue.Push(func() error {
				return w.webhookPatchTask(config.Name)
			})
			break
		}
	}
}

// webhookPatchTask takes the result of patchMutatingWebhookConfig and modifies the result for use in task queue
func (w *WebhookCertPatcher) webhookPatchTask(webhookConfigName string) error {
	err := w.patchMutatingWebhookConfig(
		w.client.AdmissionregistrationV1().MutatingWebhookConfigurations(),
		webhookConfigName)

	// do not want to retry the task if these errors occur, they indicate that
	// we should no longer be patching the given webhook
	if kubeErrors.IsNotFound(err) || errors.Is(err, errWrongRevision) || errors.Is(err, errNoWebhookWithName) {
		return nil
	}

	return err
}

// patchMutatingWebhookConfig takes a webhookConfigName and patches the CA bundle for that webhook configuration
func (w *WebhookCertPatcher) patchMutatingWebhookConfig(
	client admissionregistrationv1client.MutatingWebhookConfigurationInterface,
	webhookConfigName string) error {
	config, err := client.Get(context.TODO(), webhookConfigName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	// prevents a race condition between multiple istiods when the revision is changed or modified
	v, ok := config.Labels[label.IoIstioRev.Name]
	if v != w.revision || !ok {
		return errWrongRevision
	}

	found := false
	for i, wh := range config.Webhooks {
		if strings.HasSuffix(wh.Name, w.webhookName) {
			config.Webhooks[i].ClientConfig.CABundle = w.caCertPem
			found = true
		}
	}
	if !found {
		return errNoWebhookWithName
	}

	_, err = client.Update(context.TODO(), config, metav1.UpdateOptions{})
	return err
}

func CreateValidationWebhookController(client kube.Client,
	webhookConfigName, ns, caBundlePath string) *controller.Controller {
	o := controller.Options{
		WatchedNamespace:  ns,
		CAPath:            caBundlePath,
		WebhookConfigName: webhookConfigName,
		ServiceName:       "istiod",
	}
	whController, err := controller.New(o, client)
	if err != nil {
		log.Errorf("failed to create validationWebhookController controller: %v", err)
	}
	return whController
}
