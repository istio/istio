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

package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

var accessLogger = log.New(os.Stdout, "", 0)

type ServerConfig struct {
	Name                    string
	Namespace               string
	AlwaysJSON              bool
	Cert                    certConfig
	Addr                    string
	ClientAuth              tls.ClientAuthType
	ClientAuthHumanReadable string
	LogPlainText            bool
}

type certConfig struct {
	certFile string
	keyFile  string
	caFile   string
}

type ClientCertInfo struct {
	Subject   string
	Issuer    string
	Serial    string
	DNSNames  []string
	NotBefore time.Time
	NotAfter  time.Time
}

type ResponseBody struct {
	Name      string
	Namespace string
	Client    ClientCertInfo
	Headers   map[string][]string
}

type AccessLog struct {
	Timestamp     time.Time
	Method        string
	Path          string
	ClientIP      string
	ClientSubject string
}

func initServerConfig() *ServerConfig {
	serverConfig := &ServerConfig{
		Name:      os.Getenv("SERVER_NAME"),
		Namespace: os.Getenv("SERVER_NAMESPACE"),
	}

	certFile, certFileExists := os.LookupEnv("SERVER_CERT")
	if !certFileExists {
		certFile = "server.crt"
	}
	serverConfig.Cert.certFile = certFile

	keyFile, keyFileExists := os.LookupEnv("SERVER_KEY")
	if !keyFileExists {
		keyFile = "server.key"
	}
	serverConfig.Cert.keyFile = keyFile

	caFile, caFileExists := os.LookupEnv("CA_CERT")
	if !caFileExists {
		caFile = "ca.crt"
	}
	serverConfig.Cert.caFile = caFile

	addr, addrExists := os.LookupEnv("LISTEN_ADDR")
	if !addrExists {
		addr = ":8443"
	}
	serverConfig.Addr = addr

	serverConfig.AlwaysJSON = os.Getenv("ALWAYS_JSON") == "true"

	serverConfig.LogPlainText = os.Getenv("LOG_PLAINTEXT") == "true"

	serverConfig.ClientAuth = tls.RequireAndVerifyClientCert
	if os.Getenv("INSECURE_SKIP_CLIENT_VERIFY") == "true" {
		serverConfig.ClientAuth = tls.RequireAnyClientCert
	}

	serverConfig.ClientAuthHumanReadable = serverConfig.ClientAuth.String()

	return serverConfig
}

func main() {
	log.Println("Initializing mTLS server")
	serverConfig := initServerConfig()

	jsonCfg, marshalErr := json.MarshalIndent(serverConfig, "", "  ")
	if marshalErr != nil {
		log.Printf("failed to marshal server config: %v", marshalErr)
	} else {
		log.Printf("Server config: %s", jsonCfg)
	}

	// Load server certificate and key
	cert, err := tls.LoadX509KeyPair(
		serverConfig.Cert.certFile,
		serverConfig.Cert.keyFile,
	)
	if err != nil {
		log.Fatalf("failed to load server cert/key: %v", err)
	}

	// Load CA cert
	caCertPEM, err := os.ReadFile(serverConfig.Cert.caFile)
	if err != nil {
		log.Fatalf("failed to read CA cert: %v", err)
	}
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCertPEM) {
		log.Fatalf("failed to append CA cert")
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   serverConfig.ClientAuth,
		ClientCAs:    caCertPool,
		MinVersion:   tls.VersionTLS12,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/subject", serverConfig.serveSubject)
	mux.HandleFunc("/", serverConfig.serve)

	server := &http.Server{
		Addr:      serverConfig.Addr,
		TLSConfig: tlsConfig,
		Handler:   mux,
	}

	log.Printf("Starting mTLS server on %s", serverConfig.Addr)
	if err := server.ListenAndServeTLS("", ""); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

func (c *ServerConfig) getResponseBody(r *http.Request) (*ResponseBody, *AccessLog) {
	responseBody := &ResponseBody{
		Name:      c.Name,
		Namespace: c.Namespace,
	}
	accessLog := &AccessLog{
		Timestamp: time.Now(),
		Method:    r.Method,
		Path:      r.URL.Path,
		ClientIP:  r.RemoteAddr,
	}
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		clientCert := r.TLS.PeerCertificates[0]
		responseBody.Client = ClientCertInfo{
			Subject:   clientCert.Subject.String(),
			Issuer:    clientCert.Issuer.String(),
			Serial:    clientCert.SerialNumber.String(),
			DNSNames:  clientCert.DNSNames,
			NotBefore: clientCert.NotBefore,
			NotAfter:  clientCert.NotAfter,
		}
		accessLog.ClientSubject = responseBody.Client.Subject
	}
	responseBody.Headers = make(map[string][]string)
	for key, values := range r.Header {
		responseBody.Headers[key] = values
	}
	return responseBody, accessLog
}

