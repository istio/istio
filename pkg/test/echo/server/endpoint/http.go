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

package endpoint

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/pires/go-proxyproto"
	"golang.org/x/net/http2"

	"istio.io/istio/pkg/h2c"
	"istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	readyTimeout  = 10 * time.Second
	readyInterval = 2 * time.Second
)

var webSocketUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// allow all connections by default
		return true
	},
}

var _ Instance = &httpInstance{}

type httpInstance struct {
	Config
	server *http.Server
}

func newHTTP(config Config) Instance {
	return &httpInstance{
		Config: config,
	}
}

func (s *httpInstance) GetConfig() Config {
	return s.Config
}

func (s *httpInstance) Start(onReady OnReadyFunc) error {
	h2s := &http2.Server{
		IdleTimeout: idleTimeout,
	}

	s.server = &http.Server{
		IdleTimeout: idleTimeout,
		Handler: h2c.NewHandler(&httpHandler{
			Config: s.Config,
		}, h2s),
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			return context.WithValue(ctx, ConnContextKey, c)
		},
	}

	var listener net.Listener
	var port int
	var err error
	if s.isUDS() {
		port = 0
		listener, err = listenOnUDS(s.UDSServer)
	} else if s.Port.TLS {
		cert, cerr := tls.LoadX509KeyPair(s.TLSCert, s.TLSKey)
		if cerr != nil {
			return fmt.Errorf("could not load TLS keys: %v", cerr)
		}
		caCert, err := os.ReadFile(s.TLSCACert)
		if err != nil {
			return fmt.Errorf("could not load TLS CA certificate: %v", err)
		}
		caCertPool := x509.NewCertPool()
		if ok := caCertPool.AppendCertsFromPEM(caCert); !ok {
			return fmt.Errorf("could not append TLS CA certificate")
		}
		nextProtos := []string{"h2", "http/1.1", "http/1.0"}
		if s.DisableALPN {
			nextProtos = nil
		}
		config := &tls.Config{
			Certificates: []tls.Certificate{cert},
			ClientCAs:    caCertPool,
			NextProtos:   nextProtos,
			GetConfigForClient: func(info *tls.ClientHelloInfo) (*tls.Config, error) {
				// There isn't a way to pass through all ALPNs presented by the client down to the
				// HTTP server to return in the response. However, for debugging, we can at least log
				// them at this level.
				epLog.Infof("TLS connection with alpn: %v", info.SupportedProtos)
				return nil, nil
			},
			MinVersion: tls.VersionTLS12,
		}
		if s.Port.RequireClientCert {
			config.ClientAuth = tls.RequireAndVerifyClientCert
		}
		// Listen on the given port and update the port if it changed from what was passed in.
		//nolint:ineffassign,staticcheck // not true, we check all branches for error conditions below
		listener, port, err = listenOnAddressTLS(s.ListenerIP, s.Port.Port, config)
		// Store the actual listening port back to the argument.
		s.Port.Port = port
	} else {
		// Listen on the given port and update the port if it changed from what was passed in.
		listener, port, err = listenOnAddress(s.ListenerIP, s.Port.Port)
		// Store the actual listening port back to the argument.
		s.Port.Port = port
	}

	// check error for all branches here!
	if err != nil {
		return err
	}

	if s.isUDS() {
		epLog.Infof("Listening HTTP/1.1 on %v\n", s.UDSServer)
	} else if s.Port.TLS {
		s.server.Addr = fmt.Sprintf(":%d", port)
		epLog.Infof("Listening HTTPS/1.1 on %v\n", port)
	} else {
		s.server.Addr = fmt.Sprintf(":%d", port)
		epLog.Infof("Listening HTTP/1.1 on %v\n", port)
	}

	if s.Port.ProxyProtocol {
		listener = &proxyproto.Listener{Listener: listener}
	}

	// Start serving HTTP traffic.
	go func() {
		err := s.server.Serve(listener)
		epLog.Warnf("Port %d listener terminated with error: %v", port, err)
	}()

	// Notify the WaitGroup once the port has transitioned to ready.
	go s.awaitReady(onReady, listener.Addr().String())

	return nil
}

