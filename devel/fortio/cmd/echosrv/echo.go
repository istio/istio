// Copyright 2017 Istio Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

// Adapted from istio/proxy/test/backend/echo with error handling and
// concurrency fixes and making it as low overhead as possible
// (no std output by default)

package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"istio.io/istio/devel/fortio"
)

var (
	port = flag.Int("port", 8080, "default http port")
)

// debugHandler returns debug/useful info
func debugHandler(w http.ResponseWriter, r *http.Request) {
	fortio.LogVf("%v %v %v %v", r.Method, r.URL, r.Proto, r.RemoteAddr)
	var buf bytes.Buffer
	buf.WriteString("Φορτίο version ")
	buf.WriteString(fortio.Version)
	buf.WriteString(" echo debug server on ")
	hostname, _ := os.Hostname()
	buf.WriteString(hostname)
	buf.WriteString(" - request from ")
	buf.WriteString(r.RemoteAddr)
	buf.WriteString("\n\n")
	buf.WriteString(r.Method)
	buf.WriteByte(' ')
	buf.WriteString(r.URL.String())
	buf.WriteByte(' ')
	buf.WriteString(r.Proto)
	buf.WriteByte(' ')
	buf.WriteString("\n\nheaders:\n\n")
	for name, headers := range r.Header {
		buf.WriteString(name)
		buf.WriteString(": ")
		first := true
		for _, h := range headers {
			if !first {
				buf.WriteByte(',')
			}
			buf.WriteString(h)
			first = false
		}
		buf.WriteByte('\n')
	}
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		fortio.Errf("Error reading %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	buf.WriteString("\nbody:\n\n")
	buf.WriteString(fortio.DebugSummary(data, 512))
	buf.WriteByte('\n')
	if r.FormValue("env") == "dump" {
		buf.WriteString("\nenvironment:\n\n")
		for _, v := range os.Environ() {
			buf.WriteString(v)
			buf.WriteByte('\n')
		}
	}
	w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	if _, err = w.Write(buf.Bytes()); err != nil {
		fortio.Errf("Error writing response %v to %v", err, r.RemoteAddr)
	}
}

func main() {
	flag.Parse()

	fmt.Printf("Fortio %s echo server listening on port %v\n", fortio.Version, *port)

	http.HandleFunc("/debug", debugHandler)
	http.HandleFunc("/", fortio.EchoHandler)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", *port), nil); err != nil {
		fmt.Println("Error starting server", err)
	}
}
