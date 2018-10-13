// Copyright 2018 Istio Authors
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

package validation

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/galley/cmd/shared"
	"istio.io/istio/mixer/adapter"
	"istio.io/istio/mixer/pkg/config"
	"istio.io/istio/mixer/pkg/config/store"
	runtimeConfig "istio.io/istio/mixer/pkg/runtime/config"
	"istio.io/istio/mixer/pkg/template"
	generatedTmplRepo "istio.io/istio/mixer/template"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/kube"

	"istio.io/istio/pkg/probe"
)

const (
	dns1123LabelMaxLength int    = 63
	dns1123LabelFmt       string = "[a-zA-Z0-9]([-a-z-A-Z0-9]*[a-zA-Z0-9])?"
)

var dns1123LabelRegexp = regexp.MustCompile("^" + dns1123LabelFmt + "$")

// createMixerValidator creates a mixer backend validator.
// TODO(https://github.com/istio/istio/issues/4887) - refactor mixer
// config validation to remove galley dependency on mixer internal
// packages.
func createMixerValidator() (store.BackendValidator, error) {
	info := generatedTmplRepo.SupportedTmplInfo
	templates := make(map[string]*template.Info, len(info))
	for k := range info {
		t := info[k]
		templates[k] = &t
	}
	adapters := config.AdapterInfoMap(adapter.Inventory(), template.NewRepository(info).SupportsTemplate)
	return store.NewValidator(nil, runtimeConfig.KindMap(adapters, templates)), nil
}

//RunValidation start running Galley validation mode
func RunValidation(vc *WebhookParameters, printf, faltaf shared.FormatFn, kubeConfig string,
	livenessProbeController, readinessProbeController probe.Controller) {
	mixerValidator, err := createMixerValidator()
	if err != nil {
		faltaf("cannot create mixer backend validator for %q: %v", kubeConfig, err)
	}
	clientset, err := kube.CreateClientset(kubeConfig, "")
	if err != nil {
		faltaf("could not create k8s clientset: %v", err)
	}
	vc.MixerValidator = mixerValidator
	vc.PilotDescriptor = model.IstioConfigTypes
	vc.Clientset = clientset
	wh, err := NewWebhook(*vc)
	if err != nil {
		faltaf("cannot create validation webhook service: %v", err)
	}
	if livenessProbeController != nil {
		validationLivenessProbe := probe.NewProbe()
		validationLivenessProbe.SetAvailable(nil)
		validationLivenessProbe.RegisterProbe(livenessProbeController, "validationLiveness")
		defer validationLivenessProbe.SetAvailable(errors.New("stopped"))
	}
	if readinessProbeController != nil {
		validationReadinessProbe := probe.NewProbe()
		validationReadinessProbe.SetAvailable(nil)
		validationReadinessProbe.RegisterProbe(readinessProbeController, "validationReadiness")
		defer validationReadinessProbe.SetAvailable(errors.New("stopped"))
	}
	// Create the stop channel for all of the servers.
	stop := make(chan struct{})

	go wh.Run(stop)
	cmd.WaitSignal(stop)
}

// DefaultArgs allocates an WebhookParameters struct initialized with Webhook's default configuration.
func DefaultArgs() *WebhookParameters {
	return &WebhookParameters{
		Port:                443,
		CertFile:            "/etc/istio/certs/cert-chain.pem",
		KeyFile:             "/etc/istio/certs/key.pem",
		CACertFile:          "/etc/istio/certs/root-cert.pem",
		DeploymentNamespace: "istio-system",
		DeploymentName:      "istio-galley",
		EnableValidation:    true,
	}
}

// isDNS1123Label tests for a string that conforms to the definition of a label in
// DNS (RFC 1123).
func isDNS1123Label(value string) bool {
	return len(value) <= dns1123LabelMaxLength && dns1123LabelRegexp.MatchString(value)
}

// validatePort checks that the network port is in range
func validatePort(port int) error {
	if 1 <= port && port <= 65535 {
		return nil
	}
	return fmt.Errorf("port number %d must be in the range 1..65535", port)
}

// Validate tests if the WebhookParameters has valid params.
func (args *WebhookParameters) Validate() error {
	if args == nil {
		return errors.New("nil WebhookParameters")
	}

	var errs *multierror.Error
	if args.EnableValidation {
		// Validate the options that exposed to end users
		if !isDNS1123Label(args.DeploymentNamespace) {
			errs = multierror.Append(errs, fmt.Errorf("invalid deployment namespace: %q", args.DeploymentNamespace))
		}
		if !isDNS1123Label(args.DeploymentName) {
			errs = multierror.Append(errs, fmt.Errorf("invalid deployment name: %q", args.DeploymentName))
		}
		if len(args.WebhookConfigFile) == 0 {
			errs = multierror.Append(errs, errors.New("webhookConfigFile not specified"))
		}
		if len(args.CertFile) == 0 {
			errs = multierror.Append(errs, errors.New("cert file not specified"))
		}
		if len(args.KeyFile) == 0 {
			errs = multierror.Append(errs, errors.New("key file not specified"))
		}
		if len(args.CACertFile) == 0 {
			errs = multierror.Append(errs, errors.New("CA cert file not specified"))
		}
		if err := validatePort(int(args.Port)); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	return errs.ErrorOrNil()
}
