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
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

const (
	jwtFile = "../../../tests/common/jwt/jwks.json"
)

var (
	httpPort = flag.String("http", "8000", "HTTP server port")
)

// JWTServer implements the ext_authz v2/v3 gRPC and HTTP check request API.
type JWTServer struct {
	httpServer *http.Server
	// For test only
	httpPort chan int
}

// ServeHTTP serves the JWT Keys.
func (s *JWTServer) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	key, err := ioutil.ReadFile(jwtFile)
	if err != nil {
		response.WriteHeader(http.StatusFailedDependency)
		response.Write([]byte(err.Error()))
		return
	}
	response.WriteHeader(http.StatusOK)
	response.Write([]byte(string(key)))
}

func (s *JWTServer) startHTTP(address string, wg *sync.WaitGroup) {
	defer func() {
		wg.Done()
		log.Printf("Stopped JWT HTTP server")
	}()

	listener, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("Failed to create HTTP server: %v", err)
	}
	// Store the port for test only.
	s.httpPort <- listener.Addr().(*net.TCPAddr).Port
	s.httpServer = &http.Server{Handler: s}

	log.Printf("Starting HTTP server at %s", listener.Addr())
	if err := s.httpServer.Serve(listener); err != nil {
		log.Fatalf("Failed to start HTTP server: %v", err)
	}
}

func (s *JWTServer) run(httpAddr string) {
	var wg sync.WaitGroup
	wg.Add(1)
	go s.startHTTP(httpAddr, &wg)
	wg.Wait()
}

func (s *JWTServer) stop() {
	s.httpServer.Close()
}

func NewJwtServer() *JWTServer {
	return &JWTServer{
		httpPort: make(chan int, 1),
	}
}

func main() {
	flag.Parse()
	s := NewJwtServer()
	go s.run(fmt.Sprintf(":%s", *httpPort))
	defer s.stop()

	// Wait for the process to be shutdown.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
}
