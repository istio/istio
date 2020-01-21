// Copyright 2020 Istio Authors
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
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"istio.io/pkg/filewatcher"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/webhooks/validation/server"
)

const (
	// debounce file watcher events to minimize noise in logs
	watchDebounceDelay = 100 * time.Millisecond
)

func (s *Server) getWebhookCertificate(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
	s.webhookCertMu.Lock()
	defer s.webhookCertMu.Unlock()
	return s.webhookCert, nil
}

func (s *Server) initHTTPSWebhookServer(args *PilotArgs) error {
	if features.IstiodService.Get() == "" {
		log.Info("Not starting HTTPS webhook server: istiod address not set")
		return nil
	}

	log.Info("Setting up HTTPS webhook server for istiod webhooks")

	// create the https server for hosting the k8s injectionWebhook handlers.
	s.httpsMux = http.NewServeMux()
	s.httpsServer = &http.Server{
		Addr:    args.DiscoveryOptions.HTTPSAddr,
		Handler: s.httpsMux,
		TLSConfig: &tls.Config{
			GetCertificate: s.getWebhookCertificate,
		},
	}

	// load the cert/key and setup a persistent watch for updates.
	cert, err := server.ReloadCertkey(dnsCertFile, dnsKeyFile)
	if err != nil {
		return err
	}
	s.webhookCert = cert
	keyCertWatcher := filewatcher.NewWatcher()
	for _, file := range []string{dnsCertFile, dnsKeyFile} {
		if err := keyCertWatcher.Add(file); err != nil {
			return fmt.Errorf("could not watch %v: %v", file, err)
		}
	}
	s.addStartFunc(func(stop <-chan struct{}) error {
		go func() {
			var keyCertTimerC <-chan time.Time
			for {
				select {
				case <-keyCertTimerC:
					keyCertTimerC = nil

					cert, err := server.ReloadCertkey(dnsCertFile, dnsKeyFile)
					if err != nil {
						return // error logged and metric reported by server.ReloadCertKey
					}

					s.webhookCertMu.Lock()
					s.webhookCert = cert
					s.webhookCertMu.Unlock()
				case <-keyCertWatcher.Events(dnsCertFile):
					if keyCertTimerC == nil {
						keyCertTimerC = time.After(watchDebounceDelay)
					}
				case <-keyCertWatcher.Events(dnsKeyFile):
					if keyCertTimerC == nil {
						keyCertTimerC = time.After(watchDebounceDelay)
					}
				case <-keyCertWatcher.Errors(dnsCertFile):
					log.Errorf("error watching %v: %v", dnsCertFile, err)
				case <-keyCertWatcher.Errors(dnsKeyFile):
					log.Errorf("error watching %v: %v", dnsKeyFile, err)
				case <-stop:
					return
				}
			}
		}()
		return nil
	})

	// setup our readiness handler and the corresponding client we'll use later to check it with.
	s.httpsMux.HandleFunc(server.HTTPSHandlerReadyPath, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	s.httpsReadyClient = &http.Client{
		Timeout: time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	return nil
}

func (s *Server) checkHTTPSWebhookServerReadiness() int {
	req := &http.Request{
		Method: http.MethodGet,
		URL: &url.URL{
			Scheme: "https",
			Host:   s.httpsServer.Addr,
			Path:   server.HTTPSHandlerReadyPath,
		},
	}

	response, err := s.httpsReadyClient.Do(req)
	if err != nil {
		return http.StatusInternalServerError
	}
	defer response.Body.Close()

	return response.StatusCode
}
