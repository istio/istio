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

package main

import (
	"os"
	"os/signal"
	"syscall"

	flag "github.com/spf13/pflag"

	"istio.io/istio/pilot/test/server/echo"
	"istio.io/istio/pkg/log"
)

var (
	ports     []int
	grpcPorts []int
	version   string
	crt       string
	key       string
)

func init() {
	flag.IntSliceVar(&ports, "port", []int{8080}, "HTTP/1.1 ports")
	flag.IntSliceVar(&grpcPorts, "grpc", []int{7070}, "GRPC ports")
	flag.StringVar(&version, "version", "", "Version string")
	flag.StringVar(&crt, "crt", "", "gRPC TLS server-side certificate")
	flag.StringVar(&key, "key", "", "gRPC TLS server-side key")
}

func main() {
	log.Configure(log.DefaultOptions())

	flag.Parse()

	s := &echo.Server{
		HTTPPorts: ports,
		GRPCPorts: grpcPorts,
		TLSCert:   crt,
		TLSCKey:   key,
		Version:   version,
	}

	if err := s.Start(); err != nil {
		log.Errora(err)
		os.Exit(-1)
	}

	// Wait for the process to be shutdown.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
}
