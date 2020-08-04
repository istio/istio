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
	"fmt"
	"os"
	"path/filepath"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/webhooks"
	"istio.io/pkg/log"
)

const (
	// Name of the webhook config in the config - no need to change it.
	webhookName = "sidecar-injector.istio.io"
)

func (s *Server) initSidecarInjector(args *PilotArgs) (*inject.Webhook, error) {
	injectPath := args.InjectionOptions.InjectionDirectory
	if injectPath == "" {
		log.Infof("Skipping sidecar injector, injection path is missing")
		return nil, nil
	}

	// If the injection path exists, we will set up injection
	if _, err := os.Stat(filepath.Join(injectPath, "config")); os.IsNotExist(err) {
		log.Infof("Skipping sidecar injector, template not found")
		return nil, nil
	}

	log.Info("initializing sidecar injector")

	parameters := inject.WebhookParameters{
		ConfigFile: filepath.Join(injectPath, "config"),
		ValuesFile: filepath.Join(injectPath, "values"),
		Env:        s.environment,
		// Disable monitoring. The injection metrics will be picked up by Pilots metrics exporter already
		MonitoringPort: -1,
		Mux:            s.httpsMux,
		Revision:       args.Revision,
	}

	wh, err := inject.NewWebhook(parameters)
	if err != nil {
		return nil, fmt.Errorf("failed to create injection webhook: %v", err)
	}
	// Patch cert if a webhook config name is provided.
	// This requires RBAC permissions - a low-priv Istiod should not attempt to patch but rely on
	// operator or CI/CD
	if features.InjectionWebhookConfigName.Get() != "" {
		s.addStartFunc(func(stop <-chan struct{}) error {
			// No leader election - different istiod revisions will patch their own cert.
			caBundlePath := s.caBundlePath
			if hasCustomTLSCerts(args.ServerOptions.TLSOptions) {
				caBundlePath = args.ServerOptions.TLSOptions.CaCertFile
			}
			webhooks.PatchCertLoop(features.InjectionWebhookConfigName.Get(), webhookName, caBundlePath, s.kubeClient, stop)
			return nil
		})
	}
	s.addStartFunc(func(stop <-chan struct{}) error {
		go wh.Run(stop)
		return nil
	})
	return wh, nil
}
