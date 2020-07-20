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
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/common/response"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/pkg/log"
)

const (
	readyTimeout  = 10 * time.Second
	readyInterval = 2 * time.Second
)

var (
	webSocketUpgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			// allow all connections by default
			return true
		},
	}
)

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

func (s *httpInstance) Start(onReady OnReadyFunc) error {
	h2s := &http2.Server{}
	s.server = &http.Server{
		Handler: h2c.NewHandler(&httpHandler{
			Config: s.Config,
		}, h2s),
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
			return fmt.Errorf("could not load TLS keys: %v", err)
		}
		config := &tls.Config{Certificates: []tls.Certificate{cert}}
		// Listen on the given port and update the port if it changed from what was passed in.
		listener, port, err = listenOnPortTLS(s.Port.Port, config)
		// Store the actual listening port back to the argument.
		s.Port.Port = port
	} else {
		// Listen on the given port and update the port if it changed from what was passed in.
		listener, port, err = listenOnPort(s.Port.Port)
		// Store the actual listening port back to the argument.
		s.Port.Port = port
	}

	if err != nil {
		return err
	}

	if s.isUDS() {
		fmt.Printf("Listening HTTP/1.1 on %v\n", s.UDSServer)
	} else if s.Port.TLS {
		s.server.Addr = fmt.Sprintf(":%d", port)
		fmt.Printf("Listening HTTPS/1.1 on %v\n", port)
	} else {
		s.server.Addr = fmt.Sprintf(":%d", port)
		fmt.Printf("Listening HTTP/1.1 on %v\n", port)
	}

	// Start serving HTTP traffic.
	go func() {
		_ = s.server.Serve(listener)
	}()

	// Notify the WaitGroup once the port has transitioned to ready.
	go s.awaitReady(onReady, port)

	return nil
}

func (s *httpInstance) isUDS() bool {
	return s.UDSServer != ""
}

func (s *httpInstance) awaitReady(onReady OnReadyFunc, port int) {
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
		url = fmt.Sprintf("https://127.0.0.1:%d", port)
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	} else {
		url = fmt.Sprintf("http://127.0.0.1:%d", port)
	}

	err := retry.UntilSuccess(func() error {
		resp, err := client.Get(url)
		if err != nil {
			return err
		}

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
		log.Errorf("readiness failed for endpoint %s: %v", url, err)
	} else {
		log.Infof("ready for HTTP endpoint %s", url)
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
	log.Infof("HTTP Request:\n  Method: %s\n  URL: %v,\n  Host: %s\n  Headers: %v",
		r.Method, r.URL, r.Host, r.Header)
	defer common.Metrics.HTTPRequests.With(common.PortLabel.Value(strconv.Itoa(h.Port.Port))).Increment()
	if !h.IsServerReady() {
		// Handle readiness probe failure.
		log.Infof("HTTP service not ready, returning 503")
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	if common.IsWebSocketRequest(r) {
		h.webSocketEcho(w, r)
	} else {
		h.echo(w, r)
	}
}

// nolint: interfacer
func writeError(out *bytes.Buffer, msg string) {
	log.Warn(msg)
	_, _ = out.WriteString(msg + "\n")
}

func (h *httpHandler) echo(w http.ResponseWriter, r *http.Request) {
	body := bytes.Buffer{}

	if err := r.ParseForm(); err != nil {
		writeError(&body, "ParseForm() error: "+err.Error())
	}

	// If the request has form ?headers=name:value[,name:value]* return those headers in response
	if err := setHeaderResponseFromHeaders(r, w); err != nil {
		writeError(&body, "response headers error: "+err.Error())
	}

	// If the request has form ?codes=code[:chance][,code[:chance]]* return those codes, rather than 200
	// For example, ?codes=500:1,200:1 returns 500 1/2 times and 200 1/2 times
	// For example, ?codes=500:90,200:10 returns 500 90% of times and 200 10% of times
	if err := setResponseFromCodes(r, w); err != nil {
		writeError(&body, "codes error: "+err.Error())
	}

	h.addResponsePayload(r, &body)

	w.Header().Set("Content-Type", "application/text")
	if _, err := w.Write(body.Bytes()); err != nil {
		log.Warna(err)
	}
	log.Infof("Response Headers: %+v", w.Header())
}

func (h *httpHandler) webSocketEcho(w http.ResponseWriter, r *http.Request) {
	// adapted from https://github.com/gorilla/websocket/blob/master/examples/echo/server.go
	// First send upgrade headers
	c, err := webSocketUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Warn("websocket-echo upgrade failed: " + err.Error())
		return
	}

	defer func() { _ = c.Close() }()

	// ping
	mt, message, err := c.ReadMessage()
	if err != nil {
		log.Warn("websocket-echo read failed: " + err.Error())
		return
	}

	body := bytes.Buffer{}
	h.addResponsePayload(r, &body)
	body.Write(message)

	writeField(&body, response.StatusCodeField, response.StatusCodeOK)

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

	writeField(body, response.ServiceVersionField, h.Version)
	writeField(body, response.ServicePortField, port)
	writeField(body, response.HostField, r.Host)
	writeField(body, response.URLField, r.URL.String())
	writeField(body, response.ClusterField, h.Cluster)

	writeField(body, "Method", r.Method)
	writeField(body, "Proto", r.Proto)
	writeField(body, "RemoteAddr", r.RemoteAddr)

	keys := []string{}
	for k := range r.Header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		values := r.Header[key]
		for _, value := range values {
			writeField(body, response.Field(key), value)
		}
	}

	if hostname, err := os.Hostname(); err == nil {
		writeField(body, response.HostnameField, hostname)
	}
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
		response.Header().Set(name, value)
	}
	return nil
}

func setResponseFromCodes(request *http.Request, response http.ResponseWriter) error {
	responseCodes := request.FormValue("codes")

	codes, err := validateCodes(responseCodes)
	if err != nil {
		return err
	}

	// Choose a random "slice" from a pie
	var totalSlices = 0
	for _, flavor := range codes {
		totalSlices += flavor.slices
	}
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

	log.Infof("Response status code: %d", responseCode)
	response.WriteHeader(responseCode)
	return nil
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
