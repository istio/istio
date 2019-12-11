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
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/hashicorp/go-multierror"
	"k8s.io/api/admissionregistration/v1beta1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/util"
	"istio.io/pkg/env"

	"istio.io/istio/pkg/kube/inject"
	"istio.io/pkg/log"
)

var (
	webhookConfigName = env.RegisterStringVar("WEBHOOK", "",
		"Name of webhook config to patch, if istioctl is not used.")
)

const (
	// Name of the webhook config in the config - no need to change it.
	webhookName = "sidecar-injector.istio.io"
)

// Was not used in Pilot for 1.3/1.4 (injector was standalone).
// In 1.5 - used as part of istiod, if the inject template exists.
func (s *Server) initSidecarInjector(args *PilotArgs) error {
	// Injector should run along, even if not used - but only if the injection template is mounted.
	// ./var/lib/istio/inject - enabled by mounting a template in the config.
	injectPath := args.InjectionOptions.InjectionDirectory
	if injectPath == "" {
		log.Infof("Skipping sidecar injector, injection path is missing")
		return nil
	}

	// If the injection path exists, we will set up injection
	if _, err := os.Stat(filepath.Join(injectPath, "config")); !os.IsNotExist(err) {
		parameters := inject.WebhookParameters{
			ConfigFile:          filepath.Join(injectPath, "config"),
			ValuesFile:          filepath.Join(injectPath, "values"),
			MeshFile:            args.Mesh.ConfigFile,
			CertFile:            filepath.Join(DNSCertDir, "cert-chain.pem"),
			KeyFile:             filepath.Join(DNSCertDir, "key.pem"),
			Port:                args.InjectionOptions.Port,
			HealthCheckFile:     "",
			HealthCheckInterval: 0,
			MonitoringPort:      s.basePort + 16, // TODO: disable the second monitoring port
		}

		wh, err := inject.NewWebhook(parameters)
		if err != nil {
			return fmt.Errorf("failed to create injection webhook: %v", err)
		}
		if webhookConfigName.Get() != "" {
			s.addStartFunc(func(stop <-chan struct{}) error {
				if err := patchCertLoop(s.kubeClient, stop); err != nil {
					return multierror.Prefix(err, "failed to start patch cert loop")
				}
				return nil
			})
		}
		s.addStartFunc(func(stop <-chan struct{}) error {
			go wh.Run(stop)
			return nil
		})
		return nil
	}
	log.Infof("Skipping sidecar injector, template not found")
	return nil
}

const delayedRetryTime = time.Second

// Moved out of injector main. Changes:
// - pass the existing k8s client
// - use the K8S root instead of citadel root CA
// - removed the watcher - the k8s CA is already mounted at startup, no more delay waiting for it
func patchCertLoop(client kubernetes.Interface, stopCh <-chan struct{}) error {

	// K8S own CA
	caCertPem, err := ioutil.ReadFile(defaultCACertPath)
	if err != nil {
		log.Warna("Skipping webhook patch, missing CA path ", defaultCACertPath)
		return err
	}

	var retry bool
	if err = util.PatchMutatingWebhookConfig(client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations(),
		webhookConfigName.Get(), webhookName, caCertPem); err != nil {
		log.Warna("Error patching Webhook ", err)
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
	log.Infof("Patched webhook %s", webhookName)
	return false
}