func textResponseBody(responseBody *ResponseBody) string {
	if responseBody == nil {
		return ""
	}
	body := ""
	if responseBody.Name != "" && responseBody.Namespace != "" {
		body = fmt.Sprintf("Hello from mTLS server %s in namespace %s!\n", responseBody.Name, responseBody.Namespace)
	} else {
		body = "Hello from mTLS server!\n"
	}
	body += fmt.Sprintf("Client certificate subject: %s\n", responseBody.Client.Subject)
	body += fmt.Sprintf("Client certificate issuer: %s\n", responseBody.Client.Issuer)
	body += fmt.Sprintf("Client certificate serial number: %s\n", responseBody.Client.Serial)
	body += fmt.Sprintf("Client certificate DNS names: %v\n", responseBody.Client.DNSNames)
	body += fmt.Sprintf("Client certificate NotBefore: %v\n", responseBody.Client.NotBefore)
	body += fmt.Sprintf("Client certificate NotAfter: %v\n", responseBody.Client.NotAfter)
	return body
}

func (c *ServerConfig) serveSubject(w http.ResponseWriter, r *http.Request) {
	responseBody, accessLog := c.getResponseBody(r)
	defer c.logAccess(accessLog)
	w.Header().Set("mtls-client-subject", responseBody.Client.Subject)
	accept := r.Header.Get("Accept")
	if c.AlwaysJSON || strings.Contains(accept, "application/json") {
		w.Header().Set("Content-Type", "application/json")
		b, err := json.MarshalIndent(map[string]string{
			"subject": responseBody.Client.Subject,
		}, "", "  ")
		if err != nil {
			http.Error(w, "failed to marshal response", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
		return
	} // else text/plain

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(responseBody.Client.Subject + "\n"))
}

func (c *ServerConfig) serve(w http.ResponseWriter, r *http.Request) {
	responseBody, accessLog := c.getResponseBody(r)
	defer c.logAccess(accessLog)
	accept := r.Header.Get("Accept")
	if c.AlwaysJSON || strings.Contains(accept, "application/json") {
		w.Header().Set("Content-Type", "application/json")
		b, err := json.MarshalIndent(responseBody, "", "  ")
		if err != nil {
			http.Error(w, "failed to marshal response", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
		return
	} // else text/plain

	body := textResponseBody(responseBody)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

func (c *ServerConfig) logAccess(accessLog *AccessLog) {
	if c.LogPlainText {
		accessLogger.Printf("access %s %s %s %s %s",
			accessLog.Timestamp.Format(time.RFC3339),
			accessLog.Method,
			accessLog.Path,
			accessLog.ClientIP,
			accessLog.ClientSubject,
		)
	} else {
		a := map[string]any{
			"access": accessLog,
		}
		json, err := json.Marshal(a)
		if err != nil {
			log.Printf("failed to marshal access log: %v", err)
			return
		}
		accessLogger.Printf("%s", json)
	}
}
