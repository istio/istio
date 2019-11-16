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

package inject

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"time"

	"github.com/howeyc/fsnotify"
	"k8s.io/api/admissionregistration/v1beta1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/util"
	"istio.io/pkg/log"
)

type WebhookCertParams struct {
	CaCertFile        string
	KubeClient        kubernetes.Interface
	WebhookConfigName string
	WebhookName       string
}

func PatchCertLoop(stopCh <-chan struct{}, p WebhookCertParams) error {
	caCertPem, err := ioutil.ReadFile(p.CaCertFile)
	if err != nil {
		return err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	watchDir, _ := filepath.Split(p.CaCertFile)
	if err = watcher.Watch(watchDir); err != nil {
		return fmt.Errorf("could not watch %v: %v", p.CaCertFile, err)
	}

	var retry bool
	if err = util.PatchMutatingWebhookConfig(p.KubeClient.AdmissionregistrationV1beta1().MutatingWebhookConfigurations(),
		p.WebhookConfigName, p.WebhookName, caCertPem); err != nil {
		retry = true
	}

	shouldPatch := make(chan struct{})

	watchlist := cache.NewListWatchFromClient(
		p.KubeClient.AdmissionregistrationV1beta1().RESTClient(),
		"mutatingwebhookconfigurations",
		"",
		fields.ParseSelectorOrDie(fmt.Sprintf("metadata.name=%s", p.WebhookConfigName)))

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
						if w.Name == p.WebhookName && !bytes.Equal(newConfig.Webhooks[i].ClientConfig.CABundle, caCertPem) {
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
				if retry := doPatch(p.KubeClient, caCertPem, p.WebhookConfigName, p.WebhookName); retry {
					delayedRetryC = time.After(delayedRetryTime)
				} else {
					log.Infof("Retried patch succeeded")
					delayedRetryC = nil
				}
			case <-shouldPatch:
				if retry := doPatch(p.KubeClient, caCertPem, p.WebhookConfigName, p.WebhookName); retry {
					if delayedRetryC == nil {
						delayedRetryC = time.After(delayedRetryTime)
					}
				} else {
					delayedRetryC = nil
				}
			case <-watcher.Event:
				if b, err := ioutil.ReadFile(p.CaCertFile); err == nil {
					log.Infof("Detected a change in CABundle (via secret), patching MutatingWebhookConfiguration again")
					caCertPem = b

					if retry := doPatch(p.KubeClient, caCertPem, p.WebhookConfigName, p.WebhookName); retry {
						if delayedRetryC == nil {
							delayedRetryC = time.After(delayedRetryTime)
							log.Infof("Patch failed - retrying every %v until success", delayedRetryTime)
						}
					} else {
						delayedRetryC = nil
					}
				} else {
					log.Errorf("CA bundle file read error: %v", err)
				}
			}
		}
	}()

	return nil
}

const delayedRetryTime = time.Second

func doPatch(cs kubernetes.Interface, caCertPem []byte, configName, name string) (retry bool) {
	client := cs.AdmissionregistrationV1beta1().MutatingWebhookConfigurations()
	if err := util.PatchMutatingWebhookConfig(client, configName, name, caCertPem); err != nil {
		log.Errorf("Patch webhook failed: %v", err)
		return true
	}
	return false
}
