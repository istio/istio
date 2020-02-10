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
	"os"
	"path/filepath"
	"strings"

	"istio.io/pkg/env"
	"istio.io/pkg/log"

	"istio.io/istio/mixer/pkg/validate"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/webhooks/validation/controller"
	"istio.io/istio/pkg/webhooks/validation/server"
)

var (
	validationWebhookConfigNameTemplateVar = "${namespace}"
	// These should be an invalid DNS-1123 label to ensure the user
	// doesn't specific a valid name that matches out template.
	validationWebhookConfigNameTemplate = "istiod-" + validationWebhookConfigNameTemplateVar

	validationWebhookConfigName = env.RegisterStringVar("VALIDATION_WEBHOOK_CONFIG_NAME", validationWebhookConfigNameTemplate,
		"Name of validatingwegbhookconfiguration to patch, if istioctl is not used.")

	deferToDeploymentName = env.RegisterStringVar("DEFER_VALIDATION_TO_DEPLOYMENT", "",
		"When set, the controller defers reconciling the validatingwebhookconfiguration to the named deployment.")
)

func (s *Server) initConfigValidation(args *PilotArgs) error {
	if features.IstiodService.Get() == "" {
		return nil
	}

	// always start the validation server
	params := server.Options{
		MixerValidator: validate.NewDefaultValidator(false),
		Schemas:        collections.Istio,
		DomainSuffix:   args.Config.ControllerOptions.DomainSuffix,
		CertFile:       filepath.Join(dnsCertDir, "cert-chain.pem"),
		KeyFile:        filepath.Join(dnsCertDir, "key.pem"),
		Mux:            s.httpsMux,
	}
	whServer, err := server.New(params)
	if err != nil {
		return err
	}

	s.addStartFunc(func(stop <-chan struct{}) error {
		whServer.Run(stop)
		return nil
	})

	if args.ValidationOptions.ValidationDirectory == "" {
		log.Infof("Webhook validation config file not found. " +
			"Not starting the webhook validation config controller. " +
			"Use istioctl or the operator to manage the config lifecycle")
		return nil
	}
	configValidationPath := filepath.Join(args.ValidationOptions.ValidationDirectory, "config")
	// If the validation path exists, we will set up the config controller
	if _, err := os.Stat(configValidationPath); os.IsNotExist(err) {
		log.Infof("Skipping config validation controller, config not found")
		return nil
	}

	client, err := kube.CreateClientset(args.Config.KubeConfig, "")
	if err != nil {
		return err
	}

	webhookConfigName := validationWebhookConfigName.Get()
	if webhookConfigName == validationWebhookConfigNameTemplate {
		webhookConfigName = strings.ReplaceAll(validationWebhookConfigNameTemplate, validationWebhookConfigNameTemplateVar, args.Namespace)
	}

	deferTo := deferToDeploymentName.Get()
	if deferTo != "" && !labels.IsDNS1123Label(deferTo) {
		log.Warnf("DEFER_VALIDATION_TO_DEPLOYMENT=%v must be a valid DNS1123 label", deferTo)
		deferTo = ""
	}

	o := controller.Options{
		WatchedNamespace:      args.Namespace,
		CAPath:                s.caBundlePath,
		WebhookConfigName:     webhookConfigName,
		WebhookConfigPath:     configValidationPath,
		ServiceName:           "istiod",
		ClusterRoleName:       "istiod-" + args.Namespace,
		DeferToDeploymentName: deferTo,
	}
	whController, err := controller.New(o, client)
	if err != nil {
		return err
	}

	if validationWebhookConfigName.Get() != "" {
		s.leaderElection.AddRunFunction(func(stop <-chan struct{}) {
			log.Infof("Starting validation controller")
			whController.Start(stop)
		})
	}
	return nil
}
