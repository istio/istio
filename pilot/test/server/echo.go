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

// An example implementation of an echo backend.
//
// To test flaky HTTP service recovery, this backend has a special features.
// If the "?codes=" query parameter is used it will return HTTP response codes other than 200
// according to a probability distribution.
// For example, ?codes=500:90,200:10 returns 500 90% of times and 200 10% of times
// For example, ?codes=500:1,200:1 returns 500 50% of times and 200 50% of times
// For example, ?codes=501:999,401:1 returns 500 99.9% of times and 401 0.1% of times.
// For example, ?codes=500,200 returns 500 50% of times and 200 50% of times

package main

import (
	"bytes"
	"log"
	"net/http"
	"strconv"

	"github.com/gorilla/websocket"
	flag "github.com/spf13/pflag"

	"istio.io/fortio/fgrpc"
	"istio.io/fortio/fhttp"
)

var (
	ports     []int
	grpcPorts []int

	crt, key string
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// allow all connections by default
		return true
	},
} //defaults

func init() {
	flag.IntSliceVar(&ports, "port", []int{8080}, "HTTP/1.1 ports")
	flag.IntSliceVar(&grpcPorts, "grpc", []int{7070}, "GRPC ports")
	flag.StringVar(&crt, "crt", "", "gRPC TLS server-side certificate")
	flag.StringVar(&key, "key", "", "gRPC TLS server-side key")
}

func serveHTTPWebsocket(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("testwebsocket") != "" {
		webSocketEcho(w, r)
		return
	}
	fhttp.EchoHandler(w, r)
}

func webSocketEcho(w http.ResponseWriter, r *http.Request) {
	body := bytes.Buffer{}

	// adapted from https://github.com/gorilla/websocket/blob/master/examples/echo/server.go
	// First send upgrade headers
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("websocket-echo upgrade failed:", err)
		return
	}

	// nolint: errcheck
	defer c.Close()

	// ping
	mt, message, err := c.ReadMessage()
	if err != nil {
		log.Println("websocket-echo read failed:", err)
		return
	}

	// pong
	body.Write(message)
	err = c.WriteMessage(mt, body.Bytes())
	if err != nil {
		log.Println("websocket-echo write failed:", err)
		return
	}
}

func main() {
	flag.Parse()
	for _, port := range ports {
		portStr := strconv.Itoa(port)
		mux, _ := fhttp.HTTPServer("pilot-test "+portStr, portStr)
		mux.HandleFunc("/", serveHTTPWebsocket)
		mux.HandleFunc("/debug", fhttp.DebugHandler)
	}
	for _, grpcPort := range grpcPorts {
		fgrpc.PingServer(strconv.Itoa(grpcPort), "")
	}
}
