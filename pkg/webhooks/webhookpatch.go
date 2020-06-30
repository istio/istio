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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"time"

	"k8s.io/api/admissionregistration/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/kubernetes"
	admissionregistrationv1beta1client "k8s.io/client-go/kubernetes/typed/admissionregistration/v1beta1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/webhooks/validation/controller"

	"istio.io/pkg/log"
)

// patchMutatingWebhookConfig patches a CA bundle into the specified webhook config.
func patchMutatingWebhookConfig(client admissionregistrationv1beta1client.MutatingWebhookConfigurationInterface,
	webhookConfigName, webhookName string, caBundle []byte) error {
	config, err := client.Get(context.TODO(), webhookConfigName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	prev, err := json.Marshal(config)
	if err != nil {
		return err
	}
	found := false
	for i, w := range config.Webhooks {
		if w.Name == webhookName {
			config.Webhooks[i].ClientConfig.CABundle = caBundle
			found = true
			break
		}
	}
	if !found {
		return apierrors.NewInternalError(fmt.Errorf(
			"webhook entry %q not found in config %q", webhookName, webhookConfigName))
	}
	curr, err := json.Marshal(config)
	if err != nil {
		return err
	}
	patch, err := strategicpatch.CreateTwoWayMergePatch(prev, curr, v1beta1.MutatingWebhookConfiguration{})
	if err != nil {
		return err
	}

	if string(patch) != "{}" {
		_, err = client.Patch(context.TODO(), webhookConfigName, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	}
	return err
}

const delayedRetryTime = time.Second

// Moved out of injector main. Changes:
// - pass the existing k8s client
// - use the K8S root instead of citadel root CA
// - removed the watcher - the k8s CA is already mounted at startup, no more delay waiting for it
func PatchCertLoop(injectionWebhookConfigName, webhookName, caBundlePath string, client kubernetes.Interface, stopCh <-chan struct{}) {
	// K8S own CA
	caCertPem, err := ioutil.ReadFile(caBundlePath)
	if err != nil {
		log.Errorf("Skipping webhook patch, missing CA path %v", caBundlePath)
		return
	}

	var retry bool
	if err = patchMutatingWebhookConfig(client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations(),
		injectionWebhookConfigName, webhookName, caCertPem); err != nil {
		log.Warna("Error patching Webhook ", err)
		retry = true
	}

	shouldPatch := make(chan struct{})

	watchlist := cache.NewListWatchFromClient(
		client.AdmissionregistrationV1beta1().RESTClient(),
		"mutatingwebhookconfigurations",
		"",
		fields.ParseSelectorOrDie(fmt.Sprintf("metadata.name=%s", injectionWebhookConfigName)))

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
							log.Infof("Detected a change in CABundle, patching MutatingWebhookConfiguration again")
							shouldPatch <- struct{}{}
							break
						}
					}
				}
			},
		},
	)
	go controller.Run(stopCh)

	go func() {
		var delayedRetryC <-chan time.Time
		if retry {
			delayedRetryC = time.After(delayedRetryTime)
		}

		for {
			select {
			case <-delayedRetryC:
				if retry := doPatch(client, injectionWebhookConfigName, webhookName, caCertPem); retry {
					delayedRetryC = time.After(delayedRetryTime)
				} else {
					log.Infof("Retried patch succeeded")
					delayedRetryC = nil
				}
			case <-shouldPatch:
				if retry := doPatch(client, injectionWebhookConfigName, webhookName, caCertPem); retry {
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

func doPatch(cs kubernetes.Interface, webhookConfigName, webhookName string, caCertPem []byte) (retry bool) {
	client := cs.AdmissionregistrationV1beta1().MutatingWebhookConfigurations()
	if err := patchMutatingWebhookConfig(client, webhookConfigName, webhookName, caCertPem); err != nil {
		log.Errorf("Patch webhook failed: %v", err)
		return true
	}
	log.Infof("Patched webhook %s", webhookName)
	return false
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
