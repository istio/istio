// Copyright 2017 Istio Authors. All Rights Reserved.
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

package test

import (
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
)

const (
	FailHeader = "x-istio-backend-fail"
	FailBody   = "Bad request from backend."
)

type HttpServer struct {
	port uint16
	lis  net.Listener
}

func handler(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Fail if there is such header.
	if r.Header.Get(FailHeader) != "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(FailBody))
		return
	}

	// echo back the Content-Type and Content-Length in the response
	for _, k := range []string{"Content-Type", "Content-Length"} {
		if v := r.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

func NewHttpServer(port uint16) (*HttpServer, error) {
	log.Printf("Http server listening on port %v\n", port)
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatal(err)
		return nil, err
	}
	return &HttpServer{
		port: port,
		lis:  lis,
	}, nil
}

func (s *HttpServer) Start() {
	go func() {
		http.HandleFunc("/", handler)
		http.Serve(s.lis, nil)
	}()

	url := fmt.Sprintf("http://localhost:%v/echo", s.port)
	WaitForHttpServer(url)
}

func (s *HttpServer) Stop() {
	log.Printf("Close HTTP server\n")
	s.lis.Close()
	log.Printf("Close HTTP server -- Done\n")
}
