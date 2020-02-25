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
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"istio.io/istio/galley/pkg/server/process"
	"istio.io/istio/mixer/pkg/validate"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/webhooks/validation/controller"
	"istio.io/istio/pkg/webhooks/validation/server"

	"istio.io/pkg/log"
	"istio.io/pkg/probe"
)

// This is for lint fix
type httpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

func monitorReadiness(stop <-chan struct{}, port uint, readiness *probe.Probe) {
	const httpsHandlerReadinessFreq = time.Second

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
		if err := webhookHTTPSHandlerReady(client, port); err != nil {
			readiness.SetAvailable(errors.New("not ready"))
			scope.Infof("https handler for validation webhook is not ready: %v\n", err)
			ready = false
		} else {
			readiness.SetAvailable(nil)
			if !ready {
				scope.Info("https handler for validation webhook is ready\n")
				ready = true
			}
		}

		select {
		case <-stop:
			return
		case <-time.After(httpsHandlerReadinessFreq):
			// check again
		}
	}
}

func webhookHTTPSHandlerReady(client httpClient, port uint) error {
	readinessURL := &url.URL{
		Scheme: "https",
		Host:   fmt.Sprintf("localhost:%v", port),
		Path:   server.HTTPSHandlerReadyPath,
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

func NewValidationServer(
	options server.Options,
	liveness probe.Controller,
	readiness probe.Controller,
) process.Component {
	stop := make(chan struct{})
	var stopped int32 // atomic
	return process.ComponentFromFns(
		// start
		func() error {
			log.Infof("Galley validation server started with \n%s", &options)

			options.MixerValidator = validate.NewDefaultValidator(false)
			options.Schemas = collections.Istio
			s, err := server.New(options)
			if err != nil {
				return fmt.Errorf("cannot create validation webhook service: %v", err)
			}

			if options.Mux == nil {
				if liveness != nil {
					validationLivenessProbe := probe.NewProbe()
					validationLivenessProbe.SetAvailable(nil)
					validationLivenessProbe.RegisterProbe(liveness, "validationLiveness")

					go func() {
						<-stop
						validationLivenessProbe.SetAvailable(errors.New("stopped"))
					}()
				}

				if readiness != nil {
					validationReadinessProbe := probe.NewProbe()
					validationReadinessProbe.SetAvailable(errors.New("init"))
					validationReadinessProbe.RegisterProbe(readiness, "validationReadiness")

					go func() {
						monitorReadiness(stop, options.Port, validationReadinessProbe)

						validationReadinessProbe.SetAvailable(errors.New("stopped"))
					}()
				}
			}

			go s.Run(stop)
			return nil
		},
		// stop
		func() {
			if atomic.CompareAndSwapInt32(&stopped, 0, 1) {
				close(stop)
			}
		})
}

func NewValidationController(options controller.Options, kubeconfig string) process.Component {
	stop := make(chan struct{})
	var stopped int32 // atomic
	return process.ComponentFromFns(
		// start
		func() error {
			log.Infof("Galley validation controller started with \n%s", &options)

			restConfig, err := newInterfaces(kubeconfig)
			if err != nil {
				return err
			}
			client, err := restConfig.KubeClient()
			if err != nil {
				return err
			}
			dynamicInterface, err := restConfig.DynamicInterface()
			if err != nil {
				return err
			}
			c, err := controller.New(options, client, dynamicInterface)
			if err != nil {
				return err
			}

			go c.Start(stop)
			return nil
		},
		// stop
		func() {
			if atomic.CompareAndSwapInt32(&stopped, 0, 1) {
				close(stop)
			}
		})
}
