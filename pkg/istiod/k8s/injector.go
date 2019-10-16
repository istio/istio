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

package k8s

import (
	"github.com/hashicorp/go-multierror"
	"istio.io/istio/pkg/kube/inject"
)

// Injector implements the sidecar injection - specific to K8S.
// Technically it doesn't require K8S - but it's not used outside.

// StartInjector will register the injector handle. No webhook patching or reconcile.
// For now use a different port.
// TLS will be handled by Envoy
func StartInjector(stop chan struct{}) error {
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

	go wh.Run(stop)
	return nil
}
