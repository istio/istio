// Copyright 2017 Istio Authors
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

package fhttp // import "fortio.org/fortio/fhttp"

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	// get /debug/pprof endpoints on a mux through SetupPPROF
	"net/http/pprof"

	"fortio.org/fortio/fnet"
	"fortio.org/fortio/log"
	"fortio.org/fortio/version"
)

// -- Echo Server --

var (
	// Start time of the server (used in debug handler for uptime).
	startTime time.Time
	// EchoRequests is the number of request received. Only updated in Debug mode.
	EchoRequests int64
)

// EchoHandler is an http server handler echoing back the input.
func EchoHandler(w http.ResponseWriter, r *http.Request) {
	if log.LogVerbose() {
		LogRequest(r, "Echo") // will also print headers
	}
	data, err := ioutil.ReadAll(r.Body) // must be done before calling FormValue
	if err != nil {
		log.Errf("Error reading %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Debugf("Read %d", len(data))
	dur := generateDelay(r.FormValue("delay"))
	if dur > 0 {
		log.LogVf("Sleeping for %v", dur)
		time.Sleep(dur)
	}
	statusStr := r.FormValue("status")
	var status int
	if statusStr != "" {
		status = generateStatus(statusStr)
	} else {
		status = http.StatusOK
	}
	if log.LogDebug() {
		// TODO: this easily lead to contention - use 'thread local'
		rqNum := atomic.AddInt64(&EchoRequests, 1)
		log.Debugf("Request # %v", rqNum)
	}
	if r.FormValue("close") != "" {
		log.Debugf("Adding Connection:close / will close socket")
		w.Header().Set("Connection", "close")
	}
	// process header(s) args, must be before size to compose properly
	for _, hdr := range r.Form["header"] {
		log.LogVf("Adding requested header %s", hdr)
		if len(hdr) == 0 {
			continue
		}
		s := strings.SplitN(hdr, ":", 2)
		if len(s) != 2 {
			log.Errf("invalid extra header '%s', expecting Key: Value", hdr)
			continue
		}
		w.Header().Add(s[0], s[1])
	}
	size := generateSize(r.FormValue("size"))
	if size >= 0 {
		log.LogVf("Writing %d size with %d status", size, status)
		writePayload(w, status, size)
		return
	}
	// echo back the Content-Type and Content-Length in the response
	for _, k := range []string{"Content-Type", "Content-Length"} {
		if v := r.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	w.WriteHeader(status)
	if _, err = w.Write(data); err != nil {
		log.Errf("Error writing response %v to %v", err, r.RemoteAddr)
	}
}

func writePayload(w http.ResponseWriter, status int, size int) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(size))
	w.WriteHeader(status)
	n, err := w.Write(fnet.Payload[:size])
	if err != nil || n != size {
		log.Errf("Error writing payload of size %d: %d %v", size, n, err)
	}
}

func closingServer(listener net.Listener) error {
	var err error
	for {
		var c net.Conn
		c, err = listener.Accept()
		if err != nil {
			log.Errf("Accept error in dummy server %v", err)
			break
		}
		log.LogVf("Got connection from %v, closing", c.RemoteAddr())
		err = c.Close()
		if err != nil {
			log.Errf("Close error in dummy server %v", err)
			break
		}
	}
	return err
}

// HTTPServer creates an http server named name on address/port port.
// Port can include binding address and/or be port 0.
func HTTPServer(name string, port string) (*http.ServeMux, net.Addr) {
	m := http.NewServeMux()
	s := &http.Server{
		Handler: m,
	}
	listener, addr := fnet.Listen(name, port)
	if listener == nil {
		return nil, nil // error already logged
	}
	go func() {
		err := s.Serve(listener)
		if err != nil {
			log.Fatalf("Unable to serve %s on %s: %v", name, addr.String(), err)
		}
	}()
	return m, addr
}

// DynamicHTTPServer listens on an available port, sets up an http or a closing
// server simulating an https server (when closing is true) server on it and
// returns the listening port and mux to which one can attach handlers to.
// Note: in a future version of istio, the closing will be actually be secure
// on/off and create an https server instead of a closing server.
// As this is a dynamic tcp socket server, the address is TCP.
func DynamicHTTPServer(closing bool) (*http.ServeMux, *net.TCPAddr) {
	if !closing {
		mux, addr := HTTPServer("dynamic", "0")
		return mux, addr.(*net.TCPAddr)
	}
	// Note: we actually use the fact it's not supported as an error server for tests - need to change that
	log.Errf("Secure setup not yet supported. Will just close incoming connections for now")
	listener, addr := fnet.Listen("closing server", "0")
	//err = http.ServeTLS(listener, nil, "", "") // go 1.9
	go func() {
		err := closingServer(listener)
		if err != nil {
			log.Fatalf("Unable to serve closing server on %s: %v", addr.String(), err)
		}
	}()
	return nil, addr.(*net.TCPAddr)
}

/*
// DebugHandlerTemplate returns debug/useful info on the http requet.
// slower heavier but nicer source code version of DebugHandler
func DebugHandlerTemplate(w http.ResponseWriter, r *http.Request) {
	log.LogVf("%v %v %v %v", r.Method, r.URL, r.Proto, r.RemoteAddr)
	hostname, _ := os.Hostname()
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Errf("Error reading %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Note: this looks nicer but is about 2x slower / less qps / more cpu and 25% bigger executable than doing the writes oneself:
	const templ = `Φορτίο version {{.Version}} echo debug server on {{.Hostname}} - request from {{.R.RemoteAddr}}

{{.R.Method}} {{.R.URL}} {{.R.Proto}}

headers:

{{ range $name, $vals := .R.Header }}{{range $val := $vals}}{{$name}}: {{ $val }}
{{end}}{{end}}
body:

{{.Body}}
{{if .DumpEnv}}
environment:
{{ range $idx, $e := .Env }}
{{$e}}{{end}}
{{end}}`
	t := template.Must(template.New("debugOutput").Parse(templ))
	err = t.Execute(w, &struct {
		R        *http.Request
		Hostname string
		Version  string
		Body     string
		DumpEnv  bool
		Env      []string
	}{r, hostname, Version, DebugSummary(data, 512), r.FormValue("env") == "dump", os.Environ()})
	if err != nil {
		Critf("Template execution failed: %v", err)
	}
	w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
}
*/

// DebugHandler returns debug/useful info to http client.
func DebugHandler(w http.ResponseWriter, r *http.Request) {
	if log.LogVerbose() {
		LogRequest(r, "Debug")
	}
	var buf bytes.Buffer
	buf.WriteString("Φορτίο version ")
	buf.WriteString(version.Long())
	buf.WriteString(" echo debug server up for ")
	buf.WriteString(fmt.Sprint(RoundDuration(time.Since(startTime))))
	buf.WriteString(" on ")
	hostname, _ := os.Hostname() // nolint: gas
	buf.WriteString(hostname)
	buf.WriteString(" - request from ")
	buf.WriteString(r.RemoteAddr)
	buf.WriteString("\n\n")
	buf.WriteString(r.Method)
	buf.WriteByte(' ')
	buf.WriteString(r.URL.String())
	buf.WriteByte(' ')
	buf.WriteString(r.Proto)
	buf.WriteString("\n\nheaders:\n\n")
	// Host is removed from headers map and put here (!)
	buf.WriteString("Host: ")
	buf.WriteString(r.Host)

	var keys []string
	for k := range r.Header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, name := range keys {
		buf.WriteByte('\n')
		buf.WriteString(name)
		buf.WriteString(": ")
		first := true
		headers := r.Header[name]
		for _, h := range headers {
			if !first {
				buf.WriteByte(',')
			}
			buf.WriteString(h)
			first = false
		}
	}
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Errf("Error reading %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	buf.WriteString("\n\nbody:\n\n")
	buf.WriteString(DebugSummary(data, 512))
	buf.WriteByte('\n')
	if r.FormValue("env") == "dump" {
		buf.WriteString("\nenvironment:\n\n")
		for _, v := range os.Environ() {
			buf.WriteString(v)
			buf.WriteByte('\n')
		}
	}
	w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
	if _, err = w.Write(buf.Bytes()); err != nil {
		log.Errf("Error writing response %v to %v", err, r.RemoteAddr)
	}
}

// CacheOn sets the header for indefinite caching.
func CacheOn(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "max-age=365000000, immutable")
}

// Serve starts a debug / echo http server on the given port.
// Returns the mux and addr where the listening socket is bound.
// The .Port can be retrieved from it when requesting the 0 port as
// input for dynamic http server.
func Serve(port, debugPath string) (*http.ServeMux, net.Addr) {
	startTime = time.Now()
	mux, addr := HTTPServer("echo", port)
	if addr == nil {
		return nil, nil // error already logged
	}
	if debugPath != "" {
		mux.HandleFunc(debugPath, DebugHandler)
	}
	mux.HandleFunc("/", EchoHandler)
	return mux, addr
}

// ServeTCP is Serve() but restricted to TCP (return address is assumed
// to be TCP - will panic for unix domain)
func ServeTCP(port, debugPath string) (*http.ServeMux, *net.TCPAddr) {
	mux, addr := Serve(port, debugPath)
	if addr == nil {
		return nil, nil // error already logged
	}
	return mux, addr.(*net.TCPAddr)
}

// -- formerly in ui handler

// SetupPPROF add pprof to the mux (mirror the init() of http pprof).
func SetupPPROF(mux *http.ServeMux) {
	mux.HandleFunc("/debug/pprof/", LogAndCall("pprof:index", pprof.Index))
	mux.HandleFunc("/debug/pprof/cmdline", LogAndCall("pprof:cmdline", pprof.Cmdline))
	mux.HandleFunc("/debug/pprof/profile", LogAndCall("pprof:profile", pprof.Profile))
	mux.HandleFunc("/debug/pprof/symbol", LogAndCall("pprof:symbol", pprof.Symbol))
	mux.HandleFunc("/debug/pprof/trace", LogAndCall("pprof:trace", pprof.Trace))
}

// -- Fetch er (simple http proxy) --

// FetcherHandler is the handler for the fetcher/proxy.
func FetcherHandler(w http.ResponseWriter, r *http.Request) {
	LogRequest(r, "Fetch (prefix stripped)")
	hj, ok := w.(http.Hijacker)
	if !ok {
		log.Critf("hijacking not supported")
		return
	}
	conn, _, err := hj.Hijack()
	if err != nil {
		log.Errf("hijacking error %v", err)
		return
	}
	// Don't forget to close the connection:
	defer conn.Close() // nolint: errcheck
	// Stripped prefix gets replaced by ./ - sometimes...
	url := strings.TrimPrefix(r.URL.String(), "./")
	opts := NewHTTPOptions("http://" + url)
	opts.HTTPReqTimeOut = 5 * time.Minute
	OnBehalfOf(opts, r)
	client := NewClient(opts)
	if client == nil {
		return // error logged already
	}
	_, data, _ := client.Fetch()
	_, err = conn.Write(data)
	if err != nil {
		log.Errf("Error writing fetched data to %v: %v", r.RemoteAddr, err)
	}
	client.Close()
}

// -- Redirection to https feature --

// RedirectToHTTPSHandler handler sends a redirect to same URL with https.
func RedirectToHTTPSHandler(w http.ResponseWriter, r *http.Request) {
	dest := "https://" + r.Host + r.URL.String()
	LogRequest(r, "Redirecting to "+dest)
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// RedirectToHTTPS Sets up a redirector to https on the given port.
// (Do not create a loop, make sure this is addressed from an ingress)
func RedirectToHTTPS(port string) net.Addr {
	m, a := HTTPServer("https redirector", port)
	if m == nil {
		return nil // error already logged
	}
	m.HandleFunc("/", RedirectToHTTPSHandler)
	return a
}

// LogRequest logs the incoming request, including headers when loglevel is verbose
func LogRequest(r *http.Request, msg string) {
	log.Infof("%s: %v %v %v %v (%s)", msg, r.Method, r.URL, r.Proto, r.RemoteAddr,
		r.Header.Get("X-Forwarded-Proto"))
	if log.LogVerbose() {
		for name, headers := range r.Header {
			for _, h := range headers {
				log.LogVf("Header %v: %v\n", name, h)
			}
		}
	}
}

// LogAndCall wrapps an HTTP handler to log the request first.
func LogAndCall(msg string, hf http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		LogRequest(r, msg)
		hf(w, r)
	})
}
