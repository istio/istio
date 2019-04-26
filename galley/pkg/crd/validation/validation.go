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
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"time"

	multierror "github.com/hashicorp/go-multierror"

	mixervalidate "istio.io/istio/mixer/pkg/validate"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/probe"
)

const (
	dns1123LabelMaxLength int    = 63
	dns1123LabelFmt       string = "[a-zA-Z0-9]([-a-z-A-Z0-9]*[a-zA-Z0-9])?"

	httpsHandlerReadinessFreq = time.Second
)

var dns1123LabelRegexp = regexp.MustCompile("^" + dns1123LabelFmt + "$")

// This is for lint fix
type httpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

func webhookHTTPSHandlerReady(client httpClient, vc *WebhookParameters) error {
	readinessURL := &url.URL{
		Scheme: "https",
		Host:   fmt.Sprintf("127.0.0.1:%v", vc.Port),
		Path:   httpsHandlerReadyPath,
	}

	req := &http.Request{
		Method: http.MethodGet,
		URL:    readinessURL,
	}

	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request to %v failed: %v", readinessURL, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %v returned non-200 status=%v",
			readinessURL, response.StatusCode)
	}
	return nil
}

//RunValidation start running Galley validation mode
func RunValidation(vc *WebhookParameters, kubeConfig string,
	livenessProbeController, readinessProbeController probe.Controller) {
	log.Infof("Galley validation started with\n%s", vc)
	mixerValidator := mixervalidate.NewDefaultValidator(false)
	clientset, err := kube.CreateClientset(kubeConfig, "")
	if err != nil {
		log.Fatalf("could not create k8s clientset: %v", err)
	}
	vc.MixerValidator = mixerValidator
	vc.PilotDescriptor = model.IstioConfigTypes
	vc.Clientset = clientset
	wh, err := NewWebhook(*vc)
	if err != nil {
		log.Fatalf("cannot create validation webhook service: %v", err)
	}
	if livenessProbeController != nil {
		validationLivenessProbe := probe.NewProbe()
		validationLivenessProbe.SetAvailable(nil)
		validationLivenessProbe.RegisterProbe(livenessProbeController, "validationLiveness")
		defer validationLivenessProbe.SetAvailable(errors.New("stopped"))
	}

	// Create the stop channel for all of the servers.
	stop := make(chan struct{})

	if readinessProbeController != nil {
		validationReadinessProbe := probe.NewProbe()
		validationReadinessProbe.SetAvailable(errors.New("init"))
		validationReadinessProbe.RegisterProbe(readinessProbeController, "validationReadiness")

		go func() {
			ready := false
			client := &http.Client{
				Timeout: time.Second,
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						InsecureSkipVerify: true,
					},
				},
			}

			for {
				if err := webhookHTTPSHandlerReady(client, vc); err != nil {
					validationReadinessProbe.SetAvailable(errors.New("not ready"))
					scope.Infof("https handler for validation webhook is not ready: %v", err)
					ready = false
				} else {
					validationReadinessProbe.SetAvailable(nil)

					if !ready {
						scope.Info("https handler for validation webhook is ready")
						ready = true
					}
				}
				select {
				case <-stop:
					validationReadinessProbe.SetAvailable(errors.New("stopped"))
					return
				case <-time.After(httpsHandlerReadinessFreq):
					// check again
				}
			}
		}()
	}

	go wh.Run(stop)
	cmd.WaitSignal(stop)
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
		if args.WebhookName == "" || !isDNS1123Label(args.WebhookName) {
			errs = multierror.Append(errs, fmt.Errorf("invalid webhook name: %q", args.WebhookName)) // nolint: lll
		}
		if args.DeploymentName == "" || !isDNS1123Label(args.DeploymentAndServiceNamespace) {
			errs = multierror.Append(errs, fmt.Errorf("invalid deployment namespace: %q", args.DeploymentAndServiceNamespace)) // nolint: lll
		}
		if args.DeploymentName == "" || !isDNS1123Label(args.DeploymentName) {
			errs = multierror.Append(errs, fmt.Errorf("invalid deployment name: %q", args.DeploymentName))
		}
		if args.ServiceName == "" || !isDNS1123Label(args.ServiceName) {
			errs = multierror.Append(errs, fmt.Errorf("invalid service name: %q", args.ServiceName))
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