func (s *httpInstance) isUDS() bool {
	return s.UDSServer != ""
}

func (s *httpInstance) awaitReady(onReady OnReadyFunc, address string) {
	defer onReady()

	client := http.Client{}
	var url string
	if s.isUDS() {
		url = "http://unix/" + s.UDSServer
		client.Transport = &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", s.UDSServer)
			},
		}
	} else if s.Port.TLS {
		url = fmt.Sprintf("https://%s", address)
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} // nolint: gosec // test only code
		if s.Port.RequireClientCert {
			clientCert, err := tls.LoadX509KeyPair(s.TLSCert, s.TLSKey)
			if err != nil {
				epLog.Errorf("could not load TLS keys: %v", err)
			}
			client.Transport.(*http.Transport).TLSClientConfig.Certificates = []tls.Certificate{clientCert}
			caCert, err := os.ReadFile(s.TLSCACert)
			if err != nil {
				epLog.Errorf("could not load TLS CA certificate: %v", err)
			}
			if client.Transport.(*http.Transport).TLSClientConfig.RootCAs == nil {
				client.Transport.(*http.Transport).TLSClientConfig.RootCAs = x509.NewCertPool()
			}
			if ok := client.Transport.(*http.Transport).TLSClientConfig.RootCAs.AppendCertsFromPEM(caCert); !ok {
				epLog.Errorf("could not append TLS CA certificate: %v", err)
			}
		}
	} else {
		url = fmt.Sprintf("http://%s", address)
	}

	err := retry.UntilSuccess(func() error {
		resp, err := client.Get(url)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		// The handler applies server readiness when handling HTTP requests. Since the
		// server won't become ready until all endpoints (including this one) report
		// ready, the handler will return 503. This means that the endpoint is now ready.
		if resp.StatusCode != http.StatusServiceUnavailable {
			return fmt.Errorf("unexpected status code %d", resp.StatusCode)
		}

		// Server is up now, we're ready.
		return nil
	}, retry.Timeout(readyTimeout), retry.Delay(readyInterval))

	if err != nil {
		epLog.Errorf("readiness failed for endpoint %s: %v", url, err)
	} else {
		epLog.Infof("ready for HTTP endpoint %s", url)
	}
}

func (s *httpInstance) Close() error {
	if s.server != nil {
		return s.server.Close()
	}
	return nil
}

type httpHandler struct {
	Config
}

// Imagine a pie of different flavors.
// The flavors are the HTTP response codes.
// The chance of a particular flavor is ( slices / sum of slices ).
type codeAndSlices struct {
	httpResponseCode int
	slices           int
}

func (h *httpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.ReportRequest()
	id := uuid.New()
	remoteAddr, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		epLog.Warnf("failed to get host from remote address: %s", err)
	}
	epLog.WithLabels("remoteAddr", remoteAddr, "method", r.Method, "url", r.URL, "host", r.Host, "headers", r.Header, "id", id).Infof("%v Request", r.Proto)
	if h.Port == nil {
		defer common.Metrics.HTTPRequests.With(common.PortLabel.Value("uds")).Increment()
	} else {
		defer common.Metrics.HTTPRequests.With(common.PortLabel.Value(strconv.Itoa(h.Port.Port))).Increment()
	}
	if !h.IsServerReady() {
		// Handle readiness probe failure.
		epLog.Infof("HTTP service not ready, returning 503")
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	if common.IsWebSocketRequest(r) {
		h.webSocketEcho(w, r)
	} else {
		h.echo(w, r, id)
	}
}

// nolint: interfacer
func writeError(out *bytes.Buffer, msg string) {
	epLog.Warn(msg)
	_, _ = out.WriteString(msg + "\n")
}

