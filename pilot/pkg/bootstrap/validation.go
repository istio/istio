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
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/webhooks/validation/controller"
	"istio.io/istio/pkg/webhooks/validation/server"
	"istio.io/pkg/log"
)

func (s *Server) initConfigValidation(args *PilotArgs) error {
	if s.kubeClient == nil {
		return nil
	}

	log.Info("initializing config validator")
	// always start the validation server
	params := server.Options{
		Schemas:      collections.Istio,
		DomainSuffix: args.RegistryOptions.KubeOptions.DomainSuffix,
		Mux:          s.httpsMux,
	}
	_, err := server.New(params)
	if err != nil {
		return err
	}

	if features.ValidationWebhookConfigName != "" && s.kubeClient != nil {
		s.addStartFunc(func(stop <-chan struct{}) error {
			log.Infof("Starting validation controller")
			go controller.NewValidatingWebhookController(
				s.kubeClient, args.Revision, args.Namespace, s.istiodCertBundleWatcher).Run(stop)
			return nil
		})
	}
	return nil
}
