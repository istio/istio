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
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync/atomic"

	log "istio.io/istio/devel/fortio"
)

var (
	port     = flag.Int("port", 8080, "default http port")
	requests int64
)

func handler(w http.ResponseWriter, r *http.Request) {
	log.LogV("%v %v %v %v", r.Method, r.URL, r.Proto, r.RemoteAddr)
	if log.DbgOn() {
		for name, headers := range r.Header {
			for _, h := range headers {
				fmt.Printf("%v: %v\n", name, h)
			}
		}
	}
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Err("Error reading %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// echo back the Content-Type and Content-Length in the response
	for _, k := range []string{"Content-Type", "Content-Length"} {
		if v := r.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	w.WriteHeader(http.StatusOK)
	if _, err = w.Write(data); err != nil {
		log.Err("Error writing response %v to %v", err, r.RemoteAddr)
	}
	if log.DbgOn() {
		// TODO: this easily lead to contention - use 'thread local'
		rqNum := atomic.AddInt64(&requests, 1)
		log.Dbg("Requests: %v", rqNum)
	}
}

func main() {
	flag.Parse()

	fmt.Printf("Fortio echo server listening on port %v\n", *port)

	http.HandleFunc("/", handler)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", *port), nil); err != nil {
		fmt.Println("Error starting server", err)
	}
}