func (h *httpHandler) echo(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	body := bytes.Buffer{}

	if err := r.ParseForm(); err != nil {
		writeError(&body, "ParseForm() error: "+err.Error())
	}

	// If the request has form ?delay=[:duration] wait for duration
	// For example, ?delay=10s will cause the response to wait 10s before responding
	if err := delayResponse(r); err != nil {
		writeError(&body, "error delaying response error: "+err.Error())
	}

	// If the request has form ?headers=name:value[,name:value]* return those headers in response
	if err := setHeaderResponseFromHeaders(r, w); err != nil {
		writeError(&body, "response headers error: "+err.Error())
	}

	// If the request has form ?codes=code[:chance][,code[:chance]]* return those codes, rather than 200
	// For example, ?codes=500:1,200:1 returns 500 1/2 times and 200 1/2 times
	// For example, ?codes=500:90,200:10 returns 500 90% of times and 200 10% of times
	code, err := setResponseFromCodes(r, w)
	if err != nil {
		writeError(&body, "codes error: "+err.Error())
	}

	h.addResponsePayload(r, &body)

	w.Header().Set("Content-Type", "application/text")
	if _, err := w.Write(body.Bytes()); err != nil {
		epLog.Warn(err)
	}
	epLog.WithLabels("code", code, "headers", w.Header(), "id", id).Infof("%v Response", r.Proto)
}

func (h *httpHandler) webSocketEcho(w http.ResponseWriter, r *http.Request) {
	// adapted from https://github.com/gorilla/websocket/blob/master/examples/echo/server.go
	// First send upgrade headers
	c, err := webSocketUpgrader.Upgrade(w, r, nil)
	if err != nil {
		epLog.Warn("websocket-echo upgrade failed: " + err.Error())
		return
	}

	defer func() { _ = c.Close() }()

	// ping
	mt, message, err := c.ReadMessage()
	if err != nil {
		epLog.Warn("websocket-echo read failed: " + err.Error())
		return
	}

	body := bytes.Buffer{}
	h.addResponsePayload(r, &body)
	body.Write(message)

	echo.StatusCodeField.Write(&body, strconv.Itoa(http.StatusOK))

	// pong
	err = c.WriteMessage(mt, body.Bytes())
	if err != nil {
		writeError(&body, "websocket-echo write failed: "+err.Error())
		return
	}
}

// nolint: interfacer
func (h *httpHandler) addResponsePayload(r *http.Request, body *bytes.Buffer) {
	port := ""
	if h.Port != nil {
		port = strconv.Itoa(h.Port.Port)
	}

	echo.ServiceVersionField.Write(body, h.Version)
	echo.ServicePortField.Write(body, port)
	echo.HostField.Write(body, r.Host)
	// Use raw path, we don't want golang normalizing anything since we use this for testing purposes
	echo.URLField.Write(body, r.RequestURI)
	echo.ClusterField.WriteNonEmpty(body, h.Cluster)
	echo.IstioVersionField.WriteNonEmpty(body, h.IstioVersion)
	echo.NamespaceField.WriteNonEmpty(body, h.Namespace)

	echo.MethodField.Write(body, r.Method)
	echo.ProtocolField.Write(body, r.Proto)
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	echo.IPField.Write(body, ip)

	if r.TLS != nil {
		// Note: since this is the NegotiatedProtocol, it will be set to empty if the client sends an ALPN
		// not supported by the server (ie one of h2,http/1.1,http/1.0)
		echo.AlpnField.WriteNonEmpty(body, r.TLS.NegotiatedProtocol)
		echo.SNIField.WriteNonEmpty(body, r.TLS.ServerName)
		// If the client cert is present, write the subject to the response
		if len(r.TLS.PeerCertificates) > 0 {
			echo.ClientCertSubjectField.WriteNonEmpty(body, r.TLS.PeerCertificates[0].Subject.String())
			echo.ClientCertSerialNumberField.WriteNonEmpty(body, r.TLS.PeerCertificates[0].SerialNumber.String())
		}
	}

	if conn := GetConn(r); conn != nil {
		if p, ok := conn.(*proxyproto.Conn); ok && p.ProxyHeader() != nil {
			echo.ProxyProtocolField.Write(body, fmt.Sprint(p.ProxyHeader().Version))
		}
	}
	var keys []string
	for k := range r.Header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		values := r.Header[key]
		for _, value := range values {
			echo.RequestHeaderField.WriteKeyValue(body, key, value)
		}
	}

	if hostname, err := os.Hostname(); err == nil {
		echo.HostnameField.Write(body, hostname)
	}
}

