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
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/pflag"
	"google.golang.org/grpc"

	"istio.io/istio/tests/e2e/apps/hop"
	"istio.io/istio/tests/e2e/apps/hop/config"
)

var (
	ports     []int
	grpcPorts []int
)

func init() {
	pflag.IntSliceVar(&ports, "port", []int{}, "HTTP/1.1 ports")
	pflag.IntSliceVar(&grpcPorts, "grpc", []int{}, "GRPC ports")
}

func runHTTP(port int) {
	fmt.Printf("Listening HTTP1.1 on %v\n", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), hop.NewApp()); err != nil {
		log.Println(err.Error())
	}
}

func runGRPC(port int) {
	fmt.Printf("Listening GRPC on %v\n", port)
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	grpcServer := grpc.NewServer()
	config.RegisterHopTestServiceServer(grpcServer, hop.NewApp())
	if err = grpcServer.Serve(lis); err != nil {
		log.Println(err.Error())
	}
}

func main() {
	pflag.Parse()
	for _, port := range ports {
		go runHTTP(port)
	}
	for _, grpcPort := range grpcPorts {
		go runGRPC(grpcPort)
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
}
