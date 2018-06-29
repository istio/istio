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
	"strconv"
	"syscall"

	flag "github.com/spf13/pflag"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/service/echo"
)

var (
	httpPorts []int
	grpcPorts []int
	version   string
	crt       string
	key       string
)

func init() {
	flag.IntSliceVar(&httpPorts, "port", []int{8080}, "HTTP/1.1 ports")
	flag.IntSliceVar(&grpcPorts, "grpc", []int{7070}, "GRPC ports")
	flag.StringVar(&version, "version", "", "Version string")
	flag.StringVar(&crt, "crt", "", "gRPC TLS server-side certificate")
	flag.StringVar(&key, "key", "", "gRPC TLS server-side key")
}

func main() {
	log.Configure(log.DefaultOptions())

	flag.Parse()

	ports := make(model.PortList, len(httpPorts)+len(grpcPorts))
	portIndex := 0
	for i, p := range httpPorts {
		ports[portIndex] = &model.Port{
			Name:     "http-" + strconv.Itoa(i),
			Protocol: model.ProtocolHTTP,
			Port:     p,
		}
		portIndex++
	}
	for i, p := range grpcPorts {
		ports[portIndex] = &model.Port{
			Name:     "grpc-" + strconv.Itoa(i),
			Protocol: model.ProtocolGRPC,
			Port:     p,
		}
		portIndex++
	}

	s := &echo.Application{
		Ports:   ports,
		TLSCert: crt,
		TLSCKey: key,
		Version: version,
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
