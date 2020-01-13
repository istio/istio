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

	"istio.io/istio/galley/pkg/config/schema/collections"
	"istio.io/istio/mixer/pkg/validate"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/webhooks/validation/controller"
	"istio.io/istio/pkg/webhooks/validation/server"
	"istio.io/pkg/log"
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
	o := controller.Options{
		WatchedNamespace:      "istio-system", // args.Namespace TODO - default on command line?
		CAPath:                defaultCACertPath,
		WebhookConfigName:     "istio-galley",
		WebhookConfigPath:     configValidationPath,
		ServiceName:           "istio-pilot",
		ClusterRoleName:       "istio-pilot-" + args.Namespace,
		DeferToDeploymentName: "istio-galley",
	}
	whController, err := controller.New(o, client)
	if err != nil {
		return err
	}

	s.addStartFunc(func(stop <-chan struct{}) error {
		go whController.Start(stop)
		return nil
	})
	return nil
}
