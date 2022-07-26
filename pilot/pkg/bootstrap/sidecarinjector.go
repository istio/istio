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

package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/webhooks"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

const (
	// Name of the webhook config in the config - no need to change it.
	webhookName = "sidecar-injector.istio.io"
	// defaultInjectorConfigMapName is the default name of the ConfigMap with the injection config
	// The actual name can be different - use getInjectorConfigMapName
	defaultInjectorConfigMapName = "istio-sidecar-injector"
)

var injectionEnabled = env.RegisterBoolVar("INJECT_ENABLED", true, "Enable mutating webhook handler.")

func (s *Server) initSidecarInjector(args *PilotArgs) (*inject.Webhook, error) {
	// currently the constant: "./var/lib/istio/inject"
	injectPath := args.InjectionOptions.InjectionDirectory
	if injectPath == "" || !injectionEnabled.Get() {
		log.Infof("Skipping sidecar injector, injection path is missing or disabled.")
		return nil, nil
	}

	// If the injection config exists either locally or remotely, we will set up injection.
	var watcher inject.Watcher
	if _, err := os.Stat(filepath.Join(injectPath, "config")); !os.IsNotExist(err) {
		configFile := filepath.Join(injectPath, "config")
		valuesFile := filepath.Join(injectPath, "values")
		watcher, err = inject.NewFileWatcher(configFile, valuesFile)
		if err != nil {
			return nil, err
		}
	} else if s.kubeClient != nil {
		configMapName := getInjectorConfigMapName(args.Revision)
		cms := s.kubeClient.Kube().CoreV1().ConfigMaps(args.Namespace)
		if _, err := cms.Get(context.TODO(), configMapName, metav1.GetOptions{}); err != nil {
			if errors.IsNotFound(err) {
				log.Infof("Skipping sidecar injector, template not found")
				return nil, nil
			}
			return nil, err
		}
		watcher = inject.NewConfigMapWatcher(s.kubeClient, args.Namespace, configMapName, "config", "values")
	} else {
		log.Infof("Skipping sidecar injector, template not found")
		return nil, nil
	}

	log.Info("initializing sidecar injector")

	parameters := inject.WebhookParameters{
		Watcher:  watcher,
		Env:      s.environment,
		Mux:      s.httpsMux,
		Revision: args.Revision,
	}

	wh, err := inject.NewWebhook(parameters)
	if err != nil {
		return nil, fmt.Errorf("failed to create injection webhook: %v", err)
	}
	// Patch cert if a webhook config name is provided.
	// This requires RBAC permissions - a low-priv Istiod should not attempt to patch but rely on
	// operator or CI/CD
	if features.InjectionWebhookConfigName != "" {
		s.addStartFunc(func(stop <-chan struct{}) error {
			// No leader election - different istiod revisions will patch their own cert.
			// update webhook configuration by watching the cabundle
			patcher, err := webhooks.NewWebhookCertPatcher(s.kubeClient, args.Revision, webhookName, s.istiodCertBundleWatcher)
			if err != nil {
				log.Errorf("failed to create webhook cert patcher: %v", err)
				return nil
			}

			go patcher.Run(stop)
			return nil
		})
	}
	s.addStartFunc(func(stop <-chan struct{}) error {
		wh.Run(stop)
		return nil
	})
	return wh, nil
}

func getInjectorConfigMapName(revision string) string {
	name := defaultInjectorConfigMapName
	if revision == "" || revision == "default" {
		return name
	}
	return name + "-" + revision
}
