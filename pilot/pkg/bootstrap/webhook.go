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
	"crypto/tls"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	istiolog "istio.io/pkg/log"
)

const (
	HTTPSHandlerReadyPath = "/httpsReady"
)

type httpServerErrorLogWriter struct{}

// Webhook http.Server.ErrorLog handler specifically to filter
// http: TLS handshake error from 127.0.0.1:<PORT>: EOF
// messages that occur when clients send RST while TLS handshake is still in progress.
// httpsReadyClient can trigger this periodically when multiple concurrent probes are hitting this endpoint.
func (*httpServerErrorLogWriter) Write(p []byte) (int, error) {
	m := strings.TrimSuffix(string(p), "\n")
	if strings.HasPrefix(m, "http: TLS handshake error") && strings.HasSuffix(m, ": EOF") {
		istiolog.Debug(m)
	} else {
		istiolog.Info(m)
	}
	return len(p), nil
}

// initSSecureWebhookServer handles initialization for the HTTPS webhook server.
// If https address is off the injection handlers will be registered on the main http endpoint, with
// TLS handled by a proxy/gateway in front of Istiod.
func (s *Server) initSecureWebhookServer(args *PilotArgs) {
	// create the https server for hosting the k8s injectionWebhook handlers.
	if args.ServerOptions.HTTPSAddr == "" {
		s.httpsMux = s.httpMux
		istiolog.Info("HTTPS port is disabled, multiplexing webhooks on the httpAddr ", args.ServerOptions.HTTPAddr)
		return
	}

	istiolog.Info("initializing secure webhook server for istiod webhooks")
	// create the https server for hosting the k8s injectionWebhook handlers.
	s.httpsMux = http.NewServeMux()
	s.httpsServer = &http.Server{
		Addr:     args.ServerOptions.HTTPSAddr,
		ErrorLog: log.New(&httpServerErrorLogWriter{}, "", 0),
		Handler:  s.httpsMux,
		TLSConfig: &tls.Config{
			GetCertificate: s.getIstiodCertificate,
			MinVersion:     tls.VersionTLS12,
			CipherSuites:   args.ServerOptions.TLSOptions.CipherSuits,
		},
	}

	// setup our readiness handler and the corresponding client we'll use later to check it with.
	s.httpsMux.HandleFunc(HTTPSHandlerReadyPath, func(w http.ResponseWriter, _ *http.Request) {
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
	s.addReadinessProbe("Secure Webhook Server", s.webhookReadyHandler)
}

func (s *Server) webhookReadyHandler() (bool, error) {
	req := &http.Request{
		Method: http.MethodGet,
		URL: &url.URL{
			Scheme: "https",
			Host:   s.httpsServer.Addr,
			Path:   HTTPSHandlerReadyPath,
		},
	}

	response, err := s.httpsReadyClient.Do(req)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()

	return response.StatusCode == http.StatusOK, nil
}
