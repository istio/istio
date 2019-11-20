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

package bootstrap

import (
	"bytes"
	"fmt"
	"github.com/hashicorp/go-multierror"
	"io/ioutil"
	"istio.io/istio/pkg/util"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
	"k8s.io/client-go/kubernetes"
	"time"
	"k8s.io/client-go/tools/cache"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/api/admissionregistration/v1beta1"

	"istio.io/istio/pkg/kube/inject"
)

var (
	webhookConfigName = env.RegisterStringVar("WEBHOOK", "",
		"Name of webhook config to patch, if istioctl is not used.")
)

const (
	// Name of the webhook config in the config - no need to change it.
	webhookName =  "sidecar-injector.istio.io"
)
// Injector implements the sidecar injection - specific to K8S.
// Technically it doesn't require K8S - but it's not used outside.

// StartInjector will register the injector handle. No webhook patching or reconcile.
// For now use a different port.
// TLS will be handled by Envoy
func StartInjector(k8s kubernetes.Interface, stop <-chan struct{}) error {
	// TODO: modify code to allow startup without TLS ( let envoy handle it)
	// TODO: switch readiness to common http based.

	parameters := inject.WebhookParameters{
		ConfigFile:          "./var/lib/istio/inject/injection-template.yaml",
		ValuesFile:          "./var/lib/istio/install/values.yaml",
		MeshFile:            "./var/lib/istio/config/mesh",
		CertFile:            "./var/run/secrets/istio-dns/cert-chain.pem",
		KeyFile:             "./var/run/secrets/istio-dns/key.pem",
		Port:                15017,
		HealthCheckInterval: 0,
		HealthCheckFile:     "",
		MonitoringPort:      0,
	}
	wh, err := inject.NewWebhook(parameters)
	if err != nil {
		return multierror.Prefix(err, "failed to create injection webhook")
	}

	if webhookConfigName.Get() != "" {
			if err := patchCertLoop(k8s, stop); err != nil {
					return multierror.Prefix(err, "failed to start patch cert loop")
			}
	}

	go wh.Run(stop)
	return nil
}

const delayedRetryTime = time.Second

// Moved out of injector main. Changes:
// - pass the existing k8s client
// - use the K8S root instead of citadel root CA
// - removed the watcher - the k8s CA is already mounted at startup, no more delay waiting for it
func patchCertLoop(client kubernetes.Interface, stopCh <-chan struct{}) error {

	// K8S own CA
	caCertPem, err := ioutil.ReadFile(DefaultCA)
	if err != nil {
		return err
	}

	var retry bool
	if err = util.PatchMutatingWebhookConfig(client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations(),
		webhookConfigName.Get(), webhookName, caCertPem); err != nil {
		retry = true
	}

	shouldPatch := make(chan struct{})

	watchlist := cache.NewListWatchFromClient(
		client.AdmissionregistrationV1beta1().RESTClient(),
		"mutatingwebhookconfigurations",
		"",
		fields.ParseSelectorOrDie(fmt.Sprintf("metadata.name=%s", webhookConfigName.Get())))

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
						if w.Name == webhookConfigName.Get() && !bytes.Equal(newConfig.Webhooks[i].ClientConfig.CABundle, caCertPem) {
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
				if retry := doPatch(client, webhookConfigName.Get(), webhookName, caCertPem); retry {
					delayedRetryC = time.After(delayedRetryTime)
				} else {
					log.Infof("Retried patch succeeded")
					delayedRetryC = nil
				}
			case <-shouldPatch:
				if retry := doPatch(client, webhookConfigName.Get(), webhookName, caCertPem); retry {
					if delayedRetryC == nil {
						delayedRetryC = time.After(delayedRetryTime)
					}
				} else {
					delayedRetryC = nil
				}
			}
		}
	}()

	return nil
}

func doPatch(cs kubernetes.Interface, webhookConfigName, webhookName string, caCertPem []byte) (retry bool) {
	client := cs.AdmissionregistrationV1beta1().MutatingWebhookConfigurations()
	if err := util.PatchMutatingWebhookConfig(client, webhookConfigName, webhookName, caCertPem); err != nil {
		log.Errorf("Patch webhook failed: %v", err)
		return true
	}
	return false
}