func delayResponse(request *http.Request) error {
	d := request.FormValue("delay")
	if len(d) == 0 {
		return nil
	}

	t, err := time.ParseDuration(d)
	if err != nil {
		return err
	}
	time.Sleep(t)
	return nil
}

func setHeaderResponseFromHeaders(request *http.Request, response http.ResponseWriter) error {
	s := request.FormValue("headers")
	if len(s) == 0 {
		return nil
	}
	responseHeaders := strings.Split(s, ",")
	for _, responseHeader := range responseHeaders {
		parts := strings.Split(responseHeader, ":")
		// require name:value format
		if len(parts) != 2 {
			return fmt.Errorf("invalid %q (want name:value)", responseHeader)
		}
		name := parts[0]
		value := parts[1]
		// Avoid using .Set() to allow users to pass non-canonical forms
		response.Header()[name] = []string{value}
	}
	return nil
}

func setResponseFromCodes(request *http.Request, response http.ResponseWriter) (int, error) {
	responseCodes := request.FormValue("codes")

	codes, err := validateCodes(responseCodes)
	if err != nil {
		return 0, err
	}

	// Choose a random "slice" from a pie
	totalSlices := 0
	for _, flavor := range codes {
		totalSlices += flavor.slices
	}
	// nolint: gosec
	// Test only code
	slice := rand.Intn(totalSlices)

	// What flavor is that slice?
	responseCode := codes[len(codes)-1].httpResponseCode // Assume the last slice
	position := 0
	for n, flavor := range codes {
		if position > slice {
			responseCode = codes[n-1].httpResponseCode // No, use an earlier slice
			break
		}
		position += flavor.slices
	}

	response.WriteHeader(responseCode)
	return responseCode, nil
}

// codes must be comma-separated HTTP response code, colon, positive integer
func validateCodes(codestrings string) ([]codeAndSlices, error) {
	if codestrings == "" {
		// Consider no codes to be "200:1" -- return HTTP 200 100% of the time.
		codestrings = strconv.Itoa(http.StatusOK) + ":1"
	}

	aCodestrings := strings.Split(codestrings, ",")
	codes := make([]codeAndSlices, len(aCodestrings))

	for i, codestring := range aCodestrings {
		codeAndSlice, err := validateCodeAndSlices(codestring)
		if err != nil {
			return []codeAndSlices{{http.StatusBadRequest, 1}}, err
		}
		codes[i] = codeAndSlice
	}

	return codes, nil
}

// code must be HTTP response code
func validateCodeAndSlices(codecount string) (codeAndSlices, error) {
	flavor := strings.Split(codecount, ":")

	// Demand code or code:number
	if len(flavor) == 0 || len(flavor) > 2 {
		return codeAndSlices{http.StatusBadRequest, 9999},
			fmt.Errorf("invalid %q (want code or code:count)", codecount)
	}

	n, err := strconv.Atoi(flavor[0])
	if err != nil {
		return codeAndSlices{http.StatusBadRequest, 9999}, err
	}

	if n < http.StatusOK || n >= 600 {
		return codeAndSlices{http.StatusBadRequest, 9999},
			fmt.Errorf("invalid HTTP response code %v", n)
	}

	count := 1
	if len(flavor) > 1 {
		count, err = strconv.Atoi(flavor[1])
		if err != nil {
			return codeAndSlices{http.StatusBadRequest, 9999}, err
		}
		if count < 0 {
			return codeAndSlices{http.StatusBadRequest, 9999},
				fmt.Errorf("invalid count %v", count)
		}
	}

	return codeAndSlices{n, count}, nil
}

type contextKey struct {
	key string
}

var ConnContextKey = &contextKey{"http-conn"}

func GetConn(r *http.Request) net.Conn {
	v, ok := r.Context().Value(ConnContextKey).(net.Conn)
	if ok {
		return v
	}
	return nil
}
