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
	"strings"

	sec_model "istio.io/istio/pilot/pkg/security/model"
	istiolog "istio.io/istio/pkg/log"
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
		istiolog.Infof("HTTPS port is disabled, multiplexing webhooks on the httpAddr %v", args.ServerOptions.HTTPAddr)
		return
	}

	tlsConfig := &tls.Config{
		GetCertificate: s.getIstiodCertificate,
		MinVersion:     tls.VersionTLS12,
		CipherSuites:   args.ServerOptions.TLSOptions.CipherSuites,
	}
	// Compliance for control plane validation and injection webhook server.
	sec_model.EnforceGoCompliance(tlsConfig)

	istiolog.Info("initializing secure webhook server for istiod webhooks")
	// create the https server for hosting the k8s injectionWebhook handlers.
	s.httpsMux = http.NewServeMux()
	s.httpsServer = &http.Server{
		Addr:      args.ServerOptions.HTTPSAddr,
		ErrorLog:  log.New(&httpServerErrorLogWriter{}, "", 0),
		Handler:   s.httpsMux,
		TLSConfig: tlsConfig,
	}

	// register istiodReadyHandler on the httpsMux so that readiness can also be checked remotely
	s.httpsMux.HandleFunc("/ready", s.istiodReadyHandler)
}
