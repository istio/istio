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

package components

import (
	"istio.io/pkg/probe"

	"istio.io/istio/galley/pkg/crd/validation"
	"istio.io/istio/galley/pkg/server/process"
	"istio.io/istio/pkg/cmd"
)

// NewValidation returns a new validation component.
func NewValidation(kubeConfig string, params *validation.WebhookParameters, liveness, readiness probe.Controller) process.Component {

	return process.ComponentFromFns(
		// start
		func() error {
			webhookServerReady := make(chan struct{})
			stopCh := make(chan struct{})
			if params.EnableValidation {
				go validation.RunValidation(webhookServerReady, stopCh, params, kubeConfig, liveness, readiness)
			}
			if params.EnableReconcileWebhookConfiguration {
				go validation.ReconcileWebhookConfiguration(webhookServerReady, stopCh, params, kubeConfig)
			}
			if params.EnableValidation || params.EnableReconcileWebhookConfiguration {
				go cmd.WaitSignal(stopCh)
			}
			return nil
		},
		// stop
		func() {
			// validation doesn't have a stop function.
		})
}
