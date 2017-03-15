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

package main

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	flag "github.com/spf13/pflag"
)

var (
	ports   []string
	version string
)

func init() {
	flag.StringArrayVar(&ports, "port", []string{"8080"}, "HTTP/1.1 ports")
	flag.StringVar(&version, "version", "", "Version string")
}

func httpHandler(w http.ResponseWriter, r *http.Request) {
	body := bytes.Buffer{}
	body.WriteString("ServiceVersion=" + version + "\n")
	body.WriteString("Method=" + r.Method + "\n")
	body.WriteString("URL=" + r.URL.String() + "\n")
	body.WriteString("Proto=" + r.Proto + "\n")
	body.WriteString("RemoteAddr=" + r.RemoteAddr + "\n")
	body.WriteString("Host=" + r.Host + "\n")
	for name, headers := range r.Header {
		for _, h := range headers {
			body.WriteString(fmt.Sprintf("%v=%v\n", name, h))
		}
	}

	w.Header().Set("Content-Type", "application/text")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body.Bytes()); err != nil {
		log.Println(err.Error())
	}
}

func run(addr string) {
	fmt.Printf("Listening HTTP1.1 on %v\n", addr)
	if err := http.ListenAndServe(":"+addr, nil); err != nil {
		log.Println(err.Error())
	}
}

func main() {
	flag.Parse()
	http.HandleFunc("/", httpHandler)
	for _, port := range ports {
		go run(port)
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
}
