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
	"fmt"
	"io/ioutil"
	"time"

	"k8s.io/api/admissionregistration/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	admissionregistrationv1beta1client "k8s.io/client-go/kubernetes/typed/admissionregistration/v1beta1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/webhooks/validation/controller"
	"istio.io/pkg/log"
)

func patchMutatingWebhookConfig(client admissionregistrationv1beta1client.MutatingWebhookConfigurationInterface,
	webhookConfigName, webhookName string, caBundle []byte) error {
	config, err := client.Get(context.TODO(), webhookConfigName, metav1.GetOptions{})
	if err != nil {
		return err
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

const delayedRetryTime = time.Second

// PatchCertLoop monitors webhooks labeled with the specified revision and patches them with the given CA bundle
func PatchCertLoop(revision, injectionWebhookConfigName, webhookName, caBundlePath string, client kubernetes.Interface, stopCh <-chan struct{}) {
	// K8S own CA
	caCertPem, err := ioutil.ReadFile(caBundlePath)
	if err != nil {
		log.Errorf("Skipping webhook patch, missing CA path %v", caBundlePath)
		return
	}

	// used for retries on patching canonical injection webhook for this revision
	shouldPatchCanonicalWebhook := make(chan struct{})

	watchlist := cache.NewFilteredListWatchFromClient(
		client.AdmissionregistrationV1beta1().RESTClient(),
		"mutatingwebhookconfigurations",
		"",
		func(options *metav1.ListOptions) {
			options.LabelSelector = fmt.Sprintf("istio.io/rev=%s", revision)
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
							// either this is the canonical webhook for the revision and we should keep trying to patch
							// the CABundle forever or it is a revision tag webhook and we should try once
							if newConfig.Name == injectionWebhookConfigName {
								shouldPatchCanonicalWebhook <- struct{}{}
							} else {
								if err := doPatch(client, newConfig.Name, webhookName, caCertPem); err != nil {
									log.Errorf("failed to patch updated webhook %s: %v", newConfig.Name, err)
								}
							}
							break
						}
					}
				}
			},
			AddFunc: func(obj interface{}) {
				config := obj.(*v1beta1.MutatingWebhookConfiguration)
				if config.Name == injectionWebhookConfigName {
					shouldPatchCanonicalWebhook <- struct{}{}
				} else {
					if err := doPatch(client, config.Name, webhookName, caCertPem); err != nil {
						log.Errorf("failed to patch updated webhook %s: %v", config.Name, err)
					}
				}
			},
		},
	)
	go controller.Run(stopCh)

	go func() {
		var delayedRetryC <-chan time.Time
		for {
			select {
			case <-delayedRetryC:
				if err := doPatch(client, injectionWebhookConfigName, webhookName, caCertPem); err != nil {
					delayedRetryC = time.After(delayedRetryTime)
				} else {
					log.Infof("Retried patch succeeded")
					delayedRetryC = nil
				}
			case <-shouldPatchCanonicalWebhook:
				if err := doPatch(client, injectionWebhookConfigName, webhookName, caCertPem); err != nil {
					if delayedRetryC == nil {
						delayedRetryC = time.After(delayedRetryTime)
					}
				} else {
					delayedRetryC = nil
				}
			}
		}
	}()
}

func doPatch(cs kubernetes.Interface, webhookConfigName, webhookName string, caCertPem []byte) error {
	client := cs.AdmissionregistrationV1beta1().MutatingWebhookConfigurations()
	err := patchMutatingWebhookConfig(client, webhookConfigName, webhookName, caCertPem)
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
