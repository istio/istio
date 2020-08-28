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
	"strings"

	"istio.io/istio/pilot/pkg/leaderelection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/webhooks/validation/controller"
	"istio.io/istio/pkg/webhooks/validation/server"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

var (
	validationWebhookConfigNameTemplateVar = "${namespace}"
	// These should be an invalid DNS-1123 label to ensure the user
	// doesn't specific a valid name that matches out template.
	validationWebhookConfigNameTemplate = "istiod-" + validationWebhookConfigNameTemplateVar

	validationWebhookConfigName = env.RegisterStringVar("VALIDATION_WEBHOOK_CONFIG_NAME", validationWebhookConfigNameTemplate,
		"Name of validatingwebhookconfiguration to patch. Empty will skip using cluster admin to patch.")

	validationEnabled = env.RegisterBoolVar("VALIDATION_ENABLED", true, "Enable config validation handler.")
)

func (s *Server) initConfigValidation(args *PilotArgs) error {
	if s.kubeClient == nil {
		return nil
	}

	if !validationEnabled.Get() {
		return nil
	}

	log.Info("initializing config validator")
	// always start the validation server
	params := server.Options{
		Schemas:      collections.Istio,
		DomainSuffix: args.RegistryOptions.KubeOptions.DomainSuffix,
		Mux:          s.httpsMux,
	}
	whServer, err := server.New(params)
	if err != nil {
		return err
	}

	s.addStartFunc(func(stop <-chan struct{}) error {
		whServer.Run(stop)
		return nil
	})

	if webhookConfigName := validationWebhookConfigName.Get(); webhookConfigName != "" && s.kubeClient != nil {
		if webhookConfigName == validationWebhookConfigNameTemplate {
			webhookConfigName = strings.ReplaceAll(validationWebhookConfigNameTemplate, validationWebhookConfigNameTemplateVar, args.Namespace)
		}

		caBundlePath := s.caBundlePath
		if hasCustomTLSCerts(args.ServerOptions.TLSOptions) {
			caBundlePath = args.ServerOptions.TLSOptions.CaCertFile
		}
		o := controller.Options{
			WatchedNamespace:  args.Namespace,
			CAPath:            caBundlePath,
			WebhookConfigName: webhookConfigName,
			ServiceName:       "istiod",
		}
		whController, err := controller.New(o, s.kubeClient)
		if err != nil {
			log.Errorf("failed to start validation controller: %v", err)
			return err
		}
		s.addTerminatingStartFunc(func(stop <-chan struct{}) error {
			le := leaderelection.NewLeaderElection(args.Namespace, args.PodName, leaderelection.ValidationController, s.kubeClient)
			le.AddRunFunction(func(leaderStop <-chan struct{}) {
				log.Infof("Starting validation controller")
				whController.Start(leaderStop)
			})
			le.Run(stop)
			return nil
		})
	}
	return nil
}
