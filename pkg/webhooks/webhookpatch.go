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
	"time"

	"k8s.io/api/admissionregistration/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	admissionregistrationv1beta1client "k8s.io/client-go/kubernetes/typed/admissionregistration/v1beta1"
	"k8s.io/client-go/tools/cache"

	"istio.io/api/label"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/webhooks/validation/controller"
	"istio.io/pkg/log"
)

var revisionError = errors.New("webhook does not belong to target revision")

func patchMutatingWebhookConfig(client admissionregistrationv1beta1client.MutatingWebhookConfigurationInterface,
	revision, webhookConfigName, webhookName string, caBundle []byte) error {
	config, err := client.Get(context.TODO(), webhookConfigName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	v, ok := config.Labels[label.IstioRev]
	if v != revision || !ok {
		return revisionError
	}

	found := false
	for i, w := range config.Webhooks {
		if w.Name == webhookName {
			config.Webhooks[i].ClientConfig.CABundle = caBundle
			found = true
		}
	}
	if !found {
		return apierrors.NewInternalError(fmt.Errorf(
			"webhook entry %q not found in config %q", webhookName, webhookConfigName))
	}

	_, err = client.Update(context.TODO(), config, metav1.UpdateOptions{})
	return err
}

// PatchCertLoop monitors webhooks labeled with the specified revision and patches them with the given CA bundle
func PatchCertLoop(revision, webhookName, caBundlePath string, client kubernetes.Interface, stopCh <-chan struct{}) {
	// K8S own CA
	caCertPem, err := ioutil.ReadFile(caBundlePath)
	if err != nil {
		log.Errorf("Skipping webhook patch, missing CA path %v", caBundlePath)
		return
	}

	watchlist := cache.NewFilteredListWatchFromClient(
		client.AdmissionregistrationV1beta1().RESTClient(),
		"mutatingwebhookconfigurations",
		"",
		func(options *metav1.ListOptions) {
			options.LabelSelector = fmt.Sprintf("%s=%s", label.IstioRev, revision)
		})

	_, controller := cache.NewInformer(
		watchlist,
		&v1beta1.MutatingWebhookConfiguration{},
		0,
		cache.ResourceEventHandlerFuncs{
			UpdateFunc: func(oldObj, newObj interface{}) {
				oldConfig := oldObj.(*v1beta1.MutatingWebhookConfiguration)
				newConfig := newObj.(*v1beta1.MutatingWebhookConfiguration)

				if oldConfig.ResourceVersion != newConfig.ResourceVersion {
					for i, w := range newConfig.Webhooks {
						if w.Name == webhookName && !bytes.Equal(newConfig.Webhooks[i].ClientConfig.CABundle, caCertPem) {
							log.Infof("Detected a change in CA bundle, patching MutatingWebhookConfiguration for %s", newConfig.Name)
							go doPatchWithRetries(client, revision, newConfig.Name, webhookName, caCertPem)
							break
						}
					}
				}
			},
			AddFunc: func(obj interface{}) {
				config := obj.(*v1beta1.MutatingWebhookConfiguration)
				log.Infof("New webhook config added, patching MutatingWebhookConfiguration for %s", config.Name)
				go doPatchWithRetries(client, revision, config.Name, webhookName, caCertPem)
			},
		},
	)
	go controller.Run(stopCh)
}

func doPatchWithRetries(client kubernetes.Interface, revision, webhookConfigName, webhookName string, caCertPem []byte) {
	retryDelay := time.Second * 2
	for {
		if err := doPatch(client, revision, webhookConfigName, webhookName, caCertPem); err != nil {
			// if the webhook no longer has the target revision, bail out
			if errors.Is(err, revisionError) {
				return
			}

			// otherwise, wait and give it another try
			time.Sleep(retryDelay)
		} else {
			return
		}
	}
}

func doPatch(cs kubernetes.Interface, revision, webhookConfigName, webhookName string, caCertPem []byte) error {
	client := cs.AdmissionregistrationV1beta1().MutatingWebhookConfigurations()
	err := patchMutatingWebhookConfig(client, revision, webhookConfigName, webhookName, caCertPem)
	if err != nil {
		log.Errorf("Patch webhook failed for webhook %s: %v", webhookName, err)
	} else {
		log.Infof("Patched webhook %s", webhookName)
	}
	return err
}

func CreateValidationWebhookController(client kube.Client,
	webhookConfigName, ns, caBundlePath string, remote bool) *controller.Controller {
	o := controller.Options{
		WatchedNamespace:    ns,
		CAPath:              caBundlePath,
		WebhookConfigName:   webhookConfigName,
		ServiceName:         "istiod",
		RemoteWebhookConfig: remote,
	}
	whController, err := controller.New(o, client)
	if err != nil {
		log.Errorf("failed to create validationWebhookController controller: %v", err)
	}
	return whController
}
